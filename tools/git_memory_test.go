package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestGitToolStatusDiffLogAndRejectsWriteOps(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(dir, "file.txt"), "one\n")
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(dir, "file.txt"), "two\n")
	mustWrite(t, filepath.Join(dir, "untracked.txt"), "draft\n")

	gitTool := GitTool{}
	status, err := gitTool.Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status", "cwd": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Content, "git status") || !strings.Contains(status.Content, "file.txt") || !strings.Contains(status.Content, "?? untracked.txt") || !strings.Contains(status.Content, "## main") {
		t.Fatalf("status mismatch: %q", status.Content)
	}
	argv, ok := status.Details["argv"].([]string)
	if status.Details["subcommand"] != "status" || status.Details["exit_status"] != 0 || status.Details["truncated"] != false || !ok || strings.Join(argv, " ") != "status --short --branch" {
		t.Fatalf("status details mismatch: %#v", status.Details)
	}
	diff, err := gitTool.Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "diff", "cwd": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Content, "-one") || !strings.Contains(diff.Content, "+two") {
		t.Fatalf("diff mismatch: %q", diff.Content)
	}
	log, err := gitTool.Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "log", "cwd": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.Content, "initial") || !strings.Contains(log.Content, "Test") {
		t.Fatalf("log mismatch: %q", log.Content)
	}
	if _, err := gitTool.Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "commit", "cwd": dir}}, nil); err == nil {
		t.Fatal("write git subcommand should be rejected")
	}
}

func TestGitToolInvalidCWDReturnsSpawnError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "missing")
	_, err := (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status", "cwd": dir}}, nil)
	if err == nil || !strings.Contains(err.Error(), "spawn git:") {
		t.Fatalf("expected upstream-style spawn error, got %v", err)
	}
}

func TestGitToolUsesOriginalCWDForProcessSpawn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	cwd := dir + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".."
	_, err := (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status", "cwd": cwd}}, nil)
	if err == nil || !strings.Contains(err.Error(), "spawn git:") {
		t.Fatalf("git should spawn with the original cwd like upstream, got %v", err)
	}
}

func TestGitToolEmptyCWDMatchesUpstream(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, err := (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status", "cwd": ""}}, nil)
	if err == nil || err.Error() != "spawn git: No such file or directory (os error 2)" {
		t.Fatalf("git should pass empty cwd through like upstream, got %v", err)
	}
}

func TestGitToolRejectsInvalidUTF8Arguments(t *testing.T) {
	_, err := (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "subcommand must be valid UTF-8" {
		t.Fatalf("expected invalid subcommand error, got %v", err)
	}

	_, err = (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status", "cwd": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "cwd must be valid UTF-8" {
		t.Fatalf("expected invalid cwd error, got %v", err)
	}

	_, err = (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status", "args": []any{string([]byte{0xff})}}}, nil)
	if err == nil || err.Error() != "args[0] must be valid UTF-8" {
		t.Fatalf("expected invalid args error, got %v", err)
	}
}

func TestGitToolLossyDecodesOutputLikeUpstream(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(dir, "file.txt"), "old\n")
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte{'b', 'a', 'd', ' ', 0xff, '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "diff", "cwd": dir, "args": []any{"--text"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "bad �") || strings.Contains(result.Content, string([]byte{0xff})) {
		t.Fatalf("git output should be lossy-decoded like upstream, got %q", result.Content)
	}
}

func TestGitToolCancelledContextReturnsCancelled(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (GitTool{}).Execute(ctx, ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": "status"}}, nil)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("expected cancelled error, got %v", err)
	}
}

func TestGitToolSubcommandArgErrorMatchesUpstream(t *testing.T) {
	_, err := (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{}}, nil)
	if err == nil || err.Error() != "missing required arg: subcommand" {
		t.Fatalf("expected missing subcommand error, got %v", err)
	}

	_, err = (GitTool{}).Execute(context.Background(), ai.ToolCall{Name: "git", Arguments: map[string]any{"subcommand": 123}}, nil)
	if err == nil || err.Error() != "missing required arg: subcommand" {
		t.Fatalf("expected non-string subcommand error, got %v", err)
	}
}

func TestGitToolDefinitionMatchesUpstream(t *testing.T) {
	tool := GitTool{}
	wantDescription := "Run a read-only git subcommand (status / diff / log) with sensible defaults and structured output. Write/network operations go through bash so the permission policy can intercept them."
	if tool.Description() != wantDescription {
		t.Fatalf("git description mismatch:\nwant: %q\n got: %q", wantDescription, tool.Description())
	}
	params := tool.Parameters()
	properties := params["properties"].(map[string]any)
	if len(properties) != 3 || params["additionalProperties"] != false {
		t.Fatalf("git schema shape mismatch: %#v", params)
	}
	subcommand := properties["subcommand"].(map[string]any)
	if subcommand["type"] != "string" || subcommand["description"] != "Which git subcommand to run." {
		t.Fatalf("git subcommand property mismatch: %#v", subcommand)
	}
	args := properties["args"].(map[string]any)
	if args["type"] != "array" || args["description"] != "Extra arguments appended after the defaults (e.g. a file path or revision)." {
		t.Fatalf("git args property mismatch: %#v", args)
	}
	cwd := properties["cwd"].(map[string]any)
	if cwd["type"] != "string" || cwd["description"] != "Optional cwd for the git invocation. Defaults to the agent's cwd." {
		t.Fatalf("git cwd property mismatch: %#v", cwd)
	}
}

func TestMemoryToolSaveListReadForgetAndLoadBlock(t *testing.T) {
	dir := t.TempDir()
	memory := NewMemoryTool(dir)
	result, err := memory.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "User Pref", "description": "prefers concise answers", "content": "Use short answers.", "type": "user"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Saved memory `user-pref`") {
		t.Fatalf("save mismatch: %q", result.Content)
	}
	if result.Details["name"] != "user-pref" || result.Details["path"] != filepath.Join(dir, "user-pref.md") {
		t.Fatalf("save details mismatch: %#v", result.Details)
	}
	index, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if !strings.Contains(string(index), "[user-pref](user-pref.md)") {
		t.Fatalf("index mismatch: %s", index)
	}
	list, err := memory.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "list"}}, nil)
	if err != nil || !strings.Contains(list.Content, "user-pref") {
		t.Fatalf("list mismatch: %q err=%v", list.Content, err)
	}
	memories, ok := list.Details["memories"].([]string)
	if !ok || len(memories) != 1 || memories[0] != "user-pref" {
		t.Fatalf("list details mismatch: %#v", list.Details)
	}
	read, err := memory.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "read", "name": "user pref"}}, nil)
	if err != nil || !strings.Contains(read.Content, "Use short answers.") {
		t.Fatalf("read mismatch: %q err=%v", read.Content, err)
	}
	if read.Details["path"] != filepath.Join(dir, "user-pref.md") {
		t.Fatalf("read details mismatch: %#v", read.Details)
	}
	block := LoadMemoryBlock(dir)
	if !strings.HasPrefix(block, "<memory>") || !strings.Contains(block, "--- user-pref.md ---") {
		t.Fatalf("memory block mismatch: %q", block)
	}
	forget, err := memory.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "forget", "name": "user pref"}}, nil)
	if err != nil || !strings.Contains(forget.Content, "Forgot memory `user-pref`") {
		t.Fatalf("forget mismatch: %q err=%v", forget.Content, err)
	}
	if forget.Details["name"] != "user-pref" {
		t.Fatalf("forget details mismatch: %#v", forget.Details)
	}
	if _, err := os.Stat(filepath.Join(dir, "user-pref.md")); !os.IsNotExist(err) {
		t.Fatalf("memory file should be gone")
	}
}

func TestLoadMemoryBlockSkipsUnreadableEntriesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing.md"), filepath.Join(dir, "broken.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if block := LoadMemoryBlock(dir); block != "" {
		t.Fatalf("expected unreadable memory entries to be skipped, got %q", block)
	}
}

func TestLoadMemoryBlockSkipsInvalidUTF8LikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte{0xff, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if block := LoadMemoryBlock(dir); block != "" {
		t.Fatalf("expected invalid UTF-8 memory entries to be skipped, got %q", block)
	}
}

func TestLoadMemoryBlockExcludesMemoryIndexFileLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("INDEX_SENTINEL_SHOULD_NOT_LEAK\n- [tabs](tabs.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tabs.md"), []byte("---\nname: tabs\ndescription: indentation\nmetadata:\n  type: user\n---\n\nThe user prefers tabs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block := LoadMemoryBlock(dir)
	if !strings.Contains(block, "The user prefers tabs.") || strings.Contains(block, "INDEX_SENTINEL_SHOULD_NOT_LEAK") {
		t.Fatalf("memory block should include entries but exclude MEMORY.md index, got %q", block)
	}
}

func TestMemoryToolReadRejectsInvalidUTF8LikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte{0xff, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "read", "name": "bad"}}, nil)
	if err == nil || err.Error() != "read memory: stream did not contain valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 read memory error, got %v", err)
	}
}

func TestMemoryToolCreatesDirectoryForListAction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	result, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "list"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "[no memories]" {
		t.Fatalf("list content mismatch: %q", result.Content)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected memory directory to be created, info=%v err=%v", info, err)
	}
}

func TestMemoryToolListIncludesMarkdownDirectoriesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "folder.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "list"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	memories, ok := result.Details["memories"].([]string)
	if !ok || len(memories) != 1 || memories[0] != "folder" || !strings.Contains(result.Content, "  folder\n") {
		t.Fatalf("memory list should include .md directory names like upstream, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestMemoryToolForgetIgnoresUnreadableIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "MEMORY.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "forget", "name": "anything"}}, nil)
	if err != nil {
		t.Fatalf("forget should ignore unreadable index, got %v", err)
	}
	if result.Content != "Forgot memory `anything`." {
		t.Fatalf("forget content mismatch: %q", result.Content)
	}
}

func TestMemoryToolIndexPreservesBlankLines(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(indexPath, []byte("# Memory\n\n- [old](old.md) — old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "New", "description": "new", "content": "body"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Memory\n\n- [old](old.md) — old\n- [new](new.md) — new\n"
	if string(data) != want {
		t.Fatalf("index mismatch:\ngot  %q\nwant %q", data, want)
	}
}

func TestMemoryToolForgetIndexPreservesBlankLines(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(indexPath, []byte("# Memory\n\n- [old](old.md) — old\n- [keep](keep.md) — keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "forget", "name": "old"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Memory\n\n- [keep](keep.md) — keep\n"
	if string(data) != want {
		t.Fatalf("forget index mismatch:\ngot  %q\nwant %q", data, want)
	}
}

func TestMemoryToolSaveTreatsInvalidUTF8IndexAsEmptyLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(indexPath, []byte{'-', ' ', '[', 'o', 'l', 'd', ']', 0xff, '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "New", "description": "new", "content": "body"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "- [new](new.md) — new\n"
	if string(data) != want {
		t.Fatalf("invalid UTF-8 index save mismatch:\ngot  %q\nwant %q", data, want)
	}
}

func TestMemoryToolForgetLeavesInvalidUTF8IndexUnchangedLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "MEMORY.md")
	original := []byte{'-', ' ', '[', 'o', 'l', 'd', ']', 0xff, '\n'}
	if err := os.WriteFile(indexPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "forget", "name": "old"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("invalid UTF-8 index forget mismatch:\ngot  %q\nwant %q", data, original)
	}
}

func TestMemoryToolSaveWithEmptyIndexDoesNotPrependBlankLine(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(indexPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "New", "description": "new", "content": "body"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "- [new](new.md) — new\n"
	if string(data) != want {
		t.Fatalf("empty index mismatch:\ngot  %q\nwant %q", data, want)
	}
}

func TestMemoryToolForgetWithEmptyIndexStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(indexPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewMemoryTool(dir).Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "forget", "name": "old"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "" {
		t.Fatalf("empty forget index mismatch: %q", data)
	}
}

func TestMemoryToolSaveAllowsEmptyDescriptionAndContent(t *testing.T) {
	dir := t.TempDir()
	memory := NewMemoryTool(dir)
	result, err := memory.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "Empty Memory", "description": "", "content": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["name"] != "empty-memory" {
		t.Fatalf("save empty details mismatch: %#v", result.Details)
	}
	data, err := os.ReadFile(filepath.Join(dir, "empty-memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "description: \n") || !strings.HasSuffix(string(data), "---\n\n\n") {
		t.Fatalf("empty memory payload mismatch: %q", data)
	}
}

func TestMemorySlugifyMatchesUpstreamWhitespace(t *testing.T) {
	if got := SlugifyMemoryName("Alpha\rBeta\vGamma\fDelta"); got != "alpha-beta-gamma-delta" {
		t.Fatalf("slug mismatch: %q", got)
	}
}

func TestMemoryToolSavePreservesEmptyType(t *testing.T) {
	dir := t.TempDir()
	tool := NewMemoryTool(dir)
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "Empty Type", "description": "desc", "content": "body", "type": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "empty-type.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "metadata:\n  type: \n") {
		t.Fatalf("empty memory type should be preserved, got %q", data)
	}
}

func TestBuiltinToolsIncludesGitAndMemoryFactory(t *testing.T) {
	builtins := BuiltinTools()
	if builtins["git"] == nil {
		t.Fatal("missing git builtin")
	}
	if NewMemoryTool(t.TempDir()).Name() != "memory" {
		t.Fatal("memory tool metadata mismatch")
	}
}

func TestMemoryToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewMemoryTool(t.TempDir())
	wantDescription := "Persistent cross-session memory. action=save (requires name/description/content/optional type), action=list, action=read (requires name), action=forget (requires name). Saved entries are auto-injected into the system prompt of future sessions."
	if tool.Description() != wantDescription {
		t.Fatalf("memory description mismatch:\nwant: %q\n got: %q", wantDescription, tool.Description())
	}
	properties := tool.Parameters()["properties"].(map[string]any)
	wantDescriptions := map[string]string{
		"action":      "Operation to perform.",
		"name":        "Short kebab-case slug (required for save/read/forget).",
		"description": "One-line summary (save only).",
		"type":        "Memory category (e.g. user/feedback/project/reference). Default: user.",
		"content":     "Body of the memory (save only).",
	}
	for field, want := range wantDescriptions {
		property := properties[field].(map[string]any)
		if property["description"] != want {
			t.Fatalf("memory %s description mismatch: %#v", field, property)
		}
	}
}

func TestMemoryToolActionArgErrorMatchesUpstream(t *testing.T) {
	tool := NewMemoryTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{}}, nil)
	if err == nil || err.Error() != "missing `action` (save | list | read | forget)" {
		t.Fatalf("expected missing action error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": 123}}, nil)
	if err == nil || err.Error() != "missing `action` (save | list | read | forget)" {
		t.Fatalf("expected non-string action error, got %v", err)
	}
}

func TestMemoryToolSaveArgErrorsMatchUpstream(t *testing.T) {
	tool := NewMemoryTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": 123, "description": "desc", "content": "body"}}, nil)
	if err == nil || err.Error() != "missing `name`" {
		t.Fatalf("expected non-string name error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "name", "description": 123, "content": "body"}}, nil)
	if err == nil || err.Error() != "missing `description`" {
		t.Fatalf("expected non-string description error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "name", "description": "desc", "content": 123}}, nil)
	if err == nil || err.Error() != "missing `content`" {
		t.Fatalf("expected non-string content error, got %v", err)
	}
}

func TestMemoryToolRejectsInvalidUTF8Arguments(t *testing.T) {
	tool := NewMemoryTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "action must be valid UTF-8" {
		t.Fatalf("expected invalid action error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": string([]byte{0xff}), "description": "desc", "content": "body"}}, nil)
	if err == nil || err.Error() != "name must be valid UTF-8" {
		t.Fatalf("expected invalid name error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "name", "description": string([]byte{0xff}), "content": "body"}}, nil)
	if err == nil || err.Error() != "description must be valid UTF-8" {
		t.Fatalf("expected invalid description error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "save", "name": "name", "description": "desc", "content": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "content must be valid UTF-8" {
		t.Fatalf("expected invalid content error, got %v", err)
	}
}

func TestMemoryToolReadForgetNameArgErrorsMatchUpstream(t *testing.T) {
	tool := NewMemoryTool(t.TempDir())
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "read", "name": 123}}, nil)
	if err == nil || err.Error() != "missing `name`" {
		t.Fatalf("expected non-string read name error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "memory", Arguments: map[string]any{"action": "forget", "name": 123}}, nil)
	if err == nil || err.Error() != "missing `name`" {
		t.Fatalf("expected non-string forget name error, got %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
