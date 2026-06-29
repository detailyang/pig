package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeEnvFileOperationsMatchUpstreamSurface(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	var _ ExecutionEnv = env

	if env.CWD() != root {
		t.Fatalf("cwd mismatch: %q", env.CWD())
	}
	absolute, err := env.AbsolutePath(context.Background(), "dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if absolute != filepath.Join(root, "dir", "file.txt") {
		t.Fatalf("absolute path mismatch: %q", absolute)
	}
	joined, err := env.JoinPath(context.Background(), []string{root, "dir", "file.txt"})
	if err != nil || joined != filepath.Join(root, "dir", "file.txt") {
		t.Fatalf("join path mismatch: %q err=%v", joined, err)
	}
	if err := env.CreateDir(context.Background(), "dir", true); err != nil {
		t.Fatal(err)
	}
	if err := env.WriteFile(context.Background(), "dir/file.txt", []byte("one\ntwo\nthree\n")); err != nil {
		t.Fatal(err)
	}
	if err := env.AppendFile(context.Background(), "dir/file.txt", []byte("four\n")); err != nil {
		t.Fatal(err)
	}
	text, err := env.ReadTextFile(context.Background(), "dir/file.txt")
	if err != nil || text != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("read text mismatch %q err=%v", text, err)
	}
	maxLines := 2
	lines, err := env.ReadTextLines(context.Background(), "dir/file.txt", &maxLines)
	if err != nil || strings.Join(lines, ",") != "one,two" {
		t.Fatalf("read lines mismatch %#v err=%v", lines, err)
	}
	info, err := env.FileInfo(context.Background(), "dir/file.txt")
	if err != nil || info.Name != "file.txt" || info.Kind != FileKindFile || info.Size == 0 || info.MTimeMS == 0 {
		t.Fatalf("file info mismatch %#v err=%v", info, err)
	}
	entries, err := env.ListDir(context.Background(), "dir")
	if err != nil || len(entries) != 1 || entries[0].Name != "file.txt" {
		t.Fatalf("list dir mismatch %#v err=%v", entries, err)
	}
	exists, err := env.Exists(context.Background(), "dir/file.txt")
	if err != nil || !exists {
		t.Fatalf("exists mismatch %v err=%v", exists, err)
	}
	canonical, err := env.CanonicalPath(context.Background(), "dir/../dir/file.txt")
	wantCanonical, wantErr := filepath.EvalSymlinks(filepath.Join(root, "dir", "file.txt"))
	if wantErr != nil {
		t.Fatal(wantErr)
	}
	if err != nil || canonical != wantCanonical {
		t.Fatalf("canonical mismatch %q want %q err=%v", canonical, wantCanonical, err)
	}
}

func TestNativeEnvCurrentCompatSurface(t *testing.T) {
	env, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if env.CWD() != cwd {
		t.Fatalf("current cwd mismatch: got %q want %q", env.CWD(), cwd)
	}
}

func TestNativeEnvPathHelpersIgnoreCanceledContextLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	absolute, err := env.AbsolutePath(ctx, "dir/file.txt")
	if err != nil || absolute != filepath.Join(root, "dir", "file.txt") {
		t.Fatalf("absolute path should ignore cancel like upstream: path=%q err=%v", absolute, err)
	}
	joined, err := env.JoinPath(ctx, []string{root, "dir", "file.txt"})
	if err != nil || joined != filepath.Join(root, "dir", "file.txt") {
		t.Fatalf("join path should ignore cancel like upstream: path=%q err=%v", joined, err)
	}
}

func TestNativeEnvAbsolutePathPreservesRelativeSegmentsLikeRustPathJoin(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	absolute, err := env.AbsolutePath(context.Background(), "dir/../file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := root + string(filepath.Separator) + "dir" + string(filepath.Separator) + ".." + string(filepath.Separator) + "file.txt"
	if absolute != want {
		t.Fatalf("absolute path should preserve relative segments like upstream, got %q want %q", absolute, want)
	}
}

func TestNativeEnvAbsolutePathEmptyPreservesTrailingSeparatorLikeRustPathJoin(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	absolute, err := env.AbsolutePath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := root + string(filepath.Separator)
	if absolute != want {
		t.Fatalf("empty relative path should preserve trailing separator like upstream, got %q want %q", absolute, want)
	}
}

func TestNativeEnvAbsolutePathDoesNotDoubleSeparatorWhenCWDHasTrailingSeparatorLikeRustPathJoin(t *testing.T) {
	root := t.TempDir() + string(filepath.Separator)
	env := NewNativeEnv(root)
	absolute, err := env.AbsolutePath(context.Background(), "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimRight(root, string(filepath.Separator)) + string(filepath.Separator) + "file.txt"
	if absolute != want {
		t.Fatalf("cwd trailing separator should not be doubled like upstream, got %q want %q", absolute, want)
	}
}

func TestNativeEnvFileInfoDotHasEmptyNameLikeRustPathFileName(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	info, err := env.FileInfo(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "" || info.Kind != FileKindDirectory {
		t.Fatalf("dot file info should use empty file_name like upstream: %#v", info)
	}
}

func TestNativeEnvFileInfoParentSegmentHasEmptyNameLikeRustPathFileName(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	if err := env.CreateDir(context.Background(), "dir", true); err != nil {
		t.Fatal(err)
	}
	info, err := env.FileInfo(context.Background(), "dir/..")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "" || info.Kind != FileKindDirectory {
		t.Fatalf("parent segment file info should use empty file_name like upstream: %#v", info)
	}
}

func TestNativeEnvFileInfoCurrentSegmentUsesParentNameLikeRustPathFileName(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	if err := env.CreateDir(context.Background(), "dir", true); err != nil {
		t.Fatal(err)
	}
	info, err := env.FileInfo(context.Background(), "dir/.")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "dir" || info.Kind != FileKindDirectory {
		t.Fatalf("current segment file info should use parent name like upstream: %#v", info)
	}
}

func TestNativeEnvJoinPathAbsolutePartResetsLikeRustPathBuf(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	joined, err := env.JoinPath(context.Background(), []string{"/root", "/abs", "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if joined != filepath.Join("/abs", "file.txt") {
		t.Fatalf("absolute join segment should reset previous parts like Rust PathBuf::push, got %q", joined)
	}
}

func TestNativeEnvJoinPathPreservesParentSegmentsLikeRustPathBuf(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	joined, err := env.JoinPath(context.Background(), []string{"root", "dir", "..", "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := "root" + string(filepath.Separator) + "dir" + string(filepath.Separator) + ".." + string(filepath.Separator) + "file.txt"
	if joined != want {
		t.Fatalf("join_path should preserve parent segments like upstream, got %q want %q", joined, want)
	}
}

func TestNativeEnvJoinPathDoesNotDoubleSeparatorAfterTrailingSeparatorLikeRustPathBuf(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	joined, err := env.JoinPath(context.Background(), []string{string(filepath.Separator) + "root" + string(filepath.Separator), "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := string(filepath.Separator) + "root" + string(filepath.Separator) + "file.txt"
	if joined != want {
		t.Fatalf("join_path should not double separator after trailing separator like upstream, got %q want %q", joined, want)
	}
}

func TestNativeEnvJoinPathEmptyPartsReturnsEmptyLikeRustPathBuf(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	joined, err := env.JoinPath(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if joined != "" {
		t.Fatalf("empty join should match Rust PathBuf::new string form, got %q", joined)
	}
}

func TestNativeEnvJoinPathSingleEmptyPartReturnsEmptyLikeRustPathBuf(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	joined, err := env.JoinPath(context.Background(), []string{""})
	if err != nil {
		t.Fatal(err)
	}
	if joined != "" {
		t.Fatalf("single empty path part should stay empty like Rust PathBuf::push, got %q", joined)
	}
}

func TestNativeEnvExistsTreatsBrokenSymlinkAsExistingLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	if err := os.Symlink("missing-target", filepath.Join(root, "broken-link")); err != nil {
		t.Fatal(err)
	}

	exists, err := env.Exists(context.Background(), "broken-link")
	if err != nil || !exists {
		t.Fatalf("broken symlink should exist like upstream: exists=%v err=%v", exists, err)
	}
}

func TestNativeEnvListDirFollowsSymlinkMetadataLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	if err := env.CreateDir(context.Background(), "target", true); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}

	entries, err := env.ListDir(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	var linkInfo FileInfo
	for _, entry := range entries {
		if entry.Name == "link-dir" {
			linkInfo = entry
			break
		}
	}
	if linkInfo.Name == "" || linkInfo.Kind != FileKindDirectory {
		t.Fatalf("list_dir should follow symlink metadata like upstream: %#v", entries)
	}
}

func TestNativeEnvListDirEntryPathPreservesParentSegmentsLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	if err := env.WriteFile(context.Background(), "file.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}

	entries, err := env.ListDir(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries mismatch: %#v", entries)
	}
	want := root + string(filepath.Separator) + "." + string(filepath.Separator) + "file.txt"
	if entries[0].Path != want {
		t.Fatalf("list_dir entry path should preserve resolved parent segments like upstream, got %q want %q", entries[0].Path, want)
	}
}

func TestNativeEnvListDirEmptyPathDoesNotDoubleSeparatorLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	if err := env.WriteFile(context.Background(), "file.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}

	entries, err := env.ListDir(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries mismatch: %#v", entries)
	}
	want := root + string(filepath.Separator) + "file.txt"
	if entries[0].Path != want {
		t.Fatalf("empty list_dir path should not double separator like upstream, got %q want %q", entries[0].Path, want)
	}
}

func TestNativeEnvListDirErrorPathUsesEntryPathLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)
	if err := os.Symlink("missing-target", filepath.Join(root, "broken-link")); err != nil {
		t.Fatal(err)
	}

	_, err := env.ListDir(context.Background(), ".")
	var fileErr FileError
	wantPath := root + string(filepath.Separator) + "." + string(filepath.Separator) + "broken-link"
	if !errors.As(err, &fileErr) || fileErr.Path != wantPath {
		t.Fatalf("list_dir error should use upstream entry.path, got %#v want path %q", err, wantPath)
	}
}

func TestNativeEnvReadTextLinesHandlesLongLinesLikeUpstream(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	longLine := strings.Repeat("x", 128*1024)
	if err := env.WriteFile(context.Background(), "long.txt", []byte(longLine+"\nshort\n")); err != nil {
		t.Fatal(err)
	}

	lines, err := env.ReadTextLines(context.Background(), "long.txt", nil)
	if err != nil || len(lines) != 2 || lines[0] != longLine || lines[1] != "short" {
		t.Fatalf("long lines mismatch len=%d err=%v", len(lines), err)
	}
}

func TestNativeEnvReadTextLinesOnlyStripsCRBeforeNewlineLikeUpstream(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	if err := env.WriteFile(context.Background(), "cr.txt", []byte("a\r\nb\r")); err != nil {
		t.Fatal(err)
	}

	lines, err := env.ReadTextLines(context.Background(), "cr.txt", nil)
	if err != nil || len(lines) != 2 || lines[0] != "a" || lines[1] != "b\r" {
		t.Fatalf("CR handling mismatch lines=%#v err=%v", lines, err)
	}
}

func TestNativeEnvTextReadersRejectInvalidUTF8LikeUpstream(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	if err := env.WriteFile(context.Background(), "invalid.txt", []byte{0xff, '\n'}); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func() error{
		"read_text_file": func() error {
			_, err := env.ReadTextFile(context.Background(), "invalid.txt")
			return err
		},
		"read_text_lines": func() error {
			_, err := env.ReadTextLines(context.Background(), "invalid.txt", nil)
			return err
		},
	} {
		err := read()
		var fileErr FileError
		if !errors.As(err, &fileErr) || fileErr.Code != FileErrorInvalidPath || fileErr.Path != "invalid.txt" {
			t.Fatalf("%s should reject invalid UTF-8 like upstream, got %#v", name, err)
		}
	}
}

func TestNativeEnvMapsMissingFileErrors(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	_, err := env.ReadTextFile(context.Background(), "missing.txt")
	var fileErr FileError
	if !errors.As(err, &fileErr) || fileErr.Code != FileErrorNotFound || fileErr.Path != "missing.txt" {
		t.Fatalf("missing file error mismatch: %#v", err)
	}
}

func TestNativeEnvWriteAndAppendCreateParentDirsLikeUpstream(t *testing.T) {
	env := NewNativeEnv(t.TempDir())

	if err := env.WriteFile(context.Background(), "nested/dir/file.txt", []byte("one")); err != nil {
		t.Fatalf("write should create parent dirs: %v", err)
	}
	if err := env.AppendFile(context.Background(), "other/dir/file.txt", []byte("two")); err != nil {
		t.Fatalf("append should create parent dirs: %v", err)
	}
	written, err := env.ReadTextFile(context.Background(), "nested/dir/file.txt")
	if err != nil || written != "one" {
		t.Fatalf("written file mismatch %q err=%v", written, err)
	}
	appended, err := env.ReadTextFile(context.Background(), "other/dir/file.txt")
	if err != nil || appended != "two" {
		t.Fatalf("appended file mismatch %q err=%v", appended, err)
	}
}

func TestNativeEnvWriteAndAppendCreateUncleanParentDirsLikeUpstream(t *testing.T) {
	root := t.TempDir()
	env := NewNativeEnv(root)

	if err := env.WriteFile(context.Background(), "write-dir/../written.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := env.AppendFile(context.Background(), "append-dir/../appended.txt", []byte("two")); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"write-dir", "append-dir"} {
		info, err := os.Stat(filepath.Join(root, dir))
		if err != nil || !info.IsDir() {
			t.Fatalf("%s should be created as parent before .. like upstream, info=%#v err=%v", dir, info, err)
		}
	}
}

func TestNativeEnvRemoveMissingPathAndTempNamesLikeUpstream(t *testing.T) {
	env := NewNativeEnv(t.TempDir())

	if err := env.Remove(context.Background(), "missing", false, false); err != nil {
		t.Fatalf("missing remove should be ignored like upstream: %v", err)
	}
	dir, err := env.CreateTempDir(context.Background(), "pig")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^pig-[0-9a-f]{32}$`).MatchString(filepath.Base(dir)) {
		t.Fatalf("temp dir prefix mismatch: %q", dir)
	}
	dashedDir, err := env.CreateTempDir(context.Background(), "pig-")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^pig--[0-9a-f]{32}$`).MatchString(filepath.Base(dashedDir)) {
		t.Fatalf("temp dir dashed prefix mismatch: %q", dashedDir)
	}
	file, err := env.CreateTempFile(context.Background(), "pig-", ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^pig-[0-9a-f]{32}\.txt$`).MatchString(filepath.Base(file)) {
		t.Fatalf("temp file name mismatch: %q", file)
	}
	defaultFile, err := env.CreateTempFile(context.Background(), "", ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}\.txt$`).MatchString(filepath.Base(defaultFile)) {
		t.Fatalf("default temp file name mismatch: %q", defaultFile)
	}
	exists, err := env.Exists(context.Background(), file)
	if err != nil || !exists {
		t.Fatalf("temp file should exist: exists=%v err=%v", exists, err)
	}
	defaultDir, err := env.CreateTempDir(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^tmp--[0-9a-f]{32}$`).MatchString(filepath.Base(defaultDir)) {
		t.Fatalf("default temp dir prefix mismatch: %q", defaultDir)
	}
}

func TestNativeEnvExecAndTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command syntax is Unix-specific")
	}
	env := NewNativeEnv(t.TempDir())
	var stdoutLines []string
	out, err := env.Exec(context.Background(), "printf 'out\\n'; printf 'err\\n' >&2", ExecOptions{OnStdout: func(line string) { stdoutLines = append(stdoutLines, line) }})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 || out.Stdout != "out\n" || out.Stderr != "err\n" || len(stdoutLines) != 1 || stdoutLines[0] != "out" {
		t.Fatalf("exec output mismatch out=%#v stdoutLines=%#v", out, stdoutLines)
	}
	_, err = env.Exec(context.Background(), "sleep 2", ExecOptions{Timeout: 50 * time.Millisecond})
	var execErr ExecutionError
	if !errors.As(err, &execErr) || execErr.Code != ExecutionErrorTimeout {
		t.Fatalf("timeout error mismatch: %#v", err)
	}
}

func TestNativeEnvExecUsesUpstreamTimeoutSecsAndAbort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command syntax is Unix-specific")
	}
	env := NewNativeEnv(t.TempDir())
	_, err := env.Exec(context.Background(), "sleep 2", ExecOptions{TimeoutSecs: 0.05})
	var execErr ExecutionError
	if !errors.As(err, &execErr) || execErr.Code != ExecutionErrorTimeout {
		t.Fatalf("expected timeout via TimeoutSecs, got %#v", err)
	}
	abort := make(chan struct{})
	close(abort)
	_, err = env.Exec(context.Background(), "sleep 2", ExecOptions{Abort: abort})
	if !errors.As(err, &execErr) || execErr.Code != ExecutionErrorAborted {
		t.Fatalf("expected aborted via Abort, got %#v", err)
	}
}

func TestNativeEnvExecRelativeCWDIsProcessRelativeLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command syntax is Unix-specific")
	}
	processRoot := t.TempDir()
	previousCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(processRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousCWD) })
	if err := os.Mkdir("run-dir", 0o755); err != nil {
		t.Fatal(err)
	}
	env := NewNativeEnv(filepath.Join(processRoot, "env-root"))

	out, err := env.Exec(context.Background(), "pwd", ExecOptions{CWD: "run-dir"})
	if err != nil {
		t.Fatal(err)
	}
	wantCWD, err := filepath.EvalSymlinks(filepath.Join(processRoot, "run-dir"))
	if err != nil {
		t.Fatal(err)
	}
	gotCWD, err := filepath.EvalSymlinks(strings.TrimSpace(out.Stdout))
	if err != nil {
		t.Fatal(err)
	}
	if gotCWD != wantCWD {
		t.Fatalf("exec cwd mismatch stdout=%q", out.Stdout)
	}
}

func TestNativeEnvExecCallbacksReceiveFinalLineWithoutNewlineLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command syntax is Unix-specific")
	}
	env := NewNativeEnv(t.TempDir())
	var stdoutLines []string
	var stderrLines []string

	out, err := env.Exec(context.Background(), "printf tail; printf errtail >&2", ExecOptions{
		OnStdout: func(line string) { stdoutLines = append(stdoutLines, line) },
		OnStderr: func(line string) { stderrLines = append(stderrLines, line) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stdout != "tail" || out.Stderr != "errtail" || strings.Join(stdoutLines, ",") != "tail" || strings.Join(stderrLines, ",") != "errtail" {
		t.Fatalf("final callback mismatch out=%#v stdoutLines=%#v stderrLines=%#v", out, stdoutLines, stderrLines)
	}
}

func TestNativeEnvExecTimeoutKillsBackgroundDescendantsLikeUpstream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group teardown is Unix-specific")
	}
	env := NewNativeEnv(t.TempDir())
	marker := filepath.Join(t.TempDir(), "leak-marker")

	_, err := env.Exec(context.Background(), "(sleep 1; touch '"+marker+"') & wait", ExecOptions{Timeout: 50 * time.Millisecond})
	var execErr ExecutionError
	if !errors.As(err, &execErr) || execErr.Code != ExecutionErrorTimeout {
		t.Fatalf("timeout error mismatch: %#v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if exists, statErr := nativeEnvFileExists(marker); statErr != nil || exists {
		t.Fatalf("background descendant survived timeout: exists=%v err=%v marker=%s", exists, statErr, marker)
	}
}

func nativeEnvFileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
