package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

func ParsePartialJSON(raw string) (any, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return nil, nil
	}
	var value any
	if err := unmarshalPartialJSONValue(candidate, &value); err == nil {
		return value, nil
	}
	closed := closePartialJSON(candidate)
	if err := unmarshalPartialJSONValue(closed, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func ParsePartialJson(raw string) (any, error) { return ParsePartialJSON(raw) }

func unmarshalPartialJSONValue(raw string, value *any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return &json.SyntaxError{}
		}
		return err
	}
	return nil
}

func parsePartialJSONObject(raw string) (map[string]any, bool) {
	value, err := parsePartialJSONPrefix(raw)
	if err != nil {
		return nil, false
	}
	args, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return args, true
}

func parsePartialJSONPrefix(raw string) (any, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return nil, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader([]byte(candidate)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err == nil {
		return value, nil
	}
	closed := closePartialJSON(candidate)
	decoder = json.NewDecoder(bytes.NewReader([]byte(closed)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func closePartialJSON(input string) string {
	stack := make([]rune, 0, 4)
	inString := false
	escape := false
	var out strings.Builder
	out.Grow(len(input) + 4)

	for _, char := range input {
		out.WriteRune(char)
		if escape {
			escape = false
			continue
		}
		if inString {
			switch char {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		var close rune
		switch char {
		case '"':
			inString = true
		case '{':
			close = '}'
		case '[':
			close = ']'
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		if close != 0 {
			stack = append(stack, close)
		}
	}

	if inString {
		out.WriteRune('"')
	}
	closed := strings.TrimRight(out.String(), " \n\r\t")
	closed = strings.TrimRight(closed, ",")
	for index := len(stack) - 1; index >= 0; index-- {
		closed += string(stack[index])
	}
	return closed
}
