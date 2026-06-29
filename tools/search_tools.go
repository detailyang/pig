package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

const maxMatchLineChars = 500

const defaultMaxGrepFiles = 5000

type FindTool struct {
	Env ExecutionEnv
}

func (FindTool) Name() string { return "find" }
func (FindTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (FindTool) Description() string {
	return "Find files by filename glob. Honors .gitignore. Output limited to 200 paths by default; use `limit` only when a larger result set is necessary."
}
func (FindTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"glob": map[string]any{"type": "string", "description": "Filename glob (e.g. *.rs, README*)"}, "path": map[string]any{"type": "string", "description": "Directory to search (default: current)"}, "limit": map[string]any{"type": "integer", "description": "Max path count"}}, "required": []string{"glob"}}
}
func (tool FindTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	root := optionalStringArgAllowEmpty(call, "path", ".")
	if !utf8.ValidString(root) {
		return agent.ToolResult{}, fmt.Errorf("path must be valid UTF-8")
	}
	glob, err := findGlobArg(call)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(glob) {
		return agent.ToolResult{}, fmt.Errorf("glob must be valid UTF-8")
	}
	limit := uintArg(call, "limit", 200)
	if err := validateOptionalGlob(glob); err != nil {
		return agent.ToolResult{}, err
	}
	if root == "" {
		return agent.ToolResult{}, fmt.Errorf("find: empty path")
	}
	root = resolveToolPath(tool.Env, root)
	ignore, err := loadGitignore(root)
	if err != nil {
		return agent.ToolResult{}, err
	}
	var matches []string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if entry.IsDir() && shouldSkipDir(entry.Name()) && path != root {
			return filepath.SkipDir
		}
		if path != root && isHiddenPath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && ignore.ignores(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		ok, err := matchOptionalGlobPath(glob, rel, entry.Name())
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, filepath.ToSlash(path))
		}
		return nil
	})
	if walkErr != nil {
		return agent.ToolResult{}, fmt.Errorf("find: %w", walkErr)
	}
	sort.Strings(matches)
	truncated := limit >= 0 && len(matches) > limit
	if limit == 0 && len(matches) > 0 {
		matches = matches[:0]
		truncated = true
	} else if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	var builder strings.Builder
	if truncated {
		builder.WriteString(fmt.Sprintf("find %s: showing first %d hits (limit reached)\n", glob, len(matches)))
	} else {
		builder.WriteString(fmt.Sprintf("find %s: %d hits\n", glob, len(matches)))
	}
	for _, match := range matches {
		builder.WriteString(match)
		builder.WriteByte('\n')
	}
	if truncated {
		builder.WriteString("... results truncated; rerun with a narrower glob/path or a higher limit if needed\n")
	}
	content := builder.String()
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: content, Details: map[string]any{"paths": append([]string(nil), matches...), "limit": limit, "stopped_at_limit": truncated}}, nil
}

func findGlobArg(call ai.ToolCall) (string, error) {
	if _, ok := call.Arguments["glob"]; ok {
		return requiredStringArg(call, "glob")
	}
	return requiredStringArg(call, "pattern")
}

type GrepTool struct {
	Env ExecutionEnv
}

func (GrepTool) Name() string { return "grep" }
func (GrepTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (GrepTool) Description() string {
	return "Search files for lines matching a regex. Honors .gitignore. Output limited to 200 matches."
}
func (GrepTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"pattern": map[string]any{"type": "string", "description": "Regex pattern"}, "path": map[string]any{"type": "string", "description": "Directory to search (default: current)"}, "glob": map[string]any{"type": "string", "description": "Optional filename glob (e.g. *.rs)"}, "case_insensitive": map[string]any{"type": "boolean", "description": "Case-insensitive match"}, "limit": map[string]any{"type": "integer", "description": "Max match count"}}, "required": []string{"pattern"}}
}
func (tool GrepTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	root := optionalStringArgAllowEmpty(call, "path", ".")
	if !utf8.ValidString(root) {
		return agent.ToolResult{}, fmt.Errorf("path must be valid UTF-8")
	}
	pattern, err := requiredStringArg(call, "pattern")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(pattern) {
		return agent.ToolResult{}, fmt.Errorf("pattern must be valid UTF-8")
	}
	if boolArgDefault(call, "case_insensitive", false) {
		pattern = "(?i)" + pattern
	}
	glob := optionalStringArgAllowEmpty(call, "glob", "*")
	if !utf8.ValidString(glob) {
		return agent.ToolResult{}, fmt.Errorf("glob must be valid UTF-8")
	}
	limit := uintArg(call, "limit", 200)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("regex: %w", err)
	}
	if err := validateOptionalGlob(glob); err != nil {
		return agent.ToolResult{}, err
	}
	if root == "" {
		return agent.ToolResult{}, fmt.Errorf("grep: empty path")
	}
	root = resolveToolPath(tool.Env, root)
	ignore, err := loadGitignore(root)
	if err != nil {
		return agent.ToolResult{}, err
	}
	var matches []string
	truncatedLines := 0
	filesScanned := 0
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if entry.IsDir() && shouldSkipDir(entry.Name()) && path != root {
			return filepath.SkipDir
		}
		if path != root && isHiddenPath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && ignore.ignores(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		ok, err := matchOptionalGlobPath(glob, rel, entry.Name())
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		filesScanned++
		if filesScanned > defaultMaxGrepFiles {
			return filepath.SkipAll
		}
		fileMatches, fileTruncatedLines, err := grepFile(root, path, re, limit-len(matches))
		if err != nil {
			return err
		}
		matches = append(matches, fileMatches...)
		truncatedLines += fileTruncatedLines
		if limit >= 0 && len(matches) >= max(limit, 1) {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return agent.ToolResult{}, fmt.Errorf("grep: %w", walkErr)
	}
	matchCount := len(matches)
	truncated := limit >= 0 && len(matches) >= limit
	visibleMatches := matches
	if limit >= 0 && len(visibleMatches) > limit {
		visibleMatches = visibleMatches[:limit]
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("grep: %d hits\n", matchCount))
	builder.WriteString(strings.Join(visibleMatches, "\n"))
	content := builder.String()
	if len(visibleMatches) > 0 {
		content += "\n"
	}
	if truncatedLines > 0 {
		content += fmt.Sprintf("[%d long matching line(s) truncated to %d chars]\n", truncatedLines, maxMatchLineChars)
	}
	if truncated {
		content += fmt.Sprintf("[truncated at %d matches]\n", limit)
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: content, Details: map[string]any{"matches": matchCount, "truncated_lines": truncatedLines, "max_match_line_chars": maxMatchLineChars}}, nil
}

func matchOptionalGlob(glob, name string) (bool, error) {
	if glob == "" {
		return name == "", nil
	}
	patterns := expandBraceGlob(glob)
	for _, pattern := range patterns {
		pattern = strings.TrimPrefix(pattern, "**/")
		pattern = translateGlobsetPattern(pattern)
		ok, err := filepath.Match(pattern, name)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func matchOptionalGlobPath(glob, rel, name string) (bool, error) {
	if glob == "**" || strings.Contains(glob, "/") {
		return matchOptionalGlobPathSegments(glob, rel)
	}
	return matchOptionalGlob(glob, name)
}

func matchOptionalGlobPathSegments(glob, rel string) (bool, error) {
	patterns := expandBraceGlob(glob)
	for _, pattern := range patterns {
		pattern = translateGlobsetPattern(pattern)
		ok, err := matchGlobSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func matchGlobSegments(patterns, segments []string) (bool, error) {
	if len(patterns) == 0 {
		return len(segments) == 0, nil
	}
	pattern := patterns[0]
	if pattern == "**" {
		if len(patterns) == 1 {
			return len(segments) > 0, nil
		}
		for count := 0; count <= len(segments); count++ {
			ok, err := matchGlobSegments(patterns[1:], segments[count:])
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
	if len(segments) == 0 {
		return false, nil
	}
	ok, err := filepath.Match(pattern, segments[0])
	if err != nil || !ok {
		return ok, err
	}
	return matchGlobSegments(patterns[1:], segments[1:])
}

func translateGlobsetPattern(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "[!", "[^")
	return translateGlobsetDashClasses(pattern)
}

func translateGlobsetDashClasses(pattern string) string {
	var builder strings.Builder
	for index := 0; index < len(pattern); index++ {
		if pattern[index] != '[' {
			builder.WriteByte(pattern[index])
			continue
		}
		end := strings.IndexByte(pattern[index+1:], ']')
		if end == 0 {
			nextEnd := strings.IndexByte(pattern[index+2:], ']')
			if nextEnd >= 0 {
				end = nextEnd + 1
			}
		} else if end == 1 && index+1 < len(pattern) && pattern[index+1] == '^' {
			nextEnd := strings.IndexByte(pattern[index+3:], ']')
			if nextEnd >= 0 {
				end = nextEnd + 2
			}
		}
		if end < 0 {
			builder.WriteByte(pattern[index])
			continue
		}
		classStart := index + 1
		classEnd := classStart + end
		class := pattern[classStart:classEnd]
		if strings.HasPrefix(class, "^") && len(class) > 1 {
			prefix := "^"
			body := class[1:]
			builder.WriteString("[")
			builder.WriteString(prefix)
			builder.WriteString(escapeEdgeDash(body))
			builder.WriteByte(']')
		} else {
			builder.WriteString("[")
			builder.WriteString(escapeEdgeDash(class))
			builder.WriteByte(']')
		}
		index = classEnd
	}
	return builder.String()
}

func escapeEdgeDash(class string) string {
	if class == "" {
		return class
	}
	if strings.HasPrefix(class, "]") {
		class = `\]` + class[1:]
	}
	if class == "-" {
		return `\-`
	}
	if strings.HasPrefix(class, "-") {
		class = `\-` + class[1:]
	}
	if strings.HasSuffix(class, "-") && !strings.HasSuffix(class, `\-`) {
		class = class[:len(class)-1] + `\-`
	}
	return class
}

func validateOptionalGlob(glob string) error {
	if err := validateDanglingEscape(glob); err != nil {
		return err
	}
	if err := validateBraceEscapes(glob); err != nil {
		return err
	}
	if err := validateRecursiveGlob(glob); err != nil {
		return err
	}
	if err := validateGlobRanges(glob); err != nil {
		return err
	}
	_, err := matchOptionalGlob(glob, "")
	if err == nil {
		return nil
	}
	return fmt.Errorf("error parsing glob '%s': %s", glob, upstreamGlobError(err))
}

func validateDanglingEscape(glob string) error {
	if !strings.HasSuffix(glob, `\`) {
		return nil
	}
	backslashes := 0
	for index := len(glob) - 1; index >= 0 && glob[index] == '\\'; index-- {
		backslashes++
	}
	if backslashes%2 == 1 {
		return fmt.Errorf("error parsing glob '%s': dangling '\\'", glob)
	}
	return nil
}

func validateBraceEscapes(glob string) error {
	if strings.Contains(glob, `\{`) && strings.Contains(glob, "}") {
		return fmt.Errorf("error parsing glob '%s': unopened alternate group; missing '{' (maybe escape '}' with '[}]'?)", glob)
	}
	if strings.Contains(glob, "{") && strings.Contains(glob, `\}`) {
		return fmt.Errorf("error parsing glob '%s': unclosed alternate group; missing '}' (maybe escape '{' with '[{]'?)", glob)
	}
	depth := 0
	for index := 0; index < len(glob); index++ {
		if glob[index] == '[' {
			index = skipGlobClass(glob, index)
			continue
		}
		switch glob[index] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return fmt.Errorf("error parsing glob '%s': unopened alternate group; missing '{' (maybe escape '}' with '[}]'?)", glob)
			}
			depth--
		}
	}
	if depth > 0 {
		return fmt.Errorf("error parsing glob '%s': unclosed alternate group; missing '}' (maybe escape '{' with '[{]'?)", glob)
	}
	return nil
}

func validateRecursiveGlob(glob string) error {
	for _, pattern := range expandBraceGlob(glob) {
		parts := strings.Split(pattern, "/")
		for _, part := range parts {
			if strings.Contains(part, "**") && part != "**" {
				return fmt.Errorf("error parsing glob '%s': invalid use of **; must be one path component", glob)
			}
		}
	}
	return nil
}

func validateGlobRanges(glob string) error {
	for index := 0; index < len(glob); index++ {
		if glob[index] != '[' {
			continue
		}
		end := strings.IndexByte(glob[index+1:], ']')
		if end < 0 {
			continue
		}
		class := glob[index+1 : index+1+end]
		for offset := 1; offset+1 < len(class); offset++ {
			if class[offset] == '-' && class[offset-1] > class[offset+1] {
				return fmt.Errorf("error parsing glob '%s': invalid range; '%c' > '%c'", glob, class[offset-1], class[offset+1])
			}
		}
		index += end + 1
	}
	return nil
}

func upstreamGlobError(err error) string {
	if errors.Is(err, filepath.ErrBadPattern) {
		return "unclosed character class; missing ']'"
	}
	return err.Error()
}

func expandBraceGlob(glob string) []string {
	open, close := firstBracePair(glob)
	if open < 0 {
		return []string{glob}
	}
	body := glob[open+1 : close]
	if body == "" {
		return expandBraceGlob(glob[:open] + glob[close+1:])
	}
	if isOnlyEmptyBraceBranches(body) {
		return expandBraceGlob(glob[:open] + glob[close+1:])
	}
	parts := nonEmptyBraceParts(body)
	if len(parts) == 0 {
		return []string{glob}
	}
	patterns := make([]string, 0, len(parts))
	for _, part := range parts {
		patterns = append(patterns, expandBraceGlob(glob[:open]+part+glob[close+1:])...)
	}
	return patterns
}

func isOnlyEmptyBraceBranches(body string) bool {
	if body == "" {
		return true
	}
	for index := 0; index < len(body); index++ {
		if body[index] != ',' {
			return false
		}
	}
	return true
}

func firstBracePair(glob string) (int, int) {
	open := -1
	depth := 0
	for index := 0; index < len(glob); index++ {
		if glob[index] == '[' {
			index = skipGlobClass(glob, index)
			continue
		}
		switch glob[index] {
		case '{':
			if depth == 0 {
				open = index
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 {
				return open, index
			}
		}
	}
	return -1, -1
}

func skipGlobClass(pattern string, open int) int {
	start := open + 1
	if start < len(pattern) && pattern[start] == '^' {
		start++
	}
	if start < len(pattern) && pattern[start] == ']' {
		start++
	}
	if close := strings.IndexByte(pattern[start:], ']'); close >= 0 {
		return start + close
	}
	return open
}

func nonEmptyBraceParts(body string) []string {
	var parts []string
	start := 0
	depth := 0
	for index := 0; index < len(body); index++ {
		if body[index] == '[' {
			index = skipGlobClass(body, index)
			continue
		}
		switch body[index] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, body[start:index])
				start = index + 1
			}
		}
	}
	parts = append(parts, body[start:])
	kept := parts[:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return kept
}

func uintArg(call ai.ToolCall, key string, fallback int) int {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		if typed >= 0 {
			return typed
		}
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 64)
		if err == nil {
			if parsed > uint64(math.MaxInt) {
				return math.MaxInt
			}
			return int(parsed)
		}
	case uint64:
		if typed > uint64(math.MaxInt) {
			return math.MaxInt
		}
		return int(typed)
	}
	return fallback
}

func grepFile(root, path string, re *regexp.Regexp, remaining int) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, nil
	}
	text := string(data)
	if !utf8.ValidString(text) {
		return nil, 0, nil
	}
	var matches []string
	truncatedLines := 0
	for lineIndex, line := range rustLines(text) {
		if match := re.FindStringIndex(line); match != nil {
			if charLen(line) > maxMatchLineChars {
				line = previewMatchLine(line, match)
				truncatedLines++
			}
			matches = append(matches, linePrefix(path, lineIndex+1, line))
			if remaining >= 0 && len(matches) >= max(remaining, 1) {
				break
			}
		}
	}
	return matches, truncatedLines, nil
}

func rustLines(text string) []string {
	if text == "" {
		return nil
	}
	var lines []string
	start := 0
	for start < len(text) {
		newline := strings.IndexByte(text[start:], '\n')
		if newline < 0 {
			lines = append(lines, text[start:])
			break
		}
		end := start + newline
		line := text[start:end]
		line = strings.TrimSuffix(line, "\r")
		lines = append(lines, line)
		start = end + 1
	}
	return lines
}

func previewMatchLine(line string, match []int) string {
	if charLen(line) <= maxMatchLineChars {
		return line
	}
	if len(match) != 2 {
		return string([]rune(line)[:maxMatchLineChars]) + "...[line truncated]"
	}
	matchStart := charLen(line[:match[0]])
	matchLength := max(charLen(line[match[0]:match[1]]), 1)
	visibleMatchLength := min(matchLength, maxMatchLineChars)
	contextBudget := maxMatchLineChars - visibleMatchLength
	beforeBudget := contextBudget / 2
	afterBudget := contextBudget - beforeBudget
	start := max(matchStart-beforeBudget, 0)
	end := min(matchStart+visibleMatchLength+afterBudget, charLen(line))
	runes := []rune(line)
	var builder strings.Builder
	if start > 0 {
		builder.WriteString("[line truncated]...")
	}
	builder.WriteString(string(runes[start:end]))
	if end < len(runes) {
		builder.WriteString("...[line truncated]")
	}
	return builder.String()
}

func charLen(text string) int {
	return len([]rune(text))
}

func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case ".git", "node_modules", ".hg", ".svn":
		return true
	default:
		return false
	}
}

func isHiddenPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}
