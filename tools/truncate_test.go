package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestTruncateCompatSurface(t *testing.T) {
	if DEFAULT_MAX_LINES != 2000 || DEFAULT_MAX_BYTES != 256*1024 {
		t.Fatalf("default truncation constants mismatch: lines=%d bytes=%d", DEFAULT_MAX_LINES, DEFAULT_MAX_BYTES)
	}
	if DEFAULTMAXLINES != DEFAULT_MAX_LINES || DEFAULTMAXBYTES != DEFAULT_MAX_BYTES {
		t.Fatalf("uppercase compact aliases mismatch: lines=%d bytes=%d", DEFAULTMAXLINES, DEFAULTMAXBYTES)
	}
	out, info := TruncateHead("a\nb\nc\n", 2, 1024)
	if out != "a\nb\n" || info.KeptLines != 2 || info.TruncatedLines != 1 {
		t.Fatalf("head truncate mismatch: out=%q info=%#v", out, info)
	}
	out, info = TruncateTail("a\nb\nc\n", 2, 1024)
	if out != "b\nc\n" || info.Note() == "" {
		t.Fatalf("tail truncate mismatch: out=%q info=%#v note=%q", out, info, info.Note())
	}
}

func TestTruncationNoteOKMatchesUpstreamOptionSemantics(t *testing.T) {
	if note, ok := (Truncation{TotalLines: 1, KeptLines: 1, TotalBytes: 2, KeptBytes: 2}).NoteOK(); ok || note != "" {
		t.Fatalf("untruncated note should be none, note=%q ok=%v", note, ok)
	}
	note, ok := (Truncation{TotalLines: 3, KeptLines: 1, TruncatedLines: 2, TotalBytes: 30, KeptBytes: 10}).NoteOK()
	if !ok || note != "[truncated: kept 1/3 lines, 10 of 30 bytes]" {
		t.Fatalf("truncated note mismatch note=%q ok=%v", note, ok)
	}
}

func TestTruncateTextAndShellOutputCompatSurface(t *testing.T) {
	if got := TruncateText("abcdef", 4); got != "[truncated, kept 4 of 6 chars]\nabcd" {
		t.Fatalf("TruncateText mismatch: %q", got)
	}
	if got := TruncateText("你好世界", 2); got != "[truncated, kept 2 of 4 chars]\n你好" {
		t.Fatalf("TruncateText should count runes like upstream: %q", got)
	}
	if got := TruncateText("abc", 4); got != "abc" {
		t.Fatalf("TruncateText should pass through short text: %q", got)
	}
	if got := TruncateShellOutput("stdout text", "stderr text", 6); got != "[truncated, kept 6 of 11 chars]\nstdout" {
		t.Fatalf("TruncateShellOutput mismatch: %q", got)
	}
}

func TestTruncateCharsMatchesUpstreamFeedHelper(t *testing.T) {
	if got := TruncateChars("abcdef", 4); got != "abcd…" {
		t.Fatalf("TruncateChars mismatch: %q", got)
	}
	if got := TruncateChars("你好世界", 2); got != "你好…" {
		t.Fatalf("TruncateChars should count runes: %q", got)
	}
	if got := TruncateChars("abc", 4); got != "abc" {
		t.Fatalf("TruncateChars should pass through short text: %q", got)
	}
}

func TestCompactToolOutputLinesMatchesUpstreamFeedHelper(t *testing.T) {
	short := []string{"ok", "done"}
	if got := CompactToolOutputLines(short, false); fmt.Sprint(got) != fmt.Sprint(short) {
		t.Fatalf("short output mismatch: %#v", got)
	}

	lines := make([]string, 40)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %d", index)
	}
	compacted := CompactToolOutputLines(lines, false)
	if len(compacted) > ToolOutputHeadLines+ToolOutputTailLines+1 || compacted[0] != "line 0" || compacted[len(compacted)-1] != "line 39" {
		t.Fatalf("compacted head/tail mismatch: %#v", compacted)
	}
	if !containsLineFragment(compacted, "truncated") || !containsLineFragment(compacted, "full output remains available to the agent") {
		t.Fatalf("compacted output missing marker: %#v", compacted)
	}

	errorLines := make([]string, 36)
	for index := range errorLines {
		errorLines[index] = fmt.Sprintf("line %d", index)
	}
	if !containsLineFragment(CompactToolOutputLines(errorLines, false), "truncated") {
		t.Fatalf("normal output should be compacted")
	}
	if got := CompactToolOutputLines(errorLines, true); len(got) != 36 {
		t.Fatalf("error output should keep more context: %#v", got)
	}

	long := strings.Repeat("你好", ToolOutputMaxLineChars+10)
	utf8Compacted := CompactToolOutputLines([]string{long}, false)
	if len(utf8Compacted) != 2 || !strings.HasSuffix(utf8Compacted[0], "…") || !containsLineFragment(utf8Compacted, "truncated") {
		t.Fatalf("utf8 compact mismatch: %#v", utf8Compacted)
	}
}

func TestCompactToolContentBlocksUsesOnlyTextBlocks(t *testing.T) {
	blocks := []ai.UserContentBlock{
		ai.NewTextUserContentBlock("one\ntwo\n"),
		{Type: ai.UserContentImage, Data: "ignored", MimeType: "image/png"},
	}
	if got := CompactToolContentBlocks(blocks, false); fmt.Sprint(got) != fmt.Sprint([]string{"one", "two"}) {
		t.Fatalf("content blocks mismatch: %#v", got)
	}
}

func TestToolCallPreviewMatchesUpstreamFeedHelper(t *testing.T) {
	preview := ToolCallPreviewOrdered([]ToolCallPreviewField{
		{Key: "cmd", Value: "echo\nhello"},
		{Key: "count", Value: 3},
		{Key: "nested", Value: map[string]any{"ok": true}},
		{Key: "ignored", Value: "later"},
	})
	if preview != `(cmd="echo\nhello", count=3, nested={"ok":true}, …)` {
		t.Fatalf("preview mismatch: %q", preview)
	}

	long := strings.Repeat("x", 61)
	if got := ToolCallPreviewOrdered([]ToolCallPreviewField{{Key: "long", Value: long}}); got != `(long="`+strings.Repeat("x", 60)+`…")` {
		t.Fatalf("long preview mismatch: %q", got)
	}
	if got := ToolCallPreview("not an object"); got != "" {
		t.Fatalf("non-object preview should be empty: %q", got)
	}

	stable := ToolCallPreview(map[string]any{"b": 2, "a": 1})
	if stable != "(a=1, b=2)" {
		t.Fatalf("map preview should be sorted for Go determinism: %q", stable)
	}

	if got := ToolCallPreviewOrdered([]ToolCallPreviewField{{Key: "nested", Value: map[string]any{"text": "<tag>&value"}}}); got != `(nested={"text":"<tag>&value"})` {
		t.Fatalf("json preview should not HTML-escape strings like upstream serde_json: %q", got)
	}
}

func containsLineFragment(lines []string, fragment string) bool {
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			return true
		}
	}
	return false
}
