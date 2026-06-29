package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

func TestUpstreamHarnessToolConstructorsAreAvailable(t *testing.T) {
	cell := NewSkillHarnessCell()
	catalog := skills.NewCatalog(skills.CatalogOptions{})
	if !cell.Set(catalog) {
		t.Fatal("cell should accept first catalog")
	}

	install := NewInstallSkillToolFromHarnessCell(cell)
	if install.Root != DefaultSkillsRoot() || install.Catalog == nil {
		t.Fatalf("install constructor mismatch: %#v", install)
	}
	builder := NewSkillBuilderToolFromHarnessCell(cell)
	if builder.Root != DefaultSkillsRoot() || builder.Catalog == nil {
		t.Fatalf("builder constructor mismatch: %#v", builder)
	}
	remove := NewRemoveSkillToolFromHarnessCell(cell)
	if remove.Root != DefaultSkillsRoot() || remove.StateDir != DefaultBaseDir() || remove.Catalog == nil {
		t.Fatalf("remove constructor mismatch: %#v", remove)
	}
	setState := NewSetSkillStateToolFromHarnessCell(cell)
	if setState.BaseDir != DefaultBaseDir() || setState.Catalog == nil {
		t.Fatalf("set state constructor mismatch: %#v", setState)
	}
	skillTool := NewSkillToolFromHarnessCell(cell)
	var _ agent.Tool = skillTool
}

func TestHarnessCellConstructorsUseLiveCellCatalog(t *testing.T) {
	cell := NewSkillHarnessCell()
	provider := &recordingSkillProvider{skills: []skills.Skill{{Name: "go-port", Source: skills.SourceUser, Content: "Body"}}}
	if !cell.Set(provider) {
		t.Fatal("cell should accept provider")
	}
	tool := NewSkillToolFromHarnessCell(cell)
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "Skill", Arguments: map[string]any{"name": "go-port"}}, nil)
	if err != nil || result.Content == "" || provider.skillsCalls == 0 {
		t.Fatalf("skill tool should read live cell provider result=%#v err=%v calls=%d", result, err, provider.skillsCalls)
	}
	if _, err := cell.ReloadSkillsFromDisk(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.reloadCalls != 1 {
		t.Fatalf("cell reload should delegate to provider, calls=%d", provider.reloadCalls)
	}
}

func TestHarnessCellLifecycleConstructorsUseProviderSnapshot(t *testing.T) {
	cell := NewSkillHarnessCell()
	provider := &recordingSkillProvider{skills: []skills.Skill{{Name: "go-port", Source: skills.SourceUser, Content: "Body"}}}
	if !cell.Set(provider) {
		t.Fatal("cell should accept provider")
	}

	listResult, err := NewCatalogListSkillsTool(catalogFromSkillHarnessCell(cell)).Execute(context.Background(), ai.ToolCall{Name: "ListSkills", Arguments: map[string]any{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listResult.Content, "go-port") {
		t.Fatalf("list tool should use cell provider skills: %q", listResult.Content)
	}

	builder := NewSkillBuilderToolFromHarnessCell(cell).WithSkillsRoot(t.TempDir())
	_, err = builder.Execute(context.Background(), ai.ToolCall{Name: "SkillBuilder", Arguments: map[string]any{
		"name":         "new-skill",
		"description":  "New skill",
		"instructions": "Do it.",
		"confirm":      true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.reloadCalls != 1 {
		t.Fatalf("builder should reload through cell provider, calls=%d", provider.reloadCalls)
	}

	statePreview, err := NewSetSkillStateToolFromHarnessCell(cell).Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{
		"name":    "go-port",
		"enabled": false,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statePreview.Content, "skill=go-port") {
		t.Fatalf("set state should use cell provider skills: %q", statePreview.Content)
	}
}

type recordingSkillProvider struct {
	skills      []skills.Skill
	skillsCalls int
	reloadCalls int
}

func (provider *recordingSkillProvider) Skills() []skills.Skill {
	provider.skillsCalls++
	return append([]skills.Skill(nil), provider.skills...)
}

func (provider *recordingSkillProvider) ReloadSkillsFromDisk(ctx context.Context) (skills.LoadOutput, error) {
	provider.reloadCalls++
	return skills.LoadOutput{Skills: provider.Skills()}, nil
}
