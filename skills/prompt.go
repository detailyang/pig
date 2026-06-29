package skills

import (
	"fmt"
	"strings"
)

func FormatSkillInvocation(skill Skill, additionalInstructions string) string {
	dir := dirnameEnvPath(skill.FilePath)
	out := fmt.Sprintf("<skill name=\"%s\" location=\"%s\">\nReferences are relative to %s.\n\n%s\n</skill>", skill.Name, skill.FilePath, dir, skill.Content)
	if additionalInstructions != "" {
		out += "\n\n" + additionalInstructions
	}
	return out
}

func FormatSkill(skill Skill, additionalInstructions string) string {
	return FormatSkillInvocation(skill, additionalInstructions)
}

func dirnameEnvPath(path string) string {
	normalized := strings.TrimRight(path, "/")
	index := strings.LastIndex(normalized, "/")
	if index > 0 {
		return normalized[:index]
	}
	return "/"
}

func FormatSkillsForSystemPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	lines := []string{
		"<skills>",
		"The user has provided skills they want you to use whenever the user request can be solved with their help.",
		"Below is a list of skills with their unique names and descriptions of what they do.",
		"Use the `Skill` tool to invoke a skill by name when applicable.",
		"When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.",
		"",
	}
	for _, skill := range skills {
		lines = append(lines, fmt.Sprintf("- name: %s", skill.Name), fmt.Sprintf("  description: %s", skill.Description))
	}
	lines = append(lines, "</skills>")
	return strings.Join(lines, "\n")
}

func FormatSkillsForSystemPromptUpstream(skills []Skill) string {
	return FormatSkillsForSystemPrompt(skills)
}
