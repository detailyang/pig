package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestHarnessFileAndExecutionTypesMatchUpstreamWire(t *testing.T) {
	fileErr := FileError{Code: FileErrorNotFound, Message: "missing", Path: "README.md"}
	if fileErr.Error() != "missing" {
		t.Fatalf("file error message mismatch: %q", fileErr.Error())
	}
	withPath := (FileError{Code: FileErrorInvalidPath, Message: "bad"}).WithPath("bad.txt")
	if withPath.Path != "bad.txt" || withPath.Code != FileErrorInvalidPath || withPath.Message != "bad" {
		t.Fatalf("file error with path mismatch: %#v", withPath)
	}
	data, err := json.Marshal(fileErr.Code)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"not_found"` {
		t.Fatalf("file error code should marshal snake_case, got %s", data)
	}

	info := FileInfo{Name: "README.md", Path: "/repo/README.md", Kind: FileKindFile, Size: 12, MTimeMS: 1234}
	data, err = json.Marshal(info.Kind)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"file"` {
		t.Fatalf("file kind should marshal lowercase, got %s", data)
	}

	execErr := ExecutionError{Code: ExecutionErrorTimeout, Message: "timed out"}
	if execErr.Error() != "timed out" {
		t.Fatalf("execution error message mismatch: %q", execErr.Error())
	}
	data, err = json.Marshal(execErr.Code)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"timeout"` {
		t.Fatalf("execution error code should marshal snake_case, got %s", data)
	}
}

func TestExecutionEnvInterfaceMatchesUpstreamSurface(t *testing.T) {
	env := stubExecutionEnv{cwd: "/repo"}
	var iface ExecutionEnv = env
	if iface.CWD() != "/repo" {
		t.Fatalf("cwd mismatch: %q", iface.CWD())
	}
	if path, err := iface.AbsolutePath(context.Background(), "README.md"); err != nil || path != "/repo/README.md" {
		t.Fatalf("absolute path mismatch: %q %v", path, err)
	}
	if output, err := iface.Exec(context.Background(), "echo hi", ExecOptions{CWD: "/repo", Timeout: time.Second}); err != nil || output.Stdout != "hi\n" || output.ExitCode != 0 {
		t.Fatalf("exec mismatch: %#v %v", output, err)
	}
	if err := iface.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup mismatch: %v", err)
	}
	if _, err := iface.ReadTextFile(context.Background(), "missing"); !errors.As(err, &FileError{}) {
		t.Fatalf("expected FileError, got %T %v", err, err)
	}
}

type stubExecutionEnv struct{ cwd string }

func (env stubExecutionEnv) CWD() string { return env.cwd }

func (env stubExecutionEnv) AbsolutePath(ctx context.Context, path string) (string, error) {
	return env.cwd + "/" + path, nil
}

func (env stubExecutionEnv) JoinPath(ctx context.Context, parts []string) (string, error) {
	return "/repo/README.md", nil
}

func (env stubExecutionEnv) ReadTextFile(ctx context.Context, path string) (string, error) {
	return "", FileError{Code: FileErrorNotFound, Message: "missing", Path: path}
}

func (env stubExecutionEnv) ReadTextLines(ctx context.Context, path string, maxLines *int) ([]string, error) {
	return nil, nil
}

func (env stubExecutionEnv) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	return nil, nil
}

func (env stubExecutionEnv) WriteFile(ctx context.Context, path string, content []byte) error {
	return nil
}

func (env stubExecutionEnv) AppendFile(ctx context.Context, path string, content []byte) error {
	return nil
}

func (env stubExecutionEnv) FileInfo(ctx context.Context, path string) (FileInfo, error) {
	return FileInfo{}, nil
}

func (env stubExecutionEnv) ListDir(ctx context.Context, path string) ([]FileInfo, error) {
	return nil, nil
}

func (env stubExecutionEnv) Exists(ctx context.Context, path string) (bool, error) { return false, nil }

func (env stubExecutionEnv) CanonicalPath(ctx context.Context, path string) (string, error) {
	return path, nil
}

func (env stubExecutionEnv) CreateDir(ctx context.Context, path string, recursive bool) error {
	return nil
}

func (env stubExecutionEnv) Remove(ctx context.Context, path string, recursive bool, force bool) error {
	return nil
}

func (env stubExecutionEnv) CreateTempDir(ctx context.Context, prefix string) (string, error) {
	return "/tmp/dir", nil
}

func (env stubExecutionEnv) CreateTempFile(ctx context.Context, prefix string, suffix string) (string, error) {
	return "/tmp/file", nil
}

func (env stubExecutionEnv) Exec(ctx context.Context, command string, options ExecOptions) (ExecOutput, error) {
	return ExecOutput{Stdout: "hi\n", ExitCode: 0}, nil
}

func (env stubExecutionEnv) Cleanup(ctx context.Context) error { return nil }
