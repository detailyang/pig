package utils

import (
	"strings"
	"testing"
)

func TestTruncateTextMatchesUpstreamCurrentBehavior(t *testing.T) {
	if got := TruncateText("hi", 10); got != "hi" {
		t.Fatalf("short text should pass through, got %q", got)
	}
	got := TruncateText("你好世界", 2)
	if got != "[truncated, kept 2 of 4 chars]\n你好" {
		t.Fatalf("unicode truncation mismatch: %q", got)
	}
}

func TestTruncateShellOutputUsesStdoutAndTruncateText(t *testing.T) {
	got := TruncateShellOutput(strings.Repeat("x", 12), "ignored stderr", 5)
	if got != "[truncated, kept 5 of 12 chars]\nxxxxx" {
		t.Fatalf("shell output truncation mismatch: %q", got)
	}
}

func TestTruncateHeadMatchesUpstreamPrimitive(t *testing.T) {
	if DEFAULT_MAX_LINES != DefaultMaxLines || DEFAULT_MAX_BYTES != DefaultMaxBytes {
		t.Fatalf("default constants mismatch: lines=%d bytes=%d", DEFAULT_MAX_LINES, DEFAULT_MAX_BYTES)
	}
	out, truncation := TruncateHead("a\nb\nc\nd\n", 2, 1024)
	if out != "a\nb\n" {
		t.Fatalf("head output mismatch: %q", out)
	}
	if truncation.TotalLines != 4 || truncation.KeptLines != 2 || truncation.TruncatedLines != 2 || truncation.TotalBytes != 8 || truncation.KeptBytes != 4 {
		t.Fatalf("head truncation mismatch: %#v", truncation)
	}
	if got := truncation.Note(); got != "[truncated: kept 2/4 lines, 4 of 8 bytes]" {
		t.Fatalf("note mismatch: %q", got)
	}
}

func TestTruncateTailMatchesUpstreamPrimitive(t *testing.T) {
	out, truncation := TruncateTail("a\nb\nc\nd\n", 2, 1024)
	if out != "c\nd\n" {
		t.Fatalf("tail output mismatch: %q", out)
	}
	if truncation.KeptLines != 2 || truncation.TruncatedLines != 2 || truncation.KeptBytes != 4 {
		t.Fatalf("tail truncation mismatch: %#v", truncation)
	}
}
