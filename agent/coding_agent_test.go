package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

func TestCodingAgentBuildsSystemPromptFromToolsSkillsAndInstructions(t *testing.T) {
	ag := NewCodingAgent(CodingAgentOptions{
		Options:      Options{Tools: []Tool{codingAgentFakeTool{name: "read", description: "Read files"}, codingAgentFakeTool{name: "write", description: "Write files"}}},
		Skills:       []skills.Skill{{Name: "grilling", Description: "Interview one question at a time", FilePath: "skill://grilling/SKILL.md", Content: "Ask one question."}},
		Instructions: "Product instructions.",
		CurrentDate:  "2026-06-23",
		Workspace:    "/repo",
	})
	prompt := ag.State().SystemPrompt
	for _, want := range []string{
		"<system_prompt>",
		"You are an expert coding assistant operating in a coding agent harness",
		"<name>read</name>",
		"<description>Read files</description>",
		"<name>write</name>",
		"<description>Write files</description>",
		"<name>grilling</name>",
		"<location>skill://grilling/SKILL.md</location>",
		"<current_date>2026-06-23</current_date>",
		"<current_working_directory>/repo</current_working_directory>",
		"Product instructions.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestCodingAgentAutoMountsSnapshotSkillTool(t *testing.T) {
	ag := NewCodingAgent(CodingAgentOptions{
		Options: Options{Tools: []Tool{codingAgentFakeTool{name: "read", description: "Read files"}}},
		Skills:  []skills.Skill{{Name: "grilling", Description: "Interview", FilePath: "skill://grilling/SKILL.md", Content: "Ask exactly one question."}},
	})
	state := ag.State()
	if len(state.Tools) != 2 || state.Tools[0].Name() != "read" || state.Tools[1].Name() != "Skill" {
		t.Fatalf("tools = %#v", state.Tools)
	}
	result, err := state.Tools[1].Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "grilling"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Ask exactly one question.") || result.Details["path"] != "skill://grilling/SKILL.md" {
		t.Fatalf("skill result = %#v", result)
	}
}

func TestCodingAgentRebuildsSkillPromptByteIdenticalFromSameDirectory(t *testing.T) {
	root := t.TempDir()
	for _, spec := range []struct {
		name        string
		description string
		body        string
		disabled    bool
	}{
		{name: "alpha", description: "first skill", body: "alpha body"},
		{name: "beta", description: "second skill", body: "beta body", disabled: true},
		{name: "gamma", description: "third skill", body: "gamma body"},
	} {
		frontmatter := "name: " + spec.name + "\ndescription: " + spec.description + "\n"
		if spec.disabled {
			frontmatter += "disable_model_invocation: true\n"
		}
		mustWriteAgentTestFile(t, filepath.Join(root, spec.name, "SKILL.md"), "---\n"+frontmatter+"---\n"+spec.body)
	}

	buildPrompt := func() string {
		loaded := skills.LoadSkills([]string{root})
		if len(loaded.Diagnostics) != 0 {
			t.Fatalf("unexpected diagnostics: %#v", loaded.Diagnostics)
		}
		ag := NewCodingAgent(CodingAgentOptions{
			Options:      Options{},
			Skills:       loaded.Skills,
			Instructions: "Use the tools you have, never invent state.",
		})
		return ag.State().SystemPrompt
	}

	first := buildPrompt()
	second := buildPrompt()
	if first != second {
		t.Fatalf("coding agent system prompt diverged across reloads")
	}
	for _, want := range []string{"<skills>", "<name>alpha</name>", "<name>beta</name>", "<name>gamma</name>", "Use the tools you have, never invent state."} {
		if !strings.Contains(first, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, first)
		}
	}
}

func TestSkillHarnessCellBuildsLiveSkillToolLikeUpstream(t *testing.T) {
	cell := NewSkillHarnessCell()
	tool := NewSkillToolFromHarnessCell(cell)
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "grilling"}}, nil); err == nil || !strings.Contains(err.Error(), "not yet initialized") {
		t.Fatalf("unset cell should be recoverable initialization error, got %v", err)
	}
	if !cell.Set(skillHarnessCellCatalog{items: []skills.Skill{{Name: "grilling", Description: "Interview", FilePath: "skill://grilling/SKILL.md", Content: "Ask one question."}}}) {
		t.Fatal("first Set should succeed")
	}
	if cell.Set(skillHarnessCellCatalog{}) {
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

func TestCodingAgentUsesToolsAsSingleSourceOfTruth(t *testing.T) {
	prompt := CodingAgentSystemPrompt(CodingAgentPromptInput{Tools: []Tool{codingAgentFakeTool{name: "only", description: "Only tool"}}})
	if !strings.Contains(prompt, "<name>only</name>") || !strings.Contains(prompt, "<description>Only tool</description>") || strings.Contains(prompt, "<name>read</name>") {
		t.Fatalf("prompt tools are not derived from tools input:\n%s", prompt)
	}
}

func mustWriteAgentTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type codingAgentFakeTool struct {
	name        string
	description string
}

type skillHarnessCellCatalog struct {
	items []skills.Skill
}

func (catalog skillHarnessCellCatalog) Skills() []skills.Skill { return catalog.items }

func (tool codingAgentFakeTool) Name() string        { return tool.name }
func (tool codingAgentFakeTool) Description() string { return tool.description }
func (tool codingAgentFakeTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{}, nil
}
