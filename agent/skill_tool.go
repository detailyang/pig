package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

type SkillTool struct {
	Skills      []skills.Skill
	Catalog     interface{ Skills() []skills.Skill }
	Cell        *SkillHarnessCell
	initialized bool
}

type SkillHarnessCell struct {
	once    sync.Once
	catalog interface{ Skills() []skills.Skill }
	set     bool
}

type SkillReloadProvider interface {
	Skills() []skills.Skill
	ReloadSkillsFromDisk(ctx context.Context) (skills.LoadOutput, error)
}

func NewSkillHarnessCell() *SkillHarnessCell { return &SkillHarnessCell{} }

func (cell *SkillHarnessCell) Set(catalog interface{ Skills() []skills.Skill }) bool {
	if cell == nil {
		return false
	}
	updated := false
	cell.once.Do(func() {
		cell.catalog = catalog
		cell.set = true
		updated = true
	})
	return updated
}

func (cell *SkillHarnessCell) Skills() []skills.Skill {
	if cell == nil || !cell.set || cell.catalog == nil {
		return nil
	}
	return cell.catalog.Skills()
}

func (cell *SkillHarnessCell) ReloadSkillsFromDisk(ctx context.Context) (skills.LoadOutput, error) {
	if cell == nil || !cell.set || cell.catalog == nil {
		return skills.LoadOutput{}, fmt.Errorf("skill harness cell is not initialized")
	}
	provider, ok := cell.catalog.(interface {
		ReloadSkillsFromDisk(context.Context) (skills.LoadOutput, error)
	})
	if !ok {
		return skills.LoadOutput{Skills: cell.Skills()}, nil
	}
	return provider.ReloadSkillsFromDisk(ctx)
}

func (cell *SkillHarnessCell) Provider() interface{ Skills() []skills.Skill } {
	if cell == nil || !cell.set {
		return nil
	}
	return cell.catalog
}

func (cell *SkillHarnessCell) IsSet() bool { return cell != nil && cell.set }

func NewSkillTool(available []skills.Skill) SkillTool {
	if available == nil {
		return SkillTool{}
	}
	return SkillTool{Skills: append([]skills.Skill(nil), available...), initialized: true}
}

func NewCatalogSkillTool(catalog interface{ Skills() []skills.Skill }) SkillTool {
	return SkillTool{Catalog: catalog, initialized: true}
}

func NewSkillToolFromHarnessCell(cell *SkillHarnessCell) SkillTool {
	return SkillTool{Cell: cell, initialized: true}
}

func (SkillTool) Name() string { return "Skill" }

func (SkillTool) ExecutionMode() ToolExecutionMode {
	return ToolExecutionParallel
}

func (SkillTool) Description() string {
	return "Invoke a skill by name. Returns the skill body wrapped in a `<skill>` block for the model to follow. Use this when the skill registry in the system prompt indicates the skill is relevant to the current task. The skill name must match exactly an entry in the registry."
}

func (SkillTool) Parameters() map[string]any {
	return map[string]any{
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
}

func (tool SkillTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	name, err := requiredStringToolArg(call, "name")
	if err != nil {
		return ToolResult{}, err
	}
	if !tool.initialized {
		return ToolResult{}, fmt.Errorf("Skill tool not yet initialized")
	}
	if tool.Cell != nil && !tool.Cell.IsSet() {
		return ToolResult{}, fmt.Errorf("Skill tool not yet initialized")
	}
	for _, skill := range tool.availableSkills() {
		if skill.Name != name {
			continue
		}
		if skill.DisableModelInvocation {
			return ToolResult{}, fmt.Errorf("skill '%s' is disabled (disable_model_invocation=true); update the frontmatter to enable", name)
		}
		return ToolResult{CallID: call.ID, Name: call.Name, Content: skills.FormatSkillInvocation(skill, ""), Details: map[string]any{"name": skill.Name, "path": skill.FilePath}}, nil
	}
	return ToolResult{}, fmt.Errorf("no skill named '%s'. Use /skills to list available skills.", name)
}

func (tool SkillTool) availableSkills() []skills.Skill {
	if tool.Cell != nil {
		return tool.Cell.Skills()
	}
	if tool.Catalog != nil {
		return tool.Catalog.Skills()
	}
	return tool.Skills
}

func requiredStringToolArg(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key].(string)
	if !ok {
		return "", fmt.Errorf("missing required arg: %s", key)
	}
	return value, nil
}
