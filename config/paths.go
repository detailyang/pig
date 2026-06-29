package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func BaseDir() string {
	if value, ok := os.LookupEnv("PIE_DIR"); ok {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".pie")
	}
	return ".pie"
}

func DefaultBaseDir() string {
	return BaseDir()
}

func DefaultSkillsRoot() string {
	return filepath.Join(BaseDir(), "skills")
}

func SessionsDirForCWD(cwd string) string {
	return filepath.Join(BaseDir(), "sessions", CWDHash(cwd))
}

func SessionsDirForCwd(cwd string) string { return SessionsDirForCWD(cwd) }

func MemoryDir() string {
	return filepath.Join(BaseDir(), "memory")
}

func CWDHash(cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(sum[:6])
}

func CwdHash(cwd string) string { return CWDHash(cwd) }

func AuthPath() string {
	return filepath.Join(BaseDir(), "auth.json")
}

func ConfigPath() string {
	return filepath.Join(BaseDir(), "config.toml")
}
