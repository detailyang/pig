package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

type SetSkillStateTool struct {
	BaseDir string
	Skills  []skills.Skill
	Catalog skillCatalog
}

func NewSetSkillStateTool(baseDir string, available []skills.Skill) SetSkillStateTool {
	return SetSkillStateTool{BaseDir: baseDir, Skills: append([]skills.Skill(nil), available...)}
}

func NewSetSkillStateToolFromHarnessCell(cell *SkillHarnessCell) SetSkillStateTool {
	return SetSkillStateTool{BaseDir: DefaultBaseDir(), Catalog: catalogFromSkillHarnessCell(cell)}
}

func (tool SetSkillStateTool) WithBaseDir(baseDir string) SetSkillStateTool {
	tool.BaseDir = baseDir
	return tool
}

func NewCatalogSetSkillStateTool(catalog skillCatalog) SetSkillStateTool {
	return SetSkillStateTool{Catalog: catalog}
}

func (SetSkillStateTool) Name() string { return "SetSkillState" }
func (SetSkillStateTool) Description() string {
	return "Enable or disable a loaded skill at runtime without editing its SKILL.md. The choice is recorded in a local overlay (~/.pie/skills-state.json) keyed by source+name and survives restarts. Works for any source — a builtin or project skill that can't be removed can still be disabled. Two-phase: first call previews (current vs target state); call again with `confirm: true` to apply. Disabling prevents the model from auto-invoking the skill via the Skill tool; the skill still appears in the catalog. Re-enabling a previously-disabled skill is a privileged control-plane write and requires explicit user confirmation through the runtime prompt card before it takes effect (issue #110); disabling does not prompt."
}
func (SetSkillStateTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (SetSkillStateTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	enabled, ok := arguments["enabled"].(bool)
	if !ok || !enabled {
		return agent.PermissionAllow
	}
	return agent.PermissionAsk
}
func (SetSkillStateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Exact skill name as shown in /skills."},
			"source":  map[string]any{"type": "string", "enum": []string{"builtin", "user", "project"}, "description": "Optional. The active source is resolved automatically; if given, must match it."},
			"enabled": map[string]any{"type": "boolean", "description": "Target state. `false` disables (no user prompt). `true` re-enables and triggers a user confirmation prompt before the change applies."},
			"confirm": map[string]any{"type": "boolean", "default": false, "description": "When false (default) returns a preview; when true applies the change."},
		},
		"required":             []string{"name", "enabled"},
		"additionalProperties": false,
	}
}
func (tool SetSkillStateTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	name, err := requiredSerdeStringArg(call, "name")
	if err != nil {
		return agent.ToolResult{}, err
	}
	enabled, err := requiredSerdeBoolArg(call, "enabled")
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
	skill, ok := findSkillByName(tool.availableSkills(), name)
	if !ok {
		return agent.ToolResult{}, fmt.Errorf("no loaded skill named '%s'. Run /skills to list loaded skills.%s", name, skillNameHint(tool.availableSkills(), name))
	}
	if requested != "" {
		requestedSource, err := parseRemoveSkillSource(requested)
		if err != nil {
			return agent.ToolResult{}, err
		}
		if requestedSource != skill.Source {
			return agent.ToolResult{}, fmt.Errorf("skill '%s' is active from source '%s', not '%s'. Omit `source` or pass '%s' (the active source).", name, skill.Source.Label(), requestedSource.Label(), skill.Source.Label())
		}
	}
	currentlyEnabled := !skill.DisableModelInvocation
	noChange := currentlyEnabled == enabled
	details := map[string]any{
		"name":              name,
		"source":            skill.Source.Label(),
		"currently_enabled": currentlyEnabled,
		"target_enabled":    enabled,
		"no_change":         noChange,
	}
	if !confirm {
		details["phase"] = "preview"
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("preview only — call again with `confirm: true` to apply. skill=%s source=%s currently=%s target=%s%s", name, skill.Source.Label(), enabledWord(currentlyEnabled), enabledWord(enabled), noChangeSuffix(noChange)), Details: details}, nil
	}
	if catalog, ok := tool.Catalog.(*skills.Catalog); ok {
		if err := catalog.SetEnabled(name, skill.Source, enabled); err != nil {
			return agent.ToolResult{}, fmt.Errorf("set skill state: %w", err)
		}
		effectiveEnabled := effectiveSkillEnabled(catalog.Skills(), name, skill.Source)
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("%s skill '%s' (source: %s).", enabledPastTense(enabled), name, skill.Source.Label()), Details: appliedSkillStateDetails(name, skill.Source, enabled, effectiveEnabled)}, nil
	}
	if _, err := skills.SetAndSave(tool.BaseDir, name, skill.Source, enabled); err != nil {
		return agent.ToolResult{}, fmt.Errorf("set skill state: %w", err)
	}
	effectiveEnabled := any(nil)
	if tool.Catalog != nil {
		if out, err := reloadSkillCatalog(ctx, tool.Catalog); err == nil {
			effectiveEnabled = effectiveSkillEnabled(out.Skills, name, skill.Source)
		} else {
			return agent.ToolResult{}, fmt.Errorf("reload skills catalog: %w", err)
		}
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("%s skill '%s' (source: %s).", enabledPastTense(enabled), name, skill.Source.Label()), Details: appliedSkillStateDetails(name, skill.Source, enabled, effectiveEnabled)}, nil
}

func skillNameHint(available []skills.Skill, name string) string {
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

func enabledWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func enabledPastTense(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func noChangeSuffix(noChange bool) string {
	if noChange {
		return " (no change)"
	}
	return ""
}

func appliedSkillStateDetails(name string, source skills.Source, enabled bool, effectiveEnabled any) map[string]any {
	return map[string]any{
		"phase":                          "applied",
		"name":                           name,
		"source":                         source.Label(),
		"enabled":                        enabled,
		"effective_enabled_after_reload": effectiveEnabled,
		"audit_entry_id":                 nil,
	}
}

func effectiveSkillEnabled(available []skills.Skill, name string, source skills.Source) any {
	for _, skill := range available {
		if skill.Name == name && skill.Source == source {
			return !skill.DisableModelInvocation
		}
	}
	return nil
}

func (tool SetSkillStateTool) availableSkills() []skills.Skill {
	if tool.Catalog != nil {
		return tool.Catalog.Skills()
	}
	return tool.Skills
}

func findSkillByName(available []skills.Skill, name string) (skills.Skill, bool) {
	for _, skill := range available {
		if skill.Name == name {
			return skill, true
		}
	}
	return skills.Skill{}, false
}

func boolArg(call ai.ToolCall, key string) (bool, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return false, fmt.Errorf("missing `%s`", key)
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("`%s` must be a boolean", key)
	}
	return typed, nil
}

func boolArgDefault(call ai.ToolCall, key string, fallback bool) bool {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}
