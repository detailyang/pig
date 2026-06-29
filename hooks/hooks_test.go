package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRuleMatchingAndPayload(t *testing.T) {
	rule := Rule{Event: ToolEnd, Tool: "bash", Command: "echo hook"}
	if !rule.Matches(EventData{Event: ToolEnd, ToolName: "bash"}) {
		t.Fatal("expected rule match")
	}
	if rule.Matches(EventData{Event: ToolEnd, ToolName: "read"}) || rule.Matches(EventData{Event: ToolStart, ToolName: "bash"}) {
		t.Fatal("unexpected rule match")
	}
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: "/tmp/project", ModelProvider: "openai", ModelID: "m", ThinkingLevel: "off", Rules: []Rule{rule}})
	payload := runner.PayloadFor(rule, EventData{Event: ToolEnd, ToolName: "bash", ToolCallID: "c1", ToolResultSummary: "ok"})
	if payload.Event != "tool_end" || payload.SessionID != "s1" || payload.ToolName != "bash" || payload.ToolCallID != "c1" {
		t.Fatalf("payload mismatch: %#v", payload)
	}
}

func TestRuleMatchesPresentEmptyToolLikeUpstream(t *testing.T) {
	rule := Rule{Event: ToolEnd, Tool: "", ToolPresent: true}
	if !rule.Matches(EventData{Event: ToolEnd, ToolName: ""}) {
		t.Fatal("present empty tool should match empty tool_name like upstream Some(\"\")")
	}
	if rule.Matches(EventData{Event: ToolEnd, ToolName: "bash"}) {
		t.Fatal("present empty tool should not behave like missing tool filter")
	}
}

func TestRunnerExecutesMatchingCommand(t *testing.T) {
	var ran []Payload
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "echo hook"}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		ran = append(ran, payload)
		return nil
	}})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0].Event != "agent_start" {
		t.Fatalf("runner mismatch: %#v", ran)
	}
}

func TestRunnerHandleDataSkipsRulesWhenContextAlreadyCancelledLikeUpstream(t *testing.T) {
	called := false
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "echo should-not-run"}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		called = true
		return nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.HandleData(ctx, EventData{Event: AgentStart}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("cancelled hook runner should skip matching rules like upstream")
	}
}

func TestRunnerHandleDataStopsBeforeNextRuleAfterCancellationLikeUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := 0
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "first"}, {Event: AgentStart, Command: "second"}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		called++
		cancel()
		return nil
	}})
	if err := runner.HandleData(ctx, EventData{Event: AgentStart}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("cancelled hook runner should stop before next matching rule like upstream, called=%d", called)
	}
}

func TestRunnerCommandExecutor(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "printf hook", Timeout: time.Second}}})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatal(err)
	}
}

func TestParseHooksFileParsesRulesAndSkipsBadEntriesLikeUpstream(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
allow_project_hooks = true

[[hook]]
event = "tool_end"
command = "echo ok"
tool = "bash"
timeout_ms = 2500
cwd = "home"
on_failure = "ignore"

[[hook]]
event = "agent_start"
command = "echo disabled"
enabled = false

[[hook]]
event = "compaction"
webhook = "https://example.test/hook"
headers = { "X-Test" = "yes", Authorization = "Bearer token" }

[[hook]]
event = "not_real"
command = "echo nope"

[[hook]]
event = "turn_start"
command = "   "
`, "test")
	if len(rules) != 2 {
		t.Fatalf("expected 2 valid rules, got %#v diagnostics=%#v", rules, diagnostics)
	}
	if rules[0].Event != ToolEnd || rules[0].Command != "echo ok" || rules[0].Tool != "bash" || rules[0].Timeout != 2500*time.Millisecond || rules[0].CWD != CWDHome || rules[0].OnFailure != OnFailureIgnore || rules[0].Source != "test" {
		t.Fatalf("first hook rule mismatch: %#v", rules[0])
	}
	if rules[1].Event != Compaction || rules[1].Webhook != "https://example.test/hook" || rules[1].Headers["X-Test"] != "yes" || rules[1].Headers["Authorization"] != "Bearer token" || rules[1].Timeout != 5*time.Second || rules[1].CWD != CWDProject || rules[1].OnFailure != OnFailureWarn {
		t.Fatalf("second hook rule mismatch: %#v", rules[1])
	}
	if len(diagnostics) != 2 || !strings.Contains(diagnostics[0], `hook #4 has unknown event "not_real"`) || !strings.Contains(diagnostics[1], "hook #5 has neither command nor webhook") {
		t.Fatalf("diagnostics mismatch: %#v", diagnostics)
	}
}

func TestLoadReadsUserHooksAndGatesProjectHooksLikeUpstream(t *testing.T) {
	pieDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("PIE_DIR", pieDir)
	if err := os.WriteFile(filepath.Join(pieDir, "hooks.toml"), []byte(`
[[hook]]
event = "agent_start"
command = "echo user"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectDir, ".pie"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".pie", "hooks.toml"), []byte(`
[[hook]]
event = "agent_end"
command = "echo project"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := Load(projectDir, RunnerOptions{SessionID: "s1"})
	if loaded.Runner.Len() != 1 || len(loaded.Diagnostics) != 1 || !strings.Contains(loaded.Diagnostics[0], "project hooks ignored") {
		t.Fatalf("project hooks should be ignored by default like upstream, got len=%d diagnostics=%#v", loaded.Runner.Len(), loaded.Diagnostics)
	}

	if err := os.WriteFile(filepath.Join(pieDir, "hooks.toml"), []byte(`
allow_project_hooks = true

[[hook]]
event = "agent_start"
command = "echo user"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded = Load(projectDir, RunnerOptions{SessionID: "s1"})
	if loaded.Runner.Len() != 2 || len(loaded.Diagnostics) != 0 {
		t.Fatalf("allowed project hooks should be loaded like upstream, got len=%d diagnostics=%#v", loaded.Runner.Len(), loaded.Diagnostics)
	}
}

func TestLoadRejectsStringAllowProjectHooksLikeUpstreamSerde(t *testing.T) {
	pieDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("PIE_DIR", pieDir)
	if err := os.WriteFile(filepath.Join(pieDir, "hooks.toml"), []byte(`
allow_project_hooks = "true"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectDir, ".pie"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".pie", "hooks.toml"), []byte(`
[[hook]]
event = "agent_start"
command = "echo project"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := Load(projectDir, RunnerOptions{SessionID: "s1"})
	if loaded.Runner.Len() != 0 || len(loaded.Diagnostics) < 2 || !strings.Contains(loaded.Diagnostics[0], "parse") || !strings.Contains(loaded.Diagnostics[1], "project hooks ignored") {
		t.Fatalf("string allow_project_hooks should fail user parse and not allow project hooks like upstream, len=%d diagnostics=%#v", loaded.Runner.Len(), loaded.Diagnostics)
	}
}

func TestLoadRejectsInvalidUTF8HooksFileLikeUpstreamReadToString(t *testing.T) {
	pieDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("PIE_DIR", pieDir)
	if err := os.WriteFile(filepath.Join(pieDir, "hooks.toml"), []byte("[[hook]]\nevent = \"agent_start\"\ncommand = \"echo user\"\n# \xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := Load(projectDir, RunnerOptions{SessionID: "s1"})
	if loaded.Runner.Len() != 0 || len(loaded.Diagnostics) == 0 || !strings.Contains(loaded.Diagnostics[0], "read") || !strings.Contains(loaded.Diagnostics[0], "invalid UTF-8") {
		t.Fatalf("invalid UTF-8 hooks.toml should fail read and skip rules like upstream, len=%d diagnostics=%#v", loaded.Runner.Len(), loaded.Diagnostics)
	}
}

func TestParseHooksFileRejectsUnknownEnumsLikeUpstreamSerde(t *testing.T) {
	cases := map[string]string{
		"cwd":        "cwd = \"workspace\"",
		"on_failure": "on_failure = \"fail\"",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
command = "echo ok"
`+line+`
`, "test")
			if len(rules) != 0 || len(diagnostics) == 0 || !strings.Contains(diagnostics[0], "invalid "+name) {
				t.Fatalf("unknown %s should reject hook like upstream serde, rules=%#v diagnostics=%#v", name, rules, diagnostics)
			}
		})
	}
}

func TestParseHooksFileRejectsInvalidBoolAndUintLikeUpstreamSerde(t *testing.T) {
	cases := map[string]string{
		"enabled":    "enabled = \"false\"",
		"timeout_ms": "timeout_ms = \"5000\"",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
command = "echo ok"
`+line+`
`, "test")
			if len(rules) != 0 || len(diagnostics) == 0 || !strings.Contains(diagnostics[0], "invalid "+name) {
				t.Fatalf("invalid %s type should reject hook like upstream serde, rules=%#v diagnostics=%#v", name, rules, diagnostics)
			}
		})
	}
}

func TestParseHooksFileRejectsInvalidStringFieldsLikeUpstreamSerde(t *testing.T) {
	cases := map[string]string{
		"event":      "event = agent_start\ncommand = \"echo ok\"",
		"command":    "command = [\"echo ok\"]",
		"webhook":    "webhook = [\"https://example.test/hook\"]",
		"tool":       "command = \"echo ok\"\ntool = [\"bash\"]",
		"cwd":        "command = \"echo ok\"\ncwd = home",
		"on_failure": "command = \"echo ok\"\non_failure = ignore",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			prefix := "[[hook]]\nevent = \"agent_start\"\n"
			if name == "event" {
				prefix = "[[hook]]\n"
			}
			rules, diagnostics := ParseHooksFile(prefix+line+"\n", "test")
			if len(rules) != 0 || len(diagnostics) == 0 || !strings.Contains(diagnostics[0], "invalid "+name) {
				t.Fatalf("invalid %s type should reject hook like upstream serde, rules=%#v diagnostics=%#v", name, rules, diagnostics)
			}
		})
	}
}

func TestParseHooksFileAcceptsSingleQuotedStringsLikeUpstreamSerde(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = 'tool_end'
command = 'echo ok'
webhook = 'https://example.test/hook'
tool = 'bash'
cwd = 'home'
on_failure = 'ignore'
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("single quoted hook config should parse like upstream serde, rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	if rules[0].Event != ToolEnd || rules[0].Command != "echo ok" || rules[0].Webhook != "https://example.test/hook" || rules[0].Tool != "bash" || rules[0].CWD != CWDHome || rules[0].OnFailure != OnFailureIgnore {
		t.Fatalf("single quoted hook rule mismatch: %#v", rules[0])
	}
}

func TestParseHooksFilePreservesPresentEmptyWebhookLikeUpstream(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
webhook = ""
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("present empty webhook should count as Some(\"\") like upstream, rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	if !rules[0].WebhookPresent || rules[0].Webhook != "" {
		t.Fatalf("empty webhook presence mismatch: %#v", rules[0])
	}
}

func TestParseHooksFilePreservesPresentEmptyToolLikeUpstream(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "tool_end"
command = "echo ok"
tool = ""
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("present empty tool should parse like upstream Some(\"\"), rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	if !rules[0].ToolPresent || rules[0].Tool != "" {
		t.Fatalf("empty tool presence mismatch: %#v", rules[0])
	}
}

func TestParseHooksFileRejectsNonStringHeadersLikeUpstreamSerde(t *testing.T) {
	cases := map[string]string{
		"inline": `headers = { "X-Test" = 123 }`,
		"nested": `[hook.headers]
"X-Test" = 123`,
		"dotted": `headers."X-Test" = 123`,
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
webhook = "https://example.test/hook"
`+headers+`
`, "test")
			if len(rules) != 0 || len(diagnostics) == 0 || !strings.Contains(diagnostics[0], "invalid headers") {
				t.Fatalf("non-string headers should reject hook like upstream serde, rules=%#v diagnostics=%#v", rules, diagnostics)
			}
		})
	}
}

func TestParseHooksFileInlineHeadersAllowCommasInsideSingleQuotedStringsLikeUpstream(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
webhook = "https://example.test/hook"
headers = { 'X-Test' = 'a,b' }
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("single quoted inline header with comma should parse like upstream, rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	if rules[0].Headers["X-Test"] != "a,b" {
		t.Fatalf("header comma value mismatch: %#v", rules[0].Headers)
	}
}

func TestPayloadSerializesSourceEvenWhenEmptyLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart}}})
	payload := runner.PayloadFor(Rule{Event: AgentStart}, EventData{Event: AgentStart})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"source":""`) {
		t.Fatalf("source should serialize as an empty string like upstream Some(rule.source), got %s", data)
	}
}

func TestPayloadMarshalDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	payload := Payload{Event: "tool_start", SessionID: "s1", CWD: "/tmp/a < b && c > d", ToolArgs: map[string]any{"command": "echo '<tag>&value'"}}
	data, err := MarshalPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("hook payload should not HTML-escape like serde_json, got %s", text)
	}
	if !strings.Contains(text, `"cwd":"/tmp/a < b && c > d"`) || !strings.Contains(text, `"command":"echo '<tag>&value'"`) {
		t.Fatalf("hook payload should preserve literal strings, got %s", text)
	}
}

func TestPayloadSerializesMissingOptionFieldsAsNullLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart}}})
	payload := runner.PayloadFor(Rule{Event: AgentStart}, EventData{Event: AgentStart})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"message_kind", "tool_name", "tool_is_error", "compaction_summary"} {
		if !strings.Contains(string(data), `"`+field+`":null`) {
			t.Fatalf("%s should serialize as null like upstream Option field, got %s", field, data)
		}
	}
}

func TestPayloadSerializesPresentEmptyOptionStringLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: MessageEnd}}})
	payload := runner.PayloadFor(Rule{Event: MessageEnd}, EventData{Event: MessageEnd, MessageKind: "assistant", MessageSummary: "", MessageSummaryPresent: true})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"message_summary":""`) {
		t.Fatalf("present empty message_summary should serialize as empty string like upstream Some(\"\"), got %s", data)
	}
}

func TestPayloadSerializesPresentEmptyMessageKindLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: MessageStart}}})
	payload := runner.PayloadFor(Rule{Event: MessageStart}, EventData{Event: MessageStart, MessageKind: ""})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"message_kind":""`) {
		t.Fatalf("present empty message_kind should serialize as empty string like upstream Some(\"\"), got %s", data)
	}
}

func TestPayloadSerializesPresentEmptyAssistantEventLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: MessageUpdate}}})
	payload := runner.PayloadFor(Rule{Event: MessageUpdate}, EventData{Event: MessageUpdate, AssistantEvent: ""})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"assistant_event":""`) {
		t.Fatalf("present empty assistant_event should serialize as empty string like upstream Some(\"\"), got %s", data)
	}
}

func TestPayloadSerializesPresentEmptyToolResultSummaryLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: ToolEnd}}})
	payload := runner.PayloadFor(Rule{Event: ToolEnd}, EventData{Event: ToolEnd, ToolName: "bash", ToolResultSummary: ""})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tool_result_summary":""`) {
		t.Fatalf("present empty tool_result_summary should serialize as empty string like upstream Some(\"\"), got %s", data)
	}
}

func TestPayloadSerializesPresentEmptyToolIDAndNameLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: ToolStart}}})
	payload := runner.PayloadFor(Rule{Event: ToolStart}, EventData{Event: ToolStart, ToolCallID: "", ToolName: ""})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tool_call_id", "tool_name"} {
		if !strings.Contains(string(data), `"`+field+`":""`) {
			t.Fatalf("present empty %s should serialize as empty string like upstream Some(\"\"), got %s", field, data)
		}
	}
}

func TestPayloadSerializesPresentEmptyCompactionSummaryLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: Compaction}}})
	tokensBefore := uint64(0)
	payload := runner.PayloadFor(Rule{Event: Compaction}, EventData{Event: Compaction, CompactionTrigger: "manual", CompactionTokensBefore: &tokensBefore, CompactionSummary: ""})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"compaction_summary":""`) {
		t.Fatalf("present empty compaction_summary should serialize as empty string like upstream Some(\"\"), got %s", data)
	}
}

func TestPayloadSerializesPresentEmptyCompactionTriggerLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: Compaction}}})
	payload := runner.PayloadFor(Rule{Event: Compaction}, EventData{Event: Compaction, CompactionTrigger: ""})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"compaction_trigger":""`) {
		t.Fatalf("present empty compaction_trigger should serialize as empty string like upstream Some(\"\"), got %s", data)
	}
}

func TestEnvIncludesPresentEmptyOptionStringsLikeUpstream(t *testing.T) {
	env := envForPayload(Payload{
		Event:                    "message_update",
		MessageKindPresent:       true,
		AssistantEventPresent:    true,
		ToolCallIDPresent:        true,
		ToolNamePresent:          true,
		CompactionTriggerPresent: true,
	}, "/tmp/payload.json")
	for _, entry := range []string{
		"PIE_MESSAGE_KIND=",
		"PIE_ASSISTANT_EVENT=",
		"PIE_TOOL_CALL_ID=",
		"PIE_TOOL_NAME=",
		"PIE_COMPACTION_TRIGGER=",
	} {
		if !containsEnv(env, entry) {
			t.Fatalf("present empty option should set env %q like upstream Some(\"\"), got %#v", entry, env)
		}
	}
}

func TestRunnerCommandReceivesEnvAndPayloadFileLikeUpstream(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "hook.out")
	command := "printf '%s %s ' \"$PIE_HOOK_EVENT\" \"$PIE_TOOL_NAME\" > " + quoteShellArg(outPath) + "; test -s \"$PIE_HOOK_PAYLOAD\""
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: ToolEnd, Tool: "bash", Command: command, Timeout: time.Second}}})
	if err := runner.HandleData(context.Background(), EventData{Event: ToolEnd, ToolName: "bash", ToolCallID: "call-1", ToolResultSummary: "ok"}); err != nil {
		t.Fatalf("hook command env should match upstream, got %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "tool_end bash " {
		t.Fatalf("hook env mismatch: %q", data)
	}
}

func TestRunCommandTimeoutErrorMatchesUpstream(t *testing.T) {
	err := runCommand(context.Background(), Rule{Event: AgentStart, Command: "sleep 1", Timeout: time.Millisecond}, Payload{Event: "agent_start"}, t.TempDir())
	if err == nil || err.Error() != "timed out after 1ms" {
		t.Fatalf("timeout error should match upstream, got %v", err)
	}
}

func TestRunCommandTimeoutKillsProcessTreeLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group semantics are Unix-specific")
	}
	markerPath := filepath.Join(t.TempDir(), "leaked-child")
	command := "(sleep 0.2; touch " + quoteShellArg(markerPath) + ") & wait"
	_ = runCommand(context.Background(), Rule{Event: AgentStart, Command: command, Timeout: 20 * time.Millisecond}, Payload{Event: "agent_start"}, t.TempDir())
	time.Sleep(350 * time.Millisecond)
	if _, err := os.Stat(markerPath); err == nil {
		t.Fatalf("timed out hook should kill child process tree like upstream; child created %s", markerPath)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestRunCommandCancellationKillsProcessTreeLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group semantics are Unix-specific")
	}
	markerPath := filepath.Join(t.TempDir(), "leaked-child")
	command := "(sleep 0.2; touch " + quoteShellArg(markerPath) + ") & wait"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = runCommand(ctx, Rule{Event: AgentStart, Command: command, Timeout: time.Second}, Payload{Event: "agent_start"}, t.TempDir())
	time.Sleep(350 * time.Millisecond)
	if _, err := os.Stat(markerPath); err == nil {
		t.Fatalf("cancelled hook should kill child process tree like upstream; child created %s", markerPath)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestRunCommandExitErrorMatchesUpstream(t *testing.T) {
	err := runCommand(context.Background(), Rule{Event: AgentStart, Command: "printf 'bad hook' >&2; exit 7", Timeout: time.Second}, Payload{Event: "agent_start"}, t.TempDir())
	if err == nil || err.Error() != "command exited 7: bad hook" {
		t.Fatalf("exit error should match upstream, got %v", err)
	}
}

func TestRunCommandCancellationErrorMatchesUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runCommand(ctx, Rule{Event: AgentStart, Command: "sleep 1", Timeout: time.Second}, Payload{Event: "agent_start"}, t.TempDir())
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("cancel error should match upstream, got %v", err)
	}
}

func TestRunnerCompactionCommandReceivesEnvAndPayloadLikeUpstream(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "hook.out")
	tokensBefore := uint64(42)
	command := "printf '%s %s %s ' \"$PIE_HOOK_EVENT\" \"$PIE_COMPACTION_TRIGGER\" \"$PIE_COMPACTION_TOKENS_BEFORE\" > " + quoteShellArg(outPath) + "; grep -q '\"compaction_summary\":\"summary text\"' \"$PIE_HOOK_PAYLOAD\""
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: Compaction, Command: command, Timeout: time.Second}}})
	if err := runner.HandleData(context.Background(), EventData{Event: Compaction, CompactionTrigger: "manual", CompactionTokensBefore: &tokensBefore, CompactionSummary: "summary text"}); err != nil {
		t.Fatalf("compaction hook command env should match upstream, got %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "compaction manual 42 " {
		t.Fatalf("compaction hook env mismatch: %q", data)
	}
}

func TestWebhookErrorIncludesResponseBodyLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad hook", http.StatusBadGateway)
	}))
	defer server.Close()
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Webhook: server.URL, OnFailure: OnFailureIgnore}}})
	err := runner.defaultExecute(context.Background(), runner.rules[0], runner.PayloadFor(runner.rules[0], EventData{Event: AgentStart}))
	if err == nil || !strings.Contains(err.Error(), "bad hook") {
		t.Fatalf("webhook errors should include response body like upstream, got %v", err)
	}
}

func TestWebhookCancellationErrorMatchesUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir()})
	err := runner.runWebhook(ctx, Rule{Event: AgentStart, Webhook: "http://127.0.0.1:1", Timeout: time.Second}, Payload{Event: "agent_start"})
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("webhook cancel error should match upstream, got %v", err)
	}
}

func TestWebhookHookSendsConfiguredHeadersLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("webhook headers mismatch: %#v", r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
webhook = "`+server.URL+`"
headers = { "X-Test" = "yes", Authorization = "Bearer token" }
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("parse mismatch rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: rules})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatalf("webhook should receive configured headers like upstream, got %v", err)
	}
}

func TestWebhookHookSendsTurnEndPayloadLikeUpstreamE2E(t *testing.T) {
	var payload Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Test") != "webhook-e2e" {
			t.Fatalf("webhook request mismatch method=%s headers=%#v", r.Method, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runner := NewRunner(RunnerOptions{SessionID: "session-webhook-e2e", CWD: "/repo", ModelProvider: "faux", ModelID: "faux", ThinkingLevel: "off", Rules: []Rule{{Event: TurnEnd, Source: "user", Webhook: server.URL, Headers: map[string]string{"X-Test": "webhook-e2e"}}}})
	err := runner.HandleData(context.Background(), EventData{Event: TurnEnd, MessageKind: "assistant", MessageSummary: "webhook ack"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Event != "turn_end" || payload.SessionID != "session-webhook-e2e" || payload.CWD != "/repo" || payload.ModelProvider != "faux" || payload.ModelID != "faux" || payload.ThinkingLevel != "off" || payload.Source != "user" || payload.MessageKind != "assistant" || payload.MessageSummary != "webhook ack" {
		t.Fatalf("webhook payload mismatch: %#v", payload)
	}
}

func TestWebhookHookSendsCompactionPayloadLikeUpstreamE2E(t *testing.T) {
	var payload Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("webhook request mismatch method=%s headers=%#v", r.Method, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	tokensBefore := uint64(10)
	runner := NewRunner(RunnerOptions{SessionID: "session-manual-compaction-e2e", CWD: "/repo", ModelProvider: "faux", ModelID: "faux", ThinkingLevel: "off", Rules: []Rule{{Event: Compaction, Webhook: server.URL}}})
	err := runner.HandleData(context.Background(), EventData{Event: Compaction, CompactionTrigger: "manual", CompactionTokensBefore: &tokensBefore, CompactionSummary: "manual summary"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Event != "compaction" || payload.SessionID != "session-manual-compaction-e2e" || payload.CWD != "/repo" || payload.ModelProvider != "faux" || payload.ModelID != "faux" || payload.ThinkingLevel != "off" || payload.CompactionTrigger != "manual" || payload.CompactionTokensBefore == nil || *payload.CompactionTokensBefore != 10 || payload.CompactionSummary != "manual summary" {
		t.Fatalf("webhook compaction payload mismatch: %#v", payload)
	}
}

func TestParseHooksFileSupportsNestedHeadersTableLikeUpstreamSerde(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
webhook = "https://example.test/hook"

[hook.headers]
"X-Test" = "yes"
Authorization = "Bearer token"
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("parse mismatch rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	if rules[0].Headers["X-Test"] != "yes" || rules[0].Headers["Authorization"] != "Bearer token" {
		t.Fatalf("nested headers should parse like upstream serde, got %#v", rules[0].Headers)
	}
}

func TestParseHooksFileSupportsDottedHeadersLikeUpstreamSerde(t *testing.T) {
	rules, diagnostics := ParseHooksFile(`
[[hook]]
event = "agent_start"
webhook = "https://example.test/hook"
headers."X-Test" = "yes"
headers.Authorization = "Bearer token"
`, "test")
	if len(diagnostics) != 0 || len(rules) != 1 {
		t.Fatalf("parse mismatch rules=%#v diagnostics=%#v", rules, diagnostics)
	}
	if rules[0].Headers["X-Test"] != "yes" || rules[0].Headers["Authorization"] != "Bearer token" {
		t.Fatalf("dotted headers should parse like upstream serde, got %#v", rules[0].Headers)
	}
}

func TestRunnerCommandUsesHomeCWDLikeUpstream(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "pwd.txt")
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "pwd > " + quoteShellArg(outPath), CWD: CWDHome, Timeout: time.Second}}})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatalf("home cwd hook should run in %q like upstream, got %v", home, err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != home {
		t.Fatalf("home cwd hook should run in %q like upstream, got %q", home, strings.TrimSpace(string(data)))
	}
}

func TestRunnerCommandUsesPieCWDLikeUpstream(t *testing.T) {
	pieDir := t.TempDir()
	t.Setenv("PIE_DIR", pieDir)
	outPath := filepath.Join(t.TempDir(), "pwd.txt")
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "pwd > " + quoteShellArg(outPath), CWD: CWDPie, Timeout: time.Second}}})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatalf("pie cwd hook should run in %q like upstream, got %v", pieDir, err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != pieDir {
		t.Fatalf("pie cwd hook should run in %q like upstream, got %q", pieDir, strings.TrimSpace(string(data)))
	}
}

func TestRunnerDoesNotReturnHookFailuresLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "hook"}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		return errors.New("boom")
	}})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatalf("hook failures should be warn-only by default like upstream, got %v", err)
	}
}

func TestRunnerSupportsIgnoreOnFailureLikeUpstream(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart, Command: "hook", OnFailure: OnFailureIgnore}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		return errors.New("boom")
	}})
	if err := runner.HandleData(context.Background(), EventData{Event: AgentStart}); err != nil {
		t.Fatalf("ignore on_failure should suppress hook errors like upstream, got %v", err)
	}
}

func TestParseEvent(t *testing.T) {
	event, ok := ParseEvent("tool_end")
	if !ok || event != ToolEnd || event.String() != "tool_end" {
		t.Fatalf("parse mismatch event=%v ok=%v", event, ok)
	}
	if _, ok := ParseEvent("unknown"); ok {
		t.Fatal("unknown event should not parse")
	}
	if !strings.Contains(AllEvents()[0].String(), "agent") {
		t.Fatalf("events list mismatch")
	}
}

func quoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func containsEnv(env []string, entry string) bool {
	for _, item := range env {
		if item == entry {
			return true
		}
	}
	return false
}
