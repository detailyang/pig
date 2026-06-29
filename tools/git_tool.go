package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

const maxGitOutputBytes = 64 * 1024

type GitTool struct{}

func (GitTool) Name() string { return "git" }
func (GitTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (GitTool) Description() string {
	return "Run a read-only git subcommand (status / diff / log) with sensible defaults and structured output. Write/network operations go through bash so the permission policy can intercept them."
}
func (GitTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"subcommand": map[string]any{"type": "string", "enum": []string{"status", "diff", "log"}, "description": "Which git subcommand to run."}, "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Extra arguments appended after the defaults (e.g. a file path or revision)."}, "cwd": map[string]any{"type": "string", "description": "Optional cwd for the git invocation. Defaults to the agent's cwd."}}, "required": []string{"subcommand"}, "additionalProperties": false}
}
func (GitTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	subcommand, err := gitSubcommandArg(call)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(subcommand) {
		return agent.ToolResult{}, fmt.Errorf("subcommand must be valid UTF-8")
	}
	if subcommand != "status" && subcommand != "diff" && subcommand != "log" {
		return agent.ToolResult{}, fmt.Errorf("unsupported git subcommand: %s (allowed: status, diff, log)", subcommand)
	}
	extra, err := gitStringSliceArg(call, "args")
	if err != nil {
		return agent.ToolResult{}, err
	}
	argv := buildGitArgv(subcommand, extra)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cwd := "."
	cwdSet := false
	if value, ok := call.Arguments["cwd"].(string); ok {
		if !utf8.ValidString(value) {
			return agent.ToolResult{}, fmt.Errorf("cwd must be valid UTF-8")
		}
		cwd = value
		cwdSet = true
	}
	if cwdSet {
		if cwd == "" {
			return agent.ToolResult{}, fmt.Errorf("spawn git: No such file or directory (os error 2)")
		}
		cmd.Dir = cwd
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if ctx.Err() != nil {
			return agent.ToolResult{}, fmt.Errorf("cancelled")
		}
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return agent.ToolResult{}, fmt.Errorf("spawn git: %w", err)
		}
	}
	body := strings.ToValidUTF8(stdout.String(), "�")
	if err != nil {
		body = fmt.Sprintf("git %s exited with status %d\n--- stderr ---\n%s", subcommand, exitCode, strings.TrimSpace(strings.ToValidUTF8(stderr.String(), "�")))
	}
	body, truncated := truncateBytes(body, maxGitOutputBytes)
	suffix := ""
	if truncated {
		suffix = fmt.Sprintf("\n\n(truncated at %d KiB)", maxGitOutputBytes/1024)
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("git %s (cwd=%s)\n%s%s", subcommand, cwd, body, suffix), Details: map[string]any{"subcommand": subcommand, "exit_status": exitCode, "argv": argv, "truncated": truncated}}, nil
}

func gitSubcommandArg(call ai.ToolCall) (string, error) {
	value, ok := call.Arguments["subcommand"]
	if !ok {
		return "", fmt.Errorf("missing required arg: subcommand")
	}
	subcommand, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("missing required arg: subcommand")
	}
	return subcommand, nil
}

func buildGitArgv(subcommand string, extra []string) []string {
	argv := []string{subcommand}
	switch subcommand {
	case "status":
		argv = append(argv, "--short", "--branch")
	case "diff":
		argv = append(argv, "--no-color", "--no-ext-diff")
	case "log":
		argv = append(argv, "--no-color", "-n", "20", "--pretty=format:%h %ci %an %s")
	}
	return append(argv, extra...)
}

func stringSliceArg(call ai.ToolCall, key string) []string {
	values, _ := gitStringSliceArg(call, key)
	return values
}

func gitStringSliceArg(call ai.ToolCall, key string) ([]string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		out := append([]string(nil), typed...)
		for index, text := range out {
			if !utf8.ValidString(text) {
				return nil, fmt.Errorf("%s[%d] must be valid UTF-8", key, index)
			}
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(typed))
		for index, item := range typed {
			if text, ok := item.(string); ok {
				if !utf8.ValidString(text) {
					return nil, fmt.Errorf("%s[%d] must be valid UTF-8", key, index)
				}
				out = append(out, text)
			}
		}
		return out, nil
	default:
		return nil, nil
	}
}

func truncateBytes(text string, maxBytes int) (string, bool) {
	if len(text) <= maxBytes {
		return text, false
	}
	end := maxBytes
	for end > 0 && !utf8ValidPrefix(text[:end]) {
		end--
	}
	return text[:end], true
}

func utf8ValidPrefix(text string) bool {
	return len(text) == 0 || strings.ToValidUTF8(text, "") == text
}
