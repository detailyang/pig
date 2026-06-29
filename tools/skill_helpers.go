package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

type ListSkillsTool struct {
	Skills  []skills.Skill
	Catalog skillCatalog
}

func NewListSkillsTool(available []skills.Skill) ListSkillsTool {
	return ListSkillsTool{Skills: append([]skills.Skill(nil), available...)}
}

func NewCatalogListSkillsTool(catalog skillCatalog) ListSkillsTool {
	return ListSkillsTool{Catalog: catalog}
}

func (ListSkillsTool) Name() string { return "ListSkills" }
func (ListSkillsTool) Description() string {
	return "List loaded skills with source and enabled state."
}
func (ListSkillsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled_only": map[string]any{"type": "boolean"},
			"source":       map[string]any{"type": "string", "enum": []string{"builtin", "user", "project"}},
		},
		"additionalProperties": false,
	}
}
func (tool ListSkillsTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	enabledOnly := boolArgDefault(call, "enabled_only", false)
	source := optionalStringArg(call, "source", "")
	if !utf8.ValidString(source) {
		return agent.ToolResult{}, fmt.Errorf("source must be valid UTF-8")
	}
	var builder strings.Builder
	builder.WriteString("Skills:\n")
	count := 0
	for _, skill := range tool.availableSkills() {
		if enabledOnly && skill.DisableModelInvocation {
			continue
		}
		if source != "" && skill.Source != skills.Source(source) {
			continue
		}
		state := "enabled"
		if skill.DisableModelInvocation {
			state = "disabled"
		}
		builder.WriteString(fmt.Sprintf("- %s [%s/%s] — %s", skill.Name, skill.Source, state, skill.Description))
		if skill.FilePath != "" {
			builder.WriteString(fmt.Sprintf(" (%s)", filepath.ToSlash(skill.FilePath)))
		}
		builder.WriteByte('\n')
		count++
	}
	if count == 0 {
		builder.WriteString("[no skills]\n")
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: builder.String()}, nil
}

func (tool ListSkillsTool) availableSkills() []skills.Skill {
	if tool.Catalog != nil {
		return tool.Catalog.Skills()
	}
	return tool.Skills
}

type SkillBuilderTool struct {
	Root    string
	Catalog skillCatalog
	Skills  []skills.Skill
}

func NewSkillBuilderTool(root string) SkillBuilderTool { return SkillBuilderTool{Root: root} }

func NewSkillBuilderToolFromHarnessCell(cell *SkillHarnessCell) SkillBuilderTool {
	return NewCatalogSkillBuilderTool(DefaultSkillsRoot(), catalogFromSkillHarnessCell(cell))
}

func (tool SkillBuilderTool) WithSkillsRoot(root string) SkillBuilderTool {
	tool.Root = root
	return tool
}

func NewCatalogSkillBuilderTool(root string, catalog skillCatalog) SkillBuilderTool {
	return SkillBuilderTool{Root: root, Catalog: catalog}
}

func (SkillBuilderTool) Name() string { return "SkillBuilder" }
func (SkillBuilderTool) Description() string {
	return "Create a NEW user skill from structured fields and hot-reload the catalog. Use this when the user asks to create, save, or codify a reusable skill, workflow, checklist, or convention — including \"summarize the recent work / this conversation into a skill\": distill the generalizable workflow from the conversation (steps actually performed, commands used, pitfalls hit) and write instructions for the general case, not a transcript of this one instance. Use InstallSkill instead when installing an existing SKILL.md from a URL, file, or pasted content. The tool renders canonical SKILL.md (frontmatter + sections) from name/description/instructions — do not hand-write frontmatter. Two-phase: first call without `confirm` validates and returns a preview (target path, hash, size, shadow warnings); show the user the planned name/description and get their go-ahead, then call again with `confirm: true` to write atomically to ~/.pie/skills/<name>/SKILL.md and reload. A same-name skill with different content additionally requires `overwrite: true`."
}
func (SkillBuilderTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (SkillBuilderTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	confirm, ok := arguments["confirm"].(bool)
	if !ok || !confirm {
		return agent.PermissionAllow
	}
	return agent.PermissionAsk
}
func (SkillBuilderTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string", "description": "Skill name: lowercase kebab-case (a-z, 0-9, hyphens), max 64 chars. Becomes the directory name and the /skill lookup key."},
			"description":  map[string]any{"type": "string", "description": "One-line summary of what the skill does AND when to use it (max 1024 chars). This is the trigger line the model sees in the catalog — include concrete cue phrases."},
			"instructions": map[string]any{"type": "string", "description": "Markdown body: the steps, conventions, and guidance the skill teaches. Rendered under an '## Instructions' heading."},
			"examples":     map[string]any{"type": "string", "description": "Optional markdown examples, rendered under an '## Examples' heading."},
			"confirm":      map[string]any{"type": "boolean", "default": false, "description": "When false (default), validates and returns a preview without writing. When true, writes the skill and reloads the catalog."},
			"overwrite":    map[string]any{"type": "boolean", "default": false, "description": "Required when a skill of the same name already exists with different content."},
		},
		"required":             []string{"name", "description", "instructions"},
		"additionalProperties": false,
	}
}
func (tool SkillBuilderTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	name, err := requiredSerdeStringArg(call, "name")
	if err != nil {
		return agent.ToolResult{}, err
	}
	description, err := requiredSerdeStringArg(call, "description")
	if err != nil {
		return agent.ToolResult{}, err
	}
	instructions, err := requiredSerdeStringArg(call, "instructions")
	if err != nil {
		return agent.ToolResult{}, err
	}
	examples, err := optionalSerdeStringArg(call, "examples", "")
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
	content, err := RenderSkillMarkdown(name, description, instructions, examples)
	if err != nil {
		return agent.ToolResult{}, err
	}
	description = strings.Join(strings.Fields(description), " ")
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
	warnings := tool.shadowWarnings(name)
	if !confirm {
		previewDetails := map[string]any{
			"phase":              "preview",
			"name":               name,
			"description":        description,
			"warnings":           warnings,
			"target_path":        target,
			"content_hash":       hash,
			"size":               len(content),
			"existing":           existing,
			"overwrite_required": overwriteRequired,
		}
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("preview only — call again with `confirm: true` to create the skill. name=%s target=%s size=%dB existing=%t overwrite_required=%t", name, target, len(content), existing, overwriteRequired), Details: previewDetails}, nil
	}
	if overwriteRequired && !overwrite {
		return agent.ToolResult{}, fmt.Errorf("skill '%s' already exists with different content. Call again with `overwrite: true` to replace it.", name)
	}
	if err := atomicWriteFile(target, []byte(content), 0o644); err != nil {
		return agent.ToolResult{}, fmt.Errorf("build skill: %w", err)
	}
	if tool.Catalog == nil {
		return agent.ToolResult{}, fmt.Errorf("SkillBuilder not yet initialized")
	}
	out, err := reloadSkillCatalog(ctx, tool.Catalog)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("reload skills catalog: %w", err)
	}
	totalSkillsAfter := len(out.Skills)
	diagnosticsCount := len(out.Diagnostics)
	installedVisible := skillVisible(out.Skills, name)
	installedDetails := map[string]any{
		"phase":                        "installed",
		"name":                         name,
		"target_path":                  target,
		"content_hash":                 hash,
		"size":                         len(content),
		"overwrote":                    overwriteRequired,
		"total_skills_after":           totalSkillsAfter,
		"diagnostics_count":            diagnosticsCount,
		"warnings":                     warnings,
		"installed_visible_in_catalog": installedVisible,
		"audit_entry_id":               nil,
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("created skill '%s' at %s (%dB). catalog now has %d skill(s).", name, target, len(content), totalSkillsAfter), Details: installedDetails}, nil
}

func (tool SkillBuilderTool) shadowWarnings(name string) []string {
	available := tool.Skills
	if tool.Catalog != nil {
		available = tool.Catalog.Skills()
	}
	var warnings []string
	for _, skill := range available {
		if skill.Name != name {
			continue
		}
		switch skill.Source {
		case skills.SourceProject:
			warnings = append(warnings, fmt.Sprintf("a project skill named '%s' exists and will shadow this user skill", name))
		case skills.SourceBuiltin:
			warnings = append(warnings, fmt.Sprintf("this will shadow the builtin skill '%s'", name))
		}
	}
	return warnings
}

func RenderSkillMarkdown(name, description, instructions, examples string) (string, error) {
	if err := validateSkillName(name); err != nil {
		return "", err
	}
	description = strings.Join(strings.Fields(description), " ")
	if description == "" {
		return "", fmt.Errorf("description must not be empty")
	}
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		return "", fmt.Errorf("instructions must not be empty")
	}
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(name)
	builder.WriteByte('\n')
	builder.WriteString("description: ")
	builder.WriteString(escapeFrontmatterScalar(description))
	builder.WriteString("\n---\n\n")
	builder.WriteString("# ")
	builder.WriteString(titleFromSkillName(name))
	builder.WriteString("\n\n## Instructions\n\n")
	builder.WriteString(instructions)
	builder.WriteByte('\n')
	if strings.TrimSpace(examples) != "" {
		builder.WriteString("\n## Examples\n\n")
		builder.WriteString(strings.TrimSpace(examples))
		builder.WriteByte('\n')
	}
	content := builder.String()
	if err := validateSkillInstall(name, content); err != nil {
		return "", err
	}
	return content, nil
}

func titleFromSkillName(name string) string {
	parts := strings.Split(name, "-")
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func escapeFrontmatterScalar(value string) string {
	if strings.ContainsAny(value, ":#{}[]&,*?|-<>=!%@`\"'") {
		return fmt.Sprintf("%q", value)
	}
	return value
}
