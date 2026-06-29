package extensions

import (
	"errors"
	"strings"
	"testing"
)

type helloExtension struct{}

func (helloExtension) Name() string        { return "hello" }
func (helloExtension) Description() string { return "hello extension" }
func (helloExtension) Init(ctx Context) (Contribution, error) {
	if ctx.SessionID != "t" || ctx.CWD == "" {
		return Contribution{}, errors.New("context mismatch")
	}
	return Contribution{Banner: "ready"}, nil
}

type boomExtension struct{}

func (boomExtension) Name() string { return "boom" }
func (boomExtension) Init(Context) (Contribution, error) {
	return Contribution{}, errors.New("intentional failure")
}

type panickingExtension struct{}

func (panickingExtension) Name() string { return "panicker" }
func (panickingExtension) Init(Context) (Contribution, error) {
	panic("oops")
}

func TestRegistryInitAllIsolatesFailingExtensions(t *testing.T) {
	registry := NewRegistry()
	registry.Register(helloExtension{})
	registry.Register(boomExtension{})
	registry.Register(panickingExtension{})

	out := registry.InitAll(Context{CWD: t.TempDir(), SessionID: "t"})
	if len(out.Banners) != 1 || out.Banners[0] != "hello: ready" {
		t.Fatalf("banners mismatch: %#v", out.Banners)
	}
	if len(out.Errors) != 2 {
		t.Fatalf("errors mismatch: %#v", out.Errors)
	}
	if !containsSubstring(out.Errors, "boom: intentional failure") || !containsSubstring(out.Errors, "panicker: panicked during init") {
		t.Fatalf("errors mismatch: %#v", out.Errors)
	}
}

func TestRegistrySnapshotIsImmutable(t *testing.T) {
	registry := NewRegistry()
	registry.Register(helloExtension{})
	registered := registry.Extensions()
	registered[0] = boomExtension{}

	out := registry.InitAll(Context{CWD: t.TempDir(), SessionID: "t"})
	if len(out.Errors) != 0 || len(out.Banners) != 1 || out.Banners[0] != "hello: ready" {
		t.Fatalf("registry should not expose mutable internals: %#v", out)
	}
}

func TestUpstreamExtensionExportedNames(t *testing.T) {
	if len(Default().Extensions()) != 0 {
		t.Fatalf("default registry should be empty")
	}
	registry := NewExtensionRegistry()
	var _ *ExtensionRegistry = registry
	var extension AgentExtension = helloExtension{}
	registry.Register(extension)
	if Name(extension) != "hello" || Description(extension) != "hello extension" {
		t.Fatalf("extension metadata mismatch: name=%q description=%q", Name(extension), Description(extension))
	}
	iter := registry.Iter()
	if len(iter) != 1 || iter[0].Name() != "hello" {
		t.Fatalf("iter mismatch: %#v", iter)
	}
	iter[0] = boomExtension{}
	if got := registry.Iter(); len(got) != 1 || got[0].Name() != "hello" {
		t.Fatalf("iter should not expose mutable registry internals: %#v", got)
	}
	ctx := ExtensionContext{CWD: t.TempDir(), SessionID: "t"}
	contributed, err := Init(extension, ctx)
	if err != nil || contributed.Banner != "ready" {
		t.Fatalf("extension init helper mismatch: contribution=%#v err=%v", contributed, err)
	}
	out := registry.InitAll(ctx)
	if len(out.Banners) != 1 || out.Banners[0] != "hello: ready" || len(out.Errors) != 0 {
		t.Fatalf("init output mismatch: %#v", out)
	}
	contribution := ExtensionContribution{Banner: "ready"}
	if contribution.Banner != "ready" {
		t.Fatalf("contribution alias mismatch: %#v", contribution)
	}
}

func TestExtensionDescriptionDefaultsEmpty(t *testing.T) {
	var extension AgentExtension = boomExtension{}
	if Description(extension) != "" {
		t.Fatalf("default description should be empty, got %q", Description(extension))
	}
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
