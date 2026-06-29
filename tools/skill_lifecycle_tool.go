package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
	"github.com/detailyang/pig/skills"
)

const (
	installSkillFetchGuardBytes = 16 * 1024 * 1024
	maxSkillDescriptionLength   = 1024
)

func DefaultBaseDir() string {
	return config.DefaultBaseDir()
}

func DefaultSkillsRoot() string {
	return config.DefaultSkillsRoot()
}

type InstallSkillTool struct {
	Root       string
	Catalog    skillCatalog
	HTTPClient *http.Client
}

func NewInstallSkillTool(root string) InstallSkillTool { return InstallSkillTool{Root: root} }

func NewInstallSkillToolFromHarnessCell(cell *SkillHarnessCell) InstallSkillTool {
	return NewCatalogInstallSkillTool(DefaultSkillsRoot(), catalogFromSkillHarnessCell(cell))
}

func (tool InstallSkillTool) WithSkillsRoot(root string) InstallSkillTool {
	tool.Root = root
	return tool
}

func NewCatalogInstallSkillTool(root string, catalog skillCatalog) InstallSkillTool {
	return InstallSkillTool{Root: root, Catalog: catalog}
}

func (InstallSkillTool) Name() string { return "InstallSkill" }
func (InstallSkillTool) Description() string {
	return "Install a new skill into the user-global skills directory (~/.pie/skills/<name>/) and hot-reload the catalog so the next turn can use it. Two-phase: first call without `confirm` returns a preview (name, description, target path, hash, size). Second call with `confirm: true` writes atomically and reloads. Same-name skill requires `overwrite: true` when the new content hash differs. Source is one of: https URL, absolute local path, or inline content. Body is never echoed back into the tool result — only metadata + preview info."
}
func (InstallSkillTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (InstallSkillTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	return agent.PermissionAsk
}
func (InstallSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{
				"type":        "object",
				"description": "Where to fetch the SKILL.md from.",
				"oneOf": []map[string]any{
					{
						"properties": map[string]any{
							"type": map[string]any{"enum": []string{"url", "https"}, "description": "Use \"url\" for HTTPS URLs. \"https\" is accepted as a compatibility alias."},
							"url":  map[string]any{"type": "string", "description": "https:// URL. http/file/data schemes are rejected; loopback and RFC1918 hosts are rejected."},
						},
						"required":             []string{"type", "url"},
						"additionalProperties": false,
					},
					{
						"properties": map[string]any{
							"type": map[string]any{"const": "path"},
							"path": map[string]any{"type": "string", "description": "Absolute path to a local SKILL.md file."},
						},
						"required":             []string{"type", "path"},
						"additionalProperties": false,
					},
					{
						"properties": map[string]any{
							"type":    map[string]any{"const": "content"},
							"content": map[string]any{"type": "string", "description": "Inline SKILL.md content (frontmatter + body)."},
						},
						"required":             []string{"type", "content"},
						"additionalProperties": false,
					},
				},
			},
			"confirm":   map[string]any{"type": "boolean", "default": false, "description": "When false (default), returns a preview without writing. When true, performs the install."},
			"overwrite": map[string]any{"type": "boolean", "default": false, "description": "Required when a skill of the same name already exists with different content."},
		},
		"required":             []string{"source"},
		"additionalProperties": false,
	}
}
func (tool InstallSkillTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	name, content, _, err := tool.installSkillInput(call)
	if err != nil {
		return agent.ToolResult{}, err
	}
	confirm, err := optionalSerdeBoolArg(call, "confirm", false)
	if err != nil {
		return agent.ToolResult{}, err
	}
	overwrite, err := optionalSerdeBoolArg(call, "overwrite", false)
	if err != nil {
		return agent.ToolResult{}, err
	}
	content = normalizeSkillLineEndings(content)
	parsed, err := normalizeSkillInstall(name, content)
	if err != nil {
		return agent.ToolResult{}, err
	}
	content = parsed.Content
	target := filepath.Join(tool.Root, name, "SKILL.md")
	existing := fileExists(target)
	var existingHash any
	if existing {
		data, err := os.ReadFile(target)
		if err != nil {
			return agent.ToolResult{}, fmt.Errorf("read existing skill: %w", err)
		}
		existingHash = shortSHA256(normalizeSkillLineEndings(string(data)))
	}
	hash := shortSHA256(content)
	overwriteRequired := existing && existingHash != hash
	if !confirm {
		previewDetails := map[string]any{
			"phase":              "preview",
			"name":               name,
			"description":        parsed.Description,
			"warnings":           parsed.Warnings,
			"target_path":        target,
			"content_hash":       hash,
			"size":               len(content),
			"existing":           existing,
			"overwrite_required": overwriteRequired,
		}
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("preview only — call again with `confirm: true` to install. name=%s target=%s size=%dB existing=%t overwrite_required=%t", name, target, len(content), existing, overwriteRequired), Details: previewDetails}, nil
	}
	if overwriteRequired && !overwrite {
		return agent.ToolResult{}, fmt.Errorf("skill '%s' already exists with different content. Call again with `overwrite: true` to replace it (existing hash differs from new content).", name)
	}
	if err := atomicWriteFile(target, []byte(content), 0o644); err != nil {
		return agent.ToolResult{}, fmt.Errorf("install skill: %w", err)
	}
	totalSkillsAfter := 0
	diagnosticsCount := 0
	installedVisible := false
	if tool.Catalog != nil {
		out, err := reloadSkillCatalog(ctx, tool.Catalog)
		if err != nil {
			return agent.ToolResult{}, fmt.Errorf("reload skills catalog: %w", err)
		}
		totalSkillsAfter = len(out.Skills)
		diagnosticsCount = len(out.Diagnostics)
		installedVisible = skillVisible(out.Skills, name)
	}
	installedDetails := map[string]any{
		"phase":                        "installed",
		"name":                         name,
		"target_path":                  target,
		"content_hash":                 hash,
		"size":                         len(content),
		"overwrote":                    overwriteRequired,
		"total_skills_after":           totalSkillsAfter,
		"diagnostics_count":            diagnosticsCount,
		"warnings":                     parsed.Warnings,
		"installed_visible_in_catalog": installedVisible,
		"audit_entry_id":               nil,
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("installed skill '%s' to %s (%dB). catalog now has %d skill(s).", name, target, len(content), totalSkillsAfter), Details: installedDetails}, nil
}

func skillVisible(available []skills.Skill, name string) bool {
	_, ok := findSkillByName(available, name)
	return ok
}

func normalizeSkillLineEndings(content string) string {
	return strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
}

func (tool InstallSkillTool) installSkillInput(call ai.ToolCall) (string, string, string, error) {
	if _, ok := call.Arguments["source"]; !ok {
		if _, hasName := call.Arguments["name"]; !hasName {
			if _, hasContent := call.Arguments["content"]; !hasContent {
				return "", "", "", fmt.Errorf("invalid arguments: missing field `source`")
			}
		}
		name, err := stringArg(call, "name")
		if err != nil {
			return "", "", "", err
		}
		if !utf8.ValidString(name) {
			return "", "", "", fmt.Errorf("name must be valid UTF-8")
		}
		content, err := stringArg(call, "content")
		if err != nil {
			return "", "", "", err
		}
		if !utf8.ValidString(content) {
			return "", "", "", fmt.Errorf("content must be valid UTF-8")
		}
		return name, content, "content", nil
	}
	source, ok := call.Arguments["source"].(map[string]any)
	if !ok {
		return "", "", "", fmt.Errorf("invalid arguments: invalid type: %s, expected internally tagged enum InstallSource", serdeType(call.Arguments["source"]))
	}
	typeValue, ok := source["type"].(string)
	if !ok || typeValue == "" {
		return "", "", "", fmt.Errorf("invalid arguments: missing field `type`")
	}
	if !utf8.ValidString(typeValue) {
		return "", "", "", fmt.Errorf("source.type must be valid UTF-8")
	}
	var content string
	switch typeValue {
	case "content":
		value, ok := source["content"]
		if !ok {
			return "", "", "", fmt.Errorf("invalid arguments: missing field `content`")
		}
		contentText, ok := value.(string)
		if !ok {
			return "", "", "", fmt.Errorf("invalid arguments: invalid type: %s, expected a string", serdeType(value))
		}
		if !utf8.ValidString(contentText) {
			return "", "", "", fmt.Errorf("source.content must be valid UTF-8")
		}
		content = contentText
	case "path":
		value, ok := source["path"]
		if !ok {
			return "", "", "", fmt.Errorf("invalid arguments: missing field `path`")
		}
		path, ok := value.(string)
		if !ok {
			return "", "", "", fmt.Errorf("invalid arguments: invalid type: %s, expected a string", serdeType(value))
		}
		if !utf8.ValidString(path) {
			return "", "", "", fmt.Errorf("source.path must be valid UTF-8")
		}
		if !filepath.IsAbs(path) {
			return "", "", "", fmt.Errorf("path must be absolute (relative paths are ambiguous in agent context)")
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", "", "", fmt.Errorf("stat %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return "", "", "", fmt.Errorf("%s is not a regular file", path)
		}
		if info.Size() > installSkillFetchGuardBytes {
			return "", "", "", fmt.Errorf("%s (%d bytes) exceeds %d-byte in-memory guard", path, info.Size(), installSkillFetchGuardBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", "", fmt.Errorf("read %s: %w", path, err)
		}
		if !utf8.Valid(data) {
			return "", "", "", fmt.Errorf("read %s: %s", path, invalidUTF8Detail(data))
		}
		content = string(data)
	case "url", "https":
		value, ok := source["url"]
		if !ok {
			return "", "", "", fmt.Errorf("invalid arguments: missing field `url`")
		}
		urlText, ok := value.(string)
		if !ok {
			return "", "", "", fmt.Errorf("invalid arguments: invalid type: %s, expected a string", serdeType(value))
		}
		if !utf8.ValidString(urlText) {
			return "", "", "", fmt.Errorf("source.url must be valid UTF-8")
		}
		data, err := fetchSkillURL(urlText, tool.HTTPClient)
		if err != nil {
			return "", "", "", err
		}
		content = data
		typeValue = "url"
	default:
		return "", "", "", fmt.Errorf("invalid arguments: unknown variant `%s`, expected one of `url`, `path`, `content`", typeValue)
	}
	if !strings.HasPrefix(normalizeSkillLineEndings(content), "---") {
		return "", "", "", fmt.Errorf("skill body missing YAML frontmatter (must start with `---` followed by name/description)")
	}
	frontmatter, _, err := skills.ParseFrontmatter(content)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid frontmatter yaml: %w", err)
	}
	name := frontmatter.Name
	if name == "" && !frontmatterHasNonNullKey(content, "name") {
		return "", "", "", fmt.Errorf("frontmatter missing required field: name")
	}
	return name, content, typeValue, nil
}

func fetchSkillURL(rawURL string, client *http.Client) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("invalid url: relative URL without a base")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("url must use https:// (http, file, data, and other schemes are refused)")
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("url must have a host")
	}
	if isPrivateOrLocalHost(parsed.Hostname()) {
		return "", fmt.Errorf("refusing to fetch from local/private host '%s' (SSRF guard)", parsed.Hostname())
	}
	if client == nil {
		client = defaultInstallSkillHTTPClient()
	}
	resp, err := client.Get(parsed.String())
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch returned non-success status: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, installSkillFetchGuardBytes+1))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if len(data) > installSkillFetchGuardBytes {
		return "", fmt.Errorf("fetched skill body exceeds %d-byte in-memory guard (%d bytes received so far); refusing to install from a stream this large", installSkillFetchGuardBytes, installSkillFetchGuardBytes)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("skill body is not valid utf-8: %s", invalidUTF8Detail(data))
	}
	return string(data), nil
}

func defaultInstallSkillHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func invalidUTF8Detail(data []byte) string {
	for offset := 0; offset < len(data); offset++ {
		if data[offset] < utf8.RuneSelf {
			continue
		}
		_, size := utf8.DecodeRune(data[offset:])
		if size == 1 {
			return fmt.Sprintf("invalid utf-8 sequence of 1 bytes from index %d", offset)
		}
		offset += size - 1
	}
	return "invalid utf-8 sequence"
}

func isPrivateOrLocalHost(host string) bool {
	normalized := strings.TrimSuffix(strings.Trim(strings.ToLower(host), "[]"), ".")
	if normalized == "localhost" || normalized == "ip6-localhost" || normalized == "ip6-loopback" || normalized == "broadcasthost" || strings.HasSuffix(normalized, ".localhost") || strings.HasSuffix(normalized, ".local") {
		return true
	}
	parsed := net.ParseIP(normalized)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() || parsed.Equal(net.IPv4bcast)
}

type RemoveSkillTool struct {
	Root     string
	StateDir string
	Catalog  skillCatalog
	Skills   []skills.Skill
}

func NewRemoveSkillTool(root string) RemoveSkillTool {
	return RemoveSkillTool{Root: root, StateDir: root}
}

func NewRemoveSkillToolFromHarnessCell(cell *SkillHarnessCell) RemoveSkillTool {
	baseDir := DefaultBaseDir()
	tool := NewCatalogRemoveSkillTool(filepath.Join(baseDir, "skills"), catalogFromSkillHarnessCell(cell))
	tool.StateDir = baseDir
	return tool
}

func (tool RemoveSkillTool) WithBaseDir(baseDir string) RemoveSkillTool {
	tool.Root = baseDir
	tool.StateDir = baseDir
	return tool
}

func NewCatalogRemoveSkillTool(root string, catalog skillCatalog) RemoveSkillTool {
	return RemoveSkillTool{Root: root, StateDir: root, Catalog: catalog}
}

func (RemoveSkillTool) Name() string { return "RemoveSkill" }
func (RemoveSkillTool) Description() string {
	return "Delete a user-installed skill (from ~/.pie/skills/) and hot-reload the catalog. Only user-installed skills can be removed — builtin skills are compiled into pie and project skills belong to the repo; for those, disable instead via SetSkillState. Two-phase: first call previews the target path; call again with `confirm: true` to delete. Removing also clears any disabled-state overlay entry for the skill."
}
func (RemoveSkillTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (RemoveSkillTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	return agent.PermissionAsk
}
func (RemoveSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Exact skill name as shown in /skills."},
			"source":  map[string]any{"type": "string", "enum": []string{"builtin", "user", "project"}, "description": "Optional. Must be `user` if given — only user-installed skills are removable."},
			"confirm": map[string]any{"type": "boolean", "default": false, "description": "When false (default) returns a preview; when true performs the deletion."},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}
func (tool RemoveSkillTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	name, err := requiredSerdeStringArg(call, "name")
	if err != nil {
		return agent.ToolResult{}, err
	}
	requested, err := optionalSerdeStringArg(call, "source", "")
	if err != nil {
		return agent.ToolResult{}, err
	}
	confirm, err := optionalSerdeBoolArg(call, "confirm", false)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if requested != "" {
		requestedSource, err := parseRemoveSkillSource(requested)
		if err != nil {
			return agent.ToolResult{}, err
		}
		if requestedSource != skills.SourceUser {
			return agent.ToolResult{}, fmt.Errorf("only user-installed skills can be removed; '%s' is a user skill, not '%s'.", name, requestedSource.Label())
		}
	}
	target := filepath.Join(tool.Root, name)
	availableSkills := tool.availableSkills()
	resolvedFromCatalog := false
	if len(availableSkills) > 0 {
		skill, ok := findSkillByName(availableSkills, name)
		if !ok {
			return agent.ToolResult{}, fmt.Errorf("no loaded skill named '%s'. Run /skills to list loaded skills.%s", name, removeSkillHint(availableSkills, name))
		}
		if skill.Source != skills.SourceUser {
			return agent.ToolResult{}, fmt.Errorf("'%s' is a %s skill and cannot be removed (builtin skills are compiled in; project skills belong to the repo). Disable it instead with SetSkillState or `/skills disable %s`.", name, skill.Source.Label(), name)
		}
		resolvedTarget, ok := removeSkillDeletionTarget(tool.Root, skill.FilePath)
		if !ok {
			return agent.ToolResult{}, fmt.Errorf("refusing to remove '%s': its file (%s) is not under the user skills root (%s).", name, skill.FilePath, tool.Root)
		}
		target = resolvedTarget
		resolvedFromCatalog = true
	} else if err := validateSkillName(name); err != nil {
		return agent.ToolResult{}, err
	}
	if !resolvedFromCatalog && !removeSkillTargetExists(target) {
		return agent.ToolResult{}, fmt.Errorf("skill %q is not installed at %s", name, target)
	}
	details := map[string]any{
		"name":        name,
		"source":      "user",
		"target_path": target,
	}
	if !confirm {
		details["phase"] = "preview"
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("preview only — call again with `confirm: true` to delete. skill=%s source=user target=%s", name, target), Details: details}, nil
	}
	if err := os.RemoveAll(target); err != nil {
		return agent.ToolResult{}, fmt.Errorf("remove skill: %w", err)
	}
	if err := skills.RemoveAndSave(tool.StateDir, name, skills.SourceUser); err != nil {
		return agent.ToolResult{}, fmt.Errorf("clear skill state: %w", err)
	}
	totalSkillsAfter := 0
	stillPresent := false
	if tool.Catalog != nil {
		out, err := reloadSkillCatalog(ctx, tool.Catalog)
		if err != nil {
			return agent.ToolResult{}, fmt.Errorf("reload skills catalog: %w", err)
		}
		stillPresent = skillVisible(out.Skills, name)
		totalSkillsAfter = len(out.Skills)
	}
	details["phase"] = "removed"
	details["still_present_after_reload"] = stillPresent
	details["total_skills_after"] = totalSkillsAfter
	details["audit_entry_id"] = nil
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("removed skill '%s' (user). catalog now has %d skill(s).", name, totalSkillsAfter), Details: details}, nil
}

func removeSkillHint(available []skills.Skill, name string) string {
	var names []string
	seen := map[string]bool{}
	for _, skill := range available {
		if len(names) >= 5 {
			break
		}
		if seen[skill.Name] || (!strings.HasPrefix(skill.Name, name) && !strings.Contains(skill.Name, name)) {
			continue
		}
		seen[skill.Name] = true
		names = append(names, skill.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return " Did you mean: " + strings.Join(names, ", ") + "?"
}

func (tool RemoveSkillTool) availableSkills() []skills.Skill {
	if len(tool.Skills) > 0 {
		return tool.Skills
	}
	if tool.Catalog != nil {
		return tool.Catalog.Skills()
	}
	return nil
}

func removeSkillDeletionTarget(root, filePath string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(filePath))
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	if first == "" || first == "." || first == ".." {
		return "", false
	}
	return filepath.Join(root, first), true
}

func removeSkillTargetExists(target string) bool {
	if fileExists(target) {
		return true
	}
	return fileExists(filepath.Join(target, "SKILL.md"))
}

func parseRemoveSkillSource(source string) (skills.Source, error) {
	switch strings.ToLower(source) {
	case string(skills.SourceBuiltin):
		return skills.SourceBuiltin, nil
	case string(skills.SourceUser):
		return skills.SourceUser, nil
	case string(skills.SourceProject):
		return skills.SourceProject, nil
	default:
		return "", fmt.Errorf("invalid `source` (expected one of: builtin, user, project)")
	}
}

type normalizedSkillInstall struct {
	Content     string
	Description string
	Warnings    []string
}

type ParsedSkill struct {
	Name              string
	Description       string
	NormalizedContent string
	ContentHash       string
	Size              int
	Warnings          []string
}

func ParseAndValidateSkillMD(content string) (ParsedSkill, error) {
	content = normalizeSkillLineEndings(content)
	frontmatter, _, err := skills.ParseFrontmatter(content)
	if err != nil {
		_, normalizeErr := normalizeSkillInstall("", content)
		if normalizeErr != nil {
			return ParsedSkill{}, normalizeErr
		}
		return ParsedSkill{}, fmt.Errorf("invalid frontmatter yaml: %w", err)
	}
	if frontmatter.Name == "" && !frontmatterHasNonNullKey(content, "name") {
		return ParsedSkill{}, fmt.Errorf("frontmatter missing required field: name")
	}
	parsed, err := normalizeSkillInstall(frontmatter.Name, content)
	if err != nil {
		return ParsedSkill{}, err
	}
	return ParsedSkill{Name: frontmatter.Name, Description: parsed.Description, NormalizedContent: parsed.Content, ContentHash: shortSHA256(parsed.Content), Size: len(parsed.Content), Warnings: append([]string(nil), parsed.Warnings...)}, nil
}

func ParseAndValidateSkillMd(content string) (ParsedSkill, error) {
	return ParseAndValidateSkillMD(content)
}

func normalizeSkillInstall(name, content string) (normalizedSkillInstall, error) {
	if err := validateSkillName(name); err != nil {
		return normalizedSkillInstall{}, err
	}
	if !strings.HasPrefix(content, "---") {
		return normalizedSkillInstall{}, fmt.Errorf("skill body missing YAML frontmatter (must start with `---` followed by name/description)")
	}
	if strings.Index(content[3:], "\n---") < 0 {
		return normalizedSkillInstall{}, fmt.Errorf("skill frontmatter missing closing `\\n---`")
	}
	frontmatter, _, err := skills.ParseFrontmatter(content)
	if err != nil {
		return normalizedSkillInstall{}, fmt.Errorf("invalid frontmatter yaml: %w", err)
	}
	if frontmatter.Name == "" && !frontmatterHasNonNullKey(content, "name") {
		return normalizedSkillInstall{}, fmt.Errorf("frontmatter missing required field: name")
	}
	if err := validateSkillName(frontmatter.Name); err != nil {
		return normalizedSkillInstall{}, err
	}
	if frontmatter.Name != "" && frontmatter.Name != name {
		return normalizedSkillInstall{}, fmt.Errorf("frontmatter name %q does not match requested skill name %q", frontmatter.Name, name)
	}
	originalDescription := frontmatter.Description
	description := strings.TrimSpace(originalDescription)
	var warnings []string
	switch {
	case description == "" && originalDescription == "":
		description = "No description provided."
		warnings = append(warnings, "description missing; using generated fallback")
		content = addSkillDescription(content, description)
	case description == "":
		description = "No description provided."
		warnings = append(warnings, "description empty; using generated fallback")
		content = replaceSkillDescription(content, description)
	case len([]rune(description)) > maxSkillDescriptionLength:
		description = "No description provided."
		warnings = append(warnings, fmt.Sprintf("description exceeds %d characters; using generated fallback", maxSkillDescriptionLength))
		content = replaceSkillDescription(content, description)
	}
	return normalizedSkillInstall{Content: content, Description: description, Warnings: warnings}, nil
}

func validateSkillInstall(name, content string) error {
	_, err := normalizeSkillInstall(name, normalizeSkillLineEndings(content))
	return err
}

func OnDiskSkillHash(targetPath string) (string, bool) {
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return "", false
	}
	return shortSHA256(normalizeSkillLineEndings(string(data))), true
}

func AtomicWriteSkill(target string, content string) error {
	return atomicWriteFile(target, []byte(content), 0o644)
}

func frontmatterHasNonNullKey(content string, key string) bool {
	value := frontmatterKeyValue(content, key)
	if value == "" {
		return false
	}
	return !isYAMLNullScalar(value)
}

func isYAMLNullScalar(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = strings.TrimSpace(before)
	}
	return strings.EqualFold(value, "null") || value == "~"
}

func frontmatterKeyValue(content string, key string) string {
	normalized := normalizeSkillLineEndings(content)
	idx := strings.Index(normalized[3:], "\n---")
	if idx < 0 {
		return ""
	}
	yamlText := normalized[4 : idx+3]
	for _, line := range strings.Split(yamlText, "\n") {
		field, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(field) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func addSkillDescription(content, description string) string {
	idx := strings.Index(content[3:], "\n---")
	if idx < 0 {
		return content
	}
	insert := idx + 3
	return content[:insert] + "\ndescription: " + description + content[insert:]
}

func replaceSkillDescription(content, description string) string {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		if strings.TrimSpace(strings.SplitN(line, ":", 2)[0]) == "description" {
			lines[index] = "description: " + description
			for index+1 < len(lines) && isIndentedSkillYAMLLine(lines[index+1]) {
				lines = append(lines[:index+1], lines[index+2:]...)
			}
			return strings.Join(lines, "\n")
		}
	}
	return addSkillDescription(content, description)
}

func isIndentedSkillYAMLLine(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.TrimSpace(line) == ""
}

func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name must not be empty")
	}
	if len([]rune(name)) > 64 {
		return fmt.Errorf("skill name exceeds 64 characters")
	}
	for _, ch := range name {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			return fmt.Errorf("skill name must contain only lowercase a-z, 0-9, and hyphens")
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("skill name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("skill name must not contain consecutive hyphens")
	}
	return nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".SKILL.md.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func shortSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
