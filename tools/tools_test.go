package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/triggers"
)

func TestReadWriteEditAndListTools(t *testing.T) {
	dir := t.TempDir()
	write := WriteTool{}
	if _, err := write.Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": filepath.Join(dir, "nested", "hello.txt"), "content": "hello old world"}}, nil); err != nil {
		t.Fatal(err)
	}

	read := ReadTool{}
	result, err := read.Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": filepath.Join(dir, "nested", "hello.txt")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "hello old world") {
		t.Fatalf("read content mismatch: %q", result.Content)
	}

	edit := EditTool{}
	if _, err := edit.Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": filepath.Join(dir, "nested", "hello.txt"), "old_string": "old", "new_string": "new"}}, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "nested", "hello.txt"))
	if string(data) != "hello new world" {
		t.Fatalf("edit mismatch: %q", data)
	}

	ls := LSTool{}
	result, err = ls.Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "nested/") {
		t.Fatalf("ls output mismatch: %q", result.Content)
	}
	var upstreamLs LsTool = LSTool{}
	if upstreamLs.Name() != "ls" {
		t.Fatalf("LsTool alias mismatch")
	}
}

func TestEditRequiresUniqueOldString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	if err := os.WriteFile(path, []byte("x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "x", "new_string": "y"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "old_string matched 2 times") || !strings.Contains(err.Error(), "replace_all=true") {
		t.Fatalf("expected multiple occurrence error, got %v", err)
	}
}

func TestEditReplaceAllHandlesMultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	if err := os.WriteFile(path, []byte("x x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "x", "new_string": "y", "replace_all": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "y y y" || !strings.Contains(result.Content, "3 replacement") {
		t.Fatalf("replace_all mismatch: content=%q result=%q", data, result.Content)
	}
}

func TestEditToolReportsDiffPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preview.txt")
	oldText := "alpha\nbeta\n"
	newText := "alpha\ngamma\n"
	if err := os.WriteFile(path, []byte("before\n"+oldText+"after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": oldText, "new_string": newText}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--- before\n", "- alpha\n", "- beta\n", "+++ after\n", "+ alpha\n", "+ gamma\n"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("edit preview missing %q in %q", want, result.Content)
		}
	}
}

func TestEditToolAllowsEmptyReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "delete.txt")
	if err := os.WriteFile(path, []byte("keep remove keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": " remove", "new_string": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep keep" || !strings.Contains(result.Content, "+++ after\n") || strings.Contains(result.Content, "+++ after\n+ ") {
		t.Fatalf("empty replacement mismatch: content=%q result=%q", data, result.Content)
	}
}

func TestFileToolsReturnStructuredDetails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "hello.txt")
	writeResult, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": path, "content": "one\ntwo\n"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if writeResult.Details["path"] != path || writeResult.Details["bytes"] != len("one\ntwo\n") || writeResult.Details["lines"] != 2 {
		t.Fatalf("write details mismatch: %#v", writeResult.Details)
	}

	editResult, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "o", "new_string": "O", "replace_all": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if editResult.Details["path"] != path || editResult.Details["replacements"] != 2 || editResult.Details["replaceAll"] != true {
		t.Fatalf("edit details mismatch: %#v", editResult.Details)
	}

	lsResult, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lsResult.Details["path"] != dir || lsResult.Details["totalEntries"] != 1 || lsResult.Details["shownEntries"] != 1 {
		t.Fatalf("ls details mismatch: %#v", lsResult.Details)
	}
}

func TestLSToolFloatLimitFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": 2.0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["shownEntries"] != 3 || strings.Contains(result.Content, "[truncated:") {
		t.Fatalf("float limit should fall back to default like serde_json::Value::as_u64, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestLSToolLargeUnsignedLimitDoesNotOverflowLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": uint64(math.MaxUint64)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["shownEntries"] != 3 || strings.Contains(result.Content, "[truncated:") {
		t.Fatalf("large unsigned limit should not overflow before listing, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestWriteToolLineCountMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "single.txt")
	result, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": path, "content": "hello"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["lines"] != 1 {
		t.Fatalf("single-line write details mismatch: %#v", result.Details)
	}
	if result.Content != fmt.Sprintf("Wrote 5 bytes (1 lines) to %s", path) {
		t.Fatalf("write content mismatch: %q", result.Content)
	}

	emptyPath := filepath.Join(dir, "empty.txt")
	emptyResult, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": emptyPath, "content": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if emptyResult.Details["lines"] != 0 {
		t.Fatalf("empty write details mismatch: %#v", emptyResult.Details)
	}

	trailingPath := filepath.Join(dir, "trailing.txt")
	trailingResult, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": trailingPath, "content": "one\n\n"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if trailingResult.Details["lines"] != 2 || trailingResult.Content != fmt.Sprintf("Wrote 5 bytes (2 lines) to %s", trailingPath) {
		t.Fatalf("trailing empty line write mismatch: content=%q details=%#v", trailingResult.Content, trailingResult.Details)
	}
}

func TestCoreToolParameterIntegerSchemaMatchesUpstream(t *testing.T) {
	cases := []struct {
		tool   interface{ Parameters() map[string]any }
		fields []string
	}{
		{tool: ReadTool{}, fields: []string{"offset", "limit"}},
		{tool: LSTool{}, fields: []string{"limit"}},
		{tool: FindTool{}, fields: []string{"limit"}},
		{tool: GrepTool{}, fields: []string{"limit"}},
	}
	for _, tc := range cases {
		properties := tc.tool.Parameters()["properties"].(map[string]any)
		for _, field := range tc.fields {
			property := properties[field].(map[string]any)
			if property["type"] != "integer" {
				t.Fatalf("%T %s type mismatch: %#v", tc.tool, field, property["type"])
			}
		}
	}
}

func TestCoreToolParameterDescriptionsMatchUpstream(t *testing.T) {
	cases := []struct {
		name       string
		params     map[string]any
		properties map[string]string
	}{
		{name: "read", params: ReadTool{}.Parameters(), properties: map[string]string{"path": "Path to the file (relative or absolute)", "offset": "Line to start reading from (1-indexed)", "limit": "Max lines to read"}},
		{name: "write", params: WriteTool{}.Parameters(), properties: map[string]string{"path": "Path to the file (relative or absolute)", "content": "Full file contents"}},
		{name: "edit", params: EditTool{}.Parameters(), properties: map[string]string{"path": "Path to the file (relative or absolute)", "old_string": "Exact substring to replace. Include enough surrounding context to make it unique within the file.", "new_string": "Replacement string. Use the empty string to delete.", "replace_all": "Replace every occurrence rather than requiring uniqueness."}},
		{name: "ls", params: LSTool{}.Parameters(), properties: map[string]string{"path": "Directory to list (default: current directory)", "limit": "Max entries (default 500)"}},
		{name: "find", params: FindTool{}.Parameters(), properties: map[string]string{"glob": "Filename glob (e.g. *.rs, README*)", "path": "Directory to search (default: current)", "limit": "Max path count"}},
		{name: "grep", params: GrepTool{}.Parameters(), properties: map[string]string{"pattern": "Regex pattern", "path": "Directory to search (default: current)", "glob": "Optional filename glob (e.g. *.rs)", "case_insensitive": "Case-insensitive match", "limit": "Max match count"}},
	}
	for _, tc := range cases {
		properties := tc.params["properties"].(map[string]any)
		if len(properties) != len(tc.properties) {
			t.Fatalf("%s property count mismatch: want %d got %d (%#v)", tc.name, len(tc.properties), len(properties), properties)
		}
		for field, want := range tc.properties {
			property, ok := properties[field].(map[string]any)
			if !ok {
				t.Fatalf("%s missing property %s in %#v", tc.name, field, properties)
			}
			if property["description"] != want {
				t.Fatalf("%s.%s description mismatch:\nwant: %q\n got: %#v", tc.name, field, want, property["description"])
			}
		}
	}
}

func TestCoreToolDescriptionsMatchUpstream(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "read", got: ReadTool{}.Description(), want: "Read the contents of a UTF-8 text file. Use offset/limit for large files; output is truncated to 2000 lines or 256 KiB (whichever first)."},
		{name: "write", got: WriteTool{}.Description(), want: "Write (or overwrite) a UTF-8 text file. Parent directories are created if missing."},
		{name: "edit", got: EditTool{}.Description(), want: "Replace an exact substring in a file. The substring must be unique unless `replace_all` is true. Use `read` first to confirm the exact text to match, including surrounding context."},
		{name: "ls", got: LSTool{}.Description(), want: "List directory entries, sorted alphabetically. Directories are suffixed with '/'. Truncated to 500 entries."},
		{name: "find", got: FindTool{}.Description(), want: "Find files by filename glob. Honors .gitignore. Output limited to 200 paths by default; use `limit` only when a larger result set is necessary."},
		{name: "grep", got: GrepTool{}.Description(), want: "Search files for lines matching a regex. Honors .gitignore. Output limited to 200 matches."},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s description mismatch:\nwant: %q\n got: %q", tc.name, tc.want, tc.got)
		}
	}
}

func TestWriteToolReportsOriginalPath(t *testing.T) {
	dir := t.TempDir()
	originalPath := dir + string(os.PathSeparator) + "nested" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "file.txt"
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": originalPath, "content": "hello"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["path"] != originalPath || !strings.Contains(result.Content, "to "+originalPath) {
		t.Fatalf("write should report original path, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestReadWriteAndLSToolsUseOriginalPathForFilesystemAccess(t *testing.T) {
	dir := t.TempDir()
	originalFilePath := dir + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "file.txt"
	mustWrite(t, filepath.Join(dir, "file.txt"), "body")

	_, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": originalFilePath}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read "+originalFilePath+":") {
		t.Fatalf("read should access the original path like upstream, got %v", err)
	}

	_, err = (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": originalFilePath, "content": "new"}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "write "+originalFilePath+":") {
		t.Fatalf("write should access the original path like upstream, got %v", err)
	}

	originalDirPath := dir + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".."
	_, err = (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": originalDirPath}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "ls "+originalDirPath+":") {
		t.Fatalf("ls should access the original path like upstream, got %v", err)
	}
}

func TestFileToolsRejectInvalidUTF8Path(t *testing.T) {
	badPath := string([]byte{0xff})
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "read", run: func() error {
			_, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": badPath}}, nil)
			return err
		}},
		{name: "write", run: func() error {
			_, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": badPath, "content": "body"}}, nil)
			return err
		}},
		{name: "edit", run: func() error {
			_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": badPath, "old_string": "old", "new_string": "new"}}, nil)
			return err
		}},
		{name: "ls", run: func() error {
			_, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": badPath}}, nil)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || err.Error() != "path must be valid UTF-8" {
				t.Fatalf("expected invalid path error, got %v", err)
			}
		})
	}
}

func TestWriteToolAllowsEmptyPathThroughWriteError(t *testing.T) {
	_, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": "", "content": "body"}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "write :") {
		t.Fatalf("expected upstream-style empty path write error, got %v", err)
	}
}

func TestWriteToolIgnoresMkdirErrorBeforeWriteLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "parent")
	mustWrite(t, parentFile, "not a directory")
	path := filepath.Join(parentFile, "child.txt")
	_, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": path, "content": "body"}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "write "+path+":") {
		t.Fatalf("expected upstream-style write error after ignored mkdir failure, got %v", err)
	}
}

func TestWriteToolNonStringArgsReportMissing(t *testing.T) {
	_, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": 123, "content": "body"}}, nil)
	if err == nil || err.Error() != "missing `path`" {
		t.Fatalf("expected upstream-style non-string path error, got %v", err)
	}

	_, err = (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": "file.txt", "content": 123}}, nil)
	if err == nil || err.Error() != "missing `content`" {
		t.Fatalf("expected upstream-style non-string content error, got %v", err)
	}
}

func TestWriteToolRejectsInvalidUTF8Content(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	_, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"path": path, "content": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "content must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 content error, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid UTF-8 content should not be written, stat err=%v", statErr)
	}
}

func TestFileToolsAcceptFilePathAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	if _, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"file_path": path, "content": "body"}}, nil); err != nil {
		t.Fatal(err)
	}
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"file_path": path}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "body") {
		t.Fatalf("read content = %q", result.Content)
	}
}

func TestFileToolsAcceptFileAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	if _, err := (WriteTool{}).Execute(context.Background(), ai.ToolCall{Name: "write", Arguments: map[string]any{"file": path, "content": "body"}}, nil); err != nil {
		t.Fatal(err)
	}
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"file": path}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "body") {
		t.Fatalf("read content = %q", result.Content)
	}
}

func TestEditToolContentMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "old", "new_string": "new"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := fmt.Sprintf("Edited %s (1 replacement).\n", path)
	if !strings.HasPrefix(result.Content, wantPrefix) {
		t.Fatalf("edit content prefix mismatch: want %q got %q", wantPrefix, result.Content)
	}

	if err := os.WriteFile(path, []byte("x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "x", "new_string": "y"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "old_string matched 2 times") || !strings.Contains(err.Error(), "replace_all=true") {
		t.Fatalf("edit duplicate error mismatch: %v", err)
	}
}

func TestEditToolReportsOriginalPath(t *testing.T) {
	dir := t.TempDir()
	cleanedPath := filepath.Join(dir, "file.txt")
	originalPath := dir + string(os.PathSeparator) + "nested" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "file.txt"
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cleanedPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": originalPath, "old_string": "old", "new_string": "new"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["path"] != originalPath || !strings.HasPrefix(result.Content, "Edited "+originalPath+" (1 replacement).\n") {
		t.Fatalf("edit should report original path, content=%q details=%#v", result.Content, result.Details)
	}

	if err := os.WriteFile(cleanedPath, []byte("x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": originalPath, "old_string": "x", "new_string": "y"}}, nil)
	if err == nil || !strings.Contains(err.Error(), " in "+originalPath+"; pass replace_all=true") {
		t.Fatalf("duplicate error should report original path, got %v", err)
	}
}

func TestEditToolUsesOriginalPathForFilesystemAccess(t *testing.T) {
	dir := t.TempDir()
	originalPath := dir + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "file.txt"
	mustWrite(t, filepath.Join(dir, "file.txt"), "old")
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": originalPath, "old_string": "old", "new_string": "new"}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read "+originalPath+":") {
		t.Fatalf("edit should access the original path like upstream, got %v", err)
	}
}

func TestEditToolAllowsEmptyPathThroughReadError(t *testing.T) {
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": "", "old_string": "old", "new_string": "new"}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read :") {
		t.Fatalf("expected upstream-style empty path read error, got %v", err)
	}
}

func TestEditToolNonStringArgsReportMissing(t *testing.T) {
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": 123, "old_string": "old", "new_string": "new"}}, nil)
	if err == nil || err.Error() != "missing `path`" {
		t.Fatalf("expected upstream-style non-string path error, got %v", err)
	}

	_, err = (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": "file.txt", "old_string": 123, "new_string": "new"}}, nil)
	if err == nil || err.Error() != "missing `old_string`" {
		t.Fatalf("expected upstream-style non-string old_string error, got %v", err)
	}

	_, err = (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": "file.txt", "old_string": "old", "new_string": 123}}, nil)
	if err == nil || err.Error() != "missing `new_string`" {
		t.Fatalf("expected upstream-style non-string new_string error, got %v", err)
	}
}

func TestEditToolRejectsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(path, []byte{0xff, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "x", "new_string": "y"}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read "+path+":") {
		t.Fatalf("expected invalid UTF-8 edit read error, got %v", err)
	}
}

func TestEditToolRejectsInvalidUTF8Arguments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	mustWrite(t, path, "hello old world")

	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": string([]byte{0xff}), "new_string": "new"}}, nil)
	if err == nil || err.Error() != "old_string must be valid UTF-8" {
		t.Fatalf("expected invalid old_string error, got %v", err)
	}

	_, err = (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "old", "new_string": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "new_string must be valid UTF-8" {
		t.Fatalf("expected invalid new_string error, got %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello old world" {
		t.Fatalf("invalid UTF-8 arguments should not edit file, got %q", data)
	}
}

func TestEditToolEqualStringsErrorMatchesUpstream(t *testing.T) {
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": "any", "old_string": "same", "new_string": "same"}}, nil)
	if err == nil || err.Error() != "old_string must differ from new_string" {
		t.Fatalf("expected equal string error, got %v", err)
	}
}

func TestEditToolAllowsEmptyOldStringThroughMatchError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-old.txt")
	mustWrite(t, path, "abc")
	_, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "", "new_string": "x"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "old_string matched 4 times") {
		t.Fatalf("expected empty old_string match error, got %v", err)
	}
	result, err := (EditTool{}).Execute(context.Background(), ai.ToolCall{Name: "edit", Arguments: map[string]any{"path": path, "old_string": "", "new_string": "x", "replace_all": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "xaxbxcx" || result.Details["replacements"] != 4 {
		t.Fatalf("empty old_string replace_all mismatch content=%q details=%#v", data, result.Details)
	}
}

func TestReadToolSupportsOffsetLimitAndDetails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "read.txt")
	mustWrite(t, path, "one\ntwo\nthree\nfour\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "offset": 2, "limit": 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "one") || !strings.Contains(result.Content, "two\nthree\n") || !strings.Contains(result.Content, "lines 2-3") {
		t.Fatalf("read content mismatch: %q", result.Content)
	}
	if result.Details["path"] != path || result.Details["totalLines"] != 4 || result.Details["keptLines"] != 2 || result.Details["offset"] != 2 {
		t.Fatalf("read details mismatch: %#v", result.Details)
	}
}

func TestReadToolAcceptsJSONNumberLimitLikeSerdeValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "json-number-limit.txt")
	mustWrite(t, path, "one\ntwo\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": json.Number("1")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "two\n") || result.Details["keptLines"] != 1 {
		t.Fatalf("json.Number limit should behave like serde_json integer, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestReadToolDefaultsToUpstreamLineLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	var builder strings.Builder
	for line := 1; line <= 2500; line++ {
		fmt.Fprintf(&builder, "line-%d\n", line)
	}
	mustWrite(t, path, builder.String())
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "line-2000\n") {
		t.Fatalf("expected default read to include line 2000")
	}
	if strings.Contains(result.Content, "line-2001\n") {
		t.Fatalf("expected default read to stop before line 2001")
	}
	if result.Details["keptLines"] != 2000 || result.Details["totalLines"] != 2001 {
		t.Fatalf("read details mismatch: %#v", result.Details)
	}
}

func TestReadToolByteTruncationKeepsWholeLinesAndReportsNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bytes.txt")
	firstLine := strings.Repeat("a", defaultMaxBytes-1) + "\n"
	mustWrite(t, path, firstLine+"dropped\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": 10}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, fmt.Sprintf("[truncated: kept 1/2 lines, %d of %d bytes]", defaultMaxBytes, defaultMaxBytes+8)) {
		t.Fatalf("expected upstream truncation note and whole kept lines, got %q", result.Content)
	}
	if strings.Contains(result.Content, "dropped") || strings.Contains(result.Content, "[truncated]\n") {
		t.Fatalf("expected no dropped line or legacy note, got %q", result.Content)
	}
	if result.Details["keptLines"] != 1 || result.Details["totalLines"] != 2 {
		t.Fatalf("read details mismatch: %#v", result.Details)
	}
}

func TestReadToolLimitZeroMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "limit-zero.txt")
	mustWrite(t, path, "one\ntwo\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "one") || strings.Contains(result.Content, "two") || !strings.Contains(result.Content, "lines 1-0") {
		t.Fatalf("read limit=0 content mismatch: %q", result.Content)
	}
	if result.Details["keptLines"] != 0 || result.Details["totalLines"] != 1 || result.Details["offset"] != 1 {
		t.Fatalf("read limit=0 details mismatch: %#v", result.Details)
	}
}

func TestReadToolOffsetZeroReportsOriginalValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "offset-zero.txt")
	mustWrite(t, path, "one\ntwo\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "offset": 0, "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "lines 1-1\none\n") {
		t.Fatalf("read offset=0 content mismatch: %q", result.Content)
	}
	if result.Details["offset"] != 0 || result.Details["keptLines"] != 1 || result.Details["totalLines"] != 2 {
		t.Fatalf("read offset=0 details mismatch: %#v", result.Details)
	}
}

func TestReadToolNegativeOffsetFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "negative-offset.txt")
	mustWrite(t, path, "one\ntwo\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "offset": -1, "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "lines 1-1\none\n") {
		t.Fatalf("read negative offset content mismatch: %q", result.Content)
	}
	if result.Details["offset"] != 1 || result.Details["keptLines"] != 1 || result.Details["totalLines"] != 2 {
		t.Fatalf("read negative offset details mismatch: %#v", result.Details)
	}
}

func TestReadToolFloatOffsetFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "float-offset.txt")
	mustWrite(t, path, "one\ntwo\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "offset": 2.0, "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "lines 1-1\none\n") {
		t.Fatalf("float offset should fall back to line 1 like serde_json::Value::as_u64, content=%q", result.Content)
	}
	if result.Details["offset"] != 1 || result.Details["keptLines"] != 1 || result.Details["totalLines"] != 2 {
		t.Fatalf("read float offset details mismatch: %#v", result.Details)
	}
}

func TestReadToolNegativeLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "negative-limit.txt")
	mustWrite(t, path, "one\ntwo\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": -1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "one\ntwo\n") || !strings.Contains(result.Content, "lines 1-2") {
		t.Fatalf("read negative limit content mismatch: %q", result.Content)
	}
	if result.Details["keptLines"] != 2 || result.Details["totalLines"] != 2 || result.Details["offset"] != 1 {
		t.Fatalf("read negative limit details mismatch: %#v", result.Details)
	}
}

func TestReadToolAllowsEmptyPathThroughReadError(t *testing.T) {
	_, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": ""}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read :") {
		t.Fatalf("expected upstream-style empty path read error, got %v", err)
	}
}

func TestReadToolNonStringPathReportsMissingPath(t *testing.T) {
	_, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": 123}}, nil)
	if err == nil || err.Error() != "missing `path`" {
		t.Fatalf("expected upstream-style non-string path error, got %v", err)
	}
}

func TestReadToolRejectsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "read "+path+":") {
		t.Fatalf("expected invalid UTF-8 read error, got %v", err)
	}
}

func TestReadToolStringLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "string-limit.txt")
	mustWrite(t, path, "a\nb\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": "1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["keptLines"] != 2 || !strings.Contains(result.Content, "b\n") {
		t.Fatalf("string limit should fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestReadToolFractionalLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fractional-limit.txt")
	mustWrite(t, path, "a\nb\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": 1.5}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["keptLines"] != 2 || !strings.Contains(result.Content, "b\n") {
		t.Fatalf("fractional limit should fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestReadToolFloatLimitFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "float-limit.txt")
	mustWrite(t, path, "a\nb\nc\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "limit": 2.0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["keptLines"] != 3 || !strings.Contains(result.Content, "c\n") {
		t.Fatalf("float limit should fall back to default like serde_json::Value::as_u64, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestReadToolIgnoresNonUpstreamMaxBytesArgument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-bytes.txt")
	mustWrite(t, path, "12345\nabcdef\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "max_bytes": 6}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "12345\nabcdef\n") || strings.Contains(result.Content, "[truncated:") {
		t.Fatalf("read should ignore non-upstream max_bytes argument, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestReadToolIgnoresNonUpstreamMaxLinesArgument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-lines.txt")
	mustWrite(t, path, "a\nb\n")
	result, err := (ReadTool{}).Execute(context.Background(), ai.ToolCall{Name: "read", Arguments: map[string]any{"path": path, "max_lines": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["keptLines"] != 2 || !strings.Contains(result.Content, "a\nb\n") {
		t.Fatalf("read should ignore non-upstream max_lines argument, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestLSToolDetailsUseActualShownEntries(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "one.txt"), "one")
	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": 500}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["totalEntries"] != 1 || result.Details["shownEntries"] != 1 {
		t.Fatalf("ls details mismatch: %#v", result.Details)
	}
	if !strings.HasPrefix(result.Content, dir+" (1 entries)\n") {
		t.Fatalf("ls header mismatch: %q", result.Content)
	}
}

func TestLSToolReportsOriginalPath(t *testing.T) {
	dir := t.TempDir()
	originalPath := dir + string(os.PathSeparator) + "nested" + string(os.PathSeparator) + ".."
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "one.txt"), "one")
	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": originalPath}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["path"] != originalPath || !strings.HasPrefix(result.Content, originalPath+" (2 entries)\n") || !strings.Contains(result.Content, "nested/") {
		t.Fatalf("ls should report original path, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestLSToolDefaultLimitMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 501; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file-%03d.txt", index)), "x")
	}
	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["shownEntries"] != 500 || result.Details["totalEntries"] != 501 {
		t.Fatalf("ls details mismatch: %#v", result.Details)
	}
	if !strings.Contains(result.Content, "file-499.txt") || strings.Contains(result.Content, "file-500.txt") {
		t.Fatalf("ls default limit mismatch: %q", result.Content)
	}
	if !strings.Contains(result.Content, "[truncated: showed 500/501]") {
		t.Fatalf("expected truncation marker, got %q", result.Content)
	}
}

func TestLSToolLimitZeroMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "one.txt"), "one")
	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "one.txt") || !strings.Contains(result.Content, "[truncated: showed 0/1]") {
		t.Fatalf("ls limit=0 content mismatch: %q", result.Content)
	}
	if result.Details["shownEntries"] != 0 || result.Details["totalEntries"] != 1 {
		t.Fatalf("ls limit=0 details mismatch: %#v", result.Details)
	}
}

func TestLSToolNegativeLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "one.txt"), "one")
	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": -1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "one.txt") || result.Details["shownEntries"] != 1 || result.Details["totalEntries"] != 1 {
		t.Fatalf("ls negative limit mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestLSToolAllowsEmptyPathThroughReadDirError(t *testing.T) {
	_, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": ""}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "ls :") {
		t.Fatalf("expected upstream-style empty path ls error, got %v", err)
	}
}

func TestFindToolAllowsEmptyPathThroughWalkErrorLikeUpstream(t *testing.T) {
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": "", "glob": "*.go"}}, nil)
	if err == nil {
		t.Fatal("expected empty find path to fail like upstream WalkBuilder")
	}
}

func TestFindToolValidatesGlobBeforeEmptyPathLikeUpstream(t *testing.T) {
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": "", "glob": "["}}, nil)
	if err == nil || !strings.Contains(err.Error(), "glob") {
		t.Fatalf("invalid glob should fail before empty path like upstream TypesBuilder, got %v", err)
	}
}

func TestLSToolTruncatesAtDefaultMaxBytes(t *testing.T) {
	dir := t.TempDir()
	longName := strings.Repeat("a", 240)
	for index := 0; index < 1200; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("%04d-%s.txt", index, longName)), "x")
	}
	result, err := (LSTool{}).Execute(context.Background(), ai.ToolCall{Name: "ls", Arguments: map[string]any{"path": dir, "limit": 1200}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) > 256*1024+len("[truncated: showed 9999/9999]\n") {
		t.Fatalf("ls content exceeded upstream byte cap: len=%d", len(result.Content))
	}
	shown, ok := result.Details["shownEntries"].(int)
	if !ok || shown >= 1200 {
		t.Fatalf("expected byte truncation before all entries, details=%#v", result.Details)
	}
	if !strings.Contains(result.Content, fmt.Sprintf("[truncated: showed %d/1200]", shown)) {
		t.Fatalf("expected byte truncation marker, got details=%#v tail=%q", result.Details, result.Content[max(0, len(result.Content)-120):])
	}
}

func TestFindAndGrepTools(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "a.go")
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\nfunc target() {}\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), "target text\n")
	mustWrite(t, filepath.Join(dir, ".git", "ignored.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(findResult.Content, "find *.go: 1 hits\n") || !strings.Contains(findResult.Content, goPath) || strings.Contains(findResult.Content, ".git") {
		t.Fatalf("find output mismatch: %q", findResult.Content)
	}
	paths, ok := findResult.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != goPath || findResult.Details["limit"] != 200 || findResult.Details["stopped_at_limit"] != false {
		t.Fatalf("find details mismatch: %#v", findResult.Details)
	}

	findResult, err = (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "pattern": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(findResult.Content, "find *.go: 1 hits\n") || !strings.Contains(findResult.Content, goPath) {
		t.Fatalf("find pattern alias output mismatch: %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(grepResult.Content, "grep: 1 hits\n") || !strings.Contains(grepResult.Content, goPath+":2: func target() {}") || strings.Contains(grepResult.Content, "b.txt") || strings.Contains(grepResult.Content, ".git") {
		t.Fatalf("grep output mismatch: %q", grepResult.Content)
	}
	if grepResult.Details["matches"] != 1 || grepResult.Details["truncated_lines"] != 0 || grepResult.Details["max_match_line_chars"] != 500 {
		t.Fatalf("grep details mismatch: %#v", grepResult.Details)
	}
}

func TestGrepToolWithoutGlobScansAllVisibleFilesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "Makefile")
	mustWrite(t, path, "target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, path+":1:") {
		t.Fatalf("grep without glob should scan every visible file like upstream, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindAndGrepHonorParentGitignoreLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(root, "ignored.txt"), "target\n")
	mustWrite(t, filepath.Join(root, "kept.txt"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": root, "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(findResult.Content, "ignored.txt") || !strings.Contains(findResult.Content, "kept.txt") {
		t.Fatalf("find should honor parent .gitignore, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": root, "pattern": "target", "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grepResult.Content, "ignored.txt") || !strings.Contains(grepResult.Content, "kept.txt") {
		t.Fatalf("grep should honor parent .gitignore, got %q", grepResult.Content)
	}
}

func TestFindAndGrepHonorIgnoreFileLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".ignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "target\n")
	mustWrite(t, filepath.Join(dir, "kept.txt"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(findResult.Content, "ignored.txt") || !strings.Contains(findResult.Content, "kept.txt") {
		t.Fatalf("find should honor .ignore, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grepResult.Content, "ignored.txt") || !strings.Contains(grepResult.Content, "kept.txt") {
		t.Fatalf("grep should honor .ignore, got %q", grepResult.Content)
	}
}

func TestFindAndGrepHonorGitInfoExcludeLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, ".git", "info", "exclude"), "ignored.txt\n")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "target\n")
	mustWrite(t, filepath.Join(dir, "kept.txt"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(findResult.Content, "ignored.txt") || !strings.Contains(findResult.Content, "kept.txt") {
		t.Fatalf("find should honor .git/info/exclude, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grepResult.Content, "ignored.txt") || !strings.Contains(grepResult.Content, "kept.txt") {
		t.Fatalf("grep should honor .git/info/exclude, got %q", grepResult.Content)
	}
}

func TestFindAndGrepSkipHiddenPaths(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "visible.go"), "target\n")
	mustWrite(t, filepath.Join(dir, ".hidden.go"), "target\n")
	mustWrite(t, filepath.Join(dir, ".hidden", "nested.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findResult.Content, "visible.go") || strings.Contains(findResult.Content, ".hidden") {
		t.Fatalf("find hidden path mismatch: %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(grepResult.Content, "visible.go") || strings.Contains(grepResult.Content, ".hidden") {
		t.Fatalf("grep hidden path mismatch: %q", grepResult.Content)
	}
}

func TestFindToolReportsLimitTruncation(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "package main\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Content, "find *.go: showing first 1 hits (limit reached)\n") || !strings.Contains(result.Content, "... results truncated; rerun with a narrower glob/path or a higher limit if needed") {
		t.Fatalf("find truncation mismatch: %q", result.Content)
	}
}

func TestFindToolDoesNotReportLimitWhenExactlyFull(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "only.go"), "package main\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["stopped_at_limit"] != false || !strings.HasPrefix(result.Content, "find *.go: 1 hits\n") || strings.Contains(result.Content, "limit reached") {
		t.Fatalf("find exact-limit mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolLimitZeroMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 0 || result.Details["stopped_at_limit"] != true || !strings.HasPrefix(result.Content, "find *.go: showing first 0 hits (limit reached)\n") {
		t.Fatalf("find limit=0 mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolNegativeLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 201; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file-%03d.go", index)), "package main\n")
	}
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": -1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 200 || result.Details["limit"] != 200 || result.Details["stopped_at_limit"] != true {
		t.Fatalf("find negative limit mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolStringLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 3; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file-%d.go", index)), "package main\n")
	}
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": "1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 3 || result.Details["limit"] != 200 || result.Details["stopped_at_limit"] != false {
		t.Fatalf("find string limit should fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolFloatLimitFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 3; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file-%d.go", index)), "package main\n")
	}
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": 1.0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 3 || result.Details["limit"] != 200 || result.Details["stopped_at_limit"] != false {
		t.Fatalf("find float limit should fall back to default like serde_json::Value::as_u64, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolLargeUnsignedLimitDoesNotFallBackLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 201; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file-%03d.go", index)), "package main\n")
	}
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": uint64(math.MaxUint64)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 201 || result.Details["stopped_at_limit"] != false {
		t.Fatalf("find large unsigned limit should not fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolLargeJSONNumberLimitDoesNotFallBackLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 201; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file-%03d.go", index)), "package main\n")
	}
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go", "limit": json.Number(fmt.Sprint(uint64(math.MaxUint64)))}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 201 || result.Details["stopped_at_limit"] != false {
		t.Fatalf("find large json.Number limit should not fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolCancelledContextStopsBeforeScanningLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (FindTool{}).Execute(ctx, ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 0 || result.Details["stopped_at_limit"] != false || result.Content != "find *.go: 0 hits\n" {
		t.Fatalf("cancelled find should return no results like upstream cancellation, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolNonStringGlobReportsMissing(t *testing.T) {
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"glob": 123}}, nil)
	if err == nil || err.Error() != "missing `glob`" {
		t.Fatalf("expected upstream-style non-string glob error, got %v", err)
	}
}

func TestFindToolRejectsInvalidUTF8Glob(t *testing.T) {
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"glob": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "glob must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 glob error, got %v", err)
	}
}

func TestFindToolRejectsInvalidUTF8Path(t *testing.T) {
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"glob": "*", "path": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "path must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 path error, got %v", err)
	}
}

func TestFindToolEmptyGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 0 || result.Content != "find : 0 hits\n" {
		t.Fatalf("find empty glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolInvalidGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "["}}, nil)
	if err == nil || err.Error() != "error parsing glob '[': unclosed character class; missing ']'" {
		t.Fatalf("find invalid glob error mismatch: %v", err)
	}
}

func TestFindToolInvalidRangeGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "[z-a]"}}, nil)
	if err == nil || err.Error() != "error parsing glob '[z-a]': invalid range; 'z' > 'a'" {
		t.Fatalf("find invalid range glob error mismatch: %v", err)
	}
}

func TestFindToolBangNegatedClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	beePath := filepath.Join(dir, "bee.go")
	mustWrite(t, filepath.Join(dir, "app.go"), "package main\n")
	mustWrite(t, beePath, "package main\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "[!a]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != beePath || strings.Contains(result.Content, "app.go") {
		t.Fatalf("find bang negated class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolDashCharacterClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	dashPath := filepath.Join(dir, "-.go")
	bPath := filepath.Join(dir, "b.go")
	mustWrite(t, dashPath, "target dash\n")
	mustWrite(t, bPath, "target b\n")
	mustWrite(t, filepath.Join(dir, "z.go"), "target z\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "[-abc]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 2 || !slices.Contains(paths, dashPath) || !slices.Contains(paths, bPath) || strings.Contains(result.Content, "z.go") {
		t.Fatalf("find dash character class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolClosingBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	bracketPath := filepath.Join(dir, "].go")
	mustWrite(t, bracketPath, "target bracket\n")
	mustWrite(t, filepath.Join(dir, "a.go"), "target a\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "[]]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != bracketPath || strings.Contains(result.Content, "a.go") {
		t.Fatalf("find closing bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolNegatedClosingBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.go")
	mustWrite(t, aPath, "target a\n")
	mustWrite(t, filepath.Join(dir, "].go"), "target bracket\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "[!]]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != aPath || strings.Contains(result.Content, "].go") {
		t.Fatalf("find negated closing bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolBracketEscapedMetasMatchUpstream(t *testing.T) {
	dir := t.TempDir()
	literalPath := filepath.Join(dir, "_[_]_?_*_!_")
	mustWrite(t, literalPath, "target literal\n")
	mustWrite(t, filepath.Join(dir, "_a_a_b_c_!_"), "target other\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "_[[]_[]]_[?]_[*]_!_"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != literalPath || strings.Contains(result.Content, "_a_a_b_c_!_") {
		t.Fatalf("find bracket escaped metas mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolEscapedGlobMetaMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	literalPath := filepath.Join(dir, "*.go")
	mustWrite(t, literalPath, "package main\n")
	mustWrite(t, filepath.Join(dir, "app.go"), "package main\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": `\*.go`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != literalPath || strings.Contains(result.Content, "app.go") {
		t.Fatalf("find escaped glob meta mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolEscapedBraceGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": `\{a,b}.go`}}, nil)
	if err == nil || err.Error() != "error parsing glob '\\{a,b}.go': unopened alternate group; missing '{' (maybe escape '}' with '[}]'?)" {
		t.Fatalf("find escaped brace glob error mismatch: %v", err)
	}
}

func TestFindToolEscapedClosingBraceGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": `{a,b\}.go`}}, nil)
	if err == nil || err.Error() != "error parsing glob '{a,b\\}.go': unclosed alternate group; missing '}' (maybe escape '{' with '[{]'?)" {
		t.Fatalf("find escaped closing brace glob error mismatch: %v", err)
	}
}

func TestFindToolUnmatchedBraceGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		glob string
		want string
	}{
		{glob: `{a,b.go`, want: "error parsing glob '{a,b.go': unclosed alternate group; missing '}' (maybe escape '{' with '[{]'?)"},
		{glob: `a,b}.go`, want: "error parsing glob 'a,b}.go': unopened alternate group; missing '{' (maybe escape '}' with '[}]'?)"},
	} {
		_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": tc.glob}}, nil)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("find unmatched brace glob error mismatch for %q: %v", tc.glob, err)
		}
	}
}

func TestFindToolDanglingEscapeGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": `abc\`}}, nil)
	if err == nil || err.Error() != "error parsing glob 'abc\\': dangling '\\'" {
		t.Fatalf("find dangling escape glob error mismatch: %v", err)
	}
}

func TestFindToolBraceGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "a.go")
	txtPath := filepath.Join(dir, "b.txt")
	mustWrite(t, goPath, "package main\n")
	mustWrite(t, txtPath, "target\n")
	mustWrite(t, filepath.Join(dir, "c.rs"), "target\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.{go,txt}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 2 || !slices.Contains(paths, goPath) || !slices.Contains(paths, txtPath) || strings.Contains(result.Content, "c.rs") {
		t.Fatalf("find brace glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolBraceContainingBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	bracePath := filepath.Join(dir, "}")
	fooPath := filepath.Join(dir, "foo")
	mustWrite(t, bracePath, "target brace\n")
	mustWrite(t, fooPath, "target foo\n")
	mustWrite(t, filepath.Join(dir, "bar"), "target bar\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "{[}],foo}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 2 || !slices.Contains(paths, bracePath) || !slices.Contains(paths, fooPath) || strings.Contains(result.Content, "bar") {
		t.Fatalf("find brace containing bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolBraceContainingOpenBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	bracePath := filepath.Join(dir, "{")
	xPath := filepath.Join(dir, "x")
	mustWrite(t, bracePath, "target brace\n")
	mustWrite(t, xPath, "target x\n")
	mustWrite(t, filepath.Join(dir, "bar"), "target bar\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "{[{],x}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 2 || !slices.Contains(paths, bracePath) || !slices.Contains(paths, xPath) || strings.Contains(result.Content, "bar") {
		t.Fatalf("find brace containing open bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolMultipleBraceGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	matches := []string{
		filepath.Join(dir, "a.go.bak"),
		filepath.Join(dir, "b.txt.tmp"),
		filepath.Join(dir, "c.go.tmp"),
		filepath.Join(dir, "d.txt.bak"),
	}
	for _, path := range matches {
		mustWrite(t, path, "target\n")
	}
	mustWrite(t, filepath.Join(dir, "e.rs.bak"), "target\n")
	mustWrite(t, filepath.Join(dir, "f.go"), "target\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.{go,txt}.{bak,tmp}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != len(matches) {
		t.Fatalf("find multiple brace glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
	for _, path := range matches {
		if !slices.Contains(paths, path) {
			t.Fatalf("find multiple brace glob missing %s: %#v", path, paths)
		}
	}
	if strings.Contains(result.Content, "e.rs.bak") || strings.Contains(result.Content, "f.go") {
		t.Fatalf("find multiple brace glob included non-matches: %q", result.Content)
	}
}

func TestFindToolNestedBraceGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	matches := []string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "bc"),
		filepath.Join(dir, "bd"),
	}
	for _, path := range matches {
		mustWrite(t, path, "target\n")
	}
	mustWrite(t, filepath.Join(dir, "b"), "target\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "{a,b{c,d}}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != len(matches) {
		t.Fatalf("find nested brace glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
	for _, path := range matches {
		if !slices.Contains(paths, path) {
			t.Fatalf("find nested brace glob missing %s: %#v", path, paths)
		}
	}
	if strings.Contains(result.Content, filepath.Join(dir, "b")+"\n") {
		t.Fatalf("find nested brace glob included non-match: %q", result.Content)
	}
}

func TestFindToolEmptyBraceGroupMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo")
	mustWrite(t, fooPath, "target foo\n")
	mustWrite(t, filepath.Join(dir, "foo{}"), "target literal\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "foo{}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != fooPath || slices.Contains(paths, filepath.Join(dir, "foo{}")) {
		t.Fatalf("find empty brace group mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolCommaOnlyBraceGroupMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo")
	mustWrite(t, fooPath, "target foo\n")
	mustWrite(t, filepath.Join(dir, "foo{,}"), "target literal\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "foo{,}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != fooPath || slices.Contains(paths, filepath.Join(dir, "foo{,}")) {
		t.Fatalf("find comma-only brace group mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolEmptyBraceBranchMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "a.go")
	dotPath := filepath.Join(dir, "a.")
	mustWrite(t, goPath, "package main\n")
	mustWrite(t, dotPath, "target\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.{go,}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != goPath || strings.Contains(result.Content, dotPath+"\n") {
		t.Fatalf("find empty brace branch mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolLeadingGlobstarMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.go")
	nestedPath := filepath.Join(dir, "src", "nested", "app.go")
	mustWrite(t, rootPath, "package main\n")
	mustWrite(t, nestedPath, "package main\n")
	mustWrite(t, filepath.Join(dir, "skip.txt"), "target\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "**/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 2 || !slices.Contains(paths, rootPath) || !slices.Contains(paths, nestedPath) || strings.Contains(result.Content, "skip.txt") {
		t.Fatalf("find leading globstar mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolAllowsEmptyPathThroughWalkError(t *testing.T) {
	_, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": "", "glob": "*.go"}}, nil)
	if err == nil {
		t.Fatal("expected empty find path to fail like upstream WalkBuilder")
	}
}

func TestGrepToolAllowsEmptyPathThroughWalkError(t *testing.T) {
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": "", "pattern": "target"}}, nil)
	if err == nil {
		t.Fatal("expected empty grep path to fail like upstream WalkBuilder")
	}
}

func TestFindAndGrepHonorGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored/\n*.tmp\n")
	mustWrite(t, filepath.Join(dir, "keep.go"), "target\n")
	mustWrite(t, filepath.Join(dir, "ignored", "skip.go"), "target\n")
	mustWrite(t, filepath.Join(dir, "scratch.tmp"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findResult.Content, "keep.go") || strings.Contains(findResult.Content, "ignored/") {
		t.Fatalf("find gitignore mismatch: %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(grepResult.Content, "keep.go") || strings.Contains(grepResult.Content, "ignored/") || strings.Contains(grepResult.Content, "scratch.tmp") {
		t.Fatalf("grep gitignore mismatch: %q", grepResult.Content)
	}
}

func TestFindAndGrepUseOriginalPathForFilesystemAccess(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "target\n")
	originalPath := dir + string(os.PathSeparator) + "missing" + string(os.PathSeparator) + ".."

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": originalPath, "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if findResult.Content != "find *.go: 0 hits\n" {
		t.Fatalf("find should access the original path like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": originalPath, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Content != "grep: 0 hits\n" {
		t.Fatalf("grep should access the original path like upstream, got %q", grepResult.Content)
	}
}

func TestFindAndGrepSkipUnreadableSubdirectoriesLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions are platform-specific")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "target\n")
	unreadable := filepath.Join(dir, "unreadable")
	if err := os.Mkdir(unreadable, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(unreadable, "hidden.go"), "target\n")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findResult.Content, "a.go") || strings.Contains(findResult.Content, "hidden.go") {
		t.Fatalf("find should skip unreadable subdirectories like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(grepResult.Content, "a.go") || strings.Contains(grepResult.Content, "hidden.go") {
		t.Fatalf("grep should skip unreadable subdirectories like upstream, got %q", grepResult.Content)
	}
}

func TestFindAndGrepHonorNestedGitignoreAndNegation(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "*.tmp\n!keep.tmp\n")
	mustWrite(t, filepath.Join(dir, "pkg", ".gitignore"), "*.log\n!keep.log\n")
	mustWrite(t, filepath.Join(dir, "pkg", "keep.log"), "target\n")
	mustWrite(t, filepath.Join(dir, "pkg", "skip.log"), "target\n")
	mustWrite(t, filepath.Join(dir, "keep.tmp"), "target\n")
	mustWrite(t, filepath.Join(dir, "skip.tmp"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.log"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findResult.Content, "pkg/keep.log") || strings.Contains(findResult.Content, "pkg/skip.log") {
		t.Fatalf("find nested gitignore mismatch: %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(grepResult.Content, "pkg/keep.log") || !strings.Contains(grepResult.Content, "keep.tmp") || strings.Contains(grepResult.Content, "pkg/skip.log") || strings.Contains(grepResult.Content, "skip.tmp") {
		t.Fatalf("grep nested gitignore mismatch: %q", grepResult.Content)
	}
}

func TestFindAndGrepHonorAnchoredAndDoubleStarGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "/root-only.log\nlogs/**/*.txt\n")
	mustWrite(t, filepath.Join(dir, "root-only.log"), "target\n")
	mustWrite(t, filepath.Join(dir, "pkg", "root-only.log"), "target\n")
	mustWrite(t, filepath.Join(dir, "logs", "today.txt"), "target\n")
	mustWrite(t, filepath.Join(dir, "logs", "nested", "old.txt"), "target\n")
	mustWrite(t, filepath.Join(dir, "logs", "keep.md"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "*.log"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(findResult.Content, "\nroot-only.log\n") || !strings.Contains(findResult.Content, "pkg/root-only.log") {
		t.Fatalf("find anchored gitignore mismatch: %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grepResult.Content, "\nroot-only.log:1") || strings.Contains(grepResult.Content, "logs/today.txt") || strings.Contains(grepResult.Content, "logs/nested/old.txt") || !strings.Contains(grepResult.Content, "pkg/root-only.log") || !strings.Contains(grepResult.Content, "logs/keep.md") {
		t.Fatalf("grep anchored doublestar gitignore mismatch: %q", grepResult.Content)
	}
}

func TestGrepToolReportsLimitTruncation(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "target one\ntarget two\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[truncated at 1 matches]") {
		t.Fatalf("grep truncation mismatch: %q", result.Content)
	}
}

func TestGrepToolStopsScanningAtLimit(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "target first\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), strings.Repeat("b", 800)+"target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || result.Details["truncated_lines"] != 0 || strings.Contains(result.Content, "long matching line") {
		t.Fatalf("grep limit scan mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolLimitZeroMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWrite(t, path, "target first\ntarget second\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || strings.Contains(result.Content, path+":1: target first") || !strings.Contains(result.Content, "[truncated at 0 matches]") {
		t.Fatalf("grep limit=0 mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolEmptyGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 0 || result.Content != "grep: 0 hits\n" {
		t.Fatalf("grep empty glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolNonStringPatternReportsMissing(t *testing.T) {
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"pattern": 123}}, nil)
	if err == nil || err.Error() != "missing `pattern`" {
		t.Fatalf("expected upstream-style non-string pattern error, got %v", err)
	}
}

func TestGrepToolRejectsInvalidUTF8Pattern(t *testing.T) {
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"pattern": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "pattern must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 pattern error, got %v", err)
	}
}

func TestGrepToolRejectsInvalidUTF8PathAndGlob(t *testing.T) {
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"pattern": "x", "path": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "path must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 path error, got %v", err)
	}

	_, err = (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"pattern": "x", "glob": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "glob must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 glob error, got %v", err)
	}
}

func TestGrepToolCancelledContextStopsBeforeScanningLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "target\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (GrepTool{}).Execute(ctx, ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 0 || result.Content != "grep: 0 hits\n" {
		t.Fatalf("cancelled grep should return no results like upstream cancellation, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolScansSingleFilePathLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	mustWrite(t, path, "target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": path, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, path+":1: target") {
		t.Fatalf("grep single file path should scan the file like upstream WalkBuilder, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindToolScansSingleFilePathLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	mustWrite(t, path, "target\n")
	result, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": path, "glob": "README*"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := result.Details["paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != path || !strings.Contains(result.Content, path) {
		t.Fatalf("find single file path should scan the file like upstream WalkBuilder, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestFindAndGrepPathGlobMatchesRelativePathLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "app.go"), "target\n")
	mustWrite(t, filepath.Join(dir, "app.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "src/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	findPaths, ok := findResult.Details["paths"].([]string)
	if !ok || len(findPaths) != 1 || findPaths[0] != filepath.Join(dir, "src", "app.go") {
		t.Fatalf("find path glob should match relative paths like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "src/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Details["matches"] != 1 || !strings.Contains(grepResult.Content, filepath.Join(dir, "src", "app.go")+":1:") {
		t.Fatalf("grep path glob should match relative paths like upstream, got %q", grepResult.Content)
	}
}

func TestFindAndGrepGlobstarPathGlobMatchesRelativePathLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	wantPath := filepath.Join(dir, "pkg", "src", "app.go")
	mustWrite(t, wantPath, "target\n")
	mustWrite(t, filepath.Join(dir, "pkg", "other", "app.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "**/src/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	findPaths, ok := findResult.Details["paths"].([]string)
	if !ok || len(findPaths) != 1 || findPaths[0] != wantPath {
		t.Fatalf("find globstar path glob should match relative paths like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "**/src/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Details["matches"] != 1 || !strings.Contains(grepResult.Content, wantPath+":1:") {
		t.Fatalf("grep globstar path glob should match relative paths like upstream, got %q", grepResult.Content)
	}
}

func TestFindAndGrepMiddleGlobstarMatchesZeroOrMoreDirectoriesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	wantRootPath := filepath.Join(dir, "src", "app.go")
	wantNestedPath := filepath.Join(dir, "src", "nested", "app.go")
	mustWrite(t, wantRootPath, "target\n")
	mustWrite(t, wantNestedPath, "target\n")
	mustWrite(t, filepath.Join(dir, "other", "app.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "src/**/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	findPaths, ok := findResult.Details["paths"].([]string)
	if !ok || len(findPaths) != 2 || findPaths[0] != wantRootPath || findPaths[1] != wantNestedPath {
		t.Fatalf("find middle globstar should match zero or more directories like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "src/**/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Details["matches"] != 2 || !strings.Contains(grepResult.Content, wantRootPath+":1:") || !strings.Contains(grepResult.Content, wantNestedPath+":1:") || strings.Contains(grepResult.Content, filepath.Join(dir, "other", "app.go")) {
		t.Fatalf("grep middle globstar should match zero or more directories like upstream, got %q", grepResult.Content)
	}
}

func TestFindAndGrepGlobstarSuffixMatchesSubentriesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	wantRootPath := filepath.Join(dir, "src", "app.go")
	wantNestedPath := filepath.Join(dir, "src", "nested", "app.go")
	mustWrite(t, wantRootPath, "target\n")
	mustWrite(t, wantNestedPath, "target\n")
	mustWrite(t, filepath.Join(dir, "other", "app.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "src/**"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	findPaths, ok := findResult.Details["paths"].([]string)
	if !ok || len(findPaths) != 2 || findPaths[0] != wantRootPath || findPaths[1] != wantNestedPath {
		t.Fatalf("find globstar suffix should match subentries like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "src/**"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Details["matches"] != 2 || !strings.Contains(grepResult.Content, wantRootPath+":1:") || !strings.Contains(grepResult.Content, wantNestedPath+":1:") || strings.Contains(grepResult.Content, filepath.Join(dir, "other", "app.go")) {
		t.Fatalf("grep globstar suffix should match subentries like upstream, got %q", grepResult.Content)
	}
}

func TestFindAndGrepMultipleMiddleGlobstarsMatchLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	wantRootPath := filepath.Join(dir, "src", "app.go")
	wantNestedPath := filepath.Join(dir, "src", "nested", "app.go")
	mustWrite(t, wantRootPath, "target\n")
	mustWrite(t, wantNestedPath, "target\n")
	mustWrite(t, filepath.Join(dir, "other", "app.go"), "target\n")

	findResult, err := (FindTool{}).Execute(context.Background(), ai.ToolCall{Name: "find", Arguments: map[string]any{"path": dir, "glob": "src/**/**/app.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	findPaths, ok := findResult.Details["paths"].([]string)
	if !ok || len(findPaths) != 2 || findPaths[0] != wantRootPath || findPaths[1] != wantNestedPath {
		t.Fatalf("find multiple middle globstars should match like upstream, got %q", findResult.Content)
	}

	grepResult, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "src/**/**/app.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Details["matches"] != 2 || !strings.Contains(grepResult.Content, wantRootPath+":1:") || !strings.Contains(grepResult.Content, wantNestedPath+":1:") || strings.Contains(grepResult.Content, filepath.Join(dir, "other", "app.go")) {
		t.Fatalf("grep multiple middle globstars should match like upstream, got %q", grepResult.Content)
	}
}

func TestGrepToolInvalidGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "["}}, nil)
	if err == nil || err.Error() != "error parsing glob '[': unclosed character class; missing ']'" {
		t.Fatalf("grep invalid glob error mismatch: %v", err)
	}
}

func TestGrepToolValidatesGlobBeforeEmptyPathLikeUpstream(t *testing.T) {
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": "", "pattern": "target", "glob": "["}}, nil)
	if err == nil || !strings.Contains(err.Error(), "glob") {
		t.Fatalf("invalid grep glob should fail before empty path like upstream TypesBuilder, got %v", err)
	}
}

func TestGrepToolInvalidRangeGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "[z-a]"}}, nil)
	if err == nil || err.Error() != "error parsing glob '[z-a]': invalid range; 'z' > 'a'" {
		t.Fatalf("grep invalid range glob error mismatch: %v", err)
	}
}

func TestFindAndGrepInvalidRecursiveGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	for _, toolCall := range []ai.ToolCall{
		{Name: "find", Arguments: map[string]any{"path": dir, "glob": "src/**foo"}},
		{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "src/**foo"}},
	} {
		var err error
		if toolCall.Name == "find" {
			_, err = (FindTool{}).Execute(context.Background(), toolCall, nil)
		} else {
			_, err = (GrepTool{}).Execute(context.Background(), toolCall, nil)
		}
		if err == nil || err.Error() != "error parsing glob 'src/**foo': invalid use of **; must be one path component" {
			t.Fatalf("%s invalid recursive glob error mismatch: %v", toolCall.Name, err)
		}
	}
}

func TestGrepToolBangNegatedClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	beePath := filepath.Join(dir, "bee.go")
	mustWrite(t, filepath.Join(dir, "app.go"), "target app\n")
	mustWrite(t, beePath, "target bee\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "[!a]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, beePath) || strings.Contains(result.Content, "app.go") {
		t.Fatalf("grep bang negated class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolDashCharacterClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	dashPath := filepath.Join(dir, "-.go")
	bPath := filepath.Join(dir, "b.go")
	mustWrite(t, dashPath, "target dash\n")
	mustWrite(t, bPath, "target b\n")
	mustWrite(t, filepath.Join(dir, "z.go"), "target z\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "[-abc]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 2 || !strings.Contains(result.Content, dashPath+":1:") || !strings.Contains(result.Content, bPath+":1:") || strings.Contains(result.Content, "z.go") {
		t.Fatalf("grep dash character class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolClosingBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	bracketPath := filepath.Join(dir, "].go")
	mustWrite(t, bracketPath, "target bracket\n")
	mustWrite(t, filepath.Join(dir, "a.go"), "target a\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "[]]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, bracketPath+":1:") || strings.Contains(result.Content, "a.go") {
		t.Fatalf("grep closing bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolNegatedClosingBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.go")
	mustWrite(t, aPath, "target a\n")
	mustWrite(t, filepath.Join(dir, "].go"), "target bracket\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "[!]]*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, aPath+":1:") || strings.Contains(result.Content, "].go") {
		t.Fatalf("grep negated closing bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolBracketEscapedMetasMatchUpstream(t *testing.T) {
	dir := t.TempDir()
	literalPath := filepath.Join(dir, "_[_]_?_*_!_")
	mustWrite(t, literalPath, "target literal\n")
	mustWrite(t, filepath.Join(dir, "_a_a_b_c_!_"), "target other\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "_[[]_[]]_[?]_[*]_!_"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, literalPath+":1:") || strings.Contains(result.Content, "_a_a_b_c_!_") {
		t.Fatalf("grep bracket escaped metas mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolEscapedGlobMetaMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	literalPath := filepath.Join(dir, "*.go")
	mustWrite(t, literalPath, "target literal\n")
	mustWrite(t, filepath.Join(dir, "app.go"), "target app\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": `\*.go`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, literalPath) || strings.Contains(result.Content, "app.go") {
		t.Fatalf("grep escaped glob meta mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolEscapedBraceGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": `\{a,b}.go`}}, nil)
	if err == nil || err.Error() != "error parsing glob '\\{a,b}.go': unopened alternate group; missing '{' (maybe escape '}' with '[}]'?)" {
		t.Fatalf("grep escaped brace glob error mismatch: %v", err)
	}
}

func TestGrepToolEscapedClosingBraceGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": `{a,b\}.go`}}, nil)
	if err == nil || err.Error() != "error parsing glob '{a,b\\}.go': unclosed alternate group; missing '}' (maybe escape '{' with '[{]'?)" {
		t.Fatalf("grep escaped closing brace glob error mismatch: %v", err)
	}
}

func TestGrepToolUnmatchedBraceGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		glob string
		want string
	}{
		{glob: `{a,b.go`, want: "error parsing glob '{a,b.go': unclosed alternate group; missing '}' (maybe escape '{' with '[{]'?)"},
		{glob: `a,b}.go`, want: "error parsing glob 'a,b}.go': unopened alternate group; missing '{' (maybe escape '}' with '[}]'?)"},
	} {
		_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": tc.glob}}, nil)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("grep unmatched brace glob error mismatch for %q: %v", tc.glob, err)
		}
	}
}

func TestGrepToolDanglingEscapeGlobErrorMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	_, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": `abc\`}}, nil)
	if err == nil || err.Error() != "error parsing glob 'abc\\': dangling '\\'" {
		t.Fatalf("grep dangling escape glob error mismatch: %v", err)
	}
}

func TestGrepToolBraceGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "a.go")
	txtPath := filepath.Join(dir, "b.txt")
	mustWrite(t, goPath, "target go\n")
	mustWrite(t, txtPath, "target txt\n")
	mustWrite(t, filepath.Join(dir, "c.rs"), "target rs\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.{go,txt}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 2 || !strings.Contains(result.Content, goPath) || !strings.Contains(result.Content, txtPath) || strings.Contains(result.Content, "c.rs") {
		t.Fatalf("grep brace glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolBraceContainingBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	bracePath := filepath.Join(dir, "}")
	fooPath := filepath.Join(dir, "foo")
	mustWrite(t, bracePath, "target brace\n")
	mustWrite(t, fooPath, "target foo\n")
	mustWrite(t, filepath.Join(dir, "bar"), "target bar\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "{[}],foo}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 2 || !strings.Contains(result.Content, bracePath+":1:") || !strings.Contains(result.Content, fooPath+":1:") || strings.Contains(result.Content, "bar") {
		t.Fatalf("grep brace containing bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolBraceContainingOpenBracketClassMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	bracePath := filepath.Join(dir, "{")
	xPath := filepath.Join(dir, "x")
	mustWrite(t, bracePath, "target brace\n")
	mustWrite(t, xPath, "target x\n")
	mustWrite(t, filepath.Join(dir, "bar"), "target bar\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "{[{],x}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 2 || !strings.Contains(result.Content, bracePath+":1:") || !strings.Contains(result.Content, xPath+":1:") || strings.Contains(result.Content, "bar") {
		t.Fatalf("grep brace containing open bracket class mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolMultipleBraceGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	matches := []string{
		filepath.Join(dir, "a.go.bak"),
		filepath.Join(dir, "b.txt.tmp"),
		filepath.Join(dir, "c.go.tmp"),
		filepath.Join(dir, "d.txt.bak"),
	}
	for _, path := range matches {
		mustWrite(t, path, "target\n")
	}
	mustWrite(t, filepath.Join(dir, "e.rs.bak"), "target\n")
	mustWrite(t, filepath.Join(dir, "f.go"), "target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.{go,txt}.{bak,tmp}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != len(matches) {
		t.Fatalf("grep multiple brace glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
	for _, path := range matches {
		if !strings.Contains(result.Content, path) {
			t.Fatalf("grep multiple brace glob missing %s: %q", path, result.Content)
		}
	}
	if strings.Contains(result.Content, "e.rs.bak") || strings.Contains(result.Content, "f.go") {
		t.Fatalf("grep multiple brace glob included non-matches: %q", result.Content)
	}
}

func TestGrepToolNestedBraceGlobMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	matches := []string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "bc"),
		filepath.Join(dir, "bd"),
	}
	for _, path := range matches {
		mustWrite(t, path, "target\n")
	}
	mustWrite(t, filepath.Join(dir, "b"), "target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "{a,b{c,d}}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != len(matches) {
		t.Fatalf("grep nested brace glob mismatch: content=%q details=%#v", result.Content, result.Details)
	}
	for _, path := range matches {
		if !strings.Contains(result.Content, path+":1:") {
			t.Fatalf("grep nested brace glob missing %s: %q", path, result.Content)
		}
	}
	if strings.Contains(result.Content, filepath.Join(dir, "b")+":1:") {
		t.Fatalf("grep nested brace glob included non-match: %q", result.Content)
	}
}

func TestGrepToolEmptyBraceGroupMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo")
	mustWrite(t, fooPath, "target foo\n")
	mustWrite(t, filepath.Join(dir, "foo{}"), "target literal\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "foo{}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, fooPath+":1:") || strings.Contains(result.Content, "foo{}") {
		t.Fatalf("grep empty brace group mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolCommaOnlyBraceGroupMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo")
	mustWrite(t, fooPath, "target foo\n")
	mustWrite(t, filepath.Join(dir, "foo{,}"), "target literal\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "foo{,}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, fooPath+":1:") || strings.Contains(result.Content, "foo{,}") {
		t.Fatalf("grep comma-only brace group mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolEmptyBraceBranchMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "a.go")
	dotPath := filepath.Join(dir, "a.")
	mustWrite(t, goPath, "target go\n")
	mustWrite(t, dotPath, "target dot\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.{go,}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, goPath) || strings.Contains(result.Content, dotPath+":") {
		t.Fatalf("grep empty brace branch mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolLeadingGlobstarMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.go")
	nestedPath := filepath.Join(dir, "src", "nested", "app.go")
	mustWrite(t, rootPath, "target root\n")
	mustWrite(t, nestedPath, "target nested\n")
	mustWrite(t, filepath.Join(dir, "skip.txt"), "target skip\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "**/*.go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 2 || !strings.Contains(result.Content, rootPath) || !strings.Contains(result.Content, nestedPath) || strings.Contains(result.Content, "skip.txt") {
		t.Fatalf("grep leading globstar mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolNegativeLimitFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for index := 0; index < 201; index++ {
		body.WriteString(fmt.Sprintf("target %03d\n", index))
	}
	mustWrite(t, filepath.Join(dir, "a.txt"), body.String())
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": -1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 200 || !strings.Contains(result.Content, "[truncated at 200 matches]") || strings.Contains(result.Content, "target 200") {
		t.Fatalf("grep negative limit mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolFloatLimitFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "target 1\ntarget 2\ntarget 3\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": 1.0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 3 || strings.Contains(result.Content, "[truncated at") {
		t.Fatalf("grep float limit should fall back to default like serde_json::Value::as_u64, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolLargeUnsignedLimitDoesNotFallBackLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for index := 0; index < 201; index++ {
		body.WriteString(fmt.Sprintf("target %03d\n", index))
	}
	mustWrite(t, filepath.Join(dir, "a.txt"), body.String())
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": uint64(math.MaxUint64)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 201 || strings.Contains(result.Content, "[truncated at 200 matches]") {
		t.Fatalf("grep large unsigned limit should not fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolLargeJSONNumberLimitDoesNotFallBackLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for index := 0; index < 201; index++ {
		body.WriteString(fmt.Sprintf("target %03d\n", index))
	}
	mustWrite(t, filepath.Join(dir, "a.txt"), body.String())
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": json.Number(fmt.Sprint(uint64(math.MaxUint64)))}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 201 || strings.Contains(result.Content, "[truncated at 200 matches]") {
		t.Fatalf("grep large json.Number limit should not fall back to default, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolCentersPreviewAroundLongLineMatch(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("a", 1200) + "TARGET" + strings.Repeat("z", 1200)
	mustWrite(t, filepath.Join(dir, "long.txt"), line+"\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "TARGET"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[line truncated]...") || !strings.Contains(result.Content, "TARGET") || !strings.Contains(result.Content, "...[line truncated]") {
		t.Fatalf("grep long-line preview mismatch: %q", result.Content)
	}
	if result.Details["truncated_lines"] != 1 {
		t.Fatalf("grep truncated details mismatch: %#v", result.Details)
	}
	if result.Details["max_match_line_chars"] != 500 || !strings.Contains(result.Content, "truncated to 500 chars") {
		t.Fatalf("grep max line chars mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolHandlesVeryLongMatchingLineLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("a", 1024*1024) + "TARGET" + strings.Repeat("z", 1024*1024)
	mustWrite(t, filepath.Join(dir, "huge.txt"), line+"\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "TARGET"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || result.Details["truncated_lines"] != 1 || !strings.Contains(result.Content, "TARGET") {
		t.Fatalf("grep huge line mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolSupportsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "case.txt"), "Hello\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "hello", "case_insensitive": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "case.txt:1") {
		t.Fatalf("case-insensitive grep mismatch: %q", result.Content)
	}
}

func TestGrepToolSkipsInvalidUTF8Files(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "text.txt"), "target\n")
	if err := os.WriteFile(filepath.Join(dir, "binary.txt"), []byte{0xff, 't', 'a', 'r', 'g', 'e', 't', '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "text.txt") || strings.Contains(result.Content, "binary.txt") {
		t.Fatalf("grep invalid utf8 skip mismatch: %q", result.Content)
	}
}

func TestGrepToolDoesNotMatchEmptyFileAsEmptyLine(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "empty.txt"), "")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "^$"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "grep: 0 hits\n" || result.Details["matches"] != 0 {
		t.Fatalf("grep empty file line mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolTreatsCRLFLinesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	mustWrite(t, path, "target\r\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target$"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, path+":1: target\n") {
		t.Fatalf("grep CRLF line mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolSkipsUnreadableFilesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing.txt"), filepath.Join(dir, "broken.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "ok.txt"), "target\n")

	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "glob": "*.txt"}}, nil)
	if err != nil {
		t.Fatalf("grep should skip unreadable files like upstream, got %v", err)
	}
	if result.Details["matches"] != 1 || !strings.Contains(result.Content, "ok.txt") || strings.Contains(result.Content, "broken.txt") {
		t.Fatalf("grep unreadable file mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestGrepToolStopsAfterMaxScannedFiles(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 5001; index++ {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("%04d.txt", index)), "miss\n")
	}
	mustWrite(t, filepath.Join(dir, "5001.txt"), "target\n")
	result, err := (GrepTool{}).Execute(context.Background(), ai.ToolCall{Name: "grep", Arguments: map[string]any{"path": dir, "pattern": "target", "limit": 10}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Content, "grep: 0 hits\n") {
		t.Fatalf("grep max scanned files mismatch: %q", result.Content)
	}
}

func TestBashToolSuccessAndTimeout(t *testing.T) {
	dir := t.TempDir()
	bash := BashTool{Timeout: 2 * time.Second}
	result, err := bash.Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "printf hello", "cwd": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "$ printf hello") || !strings.Contains(result.Content, "hello\n[exit 0]") {
		t.Fatalf("bash result mismatch: %q", result.Content)
	}
	if result.Details["command"] != "printf hello" || result.Details["exitCode"] != 0 || result.Details["isError"] != false {
		t.Fatalf("bash details mismatch: %#v", result.Details)
	}

	bash.Timeout = 10 * time.Millisecond
	result, err = bash.Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "sleep 1", "cwd": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[timed out after 1s]") || result.Details["exitCode"] != -1 || result.Details["isError"] != true {
		t.Fatalf("expected timeout result, got %q", result.Content)
	}
}

func TestBashToolReportsNonzeroExitLikeUpstream(t *testing.T) {
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "exit 3"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[exit 3]") || result.Details["exitCode"] != 3 || result.Details["isError"] != true {
		t.Fatalf("expected nonzero exit result, got content=%q details=%#v", result.Content, result.Details)
	}
}

func TestBashToolTimeoutZeroMatchesUpstream(t *testing.T) {
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "sleep 1", "timeout": 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[timed out after 0s]") || result.Details["exitCode"] != -1 || result.Details["isError"] != true {
		t.Fatalf("expected timeout=0 result, got content=%q details=%#v", result.Content, result.Details)
	}
}

func TestBashToolFloatTimeoutFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "printf ok", "timeout": 0.0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "$ printf ok\nok\n[exit 0]" || result.Details["exitCode"] != 0 {
		t.Fatalf("float timeout should be ignored like serde_json::Value::as_u64, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestBashToolIgnoresNonUpstreamTimeoutMSArgument(t *testing.T) {
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "printf ok", "timeout_ms": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "$ printf ok\nok\n[exit 0]" || result.Details["exitCode"] != 0 {
		t.Fatalf("bash should ignore non-upstream timeout_ms argument, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestBashToolDropsInvalidUTF8OutputLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printf byte escape syntax is Unix-specific")
	}
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "printf 'ok \\377!'"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, string([]byte{0xff})) {
		t.Fatalf("invalid UTF-8 stdout should be dropped like upstream, got %q", result.Content)
	}
	if result.Content != "$ printf 'ok \\377!'\n[exit 0]" {
		t.Fatalf("bash invalid UTF-8 content mismatch: %q", result.Content)
	}
}

func TestBashToolStderrTruncationMatchesUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell loop syntax is Unix-specific")
	}
	command := "i=1; while [ $i -le 2105 ]; do echo err-$i >&2; i=$((i+1)); done"
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": command}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "[truncated:") {
		t.Fatalf("stderr truncation should not render a note, got prefix %q", result.Content[:min(len(result.Content), 180)])
	}
	if strings.Contains(result.Content, "err-1\n") {
		t.Fatalf("expected stderr head to be truncated, got prefix %q", result.Content[:min(len(result.Content), 180)])
	}
	if !strings.Contains(result.Content, "err-2105\n[exit 0]") {
		t.Fatalf("expected stderr tail to be preserved, got suffix %q", result.Content[max(0, len(result.Content)-180):])
	}
}

func TestBashToolHighVolumeStderrDoesNotDeadlockLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell pipeline syntax is Unix-specific")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	result, err := (BashTool{Timeout: 5 * time.Second}).Execute(ctx, ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "yes hello | head -c 262144 ; yes world | head -c 262144 1>&2"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("high-volume stderr drain took %s; possible sequential drain regression", elapsed)
	}
	if !strings.Contains(result.Content, "[stderr]") || !strings.Contains(result.Content, "[exit 0]") {
		t.Fatalf("high-volume stderr result mismatch: %q", result.Content[:min(len(result.Content), 200)])
	}
}

func TestBashToolDefinitionMatchesUpstream(t *testing.T) {
	tool := BashTool{}
	wantDescription := "Run a shell command via `sh -c`. Returns stdout+stderr (tail-truncated to 2000 lines / 256 KiB) and exit code. Optional `timeout` in seconds. Timeouts and cancellations kill the child process; stdout and stderr are drained concurrently so high-output commands do not deadlock the tool."
	if tool.Description() != wantDescription {
		t.Fatalf("bash description mismatch:\nwant: %q\n got: %q", wantDescription, tool.Description())
	}
	params := tool.Parameters()
	properties := params["properties"].(map[string]any)
	if len(properties) != 2 {
		t.Fatalf("bash properties mismatch: %#v", properties)
	}
	command := properties["command"].(map[string]any)
	if command["type"] != "string" || command["description"] != "Shell command to execute" {
		t.Fatalf("bash command property mismatch: %#v", command)
	}
	timeout := properties["timeout"].(map[string]any)
	if timeout["type"] != "integer" || timeout["description"] != "Timeout in seconds (optional). On timeout the child is killed and any output captured so far is returned." {
		t.Fatalf("bash timeout property mismatch: %#v", timeout)
	}
}

func TestBashToolAllowsEmptyCommand(t *testing.T) {
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "$ \n[exit 0]" {
		t.Fatalf("empty bash command content mismatch: %q", result.Content)
	}
	if result.Details["command"] != "" || result.Details["exitCode"] != 0 || result.Details["isError"] != false {
		t.Fatalf("empty bash command details mismatch: %#v", result.Details)
	}
}

func TestBashToolNonStringCommandReportsMissing(t *testing.T) {
	_, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": 123}}, nil)
	if err == nil || err.Error() != "missing `command`" {
		t.Fatalf("expected upstream-style non-string command error, got %v", err)
	}
}

func TestBashToolRejectsInvalidUTF8Command(t *testing.T) {
	_, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "command must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 command error, got %v", err)
	}
}

func TestBashToolIgnoresNonUpstreamCWDArgument(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	result, err := (BashTool{Timeout: time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "printf ok", "cwd": dir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "$ printf ok\nok\n[exit 0]" || result.Details["exitCode"] != 0 {
		t.Fatalf("bash should ignore non-upstream cwd argument, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestBashToolTruncatesOutputFromHeadKeepingTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell loop syntax is Unix-specific")
	}
	command := "i=1; while [ $i -le 2105 ]; do echo line-$i; i=$((i+1)); done"
	result, err := (BashTool{Timeout: 2 * time.Second}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": command}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "line-1\n") {
		t.Fatalf("expected head output to be truncated, got prefix %q", result.Content[:min(len(result.Content), 120)])
	}
	if !strings.Contains(result.Content, "line-2105") {
		t.Fatalf("expected tail output to be preserved, got suffix %q", result.Content[max(0, len(result.Content)-160):])
	}
	if !strings.Contains(result.Content, "[truncated: kept 2000/2105 lines") {
		t.Fatalf("expected detailed truncation note, got prefix %q", result.Content[:min(len(result.Content), 160)])
	}
}

func TestBashToolCancelledContextReportsAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (BashTool{Timeout: time.Second}).Execute(ctx, ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "sleep 1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[stderr]\n[aborted]\n[exit -1]") {
		t.Fatalf("cancelled bash content mismatch: %q", result.Content)
	}
	if result.Details["exitCode"] != -1 || result.Details["isError"] != true {
		t.Fatalf("cancelled bash details mismatch: %#v", result.Details)
	}
}

func TestBashToolCancelledContextAppendsAbortedAfterStderr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	result, err := (BashTool{Timeout: time.Second}).Execute(ctx, ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": "printf err >&2; sleep 1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stderrIndex := strings.Index(result.Content, "[stderr]\nerr")
	abortedIndex := strings.Index(result.Content, "[aborted]")
	exitIndex := strings.Index(result.Content, "[exit -1]")
	if stderrIndex < 0 || abortedIndex < 0 || exitIndex < 0 || stderrIndex > abortedIndex || abortedIndex > exitIndex {
		t.Fatalf("cancelled stderr content mismatch: %q", result.Content)
	}
}

func TestBashToolTimeoutKillsBackgroundProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group kill behavior is Unix-specific")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	command := fmt.Sprintf("(sleep 5 & echo $! > %q; wait)", pidFile)
	started := time.Now()
	result, err := (BashTool{Timeout: 20 * time.Millisecond}).Execute(context.Background(), ai.ToolCall{Name: "bash", Arguments: map[string]any{"command": command}}, nil)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > time.Second {
		t.Fatalf("timeout waited for background process: elapsed=%s result=%q", elapsed, result.Content)
	}
	if result.Details["exitCode"] != -1 {
		t.Fatalf("expected timeout exit, got %#v", result)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid := strings.TrimSpace(string(data))
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if exec.Command("kill", "-0", pid).Run() != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = exec.Command("kill", "-9", pid).Run()
	t.Fatalf("background process %s survived bash timeout", pid)
}

func TestBuiltinToolsExposeAgentTools(t *testing.T) {
	builtins := BuiltinTools()
	for _, name := range []string{"read", "write", "edit", "ls", "find", "grep", "bash"} {
		if builtins[name] == nil {
			t.Fatalf("missing builtin %s", name)
		}
		if builtins[name].Name() != name || builtins[name].Description() == "" {
			t.Fatalf("bad builtin metadata for %s", name)
		}
	}
}

func TestDefaultToolsExposeOrderedCoreToolsWithMemory(t *testing.T) {
	dir := t.TempDir()
	toolNames := toolNames(DefaultTools(dir))
	expected := []string{"read", "write", "edit", "bash", "ls", "grep", "find", "web_fetch", "web_search", "git", "memory"}
	if strings.Join(toolNames, ",") != strings.Join(expected, ",") {
		t.Fatalf("default tool names mismatch: got %v want %v", toolNames, expected)
	}
}

func TestUpstreamToolBuilderFunctionsExposeExistingTools(t *testing.T) {
	model := ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")}
	registry := triggers.NewDynamicRegistry()
	cronRegistry := triggers.NewScheduledCronRegistry()
	root := t.TempDir()
	catalog := skills.NewCatalog(skills.CatalogOptions{})

	cases := []struct {
		name string
		tool agent.Tool
	}{
		{"task", task_tool(model, nil)},
		{"Skill", skill_tool(catalog)},
		{"InstallSkill", install_skill_tool(root, catalog)},
		{"SkillBuilder", skill_builder_tool(root, catalog)},
		{"SetSkillState", set_skill_state_tool(catalog)},
		{"RemoveSkill", remove_skill_tool(root, catalog)},
		{"NewCronJob", new_cron_job_tool(cronRegistry)},
		{"ListCronJobs", list_cron_jobs_tool(cronRegistry)},
		{"RemoveCronJob", remove_cron_job_tool(cronRegistry)},
		{"SetCronJobState", set_cron_job_state_tool(cronRegistry)},
		{"NewTrigger", new_trigger_tool(registry)},
		{"ListTriggers", list_triggers_tool(registry)},
		{"RemoveTrigger", remove_trigger_tool(registry)},
		{"SetTriggerState", set_trigger_state_tool(registry)},
	}
	for _, tc := range cases {
		if tc.tool == nil || tc.tool.Name() != tc.name {
			t.Fatalf("builder %s returned %#v", tc.name, tc.tool)
		}
	}
}

func TestSubagentReadOnlyToolsExcludeMutatingTools(t *testing.T) {
	toolNames := toolNames(SubagentReadOnlyTools())
	expected := []string{"read", "ls", "grep", "find", "web_fetch", "git"}
	if strings.Join(toolNames, ",") != strings.Join(expected, ",") {
		t.Fatalf("read-only tool names mismatch: got %v want %v", toolNames, expected)
	}
}

func toolNames(toolset []agent.Tool) []string {
	names := make([]string, 0, len(toolset))
	for _, tool := range toolset {
		names = append(names, tool.Name())
	}
	return names
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
