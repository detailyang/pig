package tools

import (
	"context"
	"time"
)

type FsResult[T any] struct {
	Value T
	Err   error
}

type ExecResult[T any] struct {
	Value T
	Err   error
}

type FileErrorCode string

const (
	FileErrorNotFound         FileErrorCode = "not_found"
	FileErrorNotADirectory    FileErrorCode = "not_a_directory"
	FileErrorIsADirectory     FileErrorCode = "is_a_directory"
	FileErrorPermissionDenied FileErrorCode = "permission_denied"
	FileErrorInvalidPath      FileErrorCode = "invalid_path"
	FileErrorAborted          FileErrorCode = "aborted"
	FileErrorUnknown          FileErrorCode = "unknown"
)

type FileError struct {
	Code    FileErrorCode `json:"code"`
	Message string        `json:"message"`
	Path    string        `json:"path,omitempty"`
}

func (err FileError) Error() string { return err.Message }

func (err FileError) WithPath(path string) FileError {
	err.Path = path
	return err
}

type ExecutionErrorCode string

const (
	ExecutionErrorTimeout     ExecutionErrorCode = "timeout"
	ExecutionErrorAborted     ExecutionErrorCode = "aborted"
	ExecutionErrorSpawnFailed ExecutionErrorCode = "spawn_failed"
	ExecutionErrorUnknown     ExecutionErrorCode = "unknown"
)

type ExecutionError struct {
	Code    ExecutionErrorCode `json:"code"`
	Message string             `json:"message"`
}

func (err ExecutionError) Error() string { return err.Message }

type FileKind string

const (
	FileKindFile      FileKind = "file"
	FileKindDirectory FileKind = "directory"
	FileKindSymlink   FileKind = "symlink"
)

type FileInfo struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Kind    FileKind `json:"kind"`
	Size    uint64   `json:"size"`
	MTimeMS int64    `json:"mtime_ms"`
}

type ExecOptions struct {
	CWD         string
	Env         map[string]string
	Timeout     time.Duration
	TimeoutSecs float64
	Abort       <-chan struct{}
	OnStdout    func(string)
	OnStderr    func(string)
}

type ExecOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type ExecutionEnv interface {
	CWD() string
	AbsolutePath(context.Context, string) (string, error)
	JoinPath(context.Context, []string) (string, error)
	ReadTextFile(context.Context, string) (string, error)
	ReadTextLines(context.Context, string, *int) ([]string, error)
	ReadBinaryFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	AppendFile(context.Context, string, []byte) error
	FileInfo(context.Context, string) (FileInfo, error)
	ListDir(context.Context, string) ([]FileInfo, error)
	Exists(context.Context, string) (bool, error)
	CanonicalPath(context.Context, string) (string, error)
	CreateDir(context.Context, string, bool) error
	Remove(context.Context, string, bool, bool) error
	CreateTempDir(context.Context, string) (string, error)
	CreateTempFile(context.Context, string, string) (string, error)
	Exec(context.Context, string, ExecOptions) (ExecOutput, error)
	Cleanup(context.Context) error
}
