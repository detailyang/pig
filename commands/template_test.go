package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/templates"
)

func TestTemplateCommandListsTemplatesAndRunsOne(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Templates: []templates.Template{{Name: "review", Description: "review code"}, {Name: "plan"}}}
	listed := Dispatch(context.Background(), "/template", registry, ctx)
	if listed.Kind != OutcomeHandled || !strings.Contains(listed.Message, "Loaded templates (2):") || !strings.Contains(listed.Message, "/template review  review code") || !strings.Contains(listed.Message, "/template plan") {
		t.Fatalf("list mismatch: %#v", listed)
	}
	run := Dispatch(context.Background(), "/template review file=main.go note=hello", registry, ctx)
	if run.Kind != OutcomeRunPromptTemplate || run.Name != "review" || run.Vars["file"] != "main.go" || run.Vars["note"] != "hello" {
		t.Fatalf("run mismatch: %#v", run)
	}
}

func TestTemplateCommandEmptyAndBadArgs(t *testing.T) {
	registry := DefaultRegistry()
	empty := Dispatch(context.Background(), "/template", registry, Context{})
	if empty.Kind != OutcomeHandled || !strings.Contains(empty.Message, "(no templates loaded") {
		t.Fatalf("empty mismatch: %#v", empty)
	}
	bad := Dispatch(context.Background(), "/template review file", registry, Context{})
	if bad.Kind != OutcomeError || bad.Message != "expected k=v argument; got: file" {
		t.Fatalf("bad arg mismatch: %#v", bad)
	}
}
