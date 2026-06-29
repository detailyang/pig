package triggers

import (
	"fmt"

	"github.com/detailyang/pig/ai"
)

func stringArg(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing required arg: %s", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return text, nil
}

func boolArg(call ai.ToolCall, key string) (bool, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return false, fmt.Errorf("missing required arg: %s", key)
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return typed, nil
}

func optionalStringArg(call ai.ToolCall, key string, fallback string) string {
	value, ok := call.Arguments[key]
	if !ok || value == nil {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}

func boolArgDefault(call ai.ToolCall, key string, fallback bool) bool {
	value, ok := call.Arguments[key]
	if !ok || value == nil {
		return fallback
	}
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}
