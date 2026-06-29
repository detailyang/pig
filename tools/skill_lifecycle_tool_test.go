package tools

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

func TestInstallSkillToolPreviewConfirmAndOverwrite(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	content := "---\nname: go-port\ndescription: Port upstream behavior\n---\nBody\n"
	target := filepath.Join(root, "go-port", "SKILL.md")
	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": content}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPreviewContent := "preview only — call again with `confirm: true` to install. name=go-port target=" + target + " size=63B existing=false overwrite_required=false"
	if preview.Content != wantPreviewContent {
		t.Fatalf("preview mismatch: %q", preview.Content)
	}
	expectedHash := fullSHA256(content)
	if preview.Details["phase"] != "preview" || preview.Details["name"] != "go-port" || preview.Details["target_path"] != target || preview.Details["content_hash"] != expectedHash || preview.Details["existing"] != false || len(preview.Details) != 9 {
		t.Fatalf("preview details mismatch: %#v", preview.Details)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("preview should not write skill, stat err=%v", err)
	}

	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": content, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantInstallContent := "installed skill 'go-port' to " + target + " (63B). catalog now has 0 skill(s)."
	if installed.Content != wantInstallContent {
		t.Fatalf("install mismatch: %q", installed.Content)
	}
	auditEntryID, hasAuditEntryID := installed.Details["audit_entry_id"]
	if installed.Details["phase"] != "installed" || installed.Details["name"] != "go-port" || installed.Details["target_path"] != target || installed.Details["content_hash"] != expectedHash || installed.Details["size"] != len(content) || installed.Details["overwrote"] != false || installed.Details["total_skills_after"] != 0 || installed.Details["diagnostics_count"] != 0 || installed.Details["installed_visible_in_catalog"] != false || !hasAuditEntryID || auditEntryID != nil || len(installed.Details) != 11 {
		t.Fatalf("install details mismatch: %#v", installed.Details)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != content {
		t.Fatalf("installed content mismatch: %q err=%v", data, err)
	}

	idempotent, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": content, "confirm": true}}, nil)
	if err != nil {
		t.Fatalf("idempotent reinstall should not require overwrite: %v", err)
	}
	if idempotent.Details["overwrote"] != false || idempotent.Details["total_skills_after"] != 0 || idempotent.Details["diagnostics_count"] != 0 || idempotent.Details["installed_visible_in_catalog"] != false || len(idempotent.Details) != 11 {
		t.Fatalf("idempotent details mismatch: %#v", idempotent.Details)
	}
}

func fullSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func TestInstallSkillToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewInstallSkillTool(t.TempDir())
	if got, want := tool.Description(), "Install a new skill into the user-global skills directory (~/.pie/skills/<name>/) and hot-reload the catalog so the next turn can use it. Two-phase: first call without `confirm` returns a preview (name, description, target path, hash, size). Second call with `confirm: true` writes atomically and reloads. Same-name skill requires `overwrite: true` when the new content hash differs. Source is one of: https URL, absolute local path, or inline content. Body is never echoed back into the tool result — only metadata + preview info."; got != want {
		t.Fatalf("description mismatch:\n got: %q\nwant: %q", got, want)
	}
	params := tool.Parameters()
	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "source" {
		t.Fatalf("required mismatch: %#v", params["required"])
	}
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties mismatch: %#v", params["properties"])
	}
	if _, ok := properties["name"]; ok {
		t.Fatalf("legacy name should not be exposed in schema: %#v", properties)
	}
	source, ok := properties["source"].(map[string]any)
	if !ok || source["type"] != "object" {
		t.Fatalf("source schema mismatch: %#v", properties["source"])
	}
	oneOf, ok := source["oneOf"].([]map[string]any)
	if !ok || len(oneOf) != 3 {
		t.Fatalf("source oneOf mismatch: %#v", source["oneOf"])
	}
	confirm, ok := properties["confirm"].(map[string]any)
	if !ok || confirm["default"] != false || confirm["description"] == "" {
		t.Fatalf("confirm schema mismatch: %#v", properties["confirm"])
	}
	overwrite, ok := properties["overwrite"].(map[string]any)
	if !ok || overwrite["default"] != false || overwrite["description"] == "" {
		t.Fatalf("overwrite schema mismatch: %#v", properties["overwrite"])
	}
}

func TestInstallSkillToolOverwriteRequiredWhenContentDiffers(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	oldContent := "---\nname: go-port\ndescription: Port upstream behavior\n---\nOld body\n"
	newContent := "---\nname: go-port\ndescription: Port upstream behavior\n---\nNew body\n"
	path := filepath.Join(root, "go-port", "SKILL.md")
	mustWriteFile(t, path, oldContent)

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": newContent}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Details["existing"] != true || preview.Details["overwrite_required"] != true {
		t.Fatalf("overwrite preview details mismatch: %#v", preview.Details)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": newContent, "confirm": true}}, nil)
	wantOverwriteError := "skill 'go-port' already exists with different content. Call again with `overwrite: true` to replace it (existing hash differs from new content)."
	if err == nil || err.Error() != wantOverwriteError {
		t.Fatalf("expected overwrite error, got %v", err)
	}

	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": newContent, "confirm": true, "overwrite": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Details["overwrote"] != true || len(installed.Details) != 11 {
		t.Fatalf("overwrite install details mismatch: %#v", installed.Details)
	}
}

func TestInstallSkillToolNormalizesLineEndingsForHashAndWrite(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	lfContent := "---\nname: go-port\ndescription: Port upstream behavior\n---\nBody\n"
	crlfContent := strings.ReplaceAll(lfContent, "\n", "\r\n")
	path := filepath.Join(root, "go-port", "SKILL.md")
	mustWriteFile(t, path, crlfContent)

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": lfContent}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Details["overwrite_required"] != false {
		t.Fatalf("line ending-only diff should not require overwrite: %#v", preview.Details)
	}

	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": crlfContent, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Details["content_hash"] != fullSHA256(lfContent) || installed.Details["size"] != len(lfContent) {
		t.Fatalf("normalized install details mismatch: %#v", installed.Details)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != lfContent {
		t.Fatalf("installed content was not LF-normalized: %q", data)
	}
}

func TestInstallSkillToolAddsFallbackDescriptionWithWarning(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	content := "---\nname: only-name\n---\n# Heading\nBody body.\n"

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	warnings, ok := preview.Details["warnings"].([]string)
	if preview.Details["description"] != "No description provided." || !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "description missing") {
		t.Fatalf("fallback preview details mismatch: %#v", preview.Details)
	}

	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	warnings, ok = installed.Details["warnings"].([]string)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "description missing") {
		t.Fatalf("fallback install details mismatch: %#v", installed.Details)
	}
	written, err := os.ReadFile(filepath.Join(root, "only-name", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "description: No description provided.") {
		t.Fatalf("fallback description was not written: %q", written)
	}

	for _, tc := range []struct {
		content string
		want    string
	}{
		{"---\nname: empty-desc\ndescription: '   '\n---\nBody.\n", "description empty"},
		{"---\nname: long-desc\ndescription: " + strings.Repeat("x", 1025) + "\n---\nBody.\n", "description exceeds"},
	} {
		preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": tc.content}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		warnings, ok := preview.Details["warnings"].([]string)
		if preview.Details["description"] != "No description provided." || !ok || len(warnings) != 1 || !strings.Contains(warnings[0], tc.want) {
			t.Fatalf("recoverable description details mismatch: %#v", preview.Details)
		}
	}

	blockContent := "---\nname: block-desc\ndescription: |\n  " + strings.Repeat("x", 1025) + "\nx-custom: true\n---\nBody.\n"
	installed, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": blockContent}, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	warnings, ok = installed.Details["warnings"].([]string)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "description exceeds") {
		t.Fatalf("block scalar fallback details mismatch: %#v", installed.Details)
	}
	written, err = os.ReadFile(filepath.Join(root, "block-desc", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "description: No description provided.") || strings.Contains(string(written), strings.Repeat("x", 1025)) || !strings.Contains(string(written), "x-custom: true") {
		t.Fatalf("block scalar fallback content mismatch: %q", written)
	}
}

func TestInstallSkillToolAcceptsUpstreamSourceObject(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	content := "---\nname: go-port\ndescription: Port upstream behavior\n---\nBody\n"
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "missing source", arguments: map[string]any{}, want: "invalid arguments: missing field `source`"},
		{name: "source non-object", arguments: map[string]any{"source": "content"}, want: "invalid arguments: invalid type: string \"content\", expected internally tagged enum InstallSource"},
		{name: "source missing type", arguments: map[string]any{"source": map[string]any{"content": content}}, want: "invalid arguments: missing field `type`"},
		{name: "content missing body", arguments: map[string]any{"source": map[string]any{"type": "content"}}, want: "invalid arguments: missing field `content`"},
		{name: "content non-string", arguments: map[string]any{"source": map[string]any{"type": "content", "content": 123}}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "path missing path", arguments: map[string]any{"source": map[string]any{"type": "path"}}, want: "invalid arguments: missing field `path`"},
		{name: "path non-string", arguments: map[string]any{"source": map[string]any{"type": "path", "path": 123}}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "url missing url", arguments: map[string]any{"source": map[string]any{"type": "url"}}, want: "invalid arguments: missing field `url`"},
		{name: "url non-string", arguments: map[string]any{"source": map[string]any{"type": "url", "url": 123}}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "unsupported type", arguments: map[string]any{"source": map[string]any{"type": "ftp"}}, want: "invalid arguments: unknown variant `ftp`, expected one of `url`, `path`, `content`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: tc.arguments}, nil)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Details["phase"] != "preview" || preview.Details["name"] != "go-port" || len(preview.Details) != 9 {
		t.Fatalf("source content preview details mismatch: %#v", preview.Details)
	}

	path := filepath.Join(root, "incoming.skill.md")
	mustWriteFile(t, path, content)
	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": path}, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Details["phase"] != "installed" || installed.Details["name"] != "go-port" || len(installed.Details) != 11 {
		t.Fatalf("source path install details mismatch: %#v", installed.Details)
	}
	if _, err := os.Stat(filepath.Join(root, "go-port", "SKILL.md")); err != nil {
		t.Fatalf("source path install did not write target: %v", err)
	}
}

func TestInstallSkillToolSourceObjectTakesPrecedenceOverLegacyTopLevelFields(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	sourceContent := "---\nname: source-skill\ndescription: Source skill\n---\nBody\n"
	legacyContent := "---\nname: legacy-skill\ndescription: Legacy skill\n---\nBody\n"
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "legacy-skill", "content": legacyContent, "source": map[string]any{"type": "content", "content": sourceContent}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["name"] != "source-skill" || result.Details["target_path"] != filepath.Join(root, "source-skill", "SKILL.md") {
		t.Fatalf("source object should take precedence over legacy fields, details=%#v", result.Details)
	}
}

func TestInstallSkillToolRejectsNonBoolFlags(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	content := "---\nname: go-port\ndescription: Port upstream behavior\n---\nBody\n"
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "non-bool confirm", arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}, "confirm": "yes"}, want: "invalid arguments: invalid type: string \"yes\", expected a boolean"},
		{name: "non-bool overwrite", arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}, "overwrite": "yes"}, want: "invalid arguments: invalid type: string \"yes\", expected a boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: tc.arguments}, nil)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestInstallSkillToolSourceEmptyStringsReachFetchOrParse(t *testing.T) {
	tool := NewInstallSkillTool(t.TempDir())
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "empty content", arguments: map[string]any{"source": map[string]any{"type": "content", "content": ""}}, want: "skill body missing YAML frontmatter (must start with `---` followed by name/description)"},
		{name: "empty path", arguments: map[string]any{"source": map[string]any{"type": "path", "path": ""}}, want: "path must be absolute (relative paths are ambiguous in agent context)"},
		{name: "empty url", arguments: map[string]any{"source": map[string]any{"type": "url", "url": ""}}, want: "invalid url:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: tc.arguments}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestInstallSkillToolURLSourceValidation(t *testing.T) {
	tool := NewInstallSkillTool(t.TempDir())

	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": "http://example.com/skill.md"}}}, nil)
	if err == nil || err.Error() != "url must use https:// (http, file, data, and other schemes are refused)" {
		t.Fatalf("expected https scheme error, got %v", err)
	}
	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": "https://localhost/skill.md"}}}, nil)
	if err == nil || err.Error() != "refusing to fetch from local/private host 'localhost' (SSRF guard)" {
		t.Fatalf("expected SSRF guard error, got %v", err)
	}

	for _, url := range []string{
		"https://127.0.0.1/skill.md",
		"https://10.0.0.1/skill.md",
		"https://192.168.1.1/skill.md",
		"https://255.255.255.255/skill.md",
		"https://api.localhost/skill.md",
		"https://ip6-localhost/skill.md",
		"https://broadcasthost/skill.md",
	} {
		_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": url}}}, nil)
		if err == nil || !strings.Contains(err.Error(), "SSRF guard") {
			t.Fatalf("expected url source %q to be rejected by SSRF guard, got %v", url, err)
		}
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "https", "url": "https://localhost/skill.md"}}}, nil)
	if err == nil || strings.Contains(err.Error(), "unsupported source type") || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("https alias should be accepted before fetch, got %v", err)
	}
}

func TestInstallSkillToolURLSourceFetchesHTTPS(t *testing.T) {
	content := "---\nname: url-skill\ndescription: URL skill\n---\nBody\n"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)
	serverAddr := server.Listener.Addr().String()
	client := server.Client()
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, serverAddr)
		},
	}
	root := t.TempDir()
	tool := InstallSkillTool{Root: root, HTTPClient: client}

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": "https://example.com/skill.md"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Details["phase"] != "preview" || preview.Details["name"] != "url-skill" || len(preview.Details) != 9 {
		t.Fatalf("url preview details mismatch: %#v", preview.Details)
	}
	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": "https://example.com/skill.md"}, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Details["phase"] != "installed" || installed.Details["name"] != "url-skill" || len(installed.Details) != 11 {
		t.Fatalf("url install details mismatch: %#v", installed.Details)
	}
	if _, err := os.Stat(filepath.Join(root, "url-skill", "SKILL.md")); err != nil {
		t.Fatalf("url install did not write target: %v", err)
	}
}

func TestInstallSkillDefaultHTTPClientMatchesUpstream(t *testing.T) {
	client := defaultInstallSkillHTTPClient()
	if client.Timeout != 15*time.Second {
		t.Fatalf("default install skill timeout mismatch: %s", client.Timeout)
	}
	req := &http.Request{URL: mustParseURL(t, "https://example.com/next")}
	via := []*http.Request{
		{URL: mustParseURL(t, "https://example.com/0")},
		{URL: mustParseURL(t, "https://example.com/1")},
		{URL: mustParseURL(t, "https://example.com/2")},
		{URL: mustParseURL(t, "https://example.com/3")},
	}
	if err := client.CheckRedirect(req, via); err != nil {
		t.Fatalf("fifth redirect should be allowed: %v", err)
	}
	via = append(via, &http.Request{URL: mustParseURL(t, "https://example.com/4")})
	if err := client.CheckRedirect(req, via); err == nil {
		t.Fatalf("sixth redirect should be rejected")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestInstallSkillToolURLSourceRejectsInvalidUTF8(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xff, 0xfe})
	}))
	t.Cleanup(server.Close)
	serverAddr := server.Listener.Addr().String()
	client := server.Client()
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, serverAddr)
		},
	}
	tool := InstallSkillTool{Root: t.TempDir(), HTTPClient: client}
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": "https://example.com/skill.md"}}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "skill body is not valid utf-8: ") || !strings.Contains(err.Error(), "invalid utf-8") {
		t.Fatalf("expected invalid utf-8 url error, got %v", err)
	}
}

func TestInstallSkillToolURLSourceRejectsHugeBodyWithUpstreamMessage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", installSkillFetchGuardBytes+1)))
	}))
	t.Cleanup(server.Close)
	serverAddr := server.Listener.Addr().String()
	client := server.Client()
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, serverAddr)
		},
	}
	tool := InstallSkillTool{Root: t.TempDir(), HTTPClient: client}
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": "https://example.com/skill.md"}}}, nil)
	want := fmt.Sprintf("fetched skill body exceeds %d-byte in-memory guard (%d bytes received so far); refusing to install from a stream this large", installSkillFetchGuardBytes, installSkillFetchGuardBytes)
	if err == nil || err.Error() != want {
		t.Fatalf("expected huge body error %q, got %v", want, err)
	}
}

func TestInstallSkillToolPathSourceGuards(t *testing.T) {
	root := t.TempDir()
	tool := NewInstallSkillTool(root)
	missingPath := filepath.Join(root, "missing.skill.md")
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": missingPath}}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "stat "+missingPath+": ") {
		t.Fatalf("expected stat path error, got %v", err)
	}
	for _, path := range []string{
		"relative/SKILL.md",
		root,
	} {
		_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": path}}}, nil)
		if err == nil {
			t.Fatalf("expected path source %q to be rejected", path)
		}
	}

	largePath := filepath.Join(root, "large.skill.md")
	mediumContent := "---\nname: medium-skill\ndescription: Medium skill\n---\n" + strings.Repeat("x", MaxWebFetchBodyBytes+1)
	if err := os.WriteFile(largePath, []byte(mediumContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": largePath}}}, nil); err != nil {
		t.Fatalf("path source should allow skill body above web_fetch limit: %v", err)
	}
	tooLargePath := filepath.Join(root, "too-large.skill.md")
	if err := os.WriteFile(tooLargePath, []byte(strings.Repeat("x", installSkillFetchGuardBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": tooLargePath}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "in-memory guard") {
		t.Fatalf("expected large path guard error, got %v", err)
	}
	invalidUTF8Path := filepath.Join(root, "invalid.skill.md")
	if err := os.WriteFile(invalidUTF8Path, []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": invalidUTF8Path}}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read "+invalidUTF8Path+": ") || !strings.Contains(err.Error(), "invalid utf-8") {
		t.Fatalf("expected invalid utf-8 path error, got %v", err)
	}
}

func TestInstallSkillToolRejectsInvalidUTF8Arguments(t *testing.T) {
	tool := NewInstallSkillTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": string([]byte{0xff}), "content": "---\nname: go-port\ndescription: Port\n---\nBody"}}, nil)
	if err == nil || err.Error() != "name must be valid UTF-8" {
		t.Fatalf("expected invalid legacy name error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "content must be valid UTF-8" {
		t.Fatalf("expected invalid legacy content error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": string([]byte{0xff}), "content": "---\nname: go-port\ndescription: Port\n---\nBody"}}}, nil)
	if err == nil || err.Error() != "source.type must be valid UTF-8" {
		t.Fatalf("expected invalid source type error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": string([]byte{0xff})}}}, nil)
	if err == nil || err.Error() != "source.content must be valid UTF-8" {
		t.Fatalf("expected invalid source content error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "path", "path": string([]byte{0xff})}}}, nil)
	if err == nil || err.Error() != "source.path must be valid UTF-8" {
		t.Fatalf("expected invalid source path error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "url", "url": string([]byte{0xff})}}}, nil)
	if err == nil || err.Error() != "source.url must be valid UTF-8" {
		t.Fatalf("expected invalid source url error, got %v", err)
	}
}

func TestInstallSkillToolRejectsBadNameAndFrontmatterMismatch(t *testing.T) {
	tool := NewInstallSkillTool(t.TempDir())
	for _, content := range []string{
		"no frontmatter at all",
		"---\ndescription: only-desc\n---\nbody",
		"---\nname: go-port\n",
	} {
		if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}, "confirm": true}}, nil); err == nil {
			t.Fatalf("expected malformed skill to be rejected: %q", content)
		}
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": "no frontmatter at all", "confirm": true}}, nil); err == nil || err.Error() != "skill body missing YAML frontmatter (must start with `---` followed by name/description)" {
		t.Fatalf("expected upstream missing frontmatter error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": "---\ndescription: only-desc\n---\nbody", "confirm": true}}, nil); err == nil || err.Error() != "frontmatter missing required field: name" {
		t.Fatalf("expected upstream missing name error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": "---\nname: \"\"\ndescription: Empty name\n---\nbody", "confirm": true}}, nil); err == nil || err.Error() != "skill name must not be empty" {
		t.Fatalf("expected upstream empty name error, got %v", err)
	}
	for _, content := range []string{
		"---\nname:\ndescription: Empty name\n---\nBody",
		"---\nname: null\ndescription: Null name\n---\nBody",
	} {
		_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": content}}}, nil)
		if err == nil || err.Error() != "frontmatter missing required field: name" {
			t.Fatalf("expected missing name for %q, got %v", content, err)
		}
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": "---\nname go-port\n---\nBody", "confirm": true}}, nil); err == nil || !strings.HasPrefix(err.Error(), "invalid frontmatter yaml:") {
		t.Fatalf("expected invalid frontmatter yaml error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"source": map[string]any{"type": "content", "content": "---\nname go-port\n---\nBody"}, "confirm": true}}, nil); err == nil || !strings.HasPrefix(err.Error(), "invalid frontmatter yaml:") {
		t.Fatalf("expected source invalid frontmatter yaml error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "../bad", "content": "---\nname: bad\ndescription: Bad\n---\nBody", "confirm": true}}, nil); err == nil || err.Error() != "skill name must contain only lowercase a-z, 0-9, and hyphens" {
		t.Fatalf("expected invalid name error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": "---\nname: other\ndescription: Bad\n---\nBody", "confirm": true}}, nil); err == nil || !strings.Contains(err.Error(), "frontmatter name") {
		t.Fatalf("expected frontmatter mismatch error, got %v", err)
	}
}

func TestParseAndValidateSkillMDMatchesInstallNormalization(t *testing.T) {
	parsed, err := ParseAndValidateSkillMD("---\nname: go-port\ndescription:   Useful skill   \n---\nBody\n")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "go-port" || parsed.Description != "Useful skill" || parsed.Size != len(parsed.NormalizedContent) || len(parsed.ContentHash) != 64 || len(parsed.Warnings) != 0 {
		t.Fatalf("parsed skill mismatch: %#v", parsed)
	}

	fallback, err := ParseAndValidateSkillMD("---\nname: go-port\n---\nBody")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Description != "No description provided." || len(fallback.Warnings) != 1 || !strings.Contains(fallback.NormalizedContent, "description: No description provided.") {
		t.Fatalf("fallback normalization mismatch: %#v", fallback)
	}
	alias, err := ParseAndValidateSkillMd("---\nname: go-port\ndescription: Alias\n---\nBody")
	if err != nil || alias.Name != "go-port" || alias.Description != "Alias" {
		t.Fatalf("upstream helper alias mismatch: parsed=%#v err=%v", alias, err)
	}

	if _, err := ParseAndValidateSkillMD("---\ndescription: only-desc\n---\nbody"); err == nil || err.Error() != "frontmatter missing required field: name" {
		t.Fatalf("expected missing name error, got %v", err)
	}
}

func TestSkillInstallHelperNamesMatchUpstream(t *testing.T) {
	t.Setenv("PIE_DIR", filepath.Join(t.TempDir(), "pie-home"))
	if DefaultBaseDir() != filepath.Clean(os.Getenv("PIE_DIR")) || DefaultSkillsRoot() != filepath.Join(os.Getenv("PIE_DIR"), "skills") {
		t.Fatalf("default skill paths mismatch base=%s skills=%s", DefaultBaseDir(), DefaultSkillsRoot())
	}

	root := t.TempDir()
	target := filepath.Join(root, "go-port", "SKILL.md")
	content := "---\nname: go-port\ndescription: Go\n---\nBody\r\n"
	if err := AtomicWriteSkill(target, content); err != nil {
		t.Fatal(err)
	}
	hash, ok := OnDiskSkillHash(target)
	if !ok || hash != shortSHA256(normalizeSkillLineEndings(content)) {
		t.Fatalf("hash mismatch hash=%q ok=%v", hash, ok)
	}
	if missing, ok := OnDiskSkillHash(filepath.Join(root, "missing", "SKILL.md")); ok || missing != "" {
		t.Fatalf("missing hash mismatch hash=%q ok=%v", missing, ok)
	}
}

func TestSkillNameValidationErrorsMatchUpstream(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "", want: "skill name must not be empty"},
		{name: strings.Repeat("a", 65), want: "skill name exceeds 64 characters"},
		{name: "BadName", want: "skill name must contain only lowercase a-z, 0-9, and hyphens"},
		{name: "-bad", want: "skill name must not start or end with a hyphen"},
		{name: "bad-", want: "skill name must not start or end with a hyphen"},
		{name: "bad--name", want: "skill name must not contain consecutive hyphens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSkillName(tc.name); err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRemoveSkillToolPreviewConfirmAndMissing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go-port", "SKILL.md")
	mustWriteFile(t, path, "---\nname: go-port\ndescription: Port\n---\nBody")
	tool := NewRemoveSkillTool(root)
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "missing name", arguments: map[string]any{}, want: "invalid arguments: missing field `name`"},
		{name: "non-string name", arguments: map[string]any{"name": 123}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "non-string source", arguments: map[string]any{"name": "go-port", "source": 123}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "non-bool confirm", arguments: map[string]any{"name": "go-port", "confirm": "true"}, want: "invalid arguments: invalid type: string \"true\", expected a boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: tc.arguments}, nil)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	wantSourceMismatch := "only user-installed skills can be removed; 'go-port' is a user skill, not 'project'."
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port", "source": "project", "confirm": true}}, nil); err == nil || err.Error() != wantSourceMismatch {
		t.Fatalf("expected source mismatch error, got %v", err)
	}
	wantInvalidSource := "invalid `source` (expected one of: builtin, user, project)"
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port", "source": "unknown", "confirm": true}}, nil); err == nil || err.Error() != wantInvalidSource {
		t.Fatalf("expected invalid source error, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source mismatch should keep skill: %v", err)
	}
	userPreview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port", "source": "USER"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if userPreview.Details["phase"] != "preview" || userPreview.Details["source"] != "user" {
		t.Fatalf("source user preview details mismatch: %#v", userPreview.Details)
	}
	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRemovePreview := "preview only — call again with `confirm: true` to delete. skill=go-port source=user target=" + filepath.Join(root, "go-port")
	if preview.Content != wantRemovePreview {
		t.Fatalf("preview mismatch: %q", preview.Content)
	}
	if preview.Details["phase"] != "preview" || preview.Details["name"] != "go-port" || preview.Details["source"] != "user" || preview.Details["target_path"] != filepath.Join(root, "go-port") {
		t.Fatalf("remove preview details mismatch: %#v", preview.Details)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("preview should keep skill: %v", err)
	}

	removed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port", "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRemovedContent := "removed skill 'go-port' (user). catalog now has 0 skill(s)."
	if removed.Content != wantRemovedContent {
		t.Fatalf("remove mismatch: %q", removed.Content)
	}
	auditEntryID, hasAuditEntryID := removed.Details["audit_entry_id"]
	if removed.Details["phase"] != "removed" || removed.Details["name"] != "go-port" || removed.Details["source"] != "user" || removed.Details["target_path"] != filepath.Join(root, "go-port") || removed.Details["still_present_after_reload"] != false || removed.Details["total_skills_after"] != 0 || !hasAuditEntryID || auditEntryID != nil || len(removed.Details) != 7 {
		t.Fatalf("remove details mismatch: %#v", removed.Details)
	}
	if _, err := os.Stat(filepath.Join(root, "go-port")); !os.IsNotExist(err) {
		t.Fatalf("skill dir should be gone, stat err=%v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port", "confirm": true}}, nil); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected missing error, got %v", err)
	}
}

func TestRemoveSkillToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewRemoveSkillTool(t.TempDir())
	if got, want := tool.Description(), "Delete a user-installed skill (from ~/.pie/skills/) and hot-reload the catalog. Only user-installed skills can be removed — builtin skills are compiled into pie and project skills belong to the repo; for those, disable instead via SetSkillState. Two-phase: first call previews the target path; call again with `confirm: true` to delete. Removing also clears any disabled-state overlay entry for the skill."; got != want {
		t.Fatalf("description mismatch:\n got: %q\nwant: %q", got, want)
	}
	params := tool.Parameters()
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties mismatch: %#v", params["properties"])
	}
	for _, key := range []string{"name", "source", "confirm"} {
		if _, ok := properties[key].(map[string]any); !ok {
			t.Fatalf("missing property %s in %#v", key, properties)
		}
	}
	if name := properties["name"].(map[string]any); name["description"] == "" {
		t.Fatalf("name schema mismatch: %#v", name)
	}
	if source := properties["source"].(map[string]any); source["description"] != "Optional. Must be `user` if given — only user-installed skills are removable." {
		t.Fatalf("source schema mismatch: %#v", source)
	}
	if confirm := properties["confirm"].(map[string]any); confirm["default"] != false || confirm["description"] == "" {
		t.Fatalf("confirm schema mismatch: %#v", confirm)
	}
}

func TestRemoveSkillToolParsesConfirmBeforeLookup(t *testing.T) {
	tool := NewRemoveSkillTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "missing-skill", "confirm": "yes"}}, nil)
	if err == nil || err.Error() != "invalid arguments: invalid type: string \"yes\", expected a boolean" {
		t.Fatalf("expected confirm type error before skill lookup, got %v", err)
	}
}

func TestRemoveSkillToolRejectsNonUserCatalogSkill(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "project-skill", "SKILL.md"), "---\nname: project-skill\ndescription: Project\n---\nBody")
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}, StateDir: root})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	catalogSkill := catalog.Skills()[0]
	catalogSkill.Source = skills.SourceProject
	tool := RemoveSkillTool{Root: root, StateDir: root, Catalog: catalog, Skills: []skills.Skill{catalogSkill}}

	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "project-skill", "confirm": true}}, nil)
	wantNonUser := "'project-skill' is a project skill and cannot be removed (builtin skills are compiled in; project skills belong to the repo). Disable it instead with SetSkillState or `/skills disable project-skill`."
	if err == nil || err.Error() != wantNonUser {
		t.Fatalf("expected non-user source rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "project-skill", "SKILL.md")); err != nil {
		t.Fatalf("non-user rejection should keep skill file: %v", err)
	}
}

func TestRemoveSkillToolMissingLoadedSkillErrorMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	tool := RemoveSkillTool{Root: root, StateDir: root, Skills: []skills.Skill{
		{Name: "alpha-tool", Source: skills.SourceUser, FilePath: filepath.Join(root, "alpha-tool", "SKILL.md")},
		{Name: "beta-tool", Source: skills.SourceUser, FilePath: filepath.Join(root, "beta-tool", "SKILL.md")},
	}}
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "BadName", "confirm": true}}, nil)
	if err == nil || err.Error() != "no loaded skill named 'BadName'. Run /skills to list loaded skills." {
		t.Fatalf("invalid-shaped missing skill error mismatch: %v", err)
	}
	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "tool", "confirm": true}}, nil)
	if err == nil || err.Error() != "no loaded skill named 'tool'. Run /skills to list loaded skills. Did you mean: alpha-tool, beta-tool?" {
		t.Fatalf("missing loaded skill error mismatch: %v", err)
	}
}

func TestRemoveSkillToolTreatsMissingCatalogTargetAsRemoved(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stale", "SKILL.md")
	tool := RemoveSkillTool{Root: root, StateDir: root, Skills: []skills.Skill{{Name: "stale", Source: skills.SourceUser, FilePath: path}}}

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "stale"}}, nil)
	if err != nil {
		t.Fatalf("missing catalog target should still preview: %v", err)
	}
	if preview.Details["phase"] != "preview" || preview.Details["target_path"] != filepath.Join(root, "stale") {
		t.Fatalf("missing catalog target preview mismatch: %#v", preview.Details)
	}
	removed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "stale", "confirm": true}}, nil)
	if err != nil {
		t.Fatalf("missing catalog target should be idempotent remove: %v", err)
	}
	if removed.Details["phase"] != "removed" || removed.Details["still_present_after_reload"] != false {
		t.Fatalf("missing catalog target removal mismatch: %#v", removed.Details)
	}
}

func TestRemoveSkillToolRejectsCatalogFileOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "outside", "SKILL.md")
	tool := RemoveSkillTool{Root: root, StateDir: root, Skills: []skills.Skill{{Name: "outside", Source: skills.SourceUser, FilePath: outsidePath}}}

	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "outside", "confirm": true}}, nil)
	want := fmt.Sprintf("refusing to remove 'outside': its file (%s) is not under the user skills root (%s).", outsidePath, root)
	if err == nil || err.Error() != want {
		t.Fatalf("expected outside-root rejection %q, got %v", want, err)
	}
}

func TestRemoveSkillToolRemovesRootMarkdownSkill(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "solo.md")
	mustWriteFile(t, path, "---\nname: solo\ndescription: Root file\n---\nBody")
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}, StateDir: root})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	tool := NewCatalogRemoveSkillTool(root, catalog)

	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "solo"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Details["target_path"] != path {
		t.Fatalf("root markdown preview target mismatch: %#v", preview.Details)
	}
	removed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "solo", "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Details["target_path"] != path || removed.Details["still_present_after_reload"] != false {
		t.Fatalf("root markdown removal details mismatch: %#v", removed.Details)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("root markdown skill file should be gone, stat err=%v", err)
	}
}

func TestCatalogSkillLifecycleToolsReloadAfterChanges(t *testing.T) {
	root := t.TempDir()
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}, StateDir: root})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	install := NewCatalogInstallSkillTool(root, catalog)
	content := "---\nname: go-port\ndescription: Port upstream behavior\n---\nBody\n"
	installed, err := install.Execute(context.Background(), ai.ToolCall{Name: "InstallSkill", Arguments: map[string]any{"name": "go-port", "content": content, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Details["installed_visible_in_catalog"] != true || installed.Details["total_skills_after"] != 1 || installed.Details["diagnostics_count"] != 0 {
		t.Fatalf("install catalog details mismatch: %#v", installed.Details)
	}
	if skill, ok := findSkillByName(catalog.Skills(), "go-port"); !ok || skill.Description != "Port upstream behavior" {
		t.Fatalf("catalog did not reload installed skill: %#v ok=%v", skill, ok)
	}
	stateTool := NewCatalogSetSkillStateTool(catalog)
	if _, err := stateTool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "enabled": false, "confirm": true}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := skills.LoadState(root).Lookup("go-port", skills.SourceUser); !ok {
		t.Fatal("expected disabled overlay before remove")
	}
	remove := NewCatalogRemoveSkillTool(root, catalog)
	removed, err := remove.Execute(context.Background(), ai.ToolCall{Name: "RemoveSkill", Arguments: map[string]any{"name": "go-port", "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Details["still_present_after_reload"] != false || removed.Details["total_skills_after"] != 0 {
		t.Fatalf("remove catalog details mismatch: %#v", removed.Details)
	}
	if _, ok := findSkillByName(catalog.Skills(), "go-port"); ok {
		t.Fatalf("catalog still contains removed skill: %#v", catalog.Skills())
	}
	if _, ok := skills.LoadState(root).Lookup("go-port", skills.SourceUser); ok {
		t.Fatal("remove should clear stale disabled overlay")
	}
	if len(catalog.AuditLog()) < 3 {
		t.Fatalf("expected reload audit entries, got %#v", catalog.AuditLog())
	}
}

func TestSkillLifecycleToolMetadata(t *testing.T) {
	if NewInstallSkillTool(t.TempDir()).Name() != "InstallSkill" {
		t.Fatal("install skill tool metadata mismatch")
	}
	installRoot := filepath.Join(t.TempDir(), "install-root")
	if tool := (InstallSkillTool{}).WithSkillsRoot(installRoot); tool.Root != installRoot {
		t.Fatalf("install skill root mismatch: %#v", tool)
	}
	if NewRemoveSkillTool(t.TempDir()).Name() != "RemoveSkill" {
		t.Fatal("remove skill tool metadata mismatch")
	}
	removeRoot := filepath.Join(t.TempDir(), "remove-root")
	if tool := (RemoveSkillTool{}).WithBaseDir(removeRoot); tool.Root != removeRoot || tool.StateDir != removeRoot {
		t.Fatalf("remove skill base dir mismatch: %#v", tool)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
