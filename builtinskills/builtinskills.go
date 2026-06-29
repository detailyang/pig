package builtinskills

import "github.com/detailyang/pig/skills"

type ResolvedBuiltins = skills.ResolvedBuiltins
type UnknownBuiltinError = skills.UnknownBuiltinError

func AvailableBuiltinNames() []string {
	return skills.AvailableBuiltinNames()
}

func ResolveBuiltins(cliRequested []string, configRequested []string) (ResolvedBuiltins, error) {
	return skills.ResolveBuiltins(cliRequested, configRequested)
}

func MergeBuiltinsWithUserProject(builtins []skills.Skill, userProject []skills.Skill) []skills.Skill {
	return skills.MergeBuiltinsWithUserProject(builtins, userProject)
}

func MergeWithUserProject(builtins []skills.Skill, userProject []skills.Skill) []skills.Skill {
	return skills.MergeWithUserProject(builtins, userProject)
}

func ParseBuiltinSkillsConfig(tomlText string) []string {
	return skills.ParseBuiltinSkillsConfig(tomlText)
}

func StripFrontmatter(content string) string {
	return skills.StripFrontmatter(content)
}
