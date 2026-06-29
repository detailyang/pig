package tools

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type NativeEnv struct {
	cwd string
}

func NewNativeEnv(cwd string) NativeEnv {
	return NativeEnv{cwd: cwd}
}

func CurrentNativeEnv() (NativeEnv, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return NativeEnv{}, err
	}
	return NewNativeEnv(cwd), nil
}

func Current() (NativeEnv, error) {
	return CurrentNativeEnv()
}

func (env NativeEnv) CWD() string { return env.cwd }

func (env NativeEnv) AbsolutePath(ctx context.Context, path string) (string, error) {
	return env.resolve(path), nil
}

func (env NativeEnv) JoinPath(ctx context.Context, parts []string) (string, error) {
	if len(parts) == 0 {
		return "", nil
	}
	var path string
	for _, part := range parts {
		if filepath.IsAbs(part) {
			path = part
		} else if part == "" {
			continue
		} else if path == "" {
			path = part
		} else {
			path = nativeJoinChild(path, part)
		}
	}
	if path == "" {
		return "", nil
	}
	return path, nil
}

func (env NativeEnv) ReadTextFile(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	data, err := os.ReadFile(env.resolve(path))
	if err != nil {
		return "", mapFileError(err, path)
	}
	if !utf8.Valid(data) {
		return "", FileError{Code: FileErrorInvalidPath, Message: "stream did not contain valid UTF-8", Path: path}
	}
	return string(data), nil
}

func (env NativeEnv) ReadTextLines(ctx context.Context, path string, maxLines *int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	file, err := os.Open(env.resolve(path))
	if err != nil {
		return nil, mapFileError(err, path)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var lines []string
	for {
		if maxLines != nil && len(lines) >= *maxLines {
			break
		}
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, mapFileError(err, path)
		}
		if line != "" {
			if !utf8.ValidString(line) {
				return nil, FileError{Code: FileErrorInvalidPath, Message: "stream did not contain valid UTF-8", Path: path}
			}
			if strings.HasSuffix(line, "\n") {
				line = strings.TrimSuffix(line, "\n")
				line = strings.TrimSuffix(line, "\r")
			}
			lines = append(lines, line)
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return lines, nil
}

func (env NativeEnv) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	data, err := os.ReadFile(env.resolve(path))
	if err != nil {
		return nil, mapFileError(err, path)
	}
	return data, nil
}

func (env NativeEnv) WriteFile(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	resolved := env.resolve(path)
	if parent := nativeParentPath(resolved); parent != "" {
		_ = os.MkdirAll(parent, 0o755)
	}
	if err := os.WriteFile(resolved, content, 0o644); err != nil {
		return mapFileError(err, path)
	}
	return nil
}

func (env NativeEnv) AppendFile(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	resolved := env.resolve(path)
	if parent := nativeParentPath(resolved); parent != "" {
		_ = os.MkdirAll(parent, 0o755)
	}
	file, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return mapFileError(err, path)
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		return mapFileError(err, path)
	}
	return nil
}

func (env NativeEnv) FileInfo(ctx context.Context, path string) (FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return FileInfo{}, FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	resolved := env.resolve(path)
	meta, err := os.Lstat(resolved)
	if err != nil {
		return FileInfo{}, mapFileError(err, path)
	}
	return fileInfoFromOS(nativeFileName(path), resolved, meta), nil
}

func (env NativeEnv) ListDir(ctx context.Context, path string) ([]FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	resolved := env.resolve(path)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, mapFileError(err, path)
	}
	out := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		entryPath := nativeJoinChild(resolved, entry.Name())
		info, err := os.Stat(entryPath)
		if err != nil {
			return nil, mapFileError(err, entryPath)
		}
		out = append(out, fileInfoFromOS(entry.Name(), entryPath, info))
	}
	return out, nil
}

func (env NativeEnv) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	_, err := os.Lstat(env.resolve(path))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, mapFileError(err, path)
}

func (env NativeEnv) CanonicalPath(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	resolved, err := filepath.EvalSymlinks(env.resolve(path))
	if err != nil {
		return "", mapFileError(err, path)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", mapFileError(err, path)
	}
	return absolute, nil
}

func (env NativeEnv) CreateDir(ctx context.Context, path string, recursive bool) error {
	if err := ctx.Err(); err != nil {
		return FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	var err error
	if recursive {
		err = os.MkdirAll(env.resolve(path), 0o755)
	} else {
		err = os.Mkdir(env.resolve(path), 0o755)
	}
	if err != nil {
		return mapFileError(err, path)
	}
	return nil
}

func (env NativeEnv) Remove(ctx context.Context, path string, recursive bool, force bool) error {
	if err := ctx.Err(); err != nil {
		return FileError{Code: FileErrorAborted, Message: "aborted", Path: path}
	}
	var err error
	if recursive {
		err = os.RemoveAll(env.resolve(path))
	} else {
		err = os.Remove(env.resolve(path))
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return mapFileError(err, path)
	}
	return nil
}

func (env NativeEnv) CreateTempDir(ctx context.Context, prefix string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", FileError{Code: FileErrorAborted, Message: "aborted"}
	}
	if prefix == "" {
		prefix = "tmp-"
	}
	name, err := randomNativeTempHex()
	if err != nil {
		return "", mapFileError(err, "")
	}
	path := filepath.Join(os.TempDir(), prefix+"-"+name)
	err = os.MkdirAll(path, 0o755)
	if err != nil {
		return "", mapFileError(err, "")
	}
	return path, nil
}

func (env NativeEnv) CreateTempFile(ctx context.Context, prefix string, suffix string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", FileError{Code: FileErrorAborted, Message: "aborted"}
	}
	name, err := randomNativeTempHex()
	if err != nil {
		return "", mapFileError(err, "")
	}
	path := filepath.Join(os.TempDir(), prefix+name+suffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", mapFileError(err, "")
	}
	_ = file.Close()
	return path, nil
}

func randomNativeTempHex() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func (env NativeEnv) Exec(ctx context.Context, command string, options ExecOptions) (ExecOutput, error) {
	timeout := options.Timeout
	if timeout == 0 && options.TimeoutSecs > 0 {
		timeout = time.Duration(options.TimeoutSecs * float64(time.Second))
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if options.Abort != nil {
		abortCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-options.Abort:
				cancel()
			case <-abortCtx.Done():
			}
		}()
		ctx = abortCtx
	}
	cmd := exec.Command(shellName(), shellFlag(), command)
	configureCommandProcessGroup(cmd)
	if options.CWD != "" {
		cmd.Dir = options.CWD
	} else {
		cmd.Dir = env.cwd
	}
	if len(options.Env) > 0 {
		envValues := os.Environ()
		for key, value := range options.Env {
			envValues = append(envValues, key+"="+value)
		}
		cmd.Env = envValues
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutWriter := callbackWriter{buffer: &stdout, callback: options.OnStdout}
	stderrWriter := callbackWriter{buffer: &stderr, callback: options.OnStderr}
	cmd.Stdout = &stdoutWriter
	cmd.Stderr = &stderrWriter
	if err := cmd.Start(); err != nil {
		return ExecOutput{}, ExecutionError{Code: ExecutionErrorSpawnFailed, Message: err.Error()}
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitCh:
	case <-ctx.Done():
		killCommandProcessGroup(cmd)
		err = <-waitCh
	}
	stdoutWriter.Flush()
	stderrWriter.Flush()
	if ctx.Err() == context.DeadlineExceeded {
		return ExecOutput{}, ExecutionError{Code: ExecutionErrorTimeout, Message: "command timed out after " + formatTimeoutSeconds(timeout) + "s"}
	}
	if ctx.Err() == context.Canceled {
		return ExecOutput{}, ExecutionError{Code: ExecutionErrorAborted, Message: "command aborted"}
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecOutput{}, ExecutionError{Code: ExecutionErrorSpawnFailed, Message: err.Error()}
		}
	}
	return ExecOutput{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}, nil
}

func (env NativeEnv) Cleanup(ctx context.Context) error { return ctx.Err() }

func (env NativeEnv) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if path == "" {
		return strings.TrimRight(env.cwd, string(filepath.Separator)) + string(filepath.Separator)
	}
	return strings.TrimRight(env.cwd, string(filepath.Separator)) + string(filepath.Separator) + path
}

func mapFileError(err error, path string) FileError {
	code := FileErrorUnknown
	if errors.Is(err, os.ErrNotExist) {
		code = FileErrorNotFound
	} else if errors.Is(err, os.ErrPermission) {
		code = FileErrorPermissionDenied
	} else if errors.Is(err, os.ErrInvalid) {
		code = FileErrorInvalidPath
	}
	return FileError{Code: code, Message: err.Error(), Path: path}
}

func fileInfoFromOS(name string, path string, meta os.FileInfo) FileInfo {
	kind := FileKindFile
	if meta.Mode()&os.ModeSymlink != 0 {
		kind = FileKindSymlink
	} else if meta.IsDir() {
		kind = FileKindDirectory
	}
	mtimeMS := meta.ModTime().UnixMilli()
	if meta.ModTime().IsZero() {
		mtimeMS = 0
	}
	return FileInfo{Name: name, Path: path, Kind: kind, Size: uint64(meta.Size()), MTimeMS: mtimeMS}
}

func nativeFileName(path string) string {
	trimmed := strings.TrimRight(path, string(filepath.Separator))
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return ""
	}
	if strings.HasSuffix(trimmed, string(filepath.Separator)+"..") {
		return ""
	}
	if strings.HasSuffix(trimmed, string(filepath.Separator)+".") {
		return filepath.Base(filepath.Dir(trimmed))
	}
	return filepath.Base(trimmed)
}

func nativeJoinChild(parent string, child string) string {
	return strings.TrimRight(parent, string(filepath.Separator)) + string(filepath.Separator) + child
}

func nativeParentPath(path string) string {
	trimmed := strings.TrimRight(path, string(filepath.Separator))
	index := strings.LastIndex(trimmed, string(filepath.Separator))
	if index < 0 {
		return ""
	}
	if index == 0 {
		return string(filepath.Separator)
	}
	return trimmed[:index]
}

type callbackWriter struct {
	buffer   *bytes.Buffer
	callback func(string)
	mu       sync.Mutex
	pending  string
}

func (writer *callbackWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.buffer.Write(data)
	if writer.callback != nil {
		writer.pending += string(data)
		for {
			index := strings.IndexByte(writer.pending, '\n')
			if index < 0 {
				break
			}
			line := writer.pending[:index]
			writer.callback(line)
			writer.pending = writer.pending[index+1:]
		}
	}
	return len(data), nil
}

func (writer *callbackWriter) Flush() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.callback == nil || writer.pending == "" {
		return
	}
	writer.callback(writer.pending)
	writer.pending = ""
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

func shellFlag() string {
	if runtime.GOOS == "windows" {
		return "/C"
	}
	return "-c"
}

func formatTimeoutSeconds(timeout time.Duration) string {
	seconds := int(timeout.Seconds())
	return strconv.Itoa(seconds)
}

var _ io.Writer = (*callbackWriter)(nil)
