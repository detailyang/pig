package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkillMarshalDoesNotHTMLEscapeLikeUpstreamSerdeJSON(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(Skill{Name: "<tag>&value", Description: "a < b && c > d", Content: "body <>&"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("skill JSON should not HTML-escape strings like upstream serde_json: %s", text)
	}
	if !strings.Contains(text, `"source":"user"`) {
		t.Fatalf("skill JSON should keep default user source, got %s", text)
	}
}

func TestLoadSkillsParsesFrontmatterAndRootMarkdown(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "my-skill", "SKILL.md"), "---\nname: my-skill\ndescription: Do useful things\ndisable_model_invocation: true\n---\nBody text\n")
	mustWrite(t, filepath.Join(root, "loose.md"), "---\ndescription: Loose root skill\n---\nLoose body\n")
	mustWrite(t, filepath.Join(root, "ignored", "SKILL.md"), "---\nname: ignored\ndescription: ignored\n---\nignored\n")
	mustWrite(t, filepath.Join(root, ".ignore"), "ignored\n")
	mustWrite(t, filepath.Join(root, "space", "space-ignored", "SKILL.md"), "---\nname: space-ignored\ndescription: not ignored upstream\n---\nspace ignored\n")
	mustWrite(t, filepath.Join(root, "space", ".ignore"), " space-ignored\n")
	mustWrite(t, filepath.Join(root, "fd-ignored", "SKILL.md"), "---\nname: fd-ignored\ndescription: fd ignored\n---\nfd ignored\n")
	mustWrite(t, filepath.Join(root, ".fdignore"), "fd-ignored\n")
	if err := os.Mkdir(filepath.Join(root, "my-skill", ".ignore"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %#v", out.Skills)
	}
	byName := map[string]Skill{}
	for _, skill := range out.Skills {
		byName[skill.Name] = skill
	}
	if byName["my-skill"].Content != "Body text" || !byName["my-skill"].DisableModelInvocation {
		t.Fatalf("frontmatter skill mismatch: %#v", byName["my-skill"])
	}
	rootName := filepath.Base(root)
	if byName[rootName].Description != "Loose root skill" || byName[rootName].Content != "Loose body" {
		t.Fatalf("root markdown skill mismatch: %#v", byName[rootName])
	}
}

func TestLoadSkillsRejectsInvalidUTF8SkillFileLikeUpstreamReadTextFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: bad-skill\ndescription: Bad skill\n---\nBody \xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticReadFailed || !strings.Contains(out.Diagnostics[0].Message, "invalid UTF-8") {
		t.Fatalf("invalid UTF-8 skill file should read-fail like upstream, skills=%#v diagnostics=%#v", out.Skills, out.Diagnostics)
	}
}

func TestSkillsUpstreamExportedNames(t *testing.T) {
	var source SkillSource = SourceProject
	if source.Label() != "project" {
		t.Fatalf("skill source alias mismatch: %q", source.Label())
	}

	var skill Skill = Skill{Name: "review", Description: "Review code", Source: SkillSourceUser}
	if skill.Source != SourceUser {
		t.Fatalf("skill source default alias mismatch: %#v", skill)
	}

	var frontmatter SkillFrontmatter = Frontmatter{Name: "review", HasName: true}
	if !frontmatter.HasName || frontmatter.Name != "review" {
		t.Fatalf("skill frontmatter alias mismatch: %#v", frontmatter)
	}

	var diagnostic SkillDiagnostic = Diagnostic{Code: SkillDiagnosticInvalidMetadata, Message: "bad", Path: "SKILL.md"}
	if diagnostic.Code != DiagnosticInvalidMetadata {
		t.Fatalf("skill diagnostic alias mismatch: %#v", diagnostic)
	}

	var output LoadSkillsOutput = LoadOutput{Skills: []Skill{skill}, Diagnostics: []Diagnostic{diagnostic}}
	if len(output.Skills) != 1 || len(output.Diagnostics) != 1 {
		t.Fatalf("load skills output alias mismatch: %#v", output)
	}

	formatted := FormatSkill(Skill{Name: "review", FilePath: "/tmp/review/SKILL.md", Content: "Body"}, "extra")
	if !strings.Contains(formatted, "<skill name=\"review\"") || !strings.Contains(formatted, "extra") {
		t.Fatalf("format skill alias mismatch: %q", formatted)
	}
}

func TestSkillsStateLoadSaveAliasesMatchUpstreamNames(t *testing.T) {
	baseDir := t.TempDir()
	state := SkillsState{}
	state.Set("review", SourceProject, false)
	if err := Save(baseDir, state); err != nil {
		t.Fatal(err)
	}
	loaded := Load(baseDir)
	entry, ok := loaded.Lookup("review", SourceProject)
	if !ok || entry.Enabled {
		t.Fatalf("load/save aliases mismatch: %#v", loaded)
	}
}

func TestDedupeProjectWinsReplacesSameName(t *testing.T) {
	combined := []Skill{{Name: "review", Source: SourceUser, FilePath: "/user/review/SKILL.md"}}
	DedupeProjectWins(&combined, Skill{Name: "review", Source: SourceProject, FilePath: "/repo/.pie/skills/review/SKILL.md"})
	DedupeProjectWins(&combined, Skill{Name: "lint", Source: SourceProject, FilePath: "/repo/.pie/skills/lint/SKILL.md"})

	if len(combined) != 2 || combined[0].Source != SourceProject || combined[0].FilePath != "/repo/.pie/skills/review/SKILL.md" || combined[1].Name != "lint" {
		t.Fatalf("deduped skills mismatch: %#v", combined)
	}
}

func TestLoadSkillsSiblingDirectoryOrderMatchesUpstreamStackTraversal(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha skill\n---\nAlpha body\n")
	mustWrite(t, filepath.Join(root, "beta", "SKILL.md"), "---\nname: beta\ndescription: Beta skill\n---\nBeta body\n")
	mustWrite(t, filepath.Join(root, "gamma", "SKILL.md"), "---\nname: gamma\ndescription: Gamma skill\n---\nGamma body\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	var names []string
	for _, skill := range out.Skills {
		names = append(names, skill.Name)
	}
	want := []string{"gamma", "beta", "alpha"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names=%#v want %#v", names, want)
	}
}

func TestLoadSkillsNestedIgnorePrefixMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "nested", "blocked", "SKILL.md"), "---\nname: blocked\ndescription: Blocked\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", ".ignore"), "blocked\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 0 {
		t.Fatalf("nested ignore should ignore nested/blocked like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsInvalidUTF8IgnoreFileReadFailsLikeUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "blocked", "SKILL.md"), "---\nname: blocked\ndescription: Blocked\n---\nBody\n")
	if err := os.WriteFile(filepath.Join(root, ".ignore"), []byte("blocked\n\xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := LoadSkills([]string{root})
	if len(out.Skills) != 1 || out.Skills[0].Name != "blocked" || len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticReadFailed || !strings.Contains(out.Diagnostics[0].Message, "invalid UTF-8") {
		t.Fatalf("invalid UTF-8 ignore file should read-fail and not ignore skills, skills=%#v diagnostics=%#v", out.Skills, out.Diagnostics)
	}
}

func TestLoadSkillsNestedIgnoreWildcardMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "nested", "blocked-one", "SKILL.md"), "---\nname: blocked-one\ndescription: Blocked one\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", "blocked-two", "SKILL.md"), "---\nname: blocked-two\ndescription: Blocked two\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", "kept", "SKILL.md"), "---\nname: kept\ndescription: Kept\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", ".ignore"), "blocked-*\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "kept" {
		t.Fatalf("nested wildcard ignore should keep only kept skill like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsNestedIgnoreRecursiveGlobMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "nested", "blocked", "one", "SKILL.md"), "---\nname: one\ndescription: One\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", "blocked", "two", "deep", "SKILL.md"), "---\nname: deep\ndescription: Deep\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", "kept", "SKILL.md"), "---\nname: kept\ndescription: Kept\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", ".ignore"), "blocked/**\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "kept" {
		t.Fatalf("nested recursive ignore should keep only kept skill like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsIgnoreRecursiveFileGlobMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "nested", "one", "SKILL.md"), "---\nname: one\ndescription: One\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "nested", "two", "deep", "SKILL.md"), "---\nname: deep\ndescription: Deep\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "root", "SKILL.md"), "---\nname: root\ndescription: Root\n---\nBody\n")
	mustWrite(t, filepath.Join(root, ".ignore"), "**/SKILL.md\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 0 {
		t.Fatalf("recursive file glob should ignore all SKILL.md files like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsIgnoreEscapedHashMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "#secret", "SKILL.md"), "---\nname: #secret\ndescription: Secret\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "kept", "SKILL.md"), "---\nname: kept\ndescription: Kept\n---\nBody\n")
	mustWrite(t, filepath.Join(root, ".ignore"), "\\#secret\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "kept" {
		t.Fatalf("escaped hash ignore should keep only kept skill like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsIgnoreUnescapedTrailingSpaceMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ignored", "SKILL.md"), "---\nname: ignored\ndescription: Ignored\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "kept", "SKILL.md"), "---\nname: kept\ndescription: Kept\n---\nBody\n")
	mustWrite(t, filepath.Join(root, ".ignore"), "ignored \n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "kept" {
		t.Fatalf("unescaped trailing space ignore should keep only kept skill like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsIgnoreEscapedTrailingSpaceMatchesUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ignored ", "SKILL.md"), "---\nname: ignored \ndescription: Ignored\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "ignored", "SKILL.md"), "---\nname: ignored\ndescription: Ignored plain\n---\nBody\n")
	mustWrite(t, filepath.Join(root, ".ignore"), "ignored\\ \n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "ignored" {
		t.Fatalf("escaped trailing space ignore should only ignore directory with literal space like upstream, got %#v", out.Skills)
	}
}

func TestLoadSkillsNestedIgnoreReplacesOuterRulesLikeUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".ignore"), "blocked\n")
	mustWrite(t, filepath.Join(root, "nested", ".ignore"), "other\n")
	mustWrite(t, filepath.Join(root, "nested", "blocked", "SKILL.md"), "---\nname: blocked\ndescription: Blocked\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "blocked" {
		t.Fatalf("nested ignore should replace outer ignore rules, got %#v", out.Skills)
	}
}

func TestLoadSkillsDoesNotFollowSymlinkedIgnoreFilesLikeUpstream(t *testing.T) {
	root := t.TempDir()
	ignoreFile := filepath.Join(t.TempDir(), "ignore-rules")
	mustWrite(t, ignoreFile, "blocked\n")
	mustWrite(t, filepath.Join(root, "blocked", "SKILL.md"), "---\nname: blocked\ndescription: Blocked\n---\nBody\n")
	if err := os.Symlink(ignoreFile, filepath.Join(root, ".ignore")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "blocked" {
		t.Fatalf("symlinked ignore file should be skipped, got %#v", out.Skills)
	}
}

func TestLoadSkillsBrokenSymlinkFailsDirectoryListingLikeUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "valid", "SKILL.md"), "---\nname: valid\ndescription: Valid skill\n---\nBody\n")
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "broken-link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 {
		t.Fatalf("broken symlink should abort directory listing, got skills %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticListFailed || out.Diagnostics[0].Path != root {
		t.Fatalf("expected root list_failed diagnostic, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsFollowsDirectorySymlinkLikeUpstream(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	mustWrite(t, filepath.Join(target, "linked-skill", "SKILL.md"), "---\nname: linked-skill\ndescription: Linked skill\n---\nBody\n")
	if err := os.Symlink(target, filepath.Join(root, "linked-root")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 2 || out.Skills[0].Name != "linked-skill" || out.Skills[1].Name != "linked-skill" {
		t.Fatalf("child directory and symlink should both be walked, got %#v", out.Skills)
	}
}

func TestLoadSkillsFollowsExternalDirectorySymlinkLikeUpstream(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	mustWrite(t, filepath.Join(target, "linked-skill", "SKILL.md"), "---\nname: linked-skill\ndescription: Linked skill\n---\nBody\n")
	if err := os.Symlink(target, filepath.Join(root, "linked-root")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "linked-skill" {
		t.Fatalf("external directory symlink should be followed, got %#v", out.Skills)
	}
}

func TestLoadSkillsRootSymlinkKeepsSymlinkPathLikeUpstream(t *testing.T) {
	target := t.TempDir()
	mustWrite(t, filepath.Join(target, "linked-skill", "SKILL.md"), "---\nname: linked-skill\ndescription: Linked skill\n---\nBody\n")
	rootLink := filepath.Join(t.TempDir(), "skills-link")
	if err := os.Symlink(target, rootLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	out := LoadSkills([]string{rootLink})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	wantPath := filepath.Join(rootLink, "linked-skill", "SKILL.md")
	if len(out.Skills) != 1 || out.Skills[0].FilePath != wantPath {
		t.Fatalf("root symlink should keep symlink path %q, got %#v", wantPath, out.Skills)
	}
}

func TestLoadSourcedSkillsTagsEachRootAndClonesDiagnosticsLikeUpstream(t *testing.T) {
	userRoot := t.TempDir()
	projectRoot := t.TempDir()
	mustWrite(t, filepath.Join(userRoot, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha skill\n---\nAlpha body\n")
	mustWrite(t, filepath.Join(projectRoot, "beta", "SKILL.md"), "---\nname: wrong-name\ndescription: Beta skill\n---\nBeta body\n")
	mustWrite(t, filepath.Join(projectRoot, "missing-description", "SKILL.md"), "---\nname: missing-description\n---\nBody\n")

	loaded := LoadSourcedSkills([]SourcedInput{{Dir: userRoot, Source: SourceUser}, {Dir: projectRoot, Source: SourceProject}})
	if len(loaded) != 2 {
		t.Fatalf("expected loaded skills only, got %#v", loaded)
	}
	if loaded[0].Skill.Name != "alpha" || loaded[0].Source != SourceUser || len(loaded[0].Diagnostics) != 0 {
		t.Fatalf("user sourced skill mismatch: %#v", loaded[0])
	}
	if loaded[1].Skill.Name != "wrong-name" || loaded[1].Source != SourceProject || len(loaded[1].Diagnostics) != 2 {
		t.Fatalf("project sourced skill should clone all root diagnostics, got %#v", loaded[1])
	}
}

func TestLoadAllProjectOverridesUserLikeUpstreamCodingAgent(t *testing.T) {
	pieDir := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("PIE_DIR", pieDir)
	userDir, projectDir := SkillsDirs(cwd)
	mustWrite(t, filepath.Join(userDir, "review", "SKILL.md"), "---\nname: review\ndescription: user\n---\nUser body")
	mustWrite(t, filepath.Join(projectDir, "review", "SKILL.md"), "---\nname: review\ndescription: project\n---\nProject body")
	mustWrite(t, filepath.Join(userDir, "plan", "SKILL.md"), "---\nname: plan\ndescription: user plan\n---\nPlan body")

	loaded := LoadAll(cwd)
	if len(loaded.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", loaded.Diagnostics)
	}
	if len(loaded.Skills) != 2 {
		t.Fatalf("skills mismatch: %#v", loaded.Skills)
	}
	review := findSkillByName(loaded.Skills, "review")
	if review == nil || review.Description != "project" || review.Source != SourceProject || review.Content != "Project body" {
		t.Fatalf("project skill should override user skill like upstream load_all, got %#v", review)
	}
	plan := findSkillByName(loaded.Skills, "plan")
	if plan == nil || plan.Source != SourceUser {
		t.Fatalf("user skill should be retained with user source, got %#v", plan)
	}
}

func findSkillByName(skills []Skill, name string) *Skill {
	for index := range skills {
		if skills[index].Name == name {
			return &skills[index]
		}
	}
	return nil
}

func TestLoadSkillsDiagnosticsForInvalidMetadata(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "bad-name", "SKILL.md"), "---\nname: BadName\ndescription: ok\n---\nbody\n")
	mustWrite(t, filepath.Join(root, "no-desc", "SKILL.md"), "---\nname: no-desc\n---\nbody\n")
	longName := strings.Repeat("a", maxNameLength+1)
	mustWrite(t, filepath.Join(root, longName, "SKILL.md"), "---\nname: "+longName+"\ndescription: long name\n---\nbody\n")
	longDescription := strings.Repeat("d", maxDescriptionLength+1)
	mustWrite(t, filepath.Join(root, "long-description", "SKILL.md"), "---\nname: long-description\ndescription: "+longDescription+"\n---\nbody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 3 {
		t.Fatalf("expected invalid-name skills still loaded and no-desc skipped, got %#v", out.Skills)
	}
	if len(out.Diagnostics) < 2 {
		t.Fatalf("expected diagnostics, got %#v", out.Diagnostics)
	}
	if out.Diagnostics[0].Code != DiagnosticInvalidMetadata {
		t.Fatalf("expected invalid metadata diagnostics, got %#v", out.Diagnostics)
	}
	foundLongName := false
	for _, diagnostic := range out.Diagnostics {
		if diagnostic.Message == "name exceeds 64 characters (65)" {
			foundLongName = true
		}
	}
	if !foundLongName {
		t.Fatalf("expected long-name diagnostic with actual count, got %#v", out.Diagnostics)
	}
	foundLongDescription := false
	for _, diagnostic := range out.Diagnostics {
		if diagnostic.Message == "description exceeds 1024 characters (1025)" {
			foundLongDescription = true
		}
	}
	if !foundLongDescription {
		t.Fatalf("expected long-description diagnostic with actual count, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsEmptyNameDiagnosticsMatchUpstream(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "empty-name", "SKILL.md"), "---\nname: \"\"\ndescription: Empty name\n---\nbody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 1 || out.Skills[0].Name != "" {
		t.Fatalf("empty-name skill mismatch: %#v", out.Skills)
	}
	var messages []string
	for _, diagnostic := range out.Diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	if strings.Join(messages, "|") != "name \"\" does not match parent directory \"empty-name\"" {
		t.Fatalf("empty-name diagnostics mismatch: %#v", messages)
	}
}

func TestLoadSkillsRootMarkdownDefaultsNameToRootDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root-skill")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "loose.md"), "---\ndescription: Loose root skill\n---\nLoose body\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "root-skill" {
		t.Fatalf("root markdown name mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsParsesFoldedDescriptionBlock(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "folded", "SKILL.md"), "---\nname: folded\ndescription: >\n  first line\n  second line\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "first line second line\n" {
		t.Fatalf("folded description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsParsesStripChompDescriptionBlockLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "strip", "SKILL.md"), "---\nname: strip\ndescription: |-\n  first line\n  second line\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "first line\nsecond line" {
		t.Fatalf("strip chomp literal description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsParsesKeepChompDescriptionBlockLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "keep", "SKILL.md"), "---\nname: keep\ndescription: >+\n  first line\n  second line\n\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "first line second line\n\n" {
		t.Fatalf("keep chomp folded description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsParsesIndentIndicatorDescriptionBlockLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "indent", "SKILL.md"), "---\nname: indent\ndescription: >2\n    first line\n    second line\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "  first line\n  second line\n" {
		t.Fatalf("indent indicator folded description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsParsesCombinedBlockIndicatorsLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "combo", "SKILL.md"), "---\nname: combo\ndescription: >+2\n    first line\n    second line\n\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "  first line\n  second line\n\n" {
		t.Fatalf("combined block indicator description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsAutoDetectsBlockIndentLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "auto-indent", "SKILL.md"), "---\nname: auto-indent\ndescription: >\n    first line\n      second line\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "first line\n  second line\n" {
		t.Fatalf("auto indent folded description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsFoldedBlockBlankLineMatchesSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "blank", "SKILL.md"), "---\nname: blank\ndescription: >\n  first line\n\n  second line\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "first line\nsecond line\n" {
		t.Fatalf("folded blank line description mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsRejectsInvalidBlockHeaderLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "invalid", "SKILL.md"), "---\nname: invalid\ndescription: |0\n  body\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 {
		t.Fatalf("invalid block header should fail to load, got %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "indentation indicator") {
		t.Fatalf("expected parse diagnostic for invalid block header, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsParsesInlineYamlComments(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "commented", "SKILL.md"), "---\nname: commented\ndescription: useful skill # catalog note\ndisable_model_invocation: true # locked\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "useful skill" || !out.Skills[0].DisableModelInvocation {
		t.Fatalf("inline comment parse mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsParsesCaseInsensitiveBoolLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "locked", "SKILL.md"), "---\nname: locked\ndescription: Locked\ndisable_model_invocation: TRUE\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || !out.Skills[0].DisableModelInvocation {
		t.Fatalf("uppercase bool should parse like serde_yaml, got %#v", out.Skills)
	}
}

func TestLoadSkillsParsesQuotedScalarWithTrailingComment(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "quoted", "SKILL.md"), "---\nname: quoted\ndescription: \"quoted # value\" # catalog note\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "quoted # value" {
		t.Fatalf("quoted scalar parse mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsUnescapesQuotedScalarsLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "quoted-escape", "SKILL.md"), "---\nname: quoted-escape\ndescription: \"line\\nfeed\"\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "line\nfeed" {
		t.Fatalf("quoted scalar parse mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsHandlesEscapedDoubleQuotesLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "quoted-quote", "SKILL.md"), "---\nname: quoted-quote\ndescription: \"say \\\"hello\\\"\"\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "say \"hello\"" {
		t.Fatalf("escaped double quote parse mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsUnescapesSingleQuotedScalarsLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "single-quoted", "SKILL.md"), "---\nname: single-quoted\ndescription: 'it''s useful'\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "it's useful" {
		t.Fatalf("single quoted scalar parse mismatch: %#v", out.Skills)
	}
}

func TestLoadSkillsRejectsStringDisableModelInvocation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "locked", "SKILL.md"), "---\nname: locked\ndescription: Locked\ndisable_model_invocation: \"true\"\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 {
		t.Fatalf("quoted bool frontmatter should fail to load, got %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "invalid type") {
		t.Fatalf("expected parse diagnostic for quoted bool, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsParsesNumericNameLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "numeric", "SKILL.md"), "---\nname: 123\ndescription: Numeric name\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 1 || out.Skills[0].Name != "123" {
		t.Fatalf("numeric name should parse as string and load, got %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticInvalidMetadata || !strings.Contains(out.Diagnostics[0].Message, "does not match parent directory") {
		t.Fatalf("expected parent mismatch diagnostic for string numeric name, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsParsesNumericDescriptionLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "numeric-description", "SKILL.md"), "---\nname: numeric-description\ndescription: 1.5\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Description != "1.5" {
		t.Fatalf("numeric description should parse as string, got %#v", out.Skills)
	}
}

func TestLoadSkillsParsesStringAnchorAliasLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "anchored", "SKILL.md"), "---\nname: &skill_name anchored\ndescription: *skill_name\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "anchored" || out.Skills[0].Description != "anchored" {
		t.Fatalf("string anchor alias should parse like serde_yaml, got %#v", out.Skills)
	}
}

func TestLoadSkillsParsesAliasWithInlineCommentLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "anchored", "SKILL.md"), "---\nname: &skill_name anchored # name comment\ndescription: *skill_name # desc comment\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "anchored" || out.Skills[0].Description != "anchored" {
		t.Fatalf("alias with inline comment should parse like serde_yaml, got %#v", out.Skills)
	}
}

func TestLoadSkillsParsesQuotedAnchorAliasLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "quoted-anchor", "SKILL.md"), "---\nname: quoted-anchor\ndescription: &desc \"anchored\\nvalue\"\n---\nBody\n")
	mustWrite(t, filepath.Join(root, "alias", "SKILL.md"), "---\nname: alias\ndescription: *desc\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 1 || out.Skills[0].Name != "quoted-anchor" || out.Skills[0].Description != "anchored\nvalue" {
		t.Fatalf("quoted anchor should parse like serde_yaml, got skills=%#v diagnostics=%#v", out.Skills, out.Diagnostics)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "unknown anchor") {
		t.Fatalf("anchors should be scoped per file; expected alias diagnostic, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsRejectsUnknownAliasLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "missing-alias", "SKILL.md"), "---\nname: missing-alias\ndescription: *missing\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 {
		t.Fatalf("unknown alias should fail to load, got %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "unknown anchor") {
		t.Fatalf("expected unknown anchor parse diagnostic, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsRejectsSequenceDescription(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sequence-description", "SKILL.md"), "---\nname: sequence-description\ndescription: [one, two]\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 {
		t.Fatalf("sequence description frontmatter should fail to load, got %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "invalid type") {
		t.Fatalf("expected parse diagnostic for sequence description, got %#v", out.Diagnostics)
	}
}

func TestLoadSkillsNullNameFallsBackToParent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "null-name", "SKILL.md"), "---\nname: null\ndescription: Null name\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "null-name" {
		t.Fatalf("null name should fall back to parent directory, got %#v", out.Skills)
	}
}

func TestLoadSkillsUppercaseNullNameFallsBackToParentLikeSerdeYAML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "null-name", "SKILL.md"), "---\nname: Null\ndescription: Null name\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "null-name" {
		t.Fatalf("uppercase null name should fall back to parent, got %#v", out.Skills)
	}
}

func TestLoadSkillsEmptyYamlNameFallsBackToParent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "empty-yaml-name", "SKILL.md"), "---\nname:\ndescription: Empty yaml name\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", out.Diagnostics)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "empty-yaml-name" {
		t.Fatalf("empty yaml name should fall back to parent directory, got %#v", out.Skills)
	}
}

func TestLoadSkillsRejectsDuplicateFrontmatterField(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "dupe", "SKILL.md"), "---\nname: dupe\ndescription: first\ndescription: second\n---\nBody\n")

	out := LoadSkills([]string{root})
	if len(out.Skills) != 0 {
		t.Fatalf("duplicate frontmatter field should fail to load, got %#v", out.Skills)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != DiagnosticParseFailed || !strings.Contains(out.Diagnostics[0].Message, "duplicate field") {
		t.Fatalf("expected duplicate field parse diagnostic, got %#v", out.Diagnostics)
	}
}

func TestFormatSkillInvocationAndSystemPrompt(t *testing.T) {
	skill := Skill{Name: "my-skill", Description: "Do things", FilePath: "/abs/skills/my-skill/SKILL.md", Content: "hello", Source: SourceUser}
	invocation := FormatSkillInvocation(skill, "EXTRA")
	if !strings.Contains(invocation, `<skill name="my-skill" location="/abs/skills/my-skill/SKILL.md">`) || !strings.Contains(invocation, "References are relative to /abs/skills/my-skill.") || !strings.HasSuffix(invocation, "EXTRA") {
		t.Fatalf("invocation mismatch: %q", invocation)
	}
	rootInvocation := FormatSkillInvocation(Skill{Name: "root-skill", FilePath: "SKILL.md", Content: "body"}, "")
	if !strings.Contains(rootInvocation, "References are relative to /.") {
		t.Fatalf("root invocation dirname mismatch: %q", rootInvocation)
	}
	disabled := Skill{Name: "disabled-skill", Description: "Still listed", DisableModelInvocation: true, Source: SourceUser}
	prompt := FormatSkillsForSystemPrompt([]Skill{skill, disabled})
	if !strings.HasPrefix(prompt, "<skills>\n") || !strings.Contains(prompt, "- name: my-skill\n  description: Do things\n") || !strings.Contains(prompt, "- name: disabled-skill\n  description: Still listed\n") || !strings.HasSuffix(prompt, "</skills>") {
		t.Fatalf("prompt mismatch: %q", prompt)
	}
	if FormatSkillsForSystemPrompt(nil) != "" {
		t.Fatal("empty skills should render empty prompt")
	}
}

func TestFormatSkillsForSystemPromptUpstreamWrapper(t *testing.T) {
	skills := []Skill{{Name: "alpha", Description: "first"}, {Name: "beta", Description: "second"}}
	wrapped := FormatSkillsForSystemPromptUpstream(skills)
	plain := FormatSkillsForSystemPrompt(skills)
	if wrapped != plain {
		t.Fatalf("upstream wrapper mismatch:\nwrapped=%q\nplain=%q", wrapped, plain)
	}
	if !strings.Contains(wrapped, "- name: alpha\n  description: first\n") || !strings.HasSuffix(wrapped, "</skills>") {
		t.Fatalf("upstream system prompt shape mismatch: %q", wrapped)
	}
}

func TestSkillsStateApplySaveLoad(t *testing.T) {
	dir := t.TempDir()
	state := SkillsState{}
	state.Set("foo", SourceUser, false)
	state.Set("bar", SourceProject, true)
	if !state.Remove("bar", SourceProject) {
		t.Fatal("expected remove")
	}
	if err := SaveState(dir, state); err != nil {
		t.Fatal(err)
	}
	loaded := LoadState(dir)
	if entry, ok := loaded.Lookup("foo", SourceUser); !ok || entry.Enabled {
		t.Fatalf("state mismatch: %#v ok=%v", entry, ok)
	}
	skills := []Skill{{Name: "foo", Source: SourceUser}, {Name: "foo", Source: SourceProject}}
	loaded.Apply(skills)
	if !skills[0].DisableModelInvocation || skills[1].DisableModelInvocation {
		t.Fatalf("apply mismatch: %#v", skills)
	}
	other := []Skill{{Name: "foo", Source: SourceUser}}
	Apply(loaded, other)
	if !other[0].DisableModelInvocation {
		t.Fatalf("package-level Apply mismatch: %#v", other)
	}
}

func TestSaveStateDoesNotClobberPreexistingTempFileLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "."+STATE_FILE+".tmp")
	if err := os.WriteFile(tmpPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := SkillsState{}
	state.Set("foo", SourceUser, false)
	if err := SaveState(dir, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("preexisting temp file should survive unique upstream save: %v", err)
	}
	if string(data) != "keep me" {
		t.Fatalf("preexisting temp file was clobbered: %q", data)
	}
}

func TestSaveStateDoesNotHTMLEscapeLikeUpstreamSerdeJSON(t *testing.T) {
	dir := t.TempDir()
	state := SkillsState{}
	state.Set("<tag>&value", SourceUser, false)
	if err := SaveState(dir, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("skills-state.json should not HTML-escape strings like upstream serde_json: %s", text)
	}
	if !strings.Contains(text, `"name": "<tag>&value"`) {
		t.Fatalf("skills-state.json missing unescaped skill name: %s", text)
	}
}

func TestSkillsStateUpstreamPublicNames(t *testing.T) {
	if STATE_FILE != "skills-state.json" || StatePath(t.TempDir()) == "" {
		t.Fatalf("state file mismatch: %q", STATE_FILE)
	}
	state := SkillsState{Overrides: []SkillStateEntry{{Name: "foo", Source: SkillSourceUser, Enabled: false}}}
	entry, ok := state.Lookup("foo", SkillSourceUser)
	if !ok || entry.Name != "foo" || entry.Source != SourceUser || entry.Enabled {
		t.Fatalf("entry mismatch: %#v ok=%v", entry, ok)
	}
}

func TestLoadStateIgnoresInvalidUTF8LikeUpstreamReadToString(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(StatePath(dir), []byte("{\"overrides\":[{\"name\":\"foo\",\"source\":\"user\",\"enabled\":false}],\"note\":\"\xff\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := LoadState(dir)
	if _, ok := loaded.Lookup("foo", SourceUser); ok || len(loaded.Overrides) != 0 {
		t.Fatalf("invalid UTF-8 skills-state.json should be ignored like upstream, got %#v", loaded)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
