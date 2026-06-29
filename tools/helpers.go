package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func DefaultTools(memoryDir string) []agent.Tool {
	return []agent.Tool{
		ReadTool{},
		WriteTool{},
		EditTool{},
		BashTool{},
		LSTool{},
		GrepTool{},
		FindTool{},
		WebFetchTool{},
		NewWebSearchTool(WebSearchOptions{}),
		GitTool{},
		NewMemoryTool(memoryDir),
	}
}

func BuiltinTools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"read":       ReadTool{},
		"write":      WriteTool{},
		"edit":       EditTool{},
		"ls":         LSTool{},
		"find":       FindTool{},
		"grep":       GrepTool{},
		"bash":       BashTool{},
		"git":        GitTool{},
		"web_fetch":  WebFetchTool{},
		"web_search": NewWebSearchTool(WebSearchOptions{}),
		"task":       NewTaskTool(TaskToolOptions{}),
	}
}

func stringArg(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing `%s`", key)
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("`%s` must be a non-empty string", key)
	}
	return text, nil
}

func requiredSerdeStringArg(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("invalid arguments: missing field `%s`", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid arguments: invalid type: %s, expected a string", serdeType(value))
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("invalid arguments: invalid UTF-8 in field `%s`", key)
	}
	return text, nil
}

func requiredSerdeBoolArg(call ai.ToolCall, key string) (bool, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return false, fmt.Errorf("invalid arguments: missing field `%s`", key)
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("invalid arguments: invalid type: %s, expected a boolean", serdeType(value))
	}
	return typed, nil
}

func optionalSerdeStringArg(call ai.ToolCall, key string, fallback string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid arguments: invalid type: %s, expected a string", serdeType(value))
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("invalid arguments: invalid UTF-8 in field `%s`", key)
	}
	return text, nil
}

func optionalSerdeBoolArg(call ai.ToolCall, key string, fallback bool) (bool, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback, nil
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("invalid arguments: invalid type: %s, expected a boolean", serdeType(value))
	}
	return typed, nil
}

func serdeType(value any) string {
	switch typed := value.(type) {
	case string:
		return fmt.Sprintf("string %q", typed)
	case int:
		return fmt.Sprintf("integer `%d`", typed)
	case int64:
		return fmt.Sprintf("integer `%d`", typed)
	case float64:
		return fmt.Sprintf("floating point `%v`", typed)
	case bool:
		return fmt.Sprintf("boolean `%v`", typed)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func optionalStringArg(call ai.ToolCall, key string, fallback string) string {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return fallback
	}
	return text
}

func intArg(call ai.ToolCall, key string, fallback int) int {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case json.Number:
		if parsed, ok := parseJSONNumberInt(typed); ok {
			return parsed
		}
	case float64:
		if math.Trunc(typed) != typed {
			return fallback
		}
		return int(typed)
	}
	return fallback
}

func uintIntArg(call ai.ToolCall, key string, fallback int) int {
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
		if parsed, ok := parseJSONNumberInt(typed); ok && parsed >= 0 {
			return parsed
		}
	case float64:
		if typed >= 0 && math.Trunc(typed) == typed {
			return int(typed)
		}
	}
	return fallback
}

func parseJSONNumberInt(number json.Number) (int, bool) {
	parsed, err := strconv.ParseInt(number.String(), 10, 0)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}

func cleanPath(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Clean(path)
}

func linePrefix(path string, line int, text string) string {
	return fmt.Sprintf("%s:%d: %s", filepath.ToSlash(path), line, text)
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(splitInclusiveLines(text))
}

func splitInclusiveLines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func truncateText(text string, maxLines, maxBytes int) (string, bool) {
	truncated := false
	if maxBytes > 0 && len(text) > maxBytes {
		text = text[:maxBytes]
		truncated = true
	}
	if maxLines > 0 {
		lines := strings.Split(text, "\n")
		if len(lines) > maxLines {
			text = strings.Join(lines[:maxLines], "\n")
			truncated = true
		}
	}
	return text, truncated
}

func truncateTextHead(text string, maxLines, maxBytes int) (string, truncation) {
	info := truncation{TotalBytes: len(text)}
	var builder strings.Builder
	for _, line := range splitInclusiveLines(text) {
		info.TotalLines++
		if maxLines > 0 && info.KeptLines >= maxLines {
			continue
		}
		if maxBytes > 0 && info.KeptBytes+len(line) > maxBytes {
			continue
		}
		builder.WriteString(line)
		info.KeptLines++
		info.KeptBytes += len(line)
	}
	info.TruncatedLines = info.TotalLines - info.KeptLines
	return builder.String(), info
}

type truncation struct {
	TotalLines     int
	KeptLines      int
	TruncatedLines int
	TotalBytes     int
	KeptBytes      int
}

func (truncation truncation) note() string {
	if truncation.TruncatedLines == 0 {
		return ""
	}
	return fmt.Sprintf("[truncated: kept %d/%d lines, %d of %d bytes]", truncation.KeptLines, truncation.TotalLines, truncation.KeptBytes, truncation.TotalBytes)
}

func truncateTextTail(text string, maxLines, maxBytes int) (string, truncation) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	info := truncation{TotalLines: len(lines), TotalBytes: len(text)}
	kept := make([]string, 0, len(lines))
	for index := len(lines) - 1; index >= 0; index-- {
		line := lines[index]
		if maxLines > 0 && info.KeptLines >= maxLines {
			break
		}
		if maxBytes > 0 && info.KeptBytes+len(line) > maxBytes {
			break
		}
		kept = append(kept, line)
		info.KeptLines++
		info.KeptBytes += len(line)
	}
	for left, right := 0, len(kept)-1; left < right; left, right = left+1, right-1 {
		kept[left], kept[right] = kept[right], kept[left]
	}
	info.TruncatedLines = info.TotalLines - info.KeptLines
	return strings.Join(kept, ""), info
}
