package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

func TestSetSkillStateToolPreviewAndConfirm(t *testing.T) {
	dir := t.TempDir()
	tool := NewSetSkillStateTool(dir, []skills.Skill{{Name: "go-port", Source: skills.SourceUser}})
	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "enabled": false}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Content != "preview only — call again with `confirm: true` to apply. skill=go-port source=user currently=enabled target=disabled" {
		t.Fatalf("preview mismatch: %q", preview.Content)
	}
	if preview.Details["phase"] != "preview" || preview.Details["name"] != "go-port" || preview.Details["source"] != "user" || preview.Details["currently_enabled"] != true || preview.Details["target_enabled"] != false || preview.Details["no_change"] != false {
		t.Fatalf("preview details mismatch: %#v", preview.Details)
	}
	if _, ok := skills.LoadState(dir).Lookup("go-port", skills.SourceUser); ok {
		t.Fatal("preview should not persist state")
	}

	applied, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "enabled": false, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Content != "disabled skill 'go-port' (source: user)." {
		t.Fatalf("apply mismatch: %q", applied.Content)
	}
	if applied.Details["phase"] != "applied" || applied.Details["name"] != "go-port" || applied.Details["source"] != "user" || applied.Details["enabled"] != false || applied.Details["effective_enabled_after_reload"] != nil || applied.Details["audit_entry_id"] != nil {
		t.Fatalf("apply details mismatch: %#v", applied.Details)
	}
	if _, ok := applied.Details["before_enabled"]; ok {
		t.Fatalf("apply details should not include before_enabled: %#v", applied.Details)
	}
	if _, ok := applied.Details["after_enabled"]; ok {
		t.Fatalf("apply details should not include after_enabled: %#v", applied.Details)
	}
	entry, ok := skills.LoadState(dir).Lookup("go-port", skills.SourceUser)
	if !ok || entry.Enabled {
		t.Fatalf("persisted state mismatch: %#v ok=%v", entry, ok)
	}
}

func TestSetSkillStateToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewSetSkillStateTool(t.TempDir(), nil)
	if got, want := tool.Description(), "Enable or disable a loaded skill at runtime without editing its SKILL.md. The choice is recorded in a local overlay (~/.pie/skills-state.json) keyed by source+name and survives restarts. Works for any source — a builtin or project skill that can't be removed can still be disabled. Two-phase: first call previews (current vs target state); call again with `confirm: true` to apply. Disabling prevents the model from auto-invoking the skill via the Skill tool; the skill still appears in the catalog. Re-enabling a previously-disabled skill is a privileged control-plane write and requires explicit user confirmation through the runtime prompt card before it takes effect (issue #110); disabling does not prompt."; got != want {
		t.Fatalf("description mismatch:\n got: %q\nwant: %q", got, want)
	}
	params := tool.Parameters()
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties mismatch: %#v", params["properties"])
	}
	for _, key := range []string{"name", "source", "enabled", "confirm"} {
		if _, ok := properties[key].(map[string]any); !ok {
			t.Fatalf("missing property %s in %#v", key, properties)
		}
	}
	if source := properties["source"].(map[string]any); source["description"] == "" {
		t.Fatalf("source schema mismatch: %#v", source)
	}
	if enabled := properties["enabled"].(map[string]any); enabled["description"] != "Target state. `false` disables (no user prompt). `true` re-enables and triggers a user confirmation prompt before the change applies." {
		t.Fatalf("enabled schema mismatch: %#v", enabled)
	}
	if confirm := properties["confirm"].(map[string]any); confirm["default"] != false || confirm["description"] == "" {
		t.Fatalf("confirm schema mismatch: %#v", confirm)
	}
}

func TestCatalogSetSkillStateToolReloadsAndAudits(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go-port", "SKILL.md"), "---\nname: go-port\ndescription: Port Go\n---\nBody")
	catalog := skills.NewCatalog(skills.CatalogOptions{Dirs: []string{root}, StateDir: root})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	tool := NewCatalogSetSkillStateTool(catalog)
	applied, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "enabled": false, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Content != "disabled skill 'go-port' (source: user)." {
		t.Fatalf("set mismatch: %q", applied.Content)
	}
	if applied.Details["phase"] != "applied" || applied.Details["name"] != "go-port" || applied.Details["source"] != "user" || applied.Details["enabled"] != false || applied.Details["effective_enabled_after_reload"] != false || applied.Details["audit_entry_id"] != nil {
		t.Fatalf("catalog apply details mismatch: %#v", applied.Details)
	}
	skill, ok := findSkillByName(catalog.Skills(), "go-port")
	if !ok || !skill.DisableModelInvocation {
		t.Fatalf("catalog did not reload disabled state: %#v ok=%v", skill, ok)
	}
	audit := catalog.AuditLog()
	if len(audit) < 3 || audit[len(audit)-2].Operation != skills.CatalogAuditSetEnabled || audit[len(audit)-1].Operation != skills.CatalogAuditReload {
		t.Fatalf("audit mismatch: %#v", audit)
	}
}

func TestSetSkillStateToolRejectsMissingAndSourceMismatch(t *testing.T) {
	tool := NewSetSkillStateTool(t.TempDir(), []skills.Skill{{Name: "go-port", Source: skills.SourceProject}, {Name: "go-test", Source: skills.SourceUser}})
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{name: "missing name", arguments: map[string]any{"enabled": true}, want: "invalid arguments: missing field `name`"},
		{name: "non-string name", arguments: map[string]any{"name": 123, "enabled": true}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "missing enabled", arguments: map[string]any{"name": "go-port"}, want: "invalid arguments: missing field `enabled`"},
		{name: "non-bool enabled", arguments: map[string]any{"name": "go-port", "enabled": "true"}, want: "invalid arguments: invalid type: string \"true\", expected a boolean"},
		{name: "non-string source", arguments: map[string]any{"name": "go-port", "source": 123, "enabled": true}, want: "invalid arguments: invalid type: integer `123`, expected a string"},
		{name: "non-bool confirm", arguments: map[string]any{"name": "go-port", "enabled": true, "confirm": "true"}, want: "invalid arguments: invalid type: string \"true\", expected a boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: tc.arguments}, nil)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go", "enabled": true, "confirm": true}}, nil); err == nil || err.Error() != "no loaded skill named 'go'. Run /skills to list loaded skills. Did you mean: go-port, go-test?" {
		t.Fatalf("expected missing skill error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "source": "user", "enabled": true, "confirm": true}}, nil); err == nil || err.Error() != "skill 'go-port' is active from source 'project', not 'user'. Omit `source` or pass 'project' (the active source)." {
		t.Fatalf("expected source mismatch error, got %v", err)
	}
	uppercaseSource, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "source": "PROJECT", "enabled": false}}, nil)
	if err != nil {
		t.Fatalf("uppercase source should match active source: %v", err)
	}
	if uppercaseSource.Details["phase"] != "preview" || uppercaseSource.Details["source"] != "project" {
		t.Fatalf("uppercase source preview mismatch: %#v", uppercaseSource.Details)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "source": "unknown", "enabled": false}}, nil); err == nil || err.Error() != "invalid `source` (expected one of: builtin, user, project)" {
		t.Fatalf("expected invalid source error, got %v", err)
	}
}

func TestSetSkillStateToolParsesConfirmBeforeLookup(t *testing.T) {
	tool := NewSetSkillStateTool(t.TempDir(), nil)
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "missing-skill", "enabled": true, "confirm": "yes"}}, nil)
	if err == nil || err.Error() != "invalid arguments: invalid type: string \"yes\", expected a boolean" {
		t.Fatalf("expected confirm type error before skill lookup, got %v", err)
	}
}

func TestSetSkillStateToolPreviewNoChangeUsesWords(t *testing.T) {
	tool := NewSetSkillStateTool(t.TempDir(), []skills.Skill{{Name: "go-port", Source: skills.SourceUser}})
	preview, err := tool.Execute(context.Background(), ai.ToolCall{Name: "SetSkillState", Arguments: map[string]any{"name": "go-port", "enabled": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Content != "preview only — call again with `confirm: true` to apply. skill=go-port source=user currently=enabled target=enabled (no change)" {
		t.Fatalf("preview mismatch: %q", preview.Content)
	}
	if preview.Details["no_change"] != true {
		t.Fatalf("preview details mismatch: %#v", preview.Details)
	}
}

func TestSetSkillStateToolWithBaseDirMatchesUpstreamConstructor(t *testing.T) {
	baseDir := t.TempDir()
	tool := (SetSkillStateTool{}).WithBaseDir(baseDir)
	if tool.BaseDir != baseDir {
		t.Fatalf("set skill state base dir mismatch: %#v", tool)
	}
}
