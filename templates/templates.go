package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/config"
)

type Template struct {
	Name        string
	Description string
	Content     string
	FilePath    string

	descriptionSet bool
}

type PromptTemplate = Template

func (template Template) MarshalJSON() ([]byte, error) {
	type templateJSON struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Content     string  `json:"content"`
		FilePath    string  `json:"file_path"`
	}
	var description *string
	if template.descriptionSet || template.Description != "" {
		description = &template.Description
	}
	return marshalJSONNoHTMLEscape(templateJSON{
		Name:        template.Name,
		Description: description,
		Content:     template.Content,
		FilePath:    template.FilePath,
	})
}

func (template *Template) UnmarshalJSON(data []byte) error {
	type templateJSON struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Content     string  `json:"content"`
		FilePath    string  `json:"file_path"`
	}
	var decoded templateJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	template.Name = decoded.Name
	template.Content = decoded.Content
	template.FilePath = decoded.FilePath
	template.descriptionSet = decoded.Description != nil
	if decoded.Description == nil {
		template.Description = ""
	} else {
		template.Description = *decoded.Description
	}
	return nil
}

type DiagnosticCode string

const (
	DiagnosticFileInfoFailed DiagnosticCode = "file_info_failed"
	DiagnosticListFailed     DiagnosticCode = "list_failed"
	DiagnosticReadFailed     DiagnosticCode = "read_failed"
	DiagnosticParseFailed    DiagnosticCode = "parse_failed"
)

type Diagnostic struct {
	Code    DiagnosticCode
	Message string
	Path    string
}

type LoadOutput struct {
	Templates   []Template
	Diagnostics []Diagnostic
}

type LoadTemplatesOutput = LoadOutput
type LoadedTemplates = LoadOutput

type Registry struct {
	templates []Template
}

type PromptTemplateRegistry = Registry

func NewRegistry(templates []Template) Registry {
	return Registry{templates: append([]Template(nil), templates...)}
}

func NewPromptTemplateRegistry(templates []PromptTemplate) PromptTemplateRegistry {
	return NewRegistry(templates)
}

func (registry Registry) List() []Template {
	return append([]Template(nil), registry.templates...)
}

func (registry Registry) Get(name string) (Template, bool) {
	for _, template := range registry.templates {
		if template.Name == name {
			return template, true
		}
	}
	return Template{}, false
}

func Interpolate(template Template, vars map[string]any) string {
	out := template.Content
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := vars[key]
		needle := "{{" + key + "}}"
		var replacement string
		if text, ok := value.(string); ok {
			replacement = text
		} else {
			encoded, err := marshalJSONNoHTMLEscape(value)
			if err != nil {
				replacement = fmt.Sprint(value)
			} else {
				replacement = string(encoded)
			}
		}
		out = strings.ReplaceAll(out, needle, replacement)
	}
	return out
}

func LoadAll(cwd string) LoadOutput {
	user := filepath.Join(config.BaseDir(), "templates")
	project := filepath.Join(cwd, ".pie", "templates")
	userOut := Load([]string{user})
	combined := LoadOutput{Templates: append([]Template(nil), userOut.Templates...), Diagnostics: append([]Diagnostic(nil), userOut.Diagnostics...)}
	projectOut := Load([]string{project})
	combined.Diagnostics = append(combined.Diagnostics, projectOut.Diagnostics...)
	for _, template := range projectOut.Templates {
		index := templateIndex(combined.Templates, template.Name)
		if index >= 0 {
			combined.Templates[index] = template
		} else {
			combined.Templates = append(combined.Templates, template)
		}
	}
	return combined
}

func Load(dirs []string) LoadOutput {
	out := LoadOutput{}
	for _, dir := range dirs {
		info, err := os.Lstat(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticFileInfoFailed, Message: err.Error(), Path: dir})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		entries, err := readDir(dir)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticListFailed, Message: err.Error(), Path: dir})
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := os.Stat(path)
			if err != nil {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticFileInfoFailed, Message: err.Error(), Path: path})
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticReadFailed, Message: err.Error(), Path: path})
				continue
			}
			if !utf8.Valid(raw) {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticReadFailed, Message: "invalid UTF-8", Path: path})
				continue
			}
			frontmatter, body, err := parseFrontmatter(string(raw))
			if err != nil {
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: DiagnosticParseFailed, Message: err.Error(), Path: path})
				continue
			}
			name := frontmatter.Name
			if !frontmatter.HasName {
				name = strings.TrimSuffix(entry.Name(), ".md")
			}
			out.Templates = append(out.Templates, Template{Name: name, Description: frontmatter.Description, Content: body, FilePath: path, descriptionSet: frontmatter.HasDescription})
		}
	}
	return out
}

func LoadTemplates(dirs []string) LoadTemplatesOutput {
	return Load(dirs)
}

func readDir(dir string) ([]os.DirEntry, error) {
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

type frontmatter struct {
	Name           string
	HasName        bool
	Description    string
	HasDescription bool
}

func parseFrontmatter(content string) (frontmatter, string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return frontmatter{}, normalized, nil
	}
	index := strings.Index(normalized[3:], "\n---")
	if index < 0 {
		return frontmatter{}, normalized, nil
	}
	end := index + 3
	yamlText := normalized[4:end]
	body := strings.TrimSpace(normalized[end+4:])
	fm, err := parseSimpleYAML(yamlText)
	if err != nil {
		return frontmatter{}, "", err
	}
	return fm, body, nil
}

func parseSimpleYAML(text string) (frontmatter, error) {
	fm := frontmatter{}
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
			return frontmatter{}, fmt.Errorf("yaml: invalid line %q", line)
		}
		key = strings.TrimSpace(key)
		if key == "name" || key == "description" {
			if seen[key] {
				return frontmatter{}, fmt.Errorf("yaml: duplicate field `%s`", key)
			}
			seen[key] = true
		}
		rawValue := strings.TrimSpace(value)
		value, rawValue, err := parseYAMLAnchoredScalar(value, anchors)
		if err != nil {
			return frontmatter{}, err
		}
		switch key {
		case "name":
			if isYAMLNull(rawValue) {
				continue
			}
			if err := validateYAMLString(rawValue); err != nil {
				return frontmatter{}, err
			}
			fm.Name = value
			fm.HasName = true
		case "description":
			if isYAMLNull(rawValue) {
				continue
			}
			if style, chomp, indent, ok, err := parseYAMLBlockHeader(value); err != nil {
				return frontmatter{}, err
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
					return frontmatter{}, err
				}
				fm.Description = value
			}
			fm.HasDescription = true
		}
	}
	return fm, nil
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
	if value == "" || strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`) {
		return nil
	}
	if before, _, ok := strings.Cut(value, " #"); ok {
		value = strings.TrimSpace(before)
	}
	if looksCompositeYAML(value) {
		return fmt.Errorf("yaml: invalid type: value, expected a string")
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

func templateIndex(templates []Template, name string) int {
	for index, template := range templates {
		if template.Name == name {
			return index
		}
	}
	return -1
}
