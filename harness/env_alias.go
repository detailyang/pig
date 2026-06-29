package harness

import "github.com/detailyang/pig/tools"

type NativeEnv = tools.NativeEnv

func NewNativeEnv(cwd string) NativeEnv {
	return tools.NewNativeEnv(cwd)
}

func CurrentNativeEnv() (NativeEnv, error) {
	return tools.CurrentNativeEnv()
}

type ExecOptions = tools.ExecOptions
type ExecOutput = tools.ExecOutput
type ExecResult[T any] = tools.ExecResult[T]
type ExecutionEnv = tools.ExecutionEnv
type ExecutionError = tools.ExecutionError
type ExecutionErrorCode = tools.ExecutionErrorCode
type FileError = tools.FileError
type FileErrorCode = tools.FileErrorCode
type FileInfo = tools.FileInfo
type FileKind = tools.FileKind
type FsResult[T any] = tools.FsResult[T]

const (
	FileErrorNotFound         = tools.FileErrorNotFound
	FileErrorNotADirectory    = tools.FileErrorNotADirectory
	FileErrorIsADirectory     = tools.FileErrorIsADirectory
	FileErrorPermissionDenied = tools.FileErrorPermissionDenied
	FileErrorInvalidPath      = tools.FileErrorInvalidPath
	FileErrorAborted          = tools.FileErrorAborted
	FileErrorUnknown          = tools.FileErrorUnknown

	ExecutionErrorTimeout     = tools.ExecutionErrorTimeout
	ExecutionErrorAborted     = tools.ExecutionErrorAborted
	ExecutionErrorSpawnFailed = tools.ExecutionErrorSpawnFailed
	ExecutionErrorUnknown     = tools.ExecutionErrorUnknown

	FileKindFile      = tools.FileKindFile
	FileKindDirectory = tools.FileKindDirectory
	FileKindSymlink   = tools.FileKindSymlink
)
