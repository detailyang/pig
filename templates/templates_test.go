package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemplate(t *testing.T, root, file, body string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllDualRootsProjectWins(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("PIE_DIR", home)
	userRoot := filepath.Join(home, "templates")
	projectRoot := filepath.Join(cwd, ".pie", "templates")
	writeTemplate(t, userRoot, "shared.md", "---\nname: shared\ndescription: user\n---\nUser body {{var}}\n")
	writeTemplate(t, projectRoot, "shared.md", "---\nname: shared\ndescription: project\n---\nProject body {{var}}\n")
	writeTemplate(t, userRoot, "only-user.md", "---\ndescription: user-only\n---\nOnly user\n")
	writeTemplate(t, filepath.Join(projectRoot, "nested"), "ignored.md", "Nested")
	writeTemplate(t, projectRoot, "ignore.txt", "Nope")

	out := LoadAll(cwd)
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 2 {
		t.Fatalf("templates mismatch: %#v", out.Templates)
	}
	shared, ok := NewRegistry(out.Templates).Get("shared")
	if !ok || shared.Description != "project" || !strings.Contains(shared.Content, "Project body") || !strings.Contains(shared.FilePath, projectRoot) {
		t.Fatalf("project template should win: %#v ok=%v", shared, ok)
	}
	onlyUser, ok := NewRegistry(out.Templates).Get("only-user")
	if !ok || onlyUser.Description != "user-only" || !strings.Contains(onlyUser.Content, "Only user") {
		t.Fatalf("user template missing: %#v ok=%v", onlyUser, ok)
	}
	if rendered := Interpolate(shared, map[string]any{"var": "world"}); rendered != "Project body world" {
		t.Fatalf("interpolation mismatch: %q", rendered)
	}
}

func TestInterpolateJSONValueDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	template := Template{Name: "json", Content: "payload={{value}}"}
	rendered := Interpolate(template, map[string]any{"value": map[string]any{"text": "a < b && c > d"}})
	if strings.Contains(rendered, `\u003c`) || strings.Contains(rendered, `\u003e`) || strings.Contains(rendered, `\u0026`) {
		t.Fatalf("interpolated JSON value should not HTML-escape like serde_json, got %q", rendered)
	}
	if rendered != `payload={"text":"a < b && c > d"}` {
		t.Fatalf("interpolated JSON value mismatch: %q", rendered)
	}
}

func TestTemplateMarshalDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(Template{Name: "<tag>&value", Description: "a < b && c > d", Content: "body <>&", FilePath: "/tmp/<template>.md"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("template JSON should not HTML-escape strings like upstream serde_json: %s", text)
	}
	if !strings.Contains(text, `"file_path":"/tmp/<template>.md"`) || !strings.Contains(text, `"description":"a < b && c > d"`) {
		t.Fatalf("template JSON should keep upstream field names and values, got %s", text)
	}
}

func TestLoadedTemplatesAliasMatchesUpstreamCodingAgent(t *testing.T) {
	var loaded LoadedTemplates = LoadAll(t.TempDir())
	if len(loaded.Templates) != 0 || len(loaded.Diagnostics) != 0 {
		t.Fatalf("LoadedTemplates should expose templates and diagnostics slices: %#v", loaded)
	}
}

func TestTemplatesUpstreamExportedNames(t *testing.T) {
	var template PromptTemplate = Template{Name: "fix", Description: "Fix bug", Content: "hello", FilePath: "/tmp/fix.md"}
	if template.Name != "fix" || template.FilePath != "/tmp/fix.md" {
		t.Fatalf("prompt template alias mismatch: %#v", template)
	}

	var output LoadTemplatesOutput = LoadOutput{Templates: []Template{template}}
	if len(output.Templates) != 1 {
		t.Fatalf("load templates output alias mismatch: %#v", output)
	}

	loaded := LoadTemplates([]string{t.TempDir()})
	if len(loaded.Templates) != 0 || len(loaded.Diagnostics) != 0 {
		t.Fatalf("LoadTemplates wrapper mismatch: %#v", loaded)
	}

	registry := NewPromptTemplateRegistry([]PromptTemplate{template})
	if got, ok := registry.Get("fix"); !ok || got.Name != "fix" {
		t.Fatalf("prompt template registry alias mismatch: %#v ok=%v", got, ok)
	}
}

func TestLoadTemplatesFollowsSymlinkMarkdownLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "target.md")
	if err := os.WriteFile(target, []byte("---\nname: linked\ndescription: linked\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "linked.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].FilePath != filepath.Join(dir, "linked.md") || out.Templates[0].Content != "Body" {
		t.Fatalf("symlink markdown should be loaded through symlink path, got %#v", out.Templates)
	}
}

func TestLoadTemplatesRejectsInvalidUTF8LikeUpstreamReadTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: bad\ndescription: bad\n---\nBody \xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := Load([]string{dir})
	if len(out.Templates) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticReadFailed || !strings.Contains(out.Diagnostics[0].Message, "invalid UTF-8") {
		t.Fatalf("invalid UTF-8 template should read-fail like upstream, got %#v", out)
	}
}

func TestLoadTemplatesBrokenSymlinkFailsDirectoryListingLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "valid.md", "---\nname: valid\ndescription: valid\n---\nBody")
	if err := os.Symlink(filepath.Join(dir, "missing.md"), filepath.Join(dir, "broken.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := Load([]string{dir})
	if len(out.Templates) != 0 {
		t.Fatalf("broken symlink should abort directory listing, got templates %#v", out.Templates)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticListFailed || out.Diagnostics[0].Path != dir {
		t.Fatalf("expected directory list_failed diagnostic, got %#v", out.Diagnostics)
	}
}

func TestLoadTemplatesSkipsSymlinkRootLikeUpstream(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	writeTemplate(t, target, "template.md", "---\nname: template\ndescription: desc\n---\nBody")
	link := filepath.Join(base, "linked-root")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := Load([]string{link})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 0 {
		t.Fatalf("symlink root should be skipped, got %#v", out.Templates)
	}
}

func TestLoadTemplatesMissingDirsAndParseDiagnostics(t *testing.T) {
	dir := t.TempDir()
	missing := Load([]string{filepath.Join(dir, "missing")})
	if len(missing.Templates) != 0 || len(missing.Diagnostics) != 0 {
		t.Fatalf("missing dirs should be silent: %#v", missing)
	}
	writeTemplate(t, dir, "bad.md", "---\nname broken\n---\nBody")
	out := Load([]string{dir})
	if len(out.Templates) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed {
		t.Fatalf("parse diagnostics mismatch: %#v", out)
	}
}

func TestLoadTemplatesNullNameFallsBackToFileStem(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "fallback.md", "---\nname: null\ndescription: desc\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "fallback" {
		t.Fatalf("null template name should fall back to file stem, got %#v", out.Templates)
	}
}

func TestLoadTemplatesUppercaseNullFallsBackLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "fallback.md", "---\nname: Null\ndescription: NULL\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "fallback" || out.Templates[0].Description != "" {
		t.Fatalf("uppercase null aliases should behave like serde_yaml null, got %#v", out.Templates)
	}
}

func TestLoadTemplatesMissingDescriptionSerializesAsNullLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "bare.md", "---\nname: bare\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 {
		t.Fatalf("expected one template, got %#v", out.Templates)
	}
	encoded, err := json.Marshal(out.Templates[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"description":null`) {
		t.Fatalf("missing description should serialize as null, got %s", encoded)
	}
}

func TestTemplateLiteralDescriptionSerializesString(t *testing.T) {
	template := Template{Name: "review", Description: "review code", Content: "Body", FilePath: "review.md"}
	encoded, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"description":"review code"`) {
		t.Fatalf("literal description should serialize as string, got %s", encoded)
	}
}

func TestTemplateUnmarshalJSONUsesSnakeCaseFilePathLikeUpstream(t *testing.T) {
	var template Template
	if err := json.Unmarshal([]byte(`{"name":"review","description":null,"content":"Body","file_path":"review.md"}`), &template); err != nil {
		t.Fatal(err)
	}
	if template.Name != "review" || template.Description != "" || template.Content != "Body" || template.FilePath != "review.md" {
		t.Fatalf("template unmarshal mismatch: %#v", template)
	}
	encoded, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"description":null`) || !strings.Contains(string(encoded), `"file_path":"review.md"`) {
		t.Fatalf("roundtrip JSON should preserve upstream field shape, got %s", encoded)
	}
}

func TestLoadTemplatesUnescapesQuotedScalarsLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "quoted.md", "---\nname: \"quoted\\nname\"\ndescription: \"line\\nfeed\"\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "quoted\nname" || out.Templates[0].Description != "line\nfeed" {
		t.Fatalf("quoted scalars should be unescaped like serde_yaml, got %#v", out.Templates)
	}
}

func TestLoadTemplatesHandlesEscapedDoubleQuotesLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "quoted.md", "---\nname: \"quoted\\\"name\"\ndescription: \"say \\\"hello\\\"\"\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "quoted\"name" || out.Templates[0].Description != "say \"hello\"" {
		t.Fatalf("escaped double quotes should be handled like serde_yaml, got %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesFoldedDescriptionBlockLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "folded.md", "---\nname: folded\ndescription: >\n  first line\n  second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "first line second line\n" {
		t.Fatalf("folded description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesLiteralDescriptionBlockLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "literal.md", "---\nname: literal\ndescription: |\n  first line\n  second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "first line\nsecond line\n" {
		t.Fatalf("literal description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesStripChompDescriptionBlockLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "strip.md", "---\nname: strip\ndescription: >-\n  first line\n  second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "first line second line" {
		t.Fatalf("strip chomp folded description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesKeepChompDescriptionBlockLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "keep.md", "---\nname: keep\ndescription: |+\n  first line\n  second line\n\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "first line\nsecond line\n\n" {
		t.Fatalf("keep chomp literal description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesIndentIndicatorDescriptionBlockLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "indent.md", "---\nname: indent\ndescription: |2\n    first line\n    second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "  first line\n  second line\n" {
		t.Fatalf("indent indicator literal description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesCombinedBlockIndicatorsLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "combo.md", "---\nname: combo\ndescription: |2-\n    first line\n    second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "  first line\n  second line" {
		t.Fatalf("combined block indicator description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesAutoDetectsBlockIndentLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "auto-indent.md", "---\nname: auto-indent\ndescription: |\n    first line\n      second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "first line\n  second line\n" {
		t.Fatalf("auto indent literal description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesFoldedBlockBlankLineMatchesSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "blank.md", "---\nname: blank\ndescription: >\n  first line\n\n  second line\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Description != "first line\nsecond line\n" {
		t.Fatalf("folded blank line description mismatch: %#v", out.Templates)
	}
}

func TestLoadTemplatesRejectsInvalidBlockHeaderLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "invalid.md", "---\nname: invalid\ndescription: |0\n  body\n---\nBody")

	out := Load([]string{dir})
	if len(out.Templates) != 0 {
		t.Fatalf("invalid block header should fail to load, got %#v", out.Templates)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "indentation indicator") {
		t.Fatalf("expected parse diagnostic for invalid block header, got %#v", out.Diagnostics)
	}
}

func TestLoadTemplatesUnescapesSingleQuotedScalarsLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "quoted.md", "---\nname: 'quoted''name'\ndescription: 'it''s useful'\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "quoted'name" || out.Templates[0].Description != "it's useful" {
		t.Fatalf("single quoted scalars should be unescaped like serde_yaml, got %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesNumericNameLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "numeric.md", "---\nname: 123\ndescription: desc\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "123" {
		t.Fatalf("numeric template name should parse as string, got %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesStringAnchorAliasLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "anchor.md", "---\nname: &template_name anchored\ndescription: *template_name\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "anchored" || out.Templates[0].Description != "anchored" {
		t.Fatalf("string anchor alias should parse like serde_yaml, got %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesAliasWithInlineCommentLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "anchor-comment.md", "---\nname: &template_name anchored # name comment\ndescription: *template_name # desc comment\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "anchored" || out.Templates[0].Description != "anchored" {
		t.Fatalf("alias with inline comment should parse like serde_yaml, got %#v", out.Templates)
	}
}

func TestLoadTemplatesParsesQuotedAnchorAliasLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "quoted-anchor.md", "---\nname: &template_name \"anchored\\nname\"\ndescription: *template_name\n---\nBody")

	out := Load([]string{dir})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Templates) != 1 || out.Templates[0].Name != "anchored\nname" || out.Templates[0].Description != "anchored\nname" {
		t.Fatalf("quoted anchor alias should parse like serde_yaml, got %#v", out.Templates)
	}
}

func TestLoadTemplatesRejectsUnknownAliasLikeSerdeYAML(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "missing-alias.md", "---\nname: missing-alias\ndescription: *missing\n---\nBody")

	out := Load([]string{dir})
	if len(out.Templates) != 0 {
		t.Fatalf("unknown alias should fail to load, got %#v", out.Templates)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "unknown anchor") {
		t.Fatalf("expected unknown anchor parse diagnostic, got %#v", out.Diagnostics)
	}
}

func TestLoadTemplatesRejectsDuplicateFrontmatterField(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "dupe.md", "---\nname: dupe\ndescription: first\ndescription: second\n---\nBody")

	out := Load([]string{dir})
	if len(out.Templates) != 0 {
		t.Fatalf("duplicate frontmatter field should fail to load, got %#v", out.Templates)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "duplicate field") {
		t.Fatalf("expected duplicate field parse diagnostic, got %#v", out.Diagnostics)
	}
}

func TestInterpolateLeavesMissingAndFormatsNonStrings(t *testing.T) {
	template := Template{Name: "t", Content: "hi {{who}} count={{count}} missing={{missing}}"}
	rendered := Interpolate(template, map[string]any{"who": "Ada", "count": 3})
	if rendered != "hi Ada count=3 missing={{missing}}" {
		t.Fatalf("rendered mismatch: %q", rendered)
	}
}

func TestInterpolateUsesStableKeyOrderLikeUpstream(t *testing.T) {
	template := Template{Name: "t", Content: "{{a}}"}
	rendered := Interpolate(template, map[string]any{"b": "done", "a": "{{b}}"})
	if rendered != "done" {
		t.Fatalf("rendered mismatch: %q", rendered)
	}
}
