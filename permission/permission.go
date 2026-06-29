package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/detailyang/pig/agent"
)

type Decision struct {
	Allow  bool
	Reason string
}

type PermissionDecision = Decision

func (decision Decision) Allowed() bool { return decision.Allow }

func Allow() Decision             { return Decision{Allow: true} }
func Deny(reason string) Decision { return Decision{Reason: reason} }

type Category string

type PermissionCategory = Category

const (
	Tool              Category = "tool"
	ControlPlaneWrite Category = "control_plane_write"
)

type Policy struct {
	bashToolNames  map[string]bool
	dangerPatterns []dangerPattern
}

type PermissionPolicy = Policy

type dangerPattern struct {
	label string
	re    *regexp.Regexp
}

func DefaultForCodingAgent() Policy {
	return newPolicyFromOrderedPatterns([]string{"bash"}, defaultDangerPatterns())
}

func DefaultPermissionPolicyForCodingAgent() PermissionPolicy {
	return DefaultForCodingAgent()
}

func NewPolicy(bashToolNames []string, patterns map[string]string) Policy {
	ordered := make([]dangerPatternSpec, 0, len(patterns))
	labels := make([]string, 0, len(patterns))
	for label := range patterns {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		ordered = append(ordered, dangerPatternSpec{label: label, pattern: patterns[label]})
	}
	return newPolicyFromOrderedPatterns(bashToolNames, ordered)
}

func newPolicyFromOrderedPatterns(bashToolNames []string, patterns []dangerPatternSpec) Policy {
	names := map[string]bool{}
	for _, name := range bashToolNames {
		names[name] = true
	}
	compiled := make([]dangerPattern, 0, len(patterns))
	for _, pattern := range patterns {
		compiled = append(compiled, dangerPattern{label: pattern.label, re: regexp.MustCompile(pattern.pattern)})
	}
	return Policy{bashToolNames: names, dangerPatterns: compiled}
}

func NewPermissionPolicy(bashToolNames []string, patterns map[string]string) PermissionPolicy {
	return NewPolicy(bashToolNames, patterns)
}

func (policy Policy) Evaluate(toolName string, args any) Decision {
	return policy.EvaluateWithCategory(Tool, toolName, args)
}

func (policy Policy) EvaluateWithCategory(category Category, toolName string, args any) Decision {
	if category == ControlPlaneWrite {
		return Allow()
	}
	if !policy.bashToolNames[toolName] {
		return Allow()
	}
	command := ExtractShellCommand(args)
	if strings.TrimSpace(command) == "" {
		return Allow()
	}
	for _, rule := range []struct {
		label string
		check func(string) bool
	}{{"rm recursive+force on absolute path", rmRecursiveForceOnAbsoluteTarget}, {"rm recursive+force on $HOME or ~", rmRecursiveForceOnHomeTarget}} {
		if rule.check(command) {
			return Deny(fmt.Sprintf("denied by permission policy: %s", rule.label))
		}
	}
	for _, pattern := range policy.dangerPatterns {
		if pattern.re.MatchString(command) {
			return Deny(fmt.Sprintf("denied by permission policy: %s", pattern.label))
		}
	}
	return Allow()
}

func (policy Policy) AsBeforeToolCall() agent.BeforeToolCallHook {
	return func(ctx context.Context, call agent.BeforeToolCallContext) (agent.BeforeToolCallResult, error) {
		decision := policy.Evaluate(call.Call.Name, call.Args)
		if decision.Allowed() {
			return agent.BeforeToolCallResult{}, nil
		}
		return agent.BeforeToolCallResult{Block: true, Reason: decision.Reason}, nil
	}
}

func ExtractShellCommand(args any) string {
	switch typed := args.(type) {
	case string:
		if command := extractShellCommandFromJSONString(typed); command != "" {
			return command
		}
		return typed
	case map[string]any:
		return extractShellCommandFromMap(typed)
	case map[string]json.RawMessage:
		object := make(map[string]any, len(typed))
		for key, raw := range typed {
			var value any
			if err := json.Unmarshal(raw, &value); err == nil {
				object[key] = value
			}
		}
		return extractShellCommandFromMap(object)
	case map[string]string:
		for _, key := range []string{"command", "cmd", "bash", "script"} {
			if value := typed[key]; strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

func extractShellCommandFromJSONString(text string) string {
	if !strings.HasPrefix(strings.TrimSpace(text), "{") {
		return ""
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		return ""
	}
	return extractShellCommandFromMap(object)
}

func extractShellCommandFromMap(object map[string]any) string {
	for _, key := range []string{"command", "cmd", "bash", "script"} {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type dangerPatternSpec struct {
	label   string
	pattern string
}

func defaultDangerPatterns() []dangerPatternSpec {
	return []dangerPatternSpec{
		{label: "sudo invocation", pattern: `\bsudo\b`},
		{label: "curl/wget piped into shell", pattern: `\b(curl|wget)\b[^|]*\|\s*(bash|sh|zsh|fish)\b`},
		{label: "dd writing to a block device", pattern: `\bdd\b[^\n]*\bof=/dev/(disk|sd[a-z]|nvme|hd[a-z])`},
		{label: "mkfs / format command", pattern: `\bmkfs(\.|\s)`},
		{label: "chmod 777 on absolute path", pattern: `\bchmod\b\s+777\s+/`},
		{label: "shutdown / reboot / halt", pattern: `\b(shutdown|reboot|halt|poweroff)\b`},
		{label: "git push --force on main/master", pattern: `\bgit\s+push\s+(--force|-f)\b[^\n]*\b(main|master)\b`},
		{label: "piping into eval", pattern: `\|\s*eval\b`},
		{label: ":(){:|:&};: forkbomb", pattern: `:\(\)\s*\{\s*:\|:&\s*\}\s*;\s*:`},
	}
}

func rmRecursiveForceOnAbsoluteTarget(command string) bool {
	return rmDangerousWith(command, func(operand string) bool { return strings.HasPrefix(operand, "/") })
}

func rmRecursiveForceOnHomeTarget(command string) bool {
	return rmDangerousWith(command, func(operand string) bool {
		return operand == "~" || strings.HasPrefix(operand, "~/") || operand == "$HOME" || strings.HasPrefix(operand, "$HOME/")
	})
}

func rmDangerousWith(command string, targetMatches func(string) bool) bool {
	for _, segment := range splitShellSegments(command) {
		tokens := shellFields(segment)
		if len(tokens) == 0 {
			continue
		}
		for index, token := range tokens[:1] {
			if shellProgramName(token) != "rm" {
				continue
			}
			recursive := false
			force := false
			operands := []string{}
			for _, arg := range tokens[index+1:] {
				if strings.HasPrefix(arg, "-") {
					recursive = recursive || hasRecursiveFlag(arg)
					force = force || hasForceFlag(arg)
					continue
				}
				operands = append(operands, normalizeOperand(arg))
			}
			if !(recursive && force) {
				continue
			}
			for _, operand := range operands {
				if targetMatches(operand) {
					return true
				}
			}
		}
	}
	return false
}

func shellProgramName(token string) string {
	parts := strings.Split(token, "/")
	return parts[len(parts)-1]
}

func hasRecursiveFlag(flag string) bool {
	return flag == "--recursive" || strings.Contains(strings.TrimLeft(flag, "-"), "r") || strings.Contains(strings.TrimLeft(flag, "-"), "R")
}

func hasForceFlag(flag string) bool {
	return flag == "--force" || strings.Contains(strings.TrimLeft(flag, "-"), "f")
}

func normalizeOperand(arg string) string {
	arg = strings.TrimSpace(arg)
	arg = strings.Trim(arg, `"'`)
	arg = strings.ReplaceAll(arg, "${HOME}", "$HOME")
	return arg
}

func splitShellSegments(command string) []string {
	replacer := strings.NewReplacer("&&", ";", "||", ";", "|", ";")
	return strings.Split(replacer.Replace(command), ";")
}

func shellFields(segment string) []string {
	fields := strings.Fields(segment)
	for index := range fields {
		fields[index] = strings.Trim(fields[index], `"'`)
	}
	return fields
}
