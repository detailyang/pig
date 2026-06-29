package bugreport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/bugreport"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/sessionexport"
)

func TestBuildWritesRedactedReportFromSessionTranscriptLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.log")
	if err := os.WriteFile(logPath, []byte("key=sk-abcdefghij1234567890abcd\nheader: Authorization: Bearer eyJabc.defghijklmnopqr\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("describe the system")); err != nil {
		t.Fatal(err)
	}
	transcript, err := sessionexport.Render(sess)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "report.txt")
	written, err := bugreport.Build(bugreport.DiagInputs{SessionID: "test", Model: "faux:faux", Thinking: "off", CostSummary: "n/a", LogPath: logPath}, transcript, dest, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if written != dest {
		t.Fatalf("written path mismatch: %s", written)
	}
	bodyBytes, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	for _, want := range []string{"pie bug report", "---- diagnostic ----", "---- log tail", "---- transcript ----", "describe the system", "[REDACTED:openai_anthropic_key]", "[REDACTED:bearer_token]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("report missing %q:\n%s", want, body)
		}
	}
	for _, leaked := range []string{"sk-abcdefghij", "eyJabc.defghijklmnopqr"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("secret %q leaked:\n%s", leaked, body)
		}
	}
}
