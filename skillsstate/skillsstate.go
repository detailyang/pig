package skillsstate

import "github.com/detailyang/pig/skills"

const STATE_FILE = skills.STATE_FILE
const STATEFILE = skills.STATE_FILE

type Source = skills.Source
type SkillSource = skills.SkillSource
type StateEntry = skills.StateEntry
type SkillStateEntry = skills.SkillStateEntry
type SkillsState = skills.SkillsState
type Skill = skills.Skill
type LoadedSkills = skills.LoadedSkills

const SourceBuiltin = skills.SourceBuiltin
const SourceUser = skills.SourceUser
const SourceProject = skills.SourceProject

const SkillSourceBuiltin = skills.SkillSourceBuiltin
const SkillSourceUser = skills.SkillSourceUser
const SkillSourceProject = skills.SkillSourceProject

func Apply(state SkillsState, skillList []Skill) {
	skills.Apply(state, skillList)
}

func StatePath(baseDir string) string {
	return skills.StatePath(baseDir)
}

func LoadState(baseDir string) SkillsState {
	return skills.LoadState(baseDir)
}

func Load(baseDir string) SkillsState {
	return skills.Load(baseDir)
}

func SaveState(baseDir string, state SkillsState) error {
	return skills.SaveState(baseDir, state)
}

func Save(baseDir string, state SkillsState) error {
	return skills.Save(baseDir, state)
}

func SetAndSave(baseDir, name string, source Source, enabled bool) (SkillsState, error) {
	return skills.SetAndSave(baseDir, name, source, enabled)
}

func RemoveAndSave(baseDir, name string, source Source) error {
	return skills.RemoveAndSave(baseDir, name, source)
}

func SkillsDirs(cwd string) (string, string) { return skills.SkillsDirs(cwd) }

func LoadAll(cwd string) LoadedSkills { return skills.LoadAll(cwd) }
