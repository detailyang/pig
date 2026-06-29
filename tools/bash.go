package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type BashTool struct {
	Timeout time.Duration
	Env     ExecutionEnv
}

func (BashTool) Name() string { return "bash" }
func (BashTool) Description() string {
	return "Run a shell command via `sh -c`. Returns stdout+stderr (tail-truncated to 2000 lines / 256 KiB) and exit code. Optional `timeout` in seconds. Timeouts and cancellations kill the child process; stdout and stderr are drained concurrently so high-output commands do not deadlock the tool."
}
func (BashTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string", "description": "Shell command to execute"}, "timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (optional). On timeout the child is killed and any output captured so far is returned."}}, "required": []string{"command"}}
}

func (tool BashTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	command, err := requiredStringArg(call, "command")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(command) {
		return agent.ToolResult{}, fmt.Errorf("command must be valid UTF-8")
	}
	timeout := tool.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timeoutDisplaySeconds := max(1, int(timeout/time.Second))
	if timeoutSeconds, ok := uintArgPresent(call, "timeout"); ok {
		timeout = time.Duration(timeoutSeconds) * time.Second
		timeoutDisplaySeconds = timeoutSeconds
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell, flag := shellCommand()
	cmd := exec.Command(shell, flag, command)
	if tool.Env != nil {
		cmd.Dir = tool.Env.CWD()
	}
	configureCommandProcessGroup(cmd)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Start()
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("spawn: %w", err)
	}
	if err == nil {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err = <-done:
		case <-runCtx.Done():
			killCommandProcessGroup(cmd)
			err = <-done
			if err == nil {
				err = runCtx.Err()
			}
		}
	}
	timedOut := runCtx.Err() == context.DeadlineExceeded
	cancelled := !timedOut && ctx.Err() != nil
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if timedOut || errors.Is(runCtx.Err(), context.Canceled) {
			exitCode = -1
		}
	}
	out, outTruncation := truncateTextTail(validUTF8OrEmpty(stdout.String()), 2000, 256*1024)
	errText := validUTF8OrEmpty(stderr.String())
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("$ %s\n", command))
	if note := outTruncation.note(); note != "" {
		builder.WriteString(note)
		builder.WriteByte('\n')
	}
	builder.WriteString(out)
	if out != "" && !strings.HasSuffix(out, "\n") {
		builder.WriteString("\n")
	}
	stderrText := errText
	if cancelled {
		if stderrText != "" && !strings.HasSuffix(stderrText, "\n") {
			stderrText += "\n"
		}
		stderrText += "[aborted]"
	}
	if timedOut {
		if stderrText != "" && !strings.HasSuffix(stderrText, "\n") {
			stderrText += "\n"
		}
		stderrText += fmt.Sprintf("[timed out after %ds]", timeoutDisplaySeconds)
	}
	stderrText, _ = truncateTextTail(stderrText, 2000, 256*1024)
	if stderrText != "" {
		builder.WriteString("[stderr]\n")
		builder.WriteString(stderrText)
		if !strings.HasSuffix(stderrText, "\n") {
			builder.WriteString("\n")
		}
	}
	builder.WriteString(fmt.Sprintf("[exit %d]", exitCode))
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: builder.String(), Details: map[string]any{"command": command, "exitCode": exitCode, "isError": exitCode != 0}}, nil
}

func validUTF8OrEmpty(text string) string {
	if !utf8.ValidString(text) {
		return ""
	}
	return text
}

func uintArgPresent(call ai.ToolCall, key string) (int, bool) {
	value, ok := call.Arguments[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		if typed >= 0 {
			return typed, true
		}
	case json.Number:
		if parsed, ok := parseJSONNumberInt(typed); ok && parsed >= 0 {
			return parsed, true
		}
	}
	return 0, false
}

func shellCommand() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "/bin/sh", "-c"
}
