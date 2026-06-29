package bugreport

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/config"
)

const MaxLogLines = 200
const Version = "dev"

type DiagInputs struct {
	SessionID   string
	Model       string
	Thinking    string
	ToolCount   int
	SkillCount  int
	CostSummary string
	LogPath     string
}

func DefaultDest(at ...time.Time) string {
	now := time.Now()
	if len(at) > 0 {
		now = at[0]
	}
	stamp := now.UTC().Format("20060102T150405Z")
	return filepath.Join(config.BaseDir(), "bug-reports", stamp+".txt")
}

func Write(diag DiagInputs, transcript, dest string, now time.Time) (string, error) {
	if parent := filepath.Dir(dest); parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", fmt.Errorf("create bug-reports dir %s: %w", parent, err)
		}
	}
	if err := os.WriteFile(dest, []byte(BuildBody(diag, transcript, now)), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}
	return dest, nil
}

func Build(diag DiagInputs, transcript, dest string, now time.Time) (string, error) {
	return Write(diag, transcript, dest, now)
}

func BuildBody(diag DiagInputs, transcript string, now time.Time) string {
	var body strings.Builder
	body.WriteString("pie bug report\n")
	body.WriteString(fmt.Sprintf("generated_at: %s\n", now.UTC().Format(time.RFC3339)))
	body.WriteString(fmt.Sprintf("pie_version: %s\n\n", Version))

	body.WriteString("---- diagnostic ----\n")
	body.WriteString(fmt.Sprintf("session_id    %s\n", diag.SessionID))
	model := diag.Model
	if model == "" {
		model = "(none)"
	}
	body.WriteString(fmt.Sprintf("model         %s\n", model))
	body.WriteString(fmt.Sprintf("thinking      %s\n", diag.Thinking))
	body.WriteString(fmt.Sprintf("tools         %d\n", diag.ToolCount))
	body.WriteString(fmt.Sprintf("skills        %d\n", diag.SkillCount))
	body.WriteString(fmt.Sprintf("cost          %s\n", diag.CostSummary))
	logPath := diag.LogPath
	if logPath == "" {
		logPath = "(disabled)"
	}
	body.WriteString(fmt.Sprintf("log_path      %s\n\n", logPath))

	if diag.LogPath != "" {
		body.WriteString(fmt.Sprintf("---- log tail (%d lines from %s) ----\n", MaxLogLines, diag.LogPath))
		text, err := os.ReadFile(diag.LogPath)
		if err != nil {
			body.WriteString(fmt.Sprintf("(cannot read log: %v)\n", err))
		} else if !utf8.Valid(text) {
			body.WriteString("(cannot read log: invalid UTF-8)\n")
		} else {
			for _, line := range tailLines(string(text), MaxLogLines) {
				body.WriteString(line)
				body.WriteByte('\n')
			}
		}
		body.WriteByte('\n')
	}

	body.WriteString("---- transcript ----\n")
	body.WriteString(transcript)
	return Redact(body.String())
}

func Redact(input string) string {
	out := input
	for _, redactor := range redactors {
		out = redactor.pattern.ReplaceAllString(out, "[REDACTED:"+redactor.label+"]")
	}
	return out
}

func tailLines(text string, maxLines int) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) <= maxLines {
		return lines
	}
	return lines[len(lines)-maxLines:]
}

type redactor struct {
	label   string
	pattern *regexp.Regexp
}

var redactors = []redactor{
	{"openai_anthropic_key", regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)},
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github_token", regexp.MustCompile(`\bgh[ousp]_[A-Za-z0-9]{30,}\b`)},
	{"slack_token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"bearer_token", regexp.MustCompile(`Bearer\s+[A-Za-z0-9._\-]{16,}`)},
	{"pie_hub_token", regexp.MustCompile(`\bhub_(?:agent|hs)_[A-Za-z0-9._\-]{8,}\b`)},
	{"uuid", regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)},
}
