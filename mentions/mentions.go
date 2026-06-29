package mentions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 64 * 1024

func Expand(input, cwd string) (string, []string) {
	mentions := Extract(input)
	if len(mentions) == 0 {
		return input, nil
	}
	blocks := make([]string, 0, len(mentions))
	resolved := make([]string, 0, len(mentions))
	for _, rel := range mentions {
		path := filepath.Join(cwd, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			blocks = append(blocks, fmt.Sprintf("<file path=\"%s\" error=\"%s\" />", rel, err.Error()))
			continue
		}
		if !utf8.Valid(data) {
			blocks = append(blocks, fmt.Sprintf("<file path=\"%s\" error=\"stream did not contain valid UTF-8\" />", rel))
			continue
		}
		body, truncated := truncateString(string(data))
		if truncated {
			blocks = append(blocks, fmt.Sprintf("<file path=\"%s\">\n%s\n\n(truncated at %d KiB)\n</file>", rel, body, MaxBytes/1024))
		} else {
			blocks = append(blocks, fmt.Sprintf("<file path=\"%s\">\n%s\n</file>", rel, body))
		}
		resolved = append(resolved, path)
	}
	header := "Files in context:\n" + strings.Join(blocks, "\n") + "\n\n"
	return header + input, resolved
}

func Extract(input string) []string {
	var out []string
	runes := []rune(input)
	for index := 0; index < len(runes); {
		if runes[index] != '@' {
			index++
			continue
		}
		if index > 0 {
			previous := runes[index-1]
			if unicode.IsLetter(previous) || unicode.IsDigit(previous) || previous == '_' || previous == '.' {
				index++
				continue
			}
		}
		end := index + 1
		for end < len(runes) {
			char := runes[end]
			if unicode.IsSpace(char) || char == ';' || char == ',' || char == '(' || char == ')' || char == '"' || char == '\'' || char == '`' {
				break
			}
			end++
		}
		if end > index+1 {
			path := strings.TrimRight(string(runes[index+1:end]), ".!?:")
			if path != "" {
				out = append(out, path)
			}
		}
		index = end
	}
	return out
}

func ExtractMentions(input string) []string {
	return Extract(input)
}

func truncateString(text string) (string, bool) {
	if len(text) <= MaxBytes {
		return text, false
	}
	end := MaxBytes
	for end > 0 && !isCharBoundary(text, end) {
		end--
	}
	return text[:end], true
}

func Truncate(text string) (string, bool) {
	return truncateString(text)
}

func isCharBoundary(text string, index int) bool {
	if index == 0 || index == len(text) {
		return true
	}
	return index >= 0 && index < len(text) && (text[index]&0xC0) != 0x80
}
