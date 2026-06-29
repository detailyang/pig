package mcp

import "context"

type Transport interface {
	SendLine(ctx context.Context, line string) error
	RecvLine(ctx context.Context) (line string, ok bool, err error)
	Close() error
}
