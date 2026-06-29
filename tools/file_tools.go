package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

const defaultMaxBytes = 256 * 1024

type ReadTool struct {
	Env ExecutionEnv
}

func (ReadTool) Name() string { return "read" }
func (ReadTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (ReadTool) Description() string {
	return "Read the contents of a UTF-8 text file. Use offset/limit for large files; output is truncated to 2000 lines or 256 KiB (whichever first)."
}
func (ReadTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Path to the file (relative or absolute)"}, "offset": map[string]any{"type": "integer", "description": "Line to start reading from (1-indexed)"}, "limit": map[string]any{"type": "integer", "description": "Max lines to read"}}, "required": []string{"path"}}
}
func (tool ReadTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	path, err := requiredStringArg(call, "path")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(path) {
		return agent.ToolResult{}, fmt.Errorf("path must be valid UTF-8")
	}
	data, err := readToolFile(ctx, tool.Env, path)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !utf8.Valid(data) {
		return agent.ToolResult{}, fmt.Errorf("read %s: stream did not contain valid UTF-8", path)
	}
	offset := lsLimitArg(call, "offset", 1)
	startLine := offset
	if startLine < 1 {
		startLine = 1
	}
	maxLines := lsLimitArg(call, "limit", 2000)
	fullText := string(data)
	skip := startLine - 1
	totalLines := 0
	taken := make([]string, 0, min(maxLines, 1024))
	for _, line := range splitInclusiveLines(fullText) {
		totalLines++
		if totalLines <= skip {
			continue
		}
		if len(taken) >= maxLines {
			break
		}
		taken = append(taken, line)
	}
	text, truncation := truncateTextHead(strings.Join(taken, ""), maxLines, defaultMaxBytes)
	keptLines := truncation.KeptLines
	if note := truncation.note(); note != "" {
		text = note + "\n" + text
	}
	content := fmt.Sprintf("[%s] lines %d-%d\n%s", path, startLine, startLine+keptLines-1, text)
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: content, Details: map[string]any{"path": path, "totalLines": totalLines, "keptLines": keptLines, "offset": offset}}, nil
}

type WriteTool struct {
	Env ExecutionEnv
}

func (WriteTool) Name() string { return "write" }
func (WriteTool) Description() string {
	return "Write (or overwrite) a UTF-8 text file. Parent directories are created if missing."
}
func (WriteTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Path to the file (relative or absolute)"}, "content": map[string]any{"type": "string", "description": "Full file contents"}}, "required": []string{"path", "content"}}
}
func (tool WriteTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	path, err := requiredStringArg(call, "path")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(path) {
		return agent.ToolResult{}, fmt.Errorf("path must be valid UTF-8")
	}
	content, err := requiredStringArg(call, "content")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(content) {
		return agent.ToolResult{}, fmt.Errorf("content must be valid UTF-8")
	}
	if err := writeToolFile(ctx, tool.Env, path, []byte(content)); err != nil {
		return agent.ToolResult{}, fmt.Errorf("write %s: %w", path, err)
	}
	lines := countContentLines(content)
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("Wrote %d bytes (%d lines) to %s", len(content), lines, path), Details: map[string]any{"path": path, "bytes": len(content), "lines": lines}}, nil
}

func countContentLines(content string) int {
	if content == "" {
		return 0
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return len(lines)
}

type EditTool struct {
	Env ExecutionEnv
}

func (EditTool) Name() string { return "edit" }
func (EditTool) Description() string {
	return "Replace an exact substring in a file. The substring must be unique unless `replace_all` is true. Use `read` first to confirm the exact text to match, including surrounding context."
}
func (EditTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Path to the file (relative or absolute)"}, "old_string": map[string]any{"type": "string", "description": "Exact substring to replace. Include enough surrounding context to make it unique within the file."}, "new_string": map[string]any{"type": "string", "description": "Replacement string. Use the empty string to delete."}, "replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence rather than requiring uniqueness."}}, "required": []string{"path", "old_string", "new_string"}}
}
func (tool EditTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	path, err := requiredStringArg(call, "path")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(path) {
		return agent.ToolResult{}, fmt.Errorf("path must be valid UTF-8")
	}
	oldString, err := requiredStringArg(call, "old_string")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(oldString) {
		return agent.ToolResult{}, fmt.Errorf("old_string must be valid UTF-8")
	}
	newString, err := requiredStringArg(call, "new_string")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(newString) {
		return agent.ToolResult{}, fmt.Errorf("new_string must be valid UTF-8")
	}
	if oldString == newString {
		return agent.ToolResult{}, fmt.Errorf("old_string must differ from new_string")
	}
	data, err := readToolFile(ctx, tool.Env, path)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !utf8.Valid(data) {
		return agent.ToolResult{}, fmt.Errorf("read %s: stream did not contain valid UTF-8", path)
	}
	text := string(data)
	count := strings.Count(text, oldString)
	replaceAll := boolArgDefault(call, "replace_all", false)
	if count == 0 {
		return agent.ToolResult{}, fmt.Errorf("old_string not found in %s", path)
	}
	if count > 1 && !replaceAll {
		return agent.ToolResult{}, fmt.Errorf("old_string matched %d times in %s; pass replace_all=true to replace every occurrence, or include more surrounding context to make it unique", count, path)
	}
	replacements := 1
	if replaceAll {
		replacements = -1
	}
	updated := strings.Replace(text, oldString, newString, replacements)
	if err := writeToolFile(ctx, tool.Env, path, []byte(updated)); err != nil {
		return agent.ToolResult{}, fmt.Errorf("write %s: %w", path, err)
	}
	preview := renderEditDiffPreview(oldString, newString)
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("Edited %s (%d replacement%s).\n%s", path, count, plural(count), preview), Details: map[string]any{"path": path, "replacements": count, "replaceAll": replaceAll}}, nil
}

func renderEditDiffPreview(oldString, newString string) string {
	var builder strings.Builder
	builder.WriteString("--- before\n")
	oldLines := previewLines(oldString)
	for _, line := range oldLines[:min(10, len(oldLines))] {
		builder.WriteString("- ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	builder.WriteString("+++ after\n")
	newLines := previewLines(newString)
	for _, line := range newLines[:min(10, len(newLines))] {
		builder.WriteString("+ ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func previewLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func stringArgAllowEmpty(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing `%s`", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("`%s` must be a string", key)
	}
	return text, nil
}

func requiredStringArg(call ai.ToolCall, key string) (string, error) {
	normalizeFilePathAlias(call.Arguments)
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing `%s`", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("missing `%s`", key)
	}
	return text, nil
}

func normalizeFilePathAlias(arguments map[string]any) {
	if arguments == nil {
		return
	}
	if _, ok := arguments["path"]; ok {
		return
	}
	for _, alias := range []string{"file_path", "file"} {
		if value, ok := arguments[alias]; ok {
			arguments["path"] = value
			return
		}
	}
}

func requiredToolArg(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing required arg: %s", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("missing required arg: %s", key)
	}
	return text, nil
}

type LSTool struct{}

type LsTool = LSTool

func (LSTool) Name() string { return "ls" }
func (LSTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (LSTool) Description() string {
	return "List directory entries, sorted alphabetically. Directories are suffixed with '/'. Truncated to 500 entries."
}
func (LSTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "Directory to list (default: current directory)"}, "limit": map[string]any{"type": "integer", "description": "Max entries (default 500)"}}}
}
func (LSTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	path := optionalStringArgAllowEmpty(call, "path", ".")
	if !utf8.ValidString(path) {
		return agent.ToolResult{}, fmt.Errorf("path must be valid UTF-8")
	}
	limit := lsLimitArg(call, "limit", 500)
	entries, err := os.ReadDir(path)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("ls %s: %w", path, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%s (%d entries)\n", path, len(entries)))
	bytesUsed := builder.Len()
	shown := 0
	for index, entry := range entries {
		if index >= limit {
			break
		}
		info, err := entry.Info()
		if err != nil {
			return agent.ToolResult{}, fmt.Errorf("metadata %s: %w", entry.Name(), err)
		}
		name := entry.Name()
		var line string
		if info.IsDir() {
			line = "  " + name + "/\n"
		} else {
			line = fmt.Sprintf("  %s (%d bytes)\n", name, info.Size())
		}
		if bytesUsed+len(line) > defaultMaxBytes {
			break
		}
		builder.WriteString(line)
		bytesUsed += len(line)
		shown++
	}
	if shown < len(entries) {
		builder.WriteString(fmt.Sprintf("[truncated: showed %d/%d]\n", shown, len(entries)))
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: builder.String(), Details: map[string]any{"path": path, "totalEntries": len(entries), "shownEntries": shown}}, nil
}

func lsLimitArg(call ai.ToolCall, key string, fallback int) int {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		if typed >= 0 {
			return typed
		}
	case int64:
		if typed >= 0 {
			return int(typed)
		}
	case uint64:
		return clampUint64ToInt(typed)
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 0)
		if err == nil {
			return clampUint64ToInt(parsed)
		}
	}
	return fallback
}

func clampUint64ToInt(value uint64) int {
	if value > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value)
}

func optionalStringArgAllowEmpty(call ai.ToolCall, key string, fallback string) string {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}
