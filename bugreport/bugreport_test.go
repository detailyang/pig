package bugreport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactKnownSecretPatterns(t *testing.T) {
	input := "key=sk-abcdefghij1234567890abcd aws=AKIAEXAMPLEEXAMPLE1A gh=gho_abcdefghijklmnopqrstuvwxyz0123456789 slack=xoxb-1234567890-abcdef header=Authorization: Bearer eyJabc.defghijklmnopqr hub=hub_agent_abcdefghijklmnopqrstuvwxyz session=hub_hs_abcdefghijklmnopqrstuvwxyz id=018fe23a-1111-4a22-8b33-123456789abc google=AIza12345678901234567890123456789012345"
	redacted := Redact(input)
	for _, leaked := range []string{"sk-abcdefghij", "AKIAEXAMPLE", "gho_", "xoxb-", "eyJabc.defghijklmnopqr", "hub_agent_", "hub_hs_", "018fe23a-1111", "AIza123"} {
		if strings.Contains(redacted, leaked) {
			t.Fatalf("secret %q leaked in %q", leaked, redacted)
		}
	}
	for _, marker := range []string{"[REDACTED:openai_anthropic_key]", "[REDACTED:aws_access_key]", "[REDACTED:github_token]", "[REDACTED:slack_token]", "[REDACTED:bearer_token]", "[REDACTED:pie_hub_token]", "[REDACTED:uuid]", "[REDACTED:google_api_key]"} {
		if !strings.Contains(redacted, marker) {
			t.Fatalf("missing marker %q in %q", marker, redacted)
		}
	}
	if Redact("hello world, no secrets here") != "hello world, no secrets here" {
		t.Fatal("normal text should be unchanged")
	}
}

func TestDefaultDestUsesPieDirAndUTCStamp(t *testing.T) {
	t.Setenv("PIE_DIR", filepath.Join(t.TempDir(), "pie-home"))
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	path := DefaultDest(now)
	if !strings.HasSuffix(path, filepath.Join("bug-reports", "20260102T030405Z.txt")) || !strings.Contains(path, "pie-home") {
		t.Fatalf("default dest mismatch: %s", path)
	}
}

func TestDefaultDestNoArgMatchesUpstreamShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	path := DefaultDest()
	if !strings.HasPrefix(path, filepath.Join(dir, "bug-reports")+string(os.PathSeparator)) || !strings.HasSuffix(path, ".txt") {
		t.Fatalf("default dest mismatch: %s", path)
	}
}

func TestBuildBodyIncludesDiagnosticsLogTailTranscriptAndRedacts(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "pie.log")
	var log strings.Builder
	for i := 0; i < MaxLogLines+3; i++ {
		fmt.Fprintf(&log, "line-%03d\n", i)
	}
	log.WriteString("token=sk-abcdefghij1234567890abcd\n")
	if err := os.WriteFile(logPath, []byte(log.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	body := BuildBody(DiagInputs{
		SessionID:   "018fe23a-1111-4a22-8b33-123456789abc",
		Model:       "gpt-test",
		Thinking:    "medium",
		ToolCount:   7,
		SkillCount:  3,
		CostSummary: "$0.01",
		LogPath:     logPath,
	}, "transcript has Bearer eyJabc.defghijklmnopqr", now)
	for _, want := range []string{"pie bug report", "generated_at: 2026-01-02T03:04:05Z", "---- diagnostic ----", "model         gpt-test", "tools         7", "skills        3", "---- log tail (200 lines", "---- transcript ----"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in body:\n%s", want, body)
		}
	}
	if strings.Contains(body, "line-000") || !strings.Contains(body, "line-004") {
		t.Fatalf("log tail mismatch")
	}
	for _, leaked := range []string{"sk-abcdefghij", "eyJabc.defghijklmnopqr", "018fe23a-1111"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("secret %q leaked in body:\n%s", leaked, body)
		}
	}
}

func TestBuildBodyInvalidUTF8LogReportsReadFailureLikeUpstream(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "pie.log")
	if err := os.WriteFile(logPath, []byte("ok\n\xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := BuildBody(DiagInputs{LogPath: logPath}, "transcript", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if !strings.Contains(body, "cannot read log") || !strings.Contains(body, "invalid UTF-8") || strings.Contains(body, "\ufffd") {
		t.Fatalf("invalid UTF-8 log should render read failure like upstream, got:\n%s", body)
	}
}

func TestWriteCreatesParentAndWritesRedactedReport(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "nested", "report.txt")
	written, err := Write(DiagInputs{SessionID: "safe", Thinking: "off", CostSummary: "none"}, "secret sk-abcdefghij1234567890abcd", dest, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if written != dest {
		t.Fatalf("written path mismatch: %s", written)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-abcdefghij") || !strings.Contains(string(data), "[REDACTED:openai_anthropic_key]") {
		t.Fatalf("report redaction mismatch:\n%s", string(data))
	}
}

func TestBuildCompatWrapperWritesReport(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "report.txt")
	written, err := Build(DiagInputs{SessionID: "safe", Thinking: "off", CostSummary: "none"}, "transcript", dest, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil || written != dest {
		t.Fatalf("Build mismatch: written=%q err=%v", written, err)
	}
}
