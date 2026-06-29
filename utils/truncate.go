package utils

import "fmt"

const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 256 * 1024

	DEFAULT_MAX_LINES = DefaultMaxLines
	DEFAULT_MAX_BYTES = DefaultMaxBytes
)

type Truncation struct {
	TotalLines     int
	KeptLines      int
	TruncatedLines int
	TotalBytes     int
	KeptBytes      int
}

func (truncation Truncation) Note() string {
	if truncation.TruncatedLines == 0 {
		return ""
	}
	return fmt.Sprintf("[truncated: kept %d/%d lines, %d of %d bytes]", truncation.KeptLines, truncation.TotalLines, truncation.KeptBytes, truncation.TotalBytes)
}

func TruncateHead(text string, maxLines, maxBytes int) (string, Truncation) {
	info := Truncation{TotalBytes: len(text)}
	out := make([]byte, 0, min(len(text), nonNegative(maxBytes)))
	for _, line := range splitInclusiveLines(text) {
		info.TotalLines++
		if info.KeptLines < maxLines && info.KeptBytes+len(line) <= maxBytes {
			out = append(out, line...)
			info.KeptLines++
			info.KeptBytes += len(line)
		}
	}
	info.TruncatedLines = info.TotalLines - info.KeptLines
	return string(out), info
}

func TruncateTail(text string, maxLines, maxBytes int) (string, Truncation) {
	lines := splitInclusiveLines(text)
	info := Truncation{TotalLines: len(lines), TotalBytes: len(text)}
	kept := make([]string, 0, min(len(lines), nonNegative(maxLines)))
	for index := len(lines) - 1; index >= 0; index-- {
		line := lines[index]
		if info.KeptLines >= maxLines || info.KeptBytes+len(line) > maxBytes {
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
	out := ""
	for _, line := range kept {
		out += line
	}
	return out, info
}

func splitInclusiveLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := make([]string, 0)
	start := 0
	for index, char := range text {
		if char == '\n' {
			lines = append(lines, text[start:index+1])
			start = index + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func TruncateText(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	if maxChars < 0 {
		maxChars = 0
	}
	truncated := string(runes[:maxChars])
	return fmt.Sprintf("[truncated, kept %d of %d chars]\n%s", maxChars, len(runes), truncated)
}

func TruncateShellOutput(stdout string, stderr string, maxChars int) string {
	return TruncateText(stdout, maxChars)
}
