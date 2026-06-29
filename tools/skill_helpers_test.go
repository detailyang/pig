package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

func TestListSkillsToolListsAndFilters(t *testing.T) {
	tool := NewListSkillsTool([]skills.Skill{
		{Name: "go-port", Description: "Port Go", Source: skills.SourceUser},
		{Name: "disabled", Description: "Off", Source: skills.SourceProject, DisableModelInvocation: true},
	})
	all, err := tool.Execute(context.Background(), ai.ToolCall{Name: "ListSkills", Arguments: map[string]any{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all.Content, "go-port") || !strings.Contains(all.Content, "disabled") || !strings.Contains(all.Content, "disabled") {
		t.Fatalf("list mismatch: %q", all.Content)
	}
	enabled, err := tool.Execute(context.Background(), ai.ToolCall{Name: "ListSkills", Arguments: map[string]any{"enabled_only": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enabled.Content, "go-port") || strings.Contains(enabled.Content, "disabled") {
		t.Fatalf("enabled filter mismatch: %q", enabled.Content)
	}
}

func TestListSkillsToolRejectsInvalidUTF8Source(t *testing.T) {
	_, err := NewListSkillsTool(nil).Execute(context.Background(), ai.ToolCall{Name: "ListSkills", Arguments: map[string]any{"source": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "source must be valid UTF-8" {
		t.Fatalf("expected invalid source error, got %v", err)
	}
}

func TestCatalogListSkillsToolUsesLatestSnapshot(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go-port", "SKILL.md"), "---\nname: go-port\ndescription: Port Go\n---\nBody")
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	tool := NewCatalogListSkillsTool(catalog)
	listed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "ListSkills", Arguments: map[string]any{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.Content, "go-port") {
		t.Fatalf("list mismatch: %q", listed.Content)
	}
	mustWriteFile(t, filepath.Join(root, "new-skill", "SKILL.md"), "---\nname: new-skill\ndescription: New skill\n---\nBody")
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	listed, err = tool.Execute(context.Background(), ai.ToolCall{Name: "ListSkills", Arguments: map[string]any{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.Content, "go-port") || !strings.Contains(listed.Content, "new-skill") {
		t.Fatalf("catalog snapshot was not refreshed: %q", listed.Content)
	}
}

func TestSkillBuilderToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewSkillBuilderTool(t.TempDir())
	if got, want := tool.Description(), "Create a NEW user skill from structured fields and hot-reload the catalog. Use this when the user asks to create, save, or codify a reusable skill, workflow, checklist, or convention — including \"summarize the recent work / this conversation into a skill\": distill the generalizable workflow from the conversation (steps actually performed, commands used, pitfalls hit) and write instructions for the general case, not a transcript of this one instance. Use InstallSkill instead when installing an existing SKILL.md from a URL, file, or pasted content. The tool renders canonical SKILL.md (frontmatter + sections) from name/description/instructions — do not hand-write frontmatter. Two-phase: first call without `confirm` validates and returns a preview (target path, hash, size, shadow warnings); show the user the planned name/description and get their go-ahead, then call again with `confirm: true` to write atomically to ~/.pie/skills/<name>/SKILL.md and reload. A same-name skill with different content additionally requires `overwrite: true`."; got != want {
		t.Fatalf("description mismatch:\n got: %q\nwant: %q", got, want)
	}
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name: lowercase kebab-case (a-z, 0-9, hyphens), max 64 chars. Becomes the directory name and the /skill lookup key.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "One-line summary of what the skill does AND when to use it (max 1024 chars). This is the trigger line the model sees in the catalog — include concrete cue phrases.",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "Markdown body: the steps, conventions, and guidance the skill teaches. Rendered under an '## Instructions' heading.",
			},
			"examples": map[string]any{
				"type":        "string",
				"description": "Optional markdown examples, rendered under an '## Examples' heading.",
			},
			"confirm": map[string]any{
				"type":        "boolean",
				"default":     false,
				"description": "When false (default), validates and returns a preview without writing. When true, writes the skill and reloads the catalog.",
			},
			"overwrite": map[string]any{
				"type":        "boolean",
				"default":     false,
				"description": "Required when a skill of the same name already exists with different content.",
			},
		},
		"required":             []string{"name", "description", "instructions"},
		"additionalProperties": false,
	}
	if got := tool.Parameters(); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSkillBuilderToolPreviewConfirmAndOverwrite(t *testing.T) {
	root := t.TempDir()
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}})
	tool := NewCatalogSkillBuilderTool(root, catalog)
	args := map[string]any{"name": "code-review", "description": "Review\n code   carefully", "instructions": "Check tests.", "examples": "- Review a diff"}
	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: args}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "code-review", "SKILL.md")
	expectedContent, err := RenderSkillMarkdown("code-review", "Review\n code   carefully", "Check tests.", "- Review a diff")
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := fullSHA256(expectedContent)
	if preview.Content != fmt.Sprintf("preview only — call again with `confirm: true` to create the skill. name=code-review target=%s size=%dB existing=false overwrite_required=false", target, len(expectedContent)) {
		t.Fatalf("preview mismatch: %q", preview.Content)
	}
	if preview.Details["phase"] != "preview" || preview.Details["name"] != "code-review" || preview.Details["description"] != "Review code carefully" || preview.Details["target_path"] != target || preview.Details["content_hash"] != expectedHash || preview.Details["size"] != len(expectedContent) || preview.Details["existing"] != false || preview.Details["overwrite_required"] != false || len(preview.Details) != 9 {
		t.Fatalf("preview details mismatch: %#v", preview.Details)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("preview should not write, stat err=%v", err)
	}
	args["confirm"] = true
	installed, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: args}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Content != fmt.Sprintf("created skill 'code-review' at %s (%dB). catalog now has 1 skill(s).", target, len(expectedContent)) {
		t.Fatalf("build mismatch: %q", installed.Content)
	}
	if installed.Details["phase"] != "installed" || installed.Details["name"] != "code-review" || installed.Details["target_path"] != target || installed.Details["content_hash"] != expectedHash || installed.Details["size"] != len(expectedContent) || installed.Details["overwrote"] != false || installed.Details["total_skills_after"] != 1 || installed.Details["diagnostics_count"] != 0 || installed.Details["installed_visible_in_catalog"] != true || installed.Details["audit_entry_id"] != nil || len(installed.Details) != 11 {
		t.Fatalf("installed details mismatch: %#v", installed.Details)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "name: code-review") || !strings.Contains(content, "description: Review code carefully") || !strings.Contains(content, "## Instructions") || !strings.Contains(content, "## Examples") {
		t.Fatalf("skill content mismatch: %q", content)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: args}, nil); err != nil {
		t.Fatalf("idempotent existing content should not require overwrite, got %v", err)
	}
	args["instructions"] = "Check tests and docs."
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: args}, nil); err == nil || err.Error() != "skill 'code-review' already exists with different content. Call again with `overwrite: true` to replace it." {
		t.Fatalf("expected overwrite error, got %v", err)
	}
}

func TestCatalogSkillBuilderToolReloadsAfterBuild(t *testing.T) {
	root := t.TempDir()
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	tool := NewCatalogSkillBuilderTool(root, catalog)
	args := map[string]any{"name": "code-review", "description": "Review code carefully", "instructions": "Check tests.", "confirm": true}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: args}, nil); err != nil {
		t.Fatal(err)
	}
	if skill, ok := findSkillByName(catalog.Skills(), "code-review"); !ok || skill.Description != "Review code carefully" {
		t.Fatalf("catalog did not reload built skill: %#v ok=%v", skill, ok)
	}
}

func TestSkillBuilderToolWarnsAboutShadowedCatalogSkills(t *testing.T) {
	for _, tc := range []struct {
		source skills.Source
		want   string
	}{
		{source: skills.SourceProject, want: "a project skill named 'code-review' exists and will shadow this user skill"},
		{source: skills.SourceBuiltin, want: "this will shadow the builtin skill 'code-review'"},
	} {
		t.Run(string(tc.source), func(t *testing.T) {
			tool := SkillBuilderTool{Root: t.TempDir(), Skills: []skills.Skill{{Name: "code-review", Source: tc.source}}}
			preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{"name": "code-review", "description": "Review code carefully", "instructions": "Check tests."}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			warnings, ok := preview.Details["warnings"].([]string)
			if !ok || len(warnings) != 1 || warnings[0] != tc.want {
				t.Fatalf("warnings mismatch: %#v", preview.Details["warnings"])
			}
		})
	}
}

func TestSkillBuilderToolValidatesFields(t *testing.T) {
	tool := NewSkillBuilderTool(t.TempDir())
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "missing name", arguments: map[string]any{"description": "x", "instructions": "x"}, want: "invalid arguments: missing field `name`"},
		{name: "non-string name", arguments: map[string]any{"name": 123, "description": "x", "instructions": "x"}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "missing description", arguments: map[string]any{"name": "ok-name", "instructions": "x"}, want: "invalid arguments: missing field `description`"},
		{name: "non-string description", arguments: map[string]any{"name": "ok-name", "description": 123, "instructions": "x"}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "missing instructions", arguments: map[string]any{"name": "ok-name", "description": "x"}, want: "invalid arguments: missing field `instructions`"},
		{name: "non-string instructions", arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": 123}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "non-string examples", arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": "x", "examples": 123}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "non-bool confirm", arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": "x", "confirm": "yes"}, want: "invalid arguments: invalid type: string \"yes\", expected a boolean"},
		{name: "non-bool overwrite", arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": "x", "overwrite": "yes"}, want: "invalid arguments: invalid type: string \"yes\", expected a boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: tc.arguments}, nil)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{"name": "BadName", "description": "x", "instructions": "x", "confirm": true}}, nil); err == nil || err.Error() != "skill name must contain only lowercase a-z, 0-9, and hyphens" {
		t.Fatalf("expected name error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": "   ", "confirm": true}}, nil); err == nil || !strings.Contains(err.Error(), "instructions") {
		t.Fatalf("expected instructions error, got %v", err)
	}
}

func TestSkillBuilderToolRejectsInvalidUTF8Arguments(t *testing.T) {
	tool := NewSkillBuilderTool(t.TempDir())
	cases := []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "name", arguments: map[string]any{"name": string([]byte{0xff}), "description": "x", "instructions": "x"}, want: "invalid arguments: invalid UTF-8 in field `name`"},
		{name: "description", arguments: map[string]any{"name": "ok-name", "description": string([]byte{0xff}), "instructions": "x"}, want: "invalid arguments: invalid UTF-8 in field `description`"},
		{name: "instructions", arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": string([]byte{0xff})}, want: "invalid arguments: invalid UTF-8 in field `instructions`"},
		{name: "examples", arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": "x", "examples": string([]byte{0xff})}, want: "invalid arguments: invalid UTF-8 in field `examples`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: tc.arguments}, nil)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSkillBuilderToolParsesBooleanArgumentsBeforeRendering(t *testing.T) {
	tool := NewSkillBuilderTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{"name": "ok-name", "description": "x", "instructions": "   ", "confirm": "yes"}}, nil)
	if err == nil || err.Error() != "invalid arguments: invalid type: string \"yes\", expected a boolean" {
		t.Fatalf("expected confirm type error before render validation, got %v", err)
	}
}

func TestSkillBuilderToolQuotesYAMLAmbiguousDescriptionLikeUpstream(t *testing.T) {
	tool := NewSkillBuilderTool(t.TempDir())
	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{"name": "boolean-desc", "description": "true", "instructions": "Use this skill."}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Details["description"] != "true" {
		t.Fatalf("description should round-trip as string like serde_yaml, got %#v", preview.Details)
	}
}

func TestSkillBuilderToolConfirmRequiresCatalogLikeUpstreamHarness(t *testing.T) {
	root := t.TempDir()
	tool := NewSkillBuilderTool(root)
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{"name": "needs-catalog", "description": "Needs catalog", "instructions": "Use this skill.", "confirm": true}}, nil)
	if err == nil || err.Error() != "SkillBuilder not yet initialized" {
		t.Fatalf("expected upstream initialization error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "needs-catalog", "SKILL.md")); statErr != nil {
		t.Fatalf("upstream writes before initialization error, stat err=%v", statErr)
	}
}

func TestSkillHelperToolMetadata(t *testing.T) {
	if NewListSkillsTool(nil).Name() != "ListSkills" {
		t.Fatal("list skills metadata mismatch")
	}
	if NewSkillBuilderTool(t.TempDir()).Name() != "SkillBuilder" {
		t.Fatal("skill builder metadata mismatch")
	}
	root := t.TempDir()
	if tool := (SkillBuilderTool{}).WithSkillsRoot(root); tool.Root != root {
		t.Fatalf("skill builder root mismatch: %#v", tool)
	}
}
