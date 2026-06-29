package config

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

func ParseTriggerPollIntervalSecs(text string) (uint64, bool, error) {
	sections, err := parseSimpleTOML(text)
	if err != nil {
		return 0, false, err
	}
	value, ok := sections["triggers"]["poll_interval_secs"]
	if !ok {
		return 0, false, nil
	}
	if value.Quoted {
		return 0, false, fmt.Errorf("parse config.toml: triggers.poll_interval_secs must be integer")
	}
	normalized, base, ok := normalizeTOMLUint(value.Value)
	if !ok {
		return 0, false, fmt.Errorf("parse config.toml: triggers.poll_interval_secs must be integer")
	}
	secs, err := strconv.ParseUint(normalized, base, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse config.toml: triggers.poll_interval_secs must be integer")
	}
	if secs == 0 {
		return 0, false, fmt.Errorf("`[triggers] poll_interval_secs` must be at least 1")
	}
	return secs, true, nil
}

type simpleTOMLValue struct {
	Value  string
	Quoted bool
}

func parseSimpleTOML(text string) (map[string]map[string]simpleTOMLValue, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("parse config.toml: invalid UTF-8")
	}
	sections := map[string]map[string]simpleTOMLValue{"": {}}
	current := ""
	for lineNumber, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(stripComment(rawLine))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			key, err := parseSimpleTOMLKey(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]")))
			if err != nil {
				return nil, fmt.Errorf("parse config.toml: %w at line %d", err, lineNumber+1)
			}
			if key == "triggers" {
				return nil, fmt.Errorf("parse config.toml: %s must be table", key)
			}
			current = key
			if sections[current] == nil {
				sections[current] = map[string]simpleTOMLValue{}
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			var err error
			current, err = parseSimpleTOMLKey(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			if err != nil {
				return nil, fmt.Errorf("parse config.toml: %w at line %d", err, lineNumber+1)
			}
			if _, exists := sections[current]; exists {
				return nil, fmt.Errorf("parse config.toml: duplicate table %s at line %d", current, lineNumber+1)
			}
			sections[current] = map[string]simpleTOMLValue{}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("parse config.toml: expected key/value at line %d", lineNumber+1)
		}
		keyParts, err := parseSimpleTOMLKeyParts(strings.TrimSpace(key))
		if err != nil {
			return nil, fmt.Errorf("parse config.toml: %w at line %d", err, lineNumber+1)
		}
		key = strings.Join(keyParts, ".")
		value = strings.TrimSpace(value)
		if current == "" && key == "triggers" {
			inline, ok, err := parseInlineTable(value, lineNumber+1)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("parse config.toml: %s must be table", key)
			}
			if _, exists := sections[key]; exists {
				return nil, fmt.Errorf("parse config.toml: duplicate table %s at line %d", key, lineNumber+1)
			}
			sections[key] = inline
			continue
		}
		valueSection := current
		valueKey := key
		if current == "" && len(keyParts) == 2 && keyParts[0] == "triggers" {
			valueSection = keyParts[0]
			valueKey = keyParts[1]
		}
		quoted := isQuotedTOMLString(value)
		if quoted {
			var err error
			value, err = parseTOMLString(value)
			if err != nil {
				return nil, fmt.Errorf("parse config.toml: %w at line %d", err, lineNumber+1)
			}
		}
		if sections[valueSection] == nil {
			sections[valueSection] = map[string]simpleTOMLValue{}
		}
		if _, exists := sections[valueSection][valueKey]; exists {
			return nil, fmt.Errorf("parse config.toml: duplicate key %s at line %d", key, lineNumber+1)
		}
		sections[valueSection][valueKey] = simpleTOMLValue{Value: value, Quoted: quoted}
	}
	return sections, nil
}

func parseInlineTable(value string, lineNumber int) (map[string]simpleTOMLValue, bool, error) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, false, nil
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}"))
	if inner == "" {
		return map[string]simpleTOMLValue{}, true, nil
	}
	section := map[string]simpleTOMLValue{}
	fields := splitTOMLCommaFields(inner)
	for index, field := range fields {
		if strings.TrimSpace(field) == "" {
			if index != len(fields)-1 {
				return nil, false, fmt.Errorf("parse config.toml: expected inline key/value at line %d", lineNumber)
			}
			continue
		}
		key, fieldValue, ok := strings.Cut(field, "=")
		if !ok {
			return nil, false, fmt.Errorf("parse config.toml: expected inline key/value at line %d", lineNumber)
		}
		key, err := parseSimpleTOMLKey(strings.TrimSpace(key))
		if err != nil {
			return nil, false, fmt.Errorf("parse config.toml: %w at line %d", err, lineNumber)
		}
		fieldValue = strings.TrimSpace(fieldValue)
		quoted := isQuotedTOMLString(fieldValue)
		if quoted {
			var err error
			fieldValue, err = parseTOMLString(fieldValue)
			if err != nil {
				return nil, false, fmt.Errorf("parse config.toml: %w at line %d", err, lineNumber)
			}
		}
		if _, exists := section[key]; exists {
			return nil, false, fmt.Errorf("parse config.toml: duplicate key %s at line %d", key, lineNumber)
		}
		section[key] = simpleTOMLValue{Value: fieldValue, Quoted: quoted}
	}
	return section, true, nil
}

func stripComment(value string) string {
	var quote rune
	escaped := false
	for index, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' || r == '\'' {
			if quote == 0 {
				quote = r
			} else if quote == r {
				quote = 0
			}
		}
		if r == '#' && quote == 0 {
			return value[:index]
		}
	}
	return value
}

func splitTOMLCommaFields(value string) []string {
	var fields []string
	start := 0
	var quote rune
	escaped := false
	for index, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' || r == '\'' {
			if quote == 0 {
				quote = r
			} else if quote == r {
				quote = 0
			}
		}
		if r == ',' && quote == 0 {
			fields = append(fields, value[start:index])
			start = index + 1
		}
	}
	fields = append(fields, value[start:])
	return fields
}

func isQuotedTOMLString(value string) bool {
	if len(value) >= 6 && ((strings.HasPrefix(value, `"""`) && strings.HasSuffix(value, `"""`)) || (strings.HasPrefix(value, `'''`) && strings.HasSuffix(value, `'''`))) {
		return true
	}
	return len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0]
}

func parseTOMLString(value string) (string, error) {
	if !isQuotedTOMLString(value) {
		return "", fmt.Errorf("expected TOML string")
	}
	if strings.HasPrefix(value, `"""`) && strings.HasSuffix(value, `"""`) && len(value) >= 6 {
		return unescapeTOMLBasicString(value[3 : len(value)-3])
	}
	if strings.HasPrefix(value, `'''`) && strings.HasSuffix(value, `'''`) && len(value) >= 6 {
		return value[3 : len(value)-3], nil
	}
	inner := value[1 : len(value)-1]
	if value[0] == '\'' {
		return inner, nil
	}
	return unescapeTOMLBasicString(inner)
}

func unescapeTOMLBasicString(inner string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(inner); {
		char, size := utf8.DecodeRuneInString(inner[index:])
		if char != '\\' {
			builder.WriteRune(char)
			index += size
			continue
		}
		index += size
		if index >= len(inner) {
			return "", fmt.Errorf("unterminated TOML string escape")
		}
		escape, escapeSize := utf8.DecodeRuneInString(inner[index:])
		switch escape {
		case '"':
			builder.WriteByte('"')
		case '\\':
			builder.WriteByte('\\')
		case 'b':
			builder.WriteByte('\b')
		case 't':
			builder.WriteByte('\t')
		case 'n':
			builder.WriteByte('\n')
		case 'f':
			builder.WriteByte('\f')
		case 'r':
			builder.WriteByte('\r')
		case 'u':
			r, err := parseTOMLUnicodeEscape(inner[index+escapeSize:], 4)
			if err != nil {
				return "", err
			}
			builder.WriteRune(r)
			index += escapeSize + 4
			continue
		case 'U':
			r, err := parseTOMLUnicodeEscape(inner[index+escapeSize:], 8)
			if err != nil {
				return "", err
			}
			builder.WriteRune(r)
			index += escapeSize + 8
			continue
		default:
			return "", fmt.Errorf("invalid TOML string escape")
		}
		index += escapeSize
	}
	return builder.String(), nil
}

func parseTOMLUnicodeEscape(value string, digits int) (rune, error) {
	if len(value) < digits {
		return 0, fmt.Errorf("invalid TOML unicode escape")
	}
	parsed, err := strconv.ParseUint(value[:digits], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid TOML unicode escape")
	}
	r := rune(parsed)
	if !utf8.ValidRune(r) {
		return 0, fmt.Errorf("invalid TOML unicode escape")
	}
	return r, nil
}

func parseSimpleTOMLKey(value string) (string, error) {
	parts, err := parseSimpleTOMLKeyParts(value)
	if err != nil {
		return "", err
	}
	return strings.Join(parts, "."), nil
}

func parseSimpleTOMLKeyParts(value string) ([]string, error) {
	if value == "" {
		return nil, fmt.Errorf("invalid TOML key")
	}
	parts, err := splitTOMLDottedKey(value)
	if err != nil {
		return nil, err
	}
	for index, part := range parts {
		parsed, err := parseSimpleTOMLKeyPart(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		parts[index] = parsed
	}
	return parts, nil
}

func splitTOMLDottedKey(value string) ([]string, error) {
	var parts []string
	var builder strings.Builder
	var quote rune
	escaped := false
	for _, char := range value {
		if escaped {
			escaped = false
			builder.WriteRune(char)
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			builder.WriteRune(char)
			continue
		}
		if char == '"' || char == '\'' {
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			}
		}
		if char == '.' && quote == 0 {
			parts = append(parts, builder.String())
			builder.Reset()
			continue
		}
		builder.WriteRune(char)
	}
	if quote != 0 {
		return nil, fmt.Errorf("invalid TOML key")
	}
	parts = append(parts, builder.String())
	return parts, nil
}

func parseSimpleTOMLKeyPart(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("invalid TOML key")
	}
	if value[0] == '"' || value[0] == '\'' {
		if !isQuotedTOMLString(value) {
			return "", fmt.Errorf("invalid TOML key")
		}
		return parseTOMLString(value)
	}
	if isQuotedTOMLString(value) {
		return parseTOMLString(value)
	}
	if !validTOMLBareKey(value) {
		return "", fmt.Errorf("invalid TOML key")
	}
	return value, nil
}

func validTOMLBareKey(value string) bool {
	for _, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeTOMLUint(value string) (string, int, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "+") {
		value = strings.TrimPrefix(value, "+")
	}
	if value == "" || value[0] == '_' || value[len(value)-1] == '_' || strings.Contains(value, "__") {
		return "", 10, false
	}
	base := 10
	if len(value) > 2 && value[0] == '0' {
		switch value[1] {
		case 'x', 'X':
			base = 16
			value = value[2:]
		case 'o', 'O':
			base = 8
			value = value[2:]
		case 'b', 'B':
			base = 2
			value = value[2:]
		default:
			return "", 10, false
		}
		if value == "" || value[0] == '_' {
			return "", 10, false
		}
	} else if len(value) > 1 && value[0] == '0' {
		return "", 10, false
	}
	for _, r := range value {
		if r == '_' {
			continue
		}
		if (base == 10 && (r < '0' || r > '9')) || (base == 8 && (r < '0' || r > '7')) || (base == 2 && (r < '0' || r > '1')) || (base == 16 && !isTOMLHexDigit(r)) {
			return "", 10, false
		}
	}
	return strings.ReplaceAll(value, "_", ""), base, true
}

func isTOMLHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
