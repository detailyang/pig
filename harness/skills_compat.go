package harness

import "github.com/detailyang/pig/skills"

func FormatSkillInvocation(skill skills.Skill, additionalInstructions string) string {
	return skills.FormatSkillInvocation(skill, additionalInstructions)
}

func FormatSkill(skill skills.Skill, additionalInstructions string) string {
	return FormatSkillInvocation(skill, additionalInstructions)
}

func FormatSkillsForSystemPrompt(skillList []skills.Skill) string {
	return skills.FormatSkillsForSystemPrompt(skillList)
}

func LoadSkills(dirs []string) LoadSkillsOutput {
	return skills.LoadSkills(dirs)
}

func LoadSourcedSkills(inputs []skills.SourcedInput) []skills.SourcedSkill {
	return skills.LoadSourcedSkills(inputs)
}
