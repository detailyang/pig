package markdown

import (
	"strings"
	"testing"
)

func TestRenderLineInlineStyles(t *testing.T) {
	bold := RenderLine("hello **world**!")
	if !strings.Contains(bold, Bold) || !strings.Contains(bold, "world") || !strings.Contains(bold, Reset) || strings.Contains(bold, "**") {
		t.Fatalf("bold mismatch: %q", bold)
	}
	italic := RenderLine("an *italic* word")
	if !strings.Contains(italic, Italic) || !strings.Contains(italic, "italic") || strings.Contains(italic, "*italic*") {
		t.Fatalf("italic mismatch: %q", italic)
	}
	code := RenderLine("call `foo()`")
	if !strings.Contains(code, Code) || !strings.Contains(code, "foo()") || strings.Contains(code, "`") {
		t.Fatalf("code mismatch: %q", code)
	}
}

func TestRenderLineHeadingBulletAndUnclosedCode(t *testing.T) {
	heading := RenderLine("## Section")
	if !strings.Contains(heading, Heading) || !strings.Contains(heading, "##Section") || !strings.HasSuffix(heading, Reset) {
		t.Fatalf("heading mismatch: %q", heading)
	}
	bullet := RenderLine("* item")
	if bullet != "* item" {
		t.Fatalf("bullet should not become italic: %q", bullet)
	}
	partial := RenderLine("partial `code")
	if partial != "partial `code" {
		t.Fatalf("unclosed code should be unchanged: %q", partial)
	}
}

func TestRendererTracksFenceAcrossLines(t *testing.T) {
	renderer := NewRenderer()
	open := renderer.RenderLine("```go")
	if !strings.Contains(open, Dim) || !strings.HasSuffix(open, Reset) {
		t.Fatalf("open fence mismatch: %q", open)
	}
	body := renderer.RenderLine("x := 1")
	if !strings.Contains(body, Code) || !strings.Contains(body, "x := 1") {
		t.Fatalf("fence body mismatch: %q", body)
	}
	close := renderer.RenderLine("```")
	if !strings.Contains(close, Dim) {
		t.Fatalf("close fence mismatch: %q", close)
	}
	after := renderer.RenderLine("plain text")
	if strings.Contains(after, Code) || after != "plain text" {
		t.Fatalf("after fence mismatch: %q", after)
	}
}

func TestMarkdownUpstreamHelperNames(t *testing.T) {
	if got := FindByte([]byte("abc`def"), 2, '`'); got != 3 {
		t.Fatalf("FindByte mismatch: %d", got)
	}
	if got := FindByte([]byte("abc"), 1, '`'); got != -1 {
		t.Fatalf("FindByte missing mismatch: %d", got)
	}
	if got := FindByte([]byte("abc"), 9, 'a'); got != -1 {
		t.Fatalf("FindByte out-of-range mismatch: %d", got)
	}

	renderer := DefaultRenderer()
	if renderer == nil || renderer.RenderLine("plain") != "plain" {
		t.Fatalf("default renderer mismatch: %#v", renderer)
	}
	if Default().RenderLine("plain") != "plain" {
		t.Fatalf("Default renderer alias mismatch")
	}
}
