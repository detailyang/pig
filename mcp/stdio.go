package mcp

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync"
)

type StdioTransport struct {
	reader  *bufio.Reader
	writer  io.Writer
	closers []io.Closer
	mu      sync.Mutex
}

func NewStdioTransport(stdout io.Reader, stdin io.Writer, closers ...io.Closer) *StdioTransport {
	return &StdioTransport{reader: bufio.NewReader(stdout), writer: stdin, closers: closers}
}

func SpawnStdioTransport(command string, args ...string) (*StdioTransport, *exec.Cmd, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return NewStdioTransport(stdout, stdin, stdin), cmd, nil
}

func Spawn(command string, args ...string) (*StdioTransport, *exec.Cmd, error) {
	return SpawnStdioTransport(command, args...)
}

func (transport *StdioTransport) SendLine(ctx context.Context, line string) error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		line += "\n"
	}
	_, err := io.WriteString(transport.writer, line)
	return err
}

func (transport *StdioTransport) RecvLine(ctx context.Context) (string, bool, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := transport.reader.ReadString('\n')
		ch <- result{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case res := <-ch:
		if res.err == io.EOF && res.line == "" {
			return "", false, nil
		}
		if res.err != nil && res.err != io.EOF {
			return "", false, res.err
		}
		line := res.line
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return line, true, nil
	}
}

func (transport *StdioTransport) Close() error {
	var first error
	for _, closer := range transport.closers {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
