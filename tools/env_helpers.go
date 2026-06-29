package tools

import (
	"context"
	"os"
	"path/filepath"
)

func resolveToolPath(env ExecutionEnv, path string) string {
	if env == nil || filepath.IsAbs(path) {
		return path
	}
	return NewNativeEnv(env.CWD()).resolve(path)
}

func readToolFile(ctx context.Context, env ExecutionEnv, path string) ([]byte, error) {
	if env != nil {
		return env.ReadBinaryFile(ctx, path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeToolFile(ctx context.Context, env ExecutionEnv, path string, content []byte) error {
	if env != nil {
		return env.WriteFile(ctx, path, content)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	return os.WriteFile(path, content, 0o644)
}
