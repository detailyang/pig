package agent

import (
	"context"
	"strings"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

type CodingAgentSkill struct {
	Name        string
	Description string
	Location    string
}

type CodingAgentOptions struct {
	Options
	Instructions string
	Skills       []skills.Skill
	CurrentDate  string
	Workspace    string
}

type CodingAgent struct {
	*Agent
}

type ConfiguredAgentOptions struct {
	Options
	Instructions string
	Skills       []skills.Skill
	CurrentDate  string
	Workspace    string
}

func NewConfiguredAgent(options ConfiguredAgentOptions) *Agent {
	agentOptions := options.Options
	agentOptions.Tools = appendSkillTool(agentOptions.Tools, options.Skills)
	agentOptions.SystemPrompt = AgentSystemPrompt(AgentPromptInput{
		Instructions: options.Instructions,
		Tools:        agentOptions.Tools,
		Skills:       codingAgentSkills(options.Skills),
		CurrentDate:  options.CurrentDate,
		Workspace:    options.Workspace,
	})
	return New(agentOptions)
}

func NewCodingAgent(options CodingAgentOptions) *CodingAgent {
	agentOptions := options.Options
	agentOptions.Tools = appendSkillTool(agentOptions.Tools, options.Skills)
	agentOptions.SystemPrompt = CodingAgentSystemPrompt(CodingAgentPromptInput{
		Instructions: options.Instructions,
		Tools:        agentOptions.Tools,
		Skills:       codingAgentSkills(options.Skills),
		CurrentDate:  options.CurrentDate,
		Workspace:    options.Workspace,
	})
	return &CodingAgent{Agent: New(agentOptions)}
}

func appendSkillTool(tools []Tool, agentSkills []skills.Skill) []Tool {
	if len(agentSkills) == 0 || hasTool(tools, "Skill") {
		return tools
	}
	return append(append([]Tool{}, tools...), NewSkillTool(agentSkills))
}

func hasTool(tools []Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name() == name {
			return true
		}
	}
	return false
}

func codingAgentSkills(agentSkills []skills.Skill) []CodingAgentSkill {
	items := make([]CodingAgentSkill, 0, len(agentSkills))
	for _, skill := range agentSkills {
		items = append(items, CodingAgentSkill{Name: skill.Name, Description: skill.Description, Location: skill.FilePath})
	}
	return items
}

type CodingAgentPromptInput struct {
	Instructions string
	Tools        []Tool
	Skills       []CodingAgentSkill
	CurrentDate  string
	Workspace    string
}

func CodingAgentSystemPrompt(input CodingAgentPromptInput) string {
	input.Instructions = "You are an expert coding assistant operating in a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files.\n\n" + strings.TrimSpace(input.Instructions) + `

Coding guidelines:
- Use read to examine files instead of cat or sed.
- Use edit for precise changes (edits[].oldText must match exactly).
- When changing multiple separate locations in one file, use one edit call with multiple entries in edits[] instead of multiple edit calls.
- Each edits[].oldText is matched against the original file, not after earlier edits are applied. Do not emit overlapping or nested edits. Merge nearby changes into one edit.
- Keep edits[].oldText as small as possible while still being unique in the file. Do not pad with large unchanged regions.
- Use write only for new files or complete rewrites.
- Show file paths clearly when working with files.`
	return AgentSystemPrompt(AgentPromptInput(input))
}

type AgentPromptInput struct {
	Instructions string
	Tools        []Tool
	Skills       []CodingAgentSkill
	CurrentDate  string
	Workspace    string
}

func AgentSystemPrompt(input AgentPromptInput) string {
	var builder strings.Builder
	builder.WriteString("<system_prompt>\n")
	if strings.TrimSpace(input.Instructions) != "" {
		builder.WriteString("  <instructions>\n")
		builder.WriteString(strings.TrimSpace(input.Instructions))
		builder.WriteString("\n  </instructions>\n")
	}
	builder.WriteString("  <tools>\n")
	for _, tool := range input.Tools {
		builder.WriteString("    <tool>\n")
		builder.WriteString("      <name>")
		builder.WriteString(tool.Name())
		builder.WriteString("</name>\n")
		builder.WriteString("      <description>")
		builder.WriteString(tool.Description())
		builder.WriteString("</description>\n")
		builder.WriteString("    </tool>\n")
	}
	builder.WriteString("  </tools>\n")
	builder.WriteString("  <skills>\n")
	for _, skill := range input.Skills {
		builder.WriteString("    <skill>\n")
		builder.WriteString("      <name>")
		builder.WriteString(skill.Name)
		builder.WriteString("</name>\n")
		builder.WriteString("      <description>")
		builder.WriteString(skill.Description)
		builder.WriteString("</description>\n")
		if strings.TrimSpace(skill.Location) != "" {
			builder.WriteString("      <location>")
			builder.WriteString(skill.Location)
			builder.WriteString("</location>\n")
		}
		builder.WriteString("    </skill>\n")
	}
	builder.WriteString("  </skills>\n")
	if input.CurrentDate != "" || input.Workspace != "" {
		builder.WriteString("  <runtime_context>\n")
		if input.CurrentDate != "" {
			builder.WriteString("    <current_date>")
			builder.WriteString(input.CurrentDate)
			builder.WriteString("</current_date>\n")
		}
		if input.Workspace != "" {
			builder.WriteString("    <current_working_directory>")
			builder.WriteString(input.Workspace)
			builder.WriteString("</current_working_directory>\n")
		}
		builder.WriteString("  </runtime_context>\n")
	}
	builder.WriteString("</system_prompt>")
	return strings.TrimSpace(builder.String())
}

func (agent *CodingAgent) Run(ctx context.Context, messages []Message) (State, error) {
	return agent.Agent.Run(ctx, messages)
}

func (agent *CodingAgent) Continue(ctx context.Context) (State, error) {
	return agent.Agent.Continue(ctx)
}

func (agent *CodingAgent) SetModel(model ai.Model) {
	agent.Agent.SetModel(model)
}
