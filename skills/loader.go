package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/config"
)

const maxNameLength = 64
const maxDescriptionLength = 1024

func LoadSkills(dirs []string) LoadOutput {
	out := LoadOutput{}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticFileInfoFailed, Message: err.Error(), Path: dir})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		walker := skillWalker{root: filepath.Clean(dir)}
		walker.walk(filepath.Clean(dir), &out)
	}
	return out
}

func LoadSourcedSkills(inputs []SourcedInput) []SourcedSkill {
	var out []SourcedSkill
	for _, input := range inputs {
		result := LoadSkills([]string{input.Dir})
		for _, skill := range result.Skills {
			out = append(out, SourcedSkill{Skill: skill, Source: input.Source, Diagnostics: append([]Diagnostic(nil), result.Diagnostics...)})
		}
	}
	return out
}

type LoadedSkills struct {
	Skills      []Skill
	Diagnostics []Diagnostic
}

func SkillsDirs(cwd string) (user string, project string) {
	return filepath.Join(config.BaseDir(), "skills"), filepath.Join(cwd, ".pie", "skills")
}

func LoadAll(cwd string) LoadedSkills {
	user, project := SkillsDirs(cwd)
	loaded := LoadedSkills{}
	for _, root := range []struct {
		dir    string
		source Source
	}{
		{dir: user, source: SourceUser},
		{dir: project, source: SourceProject},
	} {
		out := LoadSkills([]string{root.dir})
		loaded.Diagnostics = append(loaded.Diagnostics, out.Diagnostics...)
		for _, skill := range out.Skills {
			skill.Source = root.source
			DedupeProjectWins(&loaded.Skills, skill)
		}
	}
	return loaded
}

func DedupeProjectWins(combined *[]Skill, skill Skill) {
	index := skillIndex(*combined, skill.Name)
	if index >= 0 {
		(*combined)[index] = skill
		return
	}
	*combined = append(*combined, skill)
}

func skillIndex(skills []Skill, name string) int {
	for index, skill := range skills {
		if skill.Name == name {
			return index
		}
	}
	return -1
}

type skillWalker struct {
	root    string
	ignores []string
}

type walkItem struct {
	dir              string
	includeRootFiles bool
}

func (walker *skillWalker) walk(root string, out *LoadOutput) {
	stack := []walkItem{{dir: root, includeRootFiles: true}}
	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		walker.walkOne(item.dir, item.includeRootFiles, &stack, out)
	}
}

func (walker *skillWalker) walkOne(dir string, includeRootFiles bool, stack *[]walkItem, out *LoadOutput) {
	walker.addIgnoreRules(dir, out)
	entries, err := walker.readDir(dir)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticListFailed, Message: err.Error(), Path: dir})
		return
	}
	for _, entry := range entries {
		if entry.Name() != "SKILL.md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		isDir, ok := walker.entryIsDir(path, entry, out)
		if !ok {
			continue
		}
		if isDir || walker.isIgnored(path, false) {
			continue
		}
		walker.loadSkillFromFile(path, out)
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		path := filepath.Join(dir, name)
		isDir, ok := walker.entryIsDir(path, entry, out)
		if !ok {
			continue
		}
		if walker.isIgnored(path, isDir) {
			continue
		}
		if isDir {
			*stack = append(*stack, walkItem{dir: path})
			continue
		}
		if includeRootFiles && strings.HasSuffix(name, ".md") {
			walker.loadSkillFromFile(path, out)
		}
	}
}

func (walker *skillWalker) readDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(dir, entry.Name())); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (walker *skillWalker) entryIsDir(path string, entry os.DirEntry, out *LoadOutput) (bool, bool) {
	info, err := entry.Info()
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticFileInfoFailed, Message: err.Error(), Path: path})
		return false, false
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return info.IsDir(), true
	}
	targetInfo, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticFileInfoFailed, Message: err.Error(), Path: path})
		}
		return false, false
	}
	return targetInfo.IsDir(), true
}

func (walker *skillWalker) addIgnoreRules(dir string, out *LoadOutput) {
	var nextIgnores []string
	foundIgnore := false
	for _, name := range []string{".gitignore", ".ignore", ".fdignore"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticFileInfoFailed, Message: err.Error(), Path: path})
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticReadFailed, Message: err.Error(), Path: path})
			continue
		}
		if !utf8.Valid(data) {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticReadFailed, Message: "invalid UTF-8", Path: path})
			continue
		}
		prefix := relPath(walker.root, dir)
		if prefix != "" {
			prefix += "/"
		}
		for _, line := range strings.Split(string(data), "\n") {
			pattern, ok := prefixIgnorePattern(line, prefix)
			if ok {
				nextIgnores = append(nextIgnores, pattern)
			}
		}
		foundIgnore = true
	}
	if foundIgnore {
		walker.ignores = nextIgnores
	}
}

func (walker *skillWalker) isIgnored(path string, isDir bool) bool {
	rel := relPath(walker.root, path)
	if rel == "" {
		return false
	}
	if isDir {
		rel += "/"
	}
	ignored := false
	for _, pattern := range walker.ignores {
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = strings.TrimPrefix(pattern, "!")
		}
		if ignoreMatch(pattern, rel) {
			ignored = !negated
		}
	}
	return ignored
}

func (walker *skillWalker) loadSkillFromFile(path string, out *LoadOutput) {
	raw, err := os.ReadFile(path)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticReadFailed, Message: err.Error(), Path: path})
		return
	}
	if !utf8.Valid(raw) {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticReadFailed, Message: "invalid UTF-8", Path: path})
		return
	}
	frontmatter, body, err := ParseFrontmatter(string(raw))
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticParseFailed, Message: err.Error(), Path: path})
		return
	}
	skillDir := filepath.Dir(path)
	parentName := filepath.Base(skillDir)
	name := frontmatter.Name
	if !frontmatter.HasName {
		name = parentName
	}
	description := frontmatter.Description
	for _, message := range validateName(name, parentName) {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticInvalidMetadata, Message: message, Path: path})
	}
	for _, message := range validateDescription(description) {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticInvalidMetadata, Message: message, Path: path})
	}
	if strings.TrimSpace(description) == "" {
		return
	}
	out.Skills = append(out.Skills, Skill{Name: name, Description: description, FilePath: path, Content: body, DisableModelInvocation: frontmatter.DisableModelInvocation, Source: SourceUser})
}

func ParseFrontmatter(content string) (Frontmatter, string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return Frontmatter{}, normalized, nil
	}
	idx := strings.Index(normalized[3:], "\n---")
	if idx < 0 {
		return Frontmatter{}, normalized, nil
	}
	end := idx + 3
	yamlText := normalized[4:end]
	body := strings.TrimSpace(normalized[end+4:])
	frontmatter, err := parseSimpleYAML(yamlText)
	if err != nil {
		return Frontmatter{}, "", err
	}
	return frontmatter, body, nil
}

func parseSimpleYAML(text string) (Frontmatter, error) {
	fm := Frontmatter{}
	seen := map[string]bool{}
	anchors := map[string]string{}
	lines := strings.Split(text, "\n")
	for index := 0; index < len(lines); index++ {
		rawLine := lines[index]
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Frontmatter{}, fmt.Errorf("yaml: invalid line %q", line)
		}
		key = strings.TrimSpace(key)
		canonicalKey := canonicalFrontmatterKey(key)
		if canonicalKey != "" {
			if seen[canonicalKey] {
				return Frontmatter{}, fmt.Errorf("yaml: duplicate field `%s`", canonicalKey)
			}
			seen[canonicalKey] = true
		}
		rawValue := strings.TrimSpace(value)
		value, rawValue, err := parseYAMLAnchoredScalar(value, anchors)
		if err != nil {
			return Frontmatter{}, err
		}
		switch key {
		case "name":
			if isYAMLNull(rawValue) {
				continue
			}
			if err := validateYAMLString(rawValue); err != nil {
				return Frontmatter{}, err
			}
			fm.Name = value
			fm.HasName = true
		case "description":
			if isYAMLNull(rawValue) {
				continue
			}
			if style, chomp, indent, ok, err := parseYAMLBlockHeader(value); err != nil {
				return Frontmatter{}, err
			} else if ok {
				var rawBlock []string
				for index+1 < len(lines) && isIndentedYAMLLine(lines[index+1]) {
					index++
					rawBlock = append(rawBlock, lines[index])
				}
				block := yamlBlockLines(rawBlock, indent)
				if style == ">" {
					fm.Description = yamlFoldedBlockScalar(block, chomp)
				} else {
					fm.Description = yamlBlockScalar(block, "\n", chomp)
				}
			} else {
				if err := validateYAMLString(rawValue); err != nil {
					return Frontmatter{}, err
				}
				fm.Description = value
			}
		case "disable_model_invocation", "disable-model-invocation":
			parsed, err := parseYAMLBool(rawValue)
			if err != nil {
				return Frontmatter{}, err
			}
			fm.DisableModelInvocation = parsed
		}
	}
	return fm, nil
}

func canonicalFrontmatterKey(key string) string {
	switch key {
	case "name", "description":
		return key
	case "disable_model_invocation", "disable-model-invocation":
		return "disable_model_invocation"
	default:
		return ""
	}
}

func isYAMLNull(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = strings.TrimSpace(before)
	}
	return strings.EqualFold(value, "null") || value == "~"
}

func validateYAMLString(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`) || value == "|" || value == ">" {
		return nil
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = strings.TrimSpace(before)
	}
	if looksCompositeYAML(value) {
		return fmt.Errorf("yaml: invalid type: %s, expected a string", serdeYAMLType(value))
	}
	return nil
}

func looksCompositeYAML(value string) bool {
	return (strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]")) || (strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}"))
}

func looksNumericYAML(value string) bool {
	if value == "" || strings.ContainsAny(value, "_ ") {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

func parseYAMLBool(value string) (bool, error) {
	value = strings.TrimSpace(value)
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = strings.TrimSpace(before)
	}
	switch {
	case strings.EqualFold(value, "true"):
		return true, nil
	case strings.EqualFold(value, "false"):
		return false, nil
	default:
		return false, fmt.Errorf("yaml: invalid type: %s, expected a boolean", serdeYAMLType(value))
	}
}

func serdeYAMLType(value string) string {
	if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) || (strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
		return "string"
	}
	return "value"
}

func parseYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`) {
		quote := value[:1]
		if end := yamlQuotedScalarEnd(value, quote); end >= 0 {
			return unquoteYAMLScalar(value[:end+1])
		}
		return unquoteYAMLScalar(value)
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = before
	}
	return strings.TrimSpace(value)
}

func parseYAMLAnchoredScalar(value string, anchors map[string]string) (string, string, error) {
	rawValue := strings.TrimSpace(value)
	if strings.HasPrefix(rawValue, "*") {
		name := strings.TrimPrefix(stripYAMLInlineComment(rawValue), "*")
		if resolved, ok := anchors[name]; ok {
			return resolved, resolved, nil
		}
		return "", rawValue, fmt.Errorf("yaml: unknown anchor %q referenced", name)
	}
	if strings.HasPrefix(rawValue, "&") {
		anchor, rest, ok := strings.Cut(rawValue[1:], " ")
		if ok {
			rest = strings.TrimSpace(rest)
			resolved := parseYAMLScalar(rest)
			anchors[anchor] = resolved
			return resolved, rest, nil
		}
	}
	return parseYAMLScalar(value), rawValue, nil
}

func stripYAMLInlineComment(value string) string {
	if before, _, ok := strings.Cut(value, " #"); ok {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(value)
}

func unquoteYAMLScalar(value string) string {
	if strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`) {
		return strings.ReplaceAll(value[1:len(value)-1], `''`, `'`)
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return strings.Trim(value, `"'`)
}

func yamlQuotedScalarEnd(value string, quote string) int {
	if quote != `'` {
		for index := 1; index < len(value); index++ {
			if value[index] != '"' {
				continue
			}
			backslashes := 0
			for cursor := index - 1; cursor >= 0 && value[cursor] == '\\'; cursor-- {
				backslashes++
			}
			if backslashes%2 == 1 {
				continue
			}
			return index
		}
		return -1
	}
	for index := 1; index < len(value); index++ {
		if value[index] != '\'' {
			continue
		}
		if index+1 < len(value) && value[index+1] == '\'' {
			index++
			continue
		}
		return index
	}
	return -1
}

func isIndentedYAMLLine(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.TrimSpace(line) == ""
}

func parseYAMLBlockHeader(value string) (string, string, int, bool, error) {
	if value == "" || (value[0] != '|' && value[0] != '>') {
		return "", "", 0, false, nil
	}
	style := value[:1]
	chomp := "clip"
	indent := 0
	for index := 1; index < len(value); index++ {
		switch value[index] {
		case '-':
			if chomp != "clip" {
				return "", "", 0, false, fmt.Errorf("yaml: invalid block scalar chomping indicator")
			}
			chomp = "strip"
		case '+':
			if chomp != "clip" {
				return "", "", 0, false, fmt.Errorf("yaml: invalid block scalar chomping indicator")
			}
			chomp = "keep"
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if indent != 0 {
				return "", "", 0, false, fmt.Errorf("yaml: invalid block scalar indentation indicator")
			}
			indent = int(value[index] - '0')
		case '0':
			return "", "", 0, false, fmt.Errorf("yaml: found an indentation indicator equal to 0")
		default:
			return "", "", 0, false, fmt.Errorf("yaml: invalid block scalar indicator")
		}
	}
	return style, chomp, indent, true, nil
}

func yamlBlockLine(line string, indent int) string {
	if indent <= 0 {
		return strings.TrimSpace(line)
	}
	for removed := 0; removed < indent && strings.HasPrefix(line, " "); removed++ {
		line = line[1:]
	}
	return line
}

func yamlBlockLines(lines []string, indent int) []string {
	if indent <= 0 {
		indent = yamlAutoBlockIndent(lines)
	}
	block := make([]string, 0, len(lines))
	for _, line := range lines {
		block = append(block, yamlBlockLine(line, indent))
	}
	return block
}

func yamlAutoBlockIndent(lines []string) int {
	indent := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		spaces := 0
		for spaces < len(line) && line[spaces] == ' ' {
			spaces++
		}
		if indent == 0 || spaces < indent {
			indent = spaces
		}
	}
	return indent
}

func yamlBlockScalar(lines []string, separator string, chomp string) string {
	if len(lines) == 0 {
		return ""
	}
	trailingBlankLines := 0
	for index := len(lines) - 1; index >= 0 && strings.TrimSpace(lines[index]) == ""; index-- {
		trailingBlankLines++
	}
	text := strings.Join(lines[:len(lines)-trailingBlankLines], separator)
	if chomp != "strip" {
		text += "\n"
	}
	if chomp == "keep" {
		text += strings.Repeat("\n", trailingBlankLines)
	}
	return text
}

func yamlFoldedBlockScalar(lines []string, chomp string) string {
	if len(lines) == 0 {
		return ""
	}
	trailingBlankLines := 0
	for index := len(lines) - 1; index >= 0 && strings.TrimSpace(lines[index]) == ""; index-- {
		trailingBlankLines++
	}
	content := lines[:len(lines)-trailingBlankLines]
	var builder strings.Builder
	for index, line := range content {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if index > 0 {
			previousBlank := false
			for previousIndex := index - 1; previousIndex >= 0 && strings.TrimSpace(content[previousIndex]) == ""; previousIndex-- {
				previousBlank = true
			}
			previous := previousNonBlankLine(content, index)
			if previousBlank || strings.HasPrefix(previous, " ") || strings.HasPrefix(line, " ") || strings.HasPrefix(previous, "\t") || strings.HasPrefix(line, "\t") {
				builder.WriteByte('\n')
			} else {
				builder.WriteByte(' ')
			}
		}
		builder.WriteString(line)
	}
	text := builder.String()
	if chomp != "strip" {
		text += "\n"
	}
	if chomp == "keep" {
		text += strings.Repeat("\n", trailingBlankLines)
	}
	return text
}

func previousNonBlankLine(lines []string, before int) string {
	for index := before - 1; index >= 0; index-- {
		if strings.TrimSpace(lines[index]) != "" {
			return lines[index]
		}
	}
	return ""
}

func validateName(name, parentDirName string) []string {
	var errors []string
	if name != parentDirName {
		errors = append(errors, fmt.Sprintf("name %q does not match parent directory %q", name, parentDirName))
	}
	nameLength := len([]rune(name))
	if nameLength > maxNameLength {
		errors = append(errors, fmt.Sprintf("name exceeds %d characters (%d)", maxNameLength, nameLength))
	}
	for _, ch := range name {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			errors = append(errors, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
			break
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errors = append(errors, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errors = append(errors, "name must not contain consecutive hyphens")
	}
	return errors
}

func validateDescription(description string) []string {
	if strings.TrimSpace(description) == "" {
		return []string{"description is required"}
	}
	descriptionLength := len([]rune(description))
	if descriptionLength > maxDescriptionLength {
		return []string{fmt.Sprintf("description exceeds %d characters (%d)", maxDescriptionLength, descriptionLength)}
	}
	return nil
}

func prefixIgnorePattern(line, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || (strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "\\#")) {
		return "", false
	}
	pattern := line
	negated := false
	if strings.HasPrefix(pattern, "!") {
		negated = true
		pattern = strings.TrimPrefix(pattern, "!")
	} else if strings.HasPrefix(pattern, "\\!") {
		pattern = strings.TrimPrefix(pattern, "\\")
	}
	pattern = trimUnescapedTrailingSpaces(pattern)
	pattern = strings.TrimPrefix(pattern, "/")
	if prefix != "" {
		pattern = prefix + pattern
	}
	if negated {
		pattern = "!" + pattern
	}
	return pattern, true
}

func trimUnescapedTrailingSpaces(pattern string) string {
	for strings.HasSuffix(pattern, " ") && !trailingSpaceEscaped(pattern) {
		pattern = strings.TrimSuffix(pattern, " ")
	}
	return strings.ReplaceAll(pattern, `\ `, " ")
}

func trailingSpaceEscaped(pattern string) bool {
	backslashes := 0
	for index := len(pattern) - 2; index >= 0 && pattern[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func ignoreMatch(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(rel, pattern)
	}
	trimmedRel := strings.TrimSuffix(rel, "/")
	if strings.Contains(pattern, "/") {
		if matchIgnorePathPattern(pattern, trimmedRel) {
			return true
		}
		if ok, _ := filepath.Match(pattern, trimmedRel); ok {
			return true
		}
	}
	if ok, _ := filepath.Match(pattern, filepath.Base(trimmedRel)); ok {
		return true
	}
	return rel == pattern || strings.HasPrefix(rel, strings.TrimSuffix(pattern, "/")+"/")
}

func matchIgnorePathPattern(pattern, rel string) bool {
	patternParts := strings.Split(pattern, "/")
	relParts := strings.Split(rel, "/")
	var match func(patternIndex, relIndex int) bool
	match = func(patternIndex, relIndex int) bool {
		if patternIndex == len(patternParts) {
			return relIndex == len(relParts)
		}
		if patternParts[patternIndex] == "**" {
			for nextRel := relIndex; nextRel <= len(relParts); nextRel++ {
				if match(patternIndex+1, nextRel) {
					return true
				}
			}
			return false
		}
		if relIndex >= len(relParts) {
			return false
		}
		ok, err := filepath.Match(patternParts[patternIndex], relParts[relIndex])
		if err != nil || !ok {
			return false
		}
		return match(patternIndex+1, relIndex+1)
	}
	return match(0, 0)
}

func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
