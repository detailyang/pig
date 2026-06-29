package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/detailyang/pig/config"
)

type Handle struct {
	LogPath string
	file    *os.File
	mu      sync.Mutex
}

type LoggingHandle = Handle

func Init(sessionID string) *Handle {
	dir := LogsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "(logging disabled: cannot create %s: %v)\n", dir, err)
		return nil
	}
	logPath := filepath.Join(dir, Short(sessionID)+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "(logging disabled: cannot open %s: %v)\n", logPath, err)
		return nil
	}
	return &Handle{LogPath: logPath, file: file}
}

func LogsDir() string {
	return filepath.Join(config.BaseDir(), "logs")
}

func Short(sessionID string) string {
	if len(sessionID) > 16 {
		return sessionID[:16]
	}
	return sessionID
}

func (handle *Handle) Write(message string) error {
	if handle == nil || handle.file == nil {
		return nil
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	_, err := fmt.Fprintf(handle.file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), message)
	return err
}

func (handle *Handle) Close() error {
	if handle == nil || handle.file == nil {
		return nil
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	err := handle.file.Close()
	handle.file = nil
	return err
}
