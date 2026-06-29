package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/detailyang/pig/ai"
)

const (
	DefaultMaxLines             = 2000
	DefaultMaxBytes             = 256 * 1024
	ToolOutputHeadLines         = 20
	ToolOutputTailLines         = 4
	ToolOutputErrorHeadLines    = 40
	ToolOutputErrorTailLines    = 8
	ToolOutputMaxLineChars      = 200
	ToolOutputErrorMaxLineChars = 240

	DEFAULT_MAX_LINES                = DefaultMaxLines
	DEFAULT_MAX_BYTES                = DefaultMaxBytes
	DEFAULTMAXLINES                  = DEFAULT_MAX_LINES
	DEFAULTMAXBYTES                  = DEFAULT_MAX_BYTES
	TOOL_OUTPUT_HEAD_LINES           = ToolOutputHeadLines
	TOOL_OUTPUT_TAIL_LINES           = ToolOutputTailLines
	TOOL_OUTPUT_ERROR_HEAD_LINES     = ToolOutputErrorHeadLines
	TOOL_OUTPUT_ERROR_TAIL_LINES     = ToolOutputErrorTailLines
	TOOL_OUTPUT_MAX_LINE_CHARS       = ToolOutputMaxLineChars
	TOOL_OUTPUT_ERROR_MAX_LINE_CHARS = ToolOutputErrorMaxLineChars
)

type Truncation struct {
	TotalLines     int
	KeptLines      int
	TruncatedLines int
	TotalBytes     int
	KeptBytes      int
}

type ToolCallPreviewField struct {
	Key   string
	Value any
}

func (info Truncation) Note() string {
	return truncation(info).note()
}

func (info Truncation) NoteOK() (string, bool) {
	note := info.Note()
	return note, note != ""
}

func TruncateHead(text string, maxLines, maxBytes int) (string, Truncation) {
	out, info := truncateTextHead(text, maxLines, maxBytes)
	return out, Truncation(info)
}

func TruncateTail(text string, maxLines, maxBytes int) (string, Truncation) {
	out, info := truncateTextTail(text, maxLines, maxBytes)
	return out, Truncation(info)
}

func TruncateText(text string, maxChars int) string {
	runeCount := len([]rune(text))
	if maxChars >= 0 && runeCount > maxChars {
		truncated := string([]rune(text)[:maxChars])
		return fmt.Sprintf("[truncated, kept %d of %d chars]\n%s", maxChars, runeCount, truncated)
	}
	return text
}

func TruncateChars(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	if maxChars < 0 {
		maxChars = 0
	}
	return string(runes[:maxChars]) + "…"
}

func TruncateShellOutput(stdout string, stderr string, maxChars int) string {
	return TruncateText(stdout, maxChars)
}

func CompactToolOutputLines(lines []string, isError bool) []string {
	headLines, tailLines, maxLineChars := ToolOutputHeadLines, ToolOutputTailLines, ToolOutputMaxLineChars
	if isError {
		headLines, tailLines, maxLineChars = ToolOutputErrorHeadLines, ToolOutputErrorTailLines, ToolOutputErrorMaxLineChars
	}
	originalLineCount := len(lines)
	hiddenBytes := 0
	compacted := make([]string, 0, len(lines))
	for _, line := range lines {
		keptBytes := len(string([]rune(line)[:min(len([]rune(line)), maxLineChars)]))
		if keptBytes < len(line) {
			hiddenBytes += len(line) - keptBytes
			compacted = append(compacted, TruncateChars(line, maxLineChars))
		} else {
			compacted = append(compacted, line)
		}
	}

	maxLines := headLines + tailLines
	hiddenLines := 0
	if len(compacted) > maxLines {
		hiddenLines = len(compacted) - maxLines
		omitted := compacted[headLines : len(compacted)-tailLines]
		for _, line := range omitted {
			hiddenBytes += len(line) + 1
		}
		out := make([]string, 0, maxLines+1)
		out = append(out, compacted[:headLines]...)
		out = append(out, truncationMarker(hiddenBytes, hiddenLines))
		out = append(out, compacted[len(compacted)-tailLines:]...)
		compacted = out
	} else if hiddenBytes > 0 {
		compacted = append(compacted, truncationMarker(hiddenBytes, hiddenLines))
	}
	if originalLineCount == 0 {
		return []string{}
	}
	return compacted
}

func CompactToolContentBlocks(blocks []ai.UserContentBlock, isError bool) []string {
	lines := []string{}
	for _, block := range blocks {
		if block.Type == ai.UserContentText {
			lines = append(lines, textLines(block.Text)...)
		}
	}
	return CompactToolOutputLines(lines, isError)
}

func ToolCallPreview(arguments any) string {
	object, ok := arguments.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]ToolCallPreviewField, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, ToolCallPreviewField{Key: key, Value: object[key]})
	}
	return ToolCallPreviewOrdered(fields)
}

func ToolCallPreviewOrdered(fields []ToolCallPreviewField) string {
	parts := make([]string, 0, min(len(fields), 4))
	for index, field := range fields {
		if index >= 3 {
			break
		}
		parts = append(parts, field.Key+"="+toolCallPreviewValue(field.Value))
	}
	if len(fields) > 3 {
		parts = append(parts, "…")
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func toolCallPreviewValue(value any) string {
	if text, ok := value.(string); ok {
		text = strings.ReplaceAll(text, "\n", "\\n")
		return "\"" + TruncateChars(text, 60) + "\""
	}
	data, err := marshalJSONNoHTMLEscape(value)
	if err != nil {
		return TruncateChars(fmt.Sprint(value), 60)
	}
	return TruncateChars(string(data), 60)
}

func marshalJSONNoHTMLEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func textLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := []string{}
	start := 0
	for index, char := range text {
		if char == '\n' {
			lines = append(lines, text[start:index])
			start = index + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

func truncationMarker(hiddenBytes int, hiddenLines int) string {
	switch {
	case hiddenBytes == 0 && hiddenLines == 0:
		return "… truncated for display; full output remains available to the agent …"
	case hiddenLines == 0:
		return fmt.Sprintf("… truncated %d bytes for display; full output remains available to the agent …", hiddenBytes)
	case hiddenBytes == 0:
		return fmt.Sprintf("… truncated %d lines for display; full output remains available to the agent …", hiddenLines)
	default:
		return fmt.Sprintf("… truncated %d bytes / %d lines for display; full output remains available to the agent …", hiddenBytes, hiddenLines)
	}
}
