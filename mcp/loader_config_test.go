package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/config"
)

func TestParseMCPConfigSupportsStdioHTTPAndFlags(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = ["server.js", "--root", "."]
inject_summary = true

[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
request_timeout_ms = 1234
sse_idle_timeout_ms = 5678
body_cap_bytes = 9999
inject_and_run = true
[server.auth]
kind = "bearer"
token_keychain_ref = "openai"
[server.reconnect]
initial_ms = 100
max_ms = 1000
max_attempts = 5
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("servers mismatch: %#v", cfg.Servers)
	}
	if len(cfg.Server) != 2 || cfg.Server[0].Name != cfg.Servers[0].Name || cfg.Server[1].Name != cfg.Servers[1].Name {
		t.Fatalf("server alias mismatch: %#v", cfg.Server)
	}
	fs := cfg.Servers[0]
	if fs.Name != "fs" || fs.Kind != ServerKindStdio || fs.Command != "node" || len(fs.Args) != 3 || fs.Args[0] != "server.js" || fs.Args[1] != "--root" || fs.Args[2] != "." || !fs.InjectSummary {
		t.Fatalf("stdio server mismatch: %#v", fs)
	}
	remote := cfg.Servers[1]
	if remote.Name != "remote" || remote.Kind != ServerKindStreamableHTTP || remote.Endpoint != "https://example.test/mcp" || remote.RequestTimeoutMS != 1234 || remote.SSEIdleTimeoutMS != 5678 || remote.BodyCapBytes != 9999 || !remote.InjectAndRun {
		t.Fatalf("http server mismatch: %#v", remote)
	}
	if remote.Auth == nil || remote.Auth.Kind != "bearer" || remote.Auth.TokenKeychainRef != "openai" {
		t.Fatalf("auth mismatch: %#v", remote.Auth)
	}
	if remote.Reconnect == nil || remote.Reconnect.InitialMS != 100 || remote.Reconnect.MaxMS != 1000 || remote.Reconnect.MaxAttempts != 5 {
		t.Fatalf("reconnect mismatch: %#v", remote.Reconnect)
	}
}

func TestParseMCPConfigAcceptsWhitespaceInTableHeadersLikeUpstream(t *testing.T) {
	text := `
[[ server ]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
[ server.auth ]
kind = "bearer"
token_keychain_ref = "openai"
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "remote" || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.Kind != "bearer" || cfg.Servers[0].Auth.TokenKeychainRef != "openai" {
		t.Fatalf("whitespace header mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigAcceptsQuotedTableHeadersLikeUpstream(t *testing.T) {
	text := `
[[ "server" ]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
[ "server"."auth" ]
kind = "bearer"
token_keychain_ref = "openai"
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "remote" || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.Kind != "bearer" || cfg.Servers[0].Auth.TokenKeychainRef != "openai" {
		t.Fatalf("quoted header mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigRejectsInvalidTableHeadersLikeUpstream(t *testing.T) {
	for _, header := range []string{`["server]`, `[server."auth]`, `[[server bad]]`} {
		text := header + `
name = "remote"
`
		if _, err := ParseMCPConfig([]byte(text)); err == nil {
			t.Fatalf("invalid table header %q should fail like serde TOML", header)
		}
	}
}

func TestParseMCPConfigAcceptsQuotedKeysLikeUpstream(t *testing.T) {
	text := `
[[server]]
"name" = "fs"
'command' = "node"
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "fs" || cfg.Servers[0].Command != "node" {
		t.Fatalf("quoted key mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigAcceptsMultilineBasicStringsLikeUpstream(t *testing.T) {
	text := "" + "\n" + `[[server]]
name = "fs"
command = """node
server"""
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Command != "node\nserver" {
		t.Fatalf("multiline basic string mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigMultilineStringsTrimInitialNewlineLikeUpstream(t *testing.T) {
	text := "" + "\n" + `[[server]]
name = "fs"
command = """
node"""
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Command != "node" {
		t.Fatalf("multiline initial newline mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigMultilineBasicStringsFoldEscapedNewlineLikeUpstream(t *testing.T) {
	text := "" + "\n" + `[[server]]
name = "fs"
command = """node\
  server"""
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Command != "nodeserver" {
		t.Fatalf("multiline escaped newline mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigAcceptsNestedDottedKeysLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
auth.kind = "bearer"
auth.token_keychain_ref = "openai"
reconnect.initial_ms = 100
reconnect.max_ms = 1000
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.Kind != "bearer" || cfg.Servers[0].Auth.TokenKeychainRef != "openai" || cfg.Servers[0].Reconnect == nil || cfg.Servers[0].Reconnect.InitialMS != 100 || cfg.Servers[0].Reconnect.MaxMS != 1000 {
		t.Fatalf("dotted nested keys mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigAcceptsWhitespaceAroundDottedKeysLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
auth . kind = "bearer"
auth . token_keychain_ref = "openai"
reconnect . max_ms = 1000
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.Kind != "bearer" || cfg.Servers[0].Auth.TokenKeychainRef != "openai" || cfg.Servers[0].Reconnect == nil || cfg.Servers[0].Reconnect.MaxMS != 1000 {
		t.Fatalf("whitespace dotted keys mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigAcceptsQuotedDottedKeySegmentsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
"auth"."kind" = "bearer"
'auth'."token_keychain_ref" = "openai"
"reconnect"."max_ms" = 1000
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.Kind != "bearer" || cfg.Servers[0].Auth.TokenKeychainRef != "openai" || cfg.Servers[0].Reconnect == nil || cfg.Servers[0].Reconnect.MaxMS != 1000 {
		t.Fatalf("quoted dotted key segments mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigRejectsEmptyQuotedKeyLikeUpstream(t *testing.T) {
	text := `
[[server]]
"" = "x"
name = "fs"
command = "node"
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("empty quoted key should fail like serde TOML")
	}
}

func TestParseMCPConfigRejectsInvalidBareKeysLikeUpstream(t *testing.T) {
	for _, key := range []string{"bad key", "bad/key", "bad$key"} {
		text := `
[[server]]
name = "fs"
command = "node"
` + key + ` = "x"
`
		if _, err := ParseMCPConfig([]byte(text)); err == nil {
			t.Fatalf("invalid bare key %q should fail like serde TOML", key)
		}
	}
}

func TestParseMCPConfigSupportsInlineAuthLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote-docs"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
auth = { kind = "bearer", token_keychain_ref = "mcp-example:default" }
request_timeout_ms = 30000
sse_idle_timeout_ms = 60000
body_cap_bytes = 1048576
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers mismatch: %#v", cfg.Servers)
	}
	server := cfg.Servers[0]
	if server.Name != "remote-docs" || server.Kind != ServerKindStreamableHTTP || server.Endpoint != "https://mcp.example.com/mcp" {
		t.Fatalf("server mismatch: %#v", server)
	}
	if server.Auth == nil || server.Auth.Kind != "bearer" || server.Auth.TokenKeychainRef != "mcp-example:default" {
		t.Fatalf("inline auth mismatch: %#v", server.Auth)
	}
}

func TestParseMCPConfigInlineAuthKeepsCommaInsideStringLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote-docs"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
auth = { kind = "bearer", token_keychain_ref = "mcp,example:default" }
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.TokenKeychainRef != "mcp,example:default" {
		t.Fatalf("inline auth comma string mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigInlineAuthHandlesEscapedQuoteBeforeCommaLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote-docs"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
auth = { kind = "bearer", token_keychain_ref = "mcp\"example,default" }
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Auth == nil || cfg.Servers[0].Auth.TokenKeychainRef != `mcp"example,default` {
		t.Fatalf("inline auth escaped quote mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigSupportsInlineReconnectLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
reconnect = { initial_ms = 100, max_ms = 1000, max_attempts = 5 }
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers mismatch: %#v", cfg.Servers)
	}
	reconnect := cfg.Servers[0].Reconnect
	if reconnect == nil || reconnect.InitialMS != 100 || reconnect.MaxMS != 1000 || reconnect.MaxAttempts != 5 {
		t.Fatalf("inline reconnect mismatch: %#v", reconnect)
	}
}

func TestParseMCPConfigAcceptsTOMLIntegerFormatsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
request_timeout_ms = 1_000
body_cap_bytes = 0x10
reconnect = { initial_ms = 0o10, max_ms = 0b10000, max_attempts = 5 }
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers mismatch: %#v", cfg.Servers)
	}
	server := cfg.Servers[0]
	if server.RequestTimeoutMS != 1000 || server.BodyCapBytes != 16 || server.Reconnect == nil || server.Reconnect.InitialMS != 8 || server.Reconnect.MaxMS != 16 || server.Reconnect.MaxAttempts != 5 {
		t.Fatalf("integer formats mismatch: %#v", server)
	}
}

func TestParseMCPConfigRejectsInvalidTOMLIntegerUnderscoresLikeUpstream(t *testing.T) {
	for _, value := range []string{"1__000", "_1000", "1000_", "0x_10"} {
		text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://mcp.example.com/mcp"
request_timeout_ms = ` + value + `
`
		if _, err := ParseMCPConfig([]byte(text)); err == nil {
			t.Fatalf("invalid TOML integer %q should fail like serde TOML", value)
		}
	}
}

func TestParseMCPConfigSupportsMultilineArgsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = [
  "server.js",
  "--root",
  ".",
]
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || len(cfg.Servers[0].Args) != 3 || cfg.Servers[0].Args[0] != "server.js" || cfg.Servers[0].Args[1] != "--root" || cfg.Servers[0].Args[2] != "." {
		t.Fatalf("multiline args mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigMultilineArgsIgnoreBracketInsideStringLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = [
  "literal ] bracket",
  "server.js",
]
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || len(cfg.Servers[0].Args) != 2 || cfg.Servers[0].Args[0] != "literal ] bracket" || cfg.Servers[0].Args[1] != "server.js" {
		t.Fatalf("multiline bracket arg mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigPreservesEmptyStringArgsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = ["", "--flag"]
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || len(cfg.Servers[0].Args) != 2 || cfg.Servers[0].Args[0] != "" || cfg.Servers[0].Args[1] != "--flag" {
		t.Fatalf("empty string args mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigArgsKeepCommaInsideStringLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = ["--set=a,b", "server.js"]
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || len(cfg.Servers[0].Args) != 2 || cfg.Servers[0].Args[0] != "--set=a,b" || cfg.Servers[0].Args[1] != "server.js" {
		t.Fatalf("comma arg mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigRejectsEmptyArrayElementLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = ["a",, "b"]
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("empty TOML array element should fail like serde TOML")
	}
}

func TestParseMCPConfigAcceptsTrailingCommaArrayLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
command = "node"
args = ["a",]
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || len(cfg.Servers[0].Args) != 1 || cfg.Servers[0].Args[0] != "a" {
		t.Fatalf("trailing comma args mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigRejectsDuplicateFieldsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs"
name = "other"
command = "node"
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("duplicate server field should fail like TOML serde")
	}
	auth := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
[server.auth]
kind = "bearer"
kind = "basic"
`
	if _, err := ParseMCPConfig([]byte(auth)); err == nil {
		t.Fatal("duplicate nested auth field should fail like TOML serde")
	}
	duplicateAuthTable := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
[server.auth]
kind = "bearer"
[server.auth]
token_keychain_ref = "openai"
`
	if _, err := ParseMCPConfig([]byte(duplicateAuthTable)); err == nil {
		t.Fatal("duplicate nested auth table should fail like TOML serde")
	}
	inlineThenTableAuth := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
auth = { kind = "bearer" }
[server.auth]
token_keychain_ref = "ref"
`
	if _, err := ParseMCPConfig([]byte(inlineThenTableAuth)); err == nil {
		t.Fatal("inline auth plus auth table should fail like TOML serde")
	}
	duplicateReconnectTable := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
[server.reconnect]
initial_ms = 100
[server.reconnect]
max_ms = 1000
`
	if _, err := ParseMCPConfig([]byte(duplicateReconnectTable)); err == nil {
		t.Fatal("duplicate nested reconnect table should fail like TOML serde")
	}
	inlineThenTableReconnect := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
reconnect = { initial_ms = 100 }
[server.reconnect]
max_ms = 1000
`
	if _, err := ParseMCPConfig([]byte(inlineThenTableReconnect)); err == nil {
		t.Fatal("inline reconnect plus reconnect table should fail like TOML serde")
	}
}

func TestParseMCPConfigRejectsMissingRequiredNameLikeUpstream(t *testing.T) {
	text := `
[[server]]
command = "node"
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("missing server name should fail like serde required String")
	}
}

func TestParseMCPConfigRejectsWrongStringTypesLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = 123
command = "node"
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("numeric name should fail like serde String")
	}
	args := `
[[server]]
name = "fs"
command = "node"
args = ["server.js", 123]
`
	if _, err := ParseMCPConfig([]byte(args)); err == nil {
		t.Fatal("numeric arg should fail like serde Vec<String>")
	}
	notArray := `
[[server]]
name = "fs"
command = "node"
args = "server.js"
`
	if _, err := ParseMCPConfig([]byte(notArray)); err == nil {
		t.Fatal("string args should fail like serde Vec<String>")
	}
}

func TestParseMCPConfigRejectsInvalidStringEscapeLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs\q"
command = "node"
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("invalid TOML string escape should fail like serde TOML")
	}
}

func TestParseMCPConfigAcceptsUnicodeStringEscapesLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs\u002Dmain"
command = "node\U0001F600"
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "fs-main" || cfg.Servers[0].Command != "node😀" {
		t.Fatalf("unicode escape mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigRejectsInvalidUnicodeEscapeLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs\uD800"
command = "node"
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("invalid Unicode escape should fail like serde TOML")
	}
}

func TestParseMCPConfigAcceptsSingleQuotedStringsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = 'fs'
command = 'node'
args = ['server.js', '--root']
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "fs" || cfg.Servers[0].Command != "node" || len(cfg.Servers[0].Args) != 2 || cfg.Servers[0].Args[0] != "server.js" || cfg.Servers[0].Args[1] != "--root" {
		t.Fatalf("single-quoted config mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigKeepsHashInsideSingleQuotedStringsLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = 'fs#1'
command = 'node'
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "fs#1" {
		t.Fatalf("single-quoted hash should not start comment: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigKeepsHashAfterEscapedQuoteInsideStringLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "fs\"#main"
command = "node"
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != `fs"#main` {
		t.Fatalf("escaped quote hash mismatch: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigIgnoresUnknownFieldsLikeUpstream(t *testing.T) {
	text := `
top_level_unknown = true
[metadata]
owner = "test"
[[server]]
name = "fs"
command = "node"
unknown = 123
[server.extra]
ignored = true
`
	cfg, err := ParseMCPConfig([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "fs" || cfg.Servers[0].Command != "node" {
		t.Fatalf("unknown field should be ignored: %#v", cfg.Servers)
	}
}

func TestParseMCPConfigRejectsInvalidUnknownInlineFieldLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
auth = { kind = "bearer", ignored = }
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("invalid unknown inline field should fail during TOML parse like upstream")
	}
}

func TestParseMCPConfigRejectsEmptyInlineKeyLikeUpstream(t *testing.T) {
	text := `
[[server]]
name = "remote"
kind = "streamable_http"
endpoint = "https://example.test/mcp"
auth = { kind = "bearer", = "x" }
`
	if _, err := ParseMCPConfig([]byte(text)); err == nil {
		t.Fatal("empty inline table key should fail during TOML parse like upstream")
	}
}

func TestLoadMCPConfigAllProjectOverridesUserAndTracksInjectSets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", filepath.Join(dir, "home"))
	cwd := filepath.Join(dir, "project")
	userPath := filepath.Join(dir, "home", "mcp.toml")
	projectPath := filepath.Join(cwd, ".pie", "mcp.toml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`[[server]]
name = "shared"
command = "user"
inject_summary = true
[[server]]
name = "user-only"
command = "u"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`[[server]]
name = "shared"
command = "project"
inject_and_run = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := LoadMCPConfigAll(cwd)
	if len(loaded.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", loaded.Diagnostics)
	}
	if len(loaded.Servers) != 2 {
		t.Fatalf("server count mismatch: %#v", loaded.Servers)
	}
	if loaded.Servers[0].Name != "shared" || loaded.Servers[0].Command != "project" || !loaded.Servers[0].InjectAndRun || loaded.Servers[0].InjectSummary {
		t.Fatalf("project override mismatch: %#v", loaded.Servers[0])
	}
	if !loaded.InjectAndRunServers["shared"] || loaded.InjectSummaryServers["shared"] || len(loaded.InjectSummaryServers) != 0 {
		t.Fatalf("inject sets mismatch: %#v %#v", loaded.InjectSummaryServers, loaded.InjectAndRunServers)
	}
}

func TestMCPUpstreamLoaderConfigNames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", filepath.Join(dir, "home"))
	cwd := filepath.Join(dir, "project")
	userPath := filepath.Join(dir, "home", "mcp.toml")
	projectPath := filepath.Join(cwd, ".pie", "mcp.toml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`[[server]]
name = "shared"
command = "user"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`[[server]]
name = "shared"
command = "project"
inject_summary = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var diagnostics []string
	cfg, ok := ReadConfig(projectPath, &diagnostics, "project")
	if !ok || len(diagnostics) != 0 || len(cfg.Servers) != 1 || cfg.Servers[0].Command != "project" {
		t.Fatalf("read config mismatch cfg=%#v ok=%v diagnostics=%#v", cfg, ok, diagnostics)
	}
	var upstreamConfig McpConfig = MCPConfig{Servers: []ServerConfig{{Name: "typed", Auth: &HTTPAuthConfig{Kind: "bearer"}}}}
	var upstreamAuth HttpAuthConfig = *upstreamConfig.Servers[0].Auth
	if upstreamConfig.Servers[0].Name != "typed" || upstreamAuth.Kind != "bearer" {
		t.Fatalf("upstream type aliases mismatch: %#v %#v", upstreamConfig, upstreamAuth)
	}
	loaded := LoadMCPConfigAll(cwd)
	if len(loaded.Diagnostics) != 0 || len(loaded.Servers) != 1 || loaded.Servers[0].Command != "project" || !loaded.InjectSummaryServers["shared"] {
		t.Fatalf("load all mismatch: %#v", loaded)
	}
}

func TestLoadMCPConfigMissingAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	missing := LoadMCPConfigFiles(filepath.Join(dir, "missing.toml"))
	if len(missing.Servers) != 0 || len(missing.Diagnostics) != 0 {
		t.Fatalf("missing files should be silent: %#v", missing)
	}
	badPath := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(badPath, []byte("[[server]]\nname broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := LoadMCPConfigFiles(badPath)
	if len(bad.Servers) != 0 || len(bad.Diagnostics) != 1 {
		t.Fatalf("bad diagnostics mismatch: %#v", bad)
	}
}

func TestLoadMCPConfigInvalidUTF8ReportsReadFailureLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.toml")
	if err := os.WriteFile(path, []byte("[[server]]\nname = \"fs\"\ncommand = \"node\"\n# \xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := LoadMCPConfigFiles(path)
	if len(loaded.Servers) != 0 || len(loaded.Diagnostics) != 1 || !strings.Contains(loaded.Diagnostics[0], "read") || !strings.Contains(loaded.Diagnostics[0], "invalid UTF-8") {
		t.Fatalf("invalid UTF-8 mcp.toml should read-fail like upstream, got %#v", loaded)
	}
}

func TestLoadMCPConfigAllDiagnosticsIncludeSourceLabelLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", filepath.Join(dir, "home"))
	cwd := filepath.Join(dir, "project")
	userPath := filepath.Join(dir, "home", "mcp.toml")
	projectPath := filepath.Join(cwd, ".pie", "mcp.toml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("[[server]]\nname broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("[[server]]\nname broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := LoadMCPConfigAll(cwd)
	if len(loaded.Diagnostics) != 2 || !strings.HasPrefix(loaded.Diagnostics[0], "mcp config (user, "+userPath+"): parse failed: ") || !strings.HasPrefix(loaded.Diagnostics[1], "mcp config (project, "+projectPath+"): parse failed: ") {
		t.Fatalf("diagnostics mismatch: %#v", loaded.Diagnostics)
	}
}

func TestStdioCommandFromServerConfigValidatesShape(t *testing.T) {
	command, args, err := StdioCommandFromServerConfig(ServerConfig{Name: "fs", Kind: ServerKindStdio, Command: "node", Args: []string{"server.js"}})
	if err != nil {
		t.Fatal(err)
	}
	if command != "node" || len(args) != 1 || args[0] != "server.js" {
		t.Fatalf("stdio command mismatch: %q %#v", command, args)
	}

	if _, _, err := StdioCommandFromServerConfig(ServerConfig{Name: "missing", Kind: ServerKindStdio}); err == nil {
		t.Fatal("missing command should fail")
	}
	if _, _, err := StdioCommandFromServerConfig(ServerConfig{Name: "bad", Kind: ServerKindStdio, Command: "node", Endpoint: "https://example.test/mcp"}); err == nil {
		t.Fatal("stdio endpoint should fail")
	}
	if _, _, err := StdioCommandFromServerConfig(ServerConfig{Name: "bad", Kind: ServerKindStdio, Command: "node", Auth: &HTTPAuthConfig{Kind: "bearer"}}); err == nil {
		t.Fatal("stdio auth should fail")
	}
}

func TestHTTPTransportOptionsFromServerConfigMapsAndValidates(t *testing.T) {
	endpoint, options, err := HTTPTransportOptionsFromServerConfig(ServerConfig{
		Name:             "remote",
		Kind:             ServerKindStreamableHTTP,
		Endpoint:         "https://example.test/mcp",
		Auth:             &HTTPAuthConfig{Kind: "bearer", TokenKeychainRef: "mcp-token"},
		RequestTimeoutMS: 1234,
		SSEIdleTimeoutMS: 5678,
		BodyCapBytes:     9999,
		Reconnect:        &ReconnectConfig{InitialMS: 100, MaxMS: 1000, MaxAttempts: 5},
	}, func(ref string) (string, bool) {
		if ref != "mcp-token" {
			t.Fatalf("token ref mismatch: %q", ref)
		}
		return "secret-token", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://example.test/mcp" {
		t.Fatalf("endpoint mismatch: %q", endpoint)
	}
	if options.Client == nil || options.Client.Timeout != 1234*time.Millisecond {
		t.Fatalf("timeout mismatch: %#v", options.Client)
	}
	if options.BodyCap != 9999 || options.SSEIdleTimeout != 5678*time.Millisecond || options.ReconnectInitialDelay != 100*time.Millisecond || options.ReconnectMaxDelay != 1000*time.Millisecond || options.ReconnectMaxAttempts != 5 {
		t.Fatalf("options mismatch: %#v", options)
	}
	if options.Headers["Authorization"] != "Bearer secret-token" {
		t.Fatalf("auth header mismatch: %#v", options.Headers)
	}

	badCases := []ServerConfig{
		{Name: "missing-endpoint", Kind: ServerKindStreamableHTTP},
		{Name: "has-command", Kind: ServerKindStreamableHTTP, Endpoint: "https://example.test/mcp", Command: "node"},
		{Name: "has-args", Kind: ServerKindStreamableHTTP, Endpoint: "https://example.test/mcp", Args: []string{"server.js"}},
		{Name: "non-https-remote", Kind: ServerKindStreamableHTTP, Endpoint: "http://example.test/mcp"},
		{Name: "bad-auth", Kind: ServerKindStreamableHTTP, Endpoint: "https://example.test/mcp", Auth: &HTTPAuthConfig{Kind: "basic", TokenKeychainRef: "x"}},
		{Name: "missing-token", Kind: ServerKindStreamableHTTP, Endpoint: "https://example.test/mcp", Auth: &HTTPAuthConfig{Kind: "bearer"}},
	}
	for _, badCase := range badCases {
		if _, _, err := HTTPTransportOptionsFromServerConfig(badCase, func(string) (string, bool) { return "", false }); err == nil {
			t.Fatalf("%s should fail", badCase.Name)
		}
	}
	if endpoint, _, err := HTTPTransportOptionsFromServerConfig(ServerConfig{Name: "local", Kind: ServerKindStreamableHTTP, Endpoint: "http://127.0.0.1:3000/mcp"}, nil); err != nil || endpoint != "http://127.0.0.1:3000/mcp" {
		t.Fatalf("127.0.0.1 http endpoint should be allowed like upstream: endpoint=%q err=%v", endpoint, err)
	}
}

func TestMCPHTTPAuthHelpersMatchUpstream(t *testing.T) {
	store := config.AuthStore{}
	store.Set("remote-docs:default", config.ProviderCredential{Value: "mcp_token_should_not_leak"})
	authConfig := &HTTPAuthConfig{Kind: "bearer", TokenKeychainRef: "remote-docs:default"}
	auth, err := ResolveHTTPAuthFromStore(authConfig, store)
	if err != nil {
		t.Fatal(err)
	}
	debug := auth.String()
	if strings.Contains(debug, "mcp_token_should_not_leak") || !strings.Contains(debug, "<redacted>") {
		t.Fatalf("auth debug should redact token: %s", debug)
	}
	if auth.HeaderValue() != "Bearer mcp_token_should_not_leak" {
		t.Fatalf("auth header mismatch: %q", auth.HeaderValue())
	}

	missingRef := "secret_ref_should_not_leak"
	_, err = ResolveHTTPAuthFromStore(&HTTPAuthConfig{Kind: "bearer", TokenKeychainRef: missingRef}, config.AuthStore{})
	if err == nil || strings.Contains(err.Error(), missingRef) || !strings.Contains(err.Error(), "<configured-token-ref>") {
		t.Fatalf("missing auth error should redact token ref, got %v", err)
	}
	if recovery := HTTPAuthRecovery(authConfig); recovery != "run /login <configured-token-ref>" {
		t.Fatalf("recovery mismatch: %q", recovery)
	}
	none, err := ResolveHTTPAuthFromStore(nil, config.AuthStore{})
	if err != nil || none.HeaderValue() != "" {
		t.Fatalf("none auth mismatch: %q err=%v", none.HeaderValue(), err)
	}
}

func TestParseMCPConfigRejectsExplicitZeroHTTPDurationsAndCaps(t *testing.T) {
	badFields := []string{
		"request_timeout_ms = 0",
		"sse_idle_timeout_ms = 0",
		"body_cap_bytes = 0",
		"[server.reconnect]\ninitial_ms = 0",
		"[server.reconnect]\nmax_ms = 0",
	}
	for _, field := range badFields {
		text := "[[server]]\nname = \"remote\"\nkind = \"streamable_http\"\nendpoint = \"https://example.test/mcp\"\n" + field + "\n"
		if _, err := ParseMCPConfig([]byte(text)); err == nil {
			t.Fatalf("%s should fail", field)
		}
	}
}

func TestParseMCPConfigRejectsInvalidEnumAndBoolLikeUpstreamSerde(t *testing.T) {
	badFields := []string{
		`kind = "future"`,
		`inject_summary = maybe`,
		`inject_and_run = 1`,
	}
	for _, field := range badFields {
		text := "[[server]]\nname = \"remote\"\n" + field + "\n"
		if _, err := ParseMCPConfig([]byte(text)); err == nil {
			t.Fatalf("%s should fail", field)
		}
	}
}
