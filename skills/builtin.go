package skills

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

type builtinSpec struct {
	name        string
	description string
	rawMarkdown string
}

type ResolvedBuiltins struct {
	Skills      []Skill
	Diagnostics []string
}

type UnknownBuiltinError struct {
	Unknown   []string
	Available []string
}

func (err *UnknownBuiltinError) Error() string {
	return fmt.Sprintf("unknown built-in skill(s) requested via --builtin-skill: %s. Available: %s.", strings.Join(err.Unknown, ", "), strings.Join(err.Available, ", "))
}

func (err *UnknownBuiltinError) String() string {
	return err.Error()
}

func AvailableBuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for _, spec := range builtins {
		names = append(names, spec.name)
	}
	sort.Strings(names)
	return names
}

func ResolveBuiltins(cliRequested []string, configRequested []string) (ResolvedBuiltins, error) {
	known := map[string]bool{}
	for _, name := range AvailableBuiltinNames() {
		known[name] = true
	}
	unknownCLI := unknownNames(cliRequested, known)
	if len(unknownCLI) != 0 {
		return ResolvedBuiltins{}, &UnknownBuiltinError{Unknown: unknownCLI, Available: AvailableBuiltinNames()}
	}
	var diagnostics []string
	unknownConfig := unknownNames(configRequested, known)
	if len(unknownConfig) != 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("config: ignoring unknown built-in skill(s) in `[builtin_skills] enabled`: %s. Available: %s.", strings.Join(unknownConfig, ", "), strings.Join(AvailableBuiltinNames(), ", ")))
	}
	enabled := map[string]bool{}
	for _, name := range append(append([]string(nil), cliRequested...), configRequested...) {
		if known[name] {
			enabled[name] = true
		}
	}
	names := make([]string, 0, len(enabled))
	for name := range enabled {
		names = append(names, name)
	}
	sort.Strings(names)
	resolved := ResolvedBuiltins{Diagnostics: diagnostics}
	for _, name := range names {
		resolved.Skills = append(resolved.Skills, specToSkill(findBuiltin(name)))
	}
	return resolved, nil
}

func MergeBuiltinsWithUserProject(builtins []Skill, userProject []Skill) []Skill {
	merged := append([]Skill(nil), builtins...)
	for _, skill := range userProject {
		replaced := false
		for index := range merged {
			if merged[index].Name == skill.Name {
				merged[index] = skill
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, skill)
		}
	}
	return merged
}

func MergeWithUserProject(builtins []Skill, userProject []Skill) []Skill {
	return MergeBuiltinsWithUserProject(builtins, userProject)
}

func ParseBuiltinSkillsConfig(tomlText string) []string {
	sections := parseSimpleConfig(tomlText)
	values := sections["builtin_skills"]["enabled"]
	if values == "" {
		return nil
	}
	return parseStringArray(values)
}

func unknownNames(names []string, known map[string]bool) []string {
	seen := map[string]bool{}
	var unknown []string
	for _, name := range names {
		if known[name] || seen[name] {
			continue
		}
		seen[name] = true
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	return unknown
}

func findBuiltin(name string) builtinSpec {
	for _, spec := range builtins {
		if spec.name == name {
			return spec
		}
	}
	return builtinSpec{}
}

func specToSkill(spec builtinSpec) Skill {
	return Skill{Name: spec.name, Description: spec.description, FilePath: fmt.Sprintf("<builtin>/%s/SKILL.md", spec.name), Content: stripSkillFrontmatter(spec.rawMarkdown), Source: SourceBuiltin}
}

func StripFrontmatter(content string) string {
	return stripSkillFrontmatter(content)
}

func stripSkillFrontmatter(content string) string {
	trimmed := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return content
	}
	withoutOpen := trimmed[3:]
	newline := strings.IndexByte(withoutOpen, '\n')
	if newline < 0 {
		return content
	}
	afterOpen := withoutOpen[newline+1:]
	searchFrom := 0
	for {
		pos := strings.Index(afterOpen[searchFrom:], "\n---")
		if pos < 0 {
			return content
		}
		absolute := searchFrom + pos + 1
		afterClose := afterOpen[absolute+3:]
		if strings.HasPrefix(afterClose, "\n") {
			return strings.TrimLeft(afterClose[1:], "\n")
		}
		if afterClose == "" {
			return ""
		}
		searchFrom = absolute + 3
	}
}

func parseSimpleConfig(text string) map[string]map[string]string {
	sections := map[string]map[string]string{"": {}}
	current := ""
	var pendingKey string
	var pendingValue strings.Builder
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(stripConfigComment(rawLine))
		if line == "" {
			continue
		}
		if pendingKey != "" {
			pendingValue.WriteString(line)
			if strings.Contains(line, "]") {
				sections[current][pendingKey] = pendingValue.String()
				pendingKey = ""
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && !strings.Contains(line, "[[") {
			current = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if _, exists := sections[current]; exists {
				return map[string]map[string]string{}
			}
			sections[current] = map[string]string{}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return map[string]map[string]string{}
		}
		if sections[current] == nil {
			sections[current] = map[string]string{}
		}
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if _, exists := sections[current][trimmedKey]; exists {
			return map[string]map[string]string{}
		}
		if strings.HasPrefix(trimmedValue, "[") && !strings.Contains(trimmedValue, "]") {
			pendingKey = trimmedKey
			pendingValue.Reset()
			pendingValue.WriteString(trimmedValue)
			continue
		}
		sections[current][trimmedKey] = trimmedValue
	}
	if pendingKey != "" {
		return map[string]map[string]string{}
	}
	return sections
}

func stripConfigComment(line string) string {
	inString := false
	for index, char := range line {
		if char == '"' {
			inString = !inString
		}
		if char == '#' && !inString {
			return line[:index]
		}
	}
	return line
}

func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if inner == "" {
		return nil
	}
	var out []string
	parts := strings.Split(inner, ",")
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" && index == len(parts)-1 {
			continue
		}
		if !((strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`)) || (strings.HasPrefix(part, `'`) && strings.HasSuffix(part, `'`))) {
			return nil
		}
		item := strings.Trim(part, `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

var builtins = []builtinSpec{{name: "karpathy-guidelines", description: "Behavioral guidelines to reduce common LLM coding mistakes. Use when writing, reviewing, or refactoring code to avoid overcomplication, make surgical changes, surface assumptions, and define verifiable success criteria.", rawMarkdown: karpathyGuidelinesMarkdown}}

//go:embed builtin/karpathy-guidelines/SKILL.md
var karpathyGuidelinesMarkdown string
