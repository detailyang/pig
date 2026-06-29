package readline

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/detailyang/pig/commands"
	pigskills "github.com/detailyang/pig/skills"
)

type Command struct {
	Name    string
	Aliases []string
}

type Skill struct {
	Name     string
	Disabled bool
}

type SlashCompleter struct {
	commands []string
}

func NewSlashCompleter(commands []Command, skills []Skill) SlashCompleter {
	items := make([]string, 0, len(commands)+len(skills))
	commandNames := map[string]bool{}
	for _, command := range commands {
		if command.Name == "" {
			continue
		}
		commandNames[command.Name] = true
		items = append(items, "/"+command.Name)
		for _, alias := range command.Aliases {
			if alias != "" {
				items = append(items, "/"+alias)
			}
		}
	}
	for _, skill := range skills {
		if skill.Name == "" || skill.Disabled || commandNames[skill.Name] {
			continue
		}
		items = append(items, "/"+skill.Name)
	}
	sort.Strings(items)
	items = dedup(items)
	return SlashCompleter{commands: items}
}

func FromRegistry(registry *commands.Registry) SlashCompleter {
	return FromRegistryAndSkills(registry, nil)
}

func FromRegistryAndSkills(registry *commands.Registry, available []pigskills.Skill) SlashCompleter {
	if registry == nil {
		return NewSlashCompleter(nil, nil)
	}
	commandSpecs := []Command{}
	for _, command := range registry.Commands() {
		spec := Command{Name: command.Name()}
		if aliased, ok := command.(commands.AliasedCommand); ok {
			spec.Aliases = append([]string(nil), aliased.Aliases()...)
		}
		commandSpecs = append(commandSpecs, spec)
	}
	skillSpecs := []Skill{}
	for _, shortcut := range commands.SkillShortcuts(available, registry) {
		name := strings.TrimPrefix(shortcut.Command, "/")
		if name != "" {
			skillSpecs = append(skillSpecs, Skill{Name: name})
		}
	}
	return NewSlashCompleter(commandSpecs, skillSpecs)
}

func (completer SlashCompleter) Matches(line string) []string {
	token := SlashToken(line)
	if token == "" {
		return nil
	}
	var matches []string
	for _, command := range completer.commands {
		if strings.HasPrefix(command, token) {
			matches = append(matches, command)
		}
	}
	if len(matches) == 1 && matches[0] == token {
		return nil
	}
	return matches
}

func SlashToken(line string) string {
	trimmed := trimStartSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}
	for _, ch := range trimmed[1:] {
		if unicode.IsSpace(ch) {
			return ""
		}
	}
	return trimmed
}

func trimStartSpace(line string) string {
	for len(line) > 0 {
		ch, size := utf8.DecodeRuneInString(line)
		if !unicode.IsSpace(ch) {
			return line
		}
		line = line[size:]
	}
	return line
}

func dedup(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
