package tools

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

type skillToolCellCatalog struct {
	items []skills.Skill
}

func (catalog skillToolCellCatalog) Skills() []skills.Skill { return catalog.items }

func TestSkillToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewSkillTool(nil)
	if got, want := tool.Description(), "Invoke a skill by name. Returns the skill body wrapped in a `<skill>` block for the model to follow. Use this when the skill registry in the system prompt indicates the skill is relevant to the current task. The skill name must match exactly an entry in the registry."; got != want {
		t.Fatalf("description mismatch:\n got: %q\nwant: %q", got, want)
	}
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Exact skill name as listed in the system-prompt registry.",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
	if got := tool.Parameters(); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSkillToolInvokesEnabledSkill(t *testing.T) {
	tool := NewSkillTool([]skills.Skill{{Name: "go-port", Description: "Port code", FilePath: "/tmp/skills/go-port/SKILL.md", Content: "Follow upstream behavior."}})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "go-port"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, `<skill name="go-port" location="/tmp/skills/go-port/SKILL.md">`) || !strings.Contains(result.Content, "Follow upstream behavior.") {
		t.Fatalf("skill invocation mismatch: %q", result.Content)
	}
	if result.Details["name"] != "go-port" || result.Details["path"] != "/tmp/skills/go-port/SKILL.md" {
		t.Fatalf("skill details mismatch: %#v", result.Details)
	}
}

func TestSkillToolUninitializedSnapshotMatchesUpstream(t *testing.T) {
	_, err := NewSkillTool(nil).Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "go-port"}}, nil)
	if err == nil || err.Error() != "Skill tool not yet initialized" {
		t.Fatalf("expected uninitialized skill tool error, got %v", err)
	}
}

func TestCatalogSkillToolUsesLatestSnapshot(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go-port", "SKILL.md"), "---\nname: go-port\ndescription: Port code\n---\nFirst body")
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	tool := NewCatalogSkillTool(catalog)
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "go-port"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "First body") {
		t.Fatalf("skill invocation mismatch: %q", result.Content)
	}
	mustWriteFile(t, filepath.Join(root, "go-port", "SKILL.md"), "---\nname: go-port\ndescription: Port code\n---\nSecond body")
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	result, err = tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "go-port"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Second body") || strings.Contains(result.Content, "First body") {
		t.Fatalf("catalog invocation was stale: %q", result.Content)
	}
}

func TestToolsSkillHarnessCellBuildsLiveSkillTool(t *testing.T) {
	cell := NewSkillHarnessCell()
	tool := NewSkillToolFromHarnessCell(cell)
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "grilling"}}, nil); err == nil || !strings.Contains(err.Error(), "not yet initialized") {
		t.Fatalf("unset cell should be initialization error, got %v", err)
	}
	if !cell.Set(skillToolCellCatalog{items: []skills.Skill{{Name: "grilling", Description: "Interview", FilePath: "skill://grilling/SKILL.md", Content: "Ask one question."}}}) {
		t.Fatal("first Set should succeed")
	}
	if cell.Set(skillToolCellCatalog{}) {
		t.Fatal("second Set should fail like OnceCell")
	}
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "grilling"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Ask one question.") || result.Details["name"] != "grilling" {
		t.Fatalf("skill result mismatch: %#v", result)
	}
}

func TestSkillToolRejectsDisabledAndMissingSkill(t *testing.T) {
	tool := NewSkillTool([]skills.Skill{{Name: "disabled", DisableModelInvocation: true}})
	for _, arguments := range []map[string]any{{}, {"name": 123}} {
		if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: arguments}, nil); err == nil || err.Error() != "missing required arg: name" {
			t.Fatalf("expected missing name error for %#v, got %v", arguments, err)
		}
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "disabled"}}, nil); err == nil || err.Error() != "skill 'disabled' is disabled (disable_model_invocation=true); update the frontmatter to enable" {
		t.Fatalf("expected disabled skill error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": ""}}, nil); err == nil || err.Error() != "no skill named ''. Use /skills to list available skills." {
		t.Fatalf("expected empty missing skill error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "missing"}}, nil); err == nil || err.Error() != "no skill named 'missing'. Use /skills to list available skills." {
		t.Fatalf("expected missing skill error, got %v", err)
	}
}

func TestBuiltinToolsIncludesSkillFactoryOnly(t *testing.T) {
	if NewSkillTool(nil).Name() != "Skill" {
		t.Fatal("skill tool metadata mismatch")
	}
}
