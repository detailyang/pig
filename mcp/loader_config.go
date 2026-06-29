package mcp

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/config"
)

type ServerKind string

const (
	ServerKindStdio          ServerKind = "stdio"
	ServerKindStreamableHTTP ServerKind = "streamable_http"
)

type MCPConfig struct {
	Server  []ServerConfig
	Servers []ServerConfig
}

type McpConfig = MCPConfig

type ServerConfig struct {
	Name             string
	Kind             ServerKind
	Command          string
	Args             []string
	Endpoint         string
	Auth             *HTTPAuthConfig
	RequestTimeoutMS uint64
	SSEIdleTimeoutMS uint64
	BodyCapBytes     uint64
	Reconnect        *ReconnectConfig
	InjectSummary    bool
	InjectAndRun     bool
}

type HTTPAuthConfig struct {
	Kind             string
	TokenKeychainRef string
}

type HttpAuthConfig = HTTPAuthConfig

type ReconnectConfig struct {
	InitialMS   uint64
	MaxMS       uint64
	MaxAttempts uint64
}

type LoadedMCPConfig struct {
	Servers              []ServerConfig
	Diagnostics          []string
	InjectSummaryServers map[string]bool
	InjectAndRunServers  map[string]bool
}

type TokenResolver func(ref string) (string, bool)

func LoadMCPConfigAll(cwd string) LoadedMCPConfig {
	return loadMCPConfigSources([]mcpConfigSource{
		{Path: filepath.Join(config.BaseDir(), "mcp.toml"), Label: "user"},
		{Path: filepath.Join(cwd, ".pie", "mcp.toml"), Label: "project"},
	})
}

func LoadMCPConfigFiles(paths ...string) LoadedMCPConfig {
	sources := make([]mcpConfigSource, 0, len(paths))
	for _, path := range paths {
		sources = append(sources, mcpConfigSource{Path: path})
	}
	return loadMCPConfigSources(sources)
}

func ReadConfig(path string, diagnostics *[]string, label string) (MCPConfig, bool) {
	loaded := loadMCPConfigSources([]mcpConfigSource{{Path: path, Label: label}})
	if diagnostics != nil {
		*diagnostics = append(*diagnostics, loaded.Diagnostics...)
	}
	if len(loaded.Diagnostics) != 0 || len(loaded.Servers) == 0 {
		return MCPConfig{}, false
	}
	return MCPConfig{Server: append([]ServerConfig(nil), loaded.Servers...), Servers: append([]ServerConfig(nil), loaded.Servers...)}, true
}

type mcpConfigSource struct {
	Path  string
	Label string
}

func loadMCPConfigSources(sources []mcpConfigSource) LoadedMCPConfig {
	loaded := LoadedMCPConfig{InjectSummaryServers: map[string]bool{}, InjectAndRunServers: map[string]bool{}}
	for _, source := range sources {
		data, err := os.ReadFile(source.Path)
		if err != nil {
			if !os.IsNotExist(err) {
				loaded.Diagnostics = append(loaded.Diagnostics, mcpConfigDiagnostic(source, "read", err))
			}
			continue
		}
		if !utf8.Valid(data) {
			loaded.Diagnostics = append(loaded.Diagnostics, mcpConfigDiagnostic(source, "read", fmt.Errorf("invalid UTF-8")))
			continue
		}
		cfg, err := ParseMCPConfig(data)
		if err != nil {
			loaded.Diagnostics = append(loaded.Diagnostics, mcpConfigDiagnostic(source, "parse", err))
			continue
		}
		for _, server := range cfg.Servers {
			index := serverIndex(loaded.Servers, server.Name)
			if index >= 0 {
				loaded.Servers[index] = server
			} else {
				loaded.Servers = append(loaded.Servers, server)
			}
		}
	}
	for _, server := range loaded.Servers {
		if server.InjectSummary {
			loaded.InjectSummaryServers[server.Name] = true
		}
		if server.InjectAndRun {
			loaded.InjectAndRunServers[server.Name] = true
		}
	}
	return loaded
}

func mcpConfigDiagnostic(source mcpConfigSource, action string, err error) string {
	if source.Label != "" {
		return fmt.Sprintf("mcp config (%s, %s): %s failed: %v", source.Label, source.Path, action, err)
	}
	return fmt.Sprintf("%s %s: %v", action, source.Path, err)
}

func StdioCommandFromServerConfig(server ServerConfig) (string, []string, error) {
	if server.Endpoint != "" || server.Auth != nil {
		return "", nil, fmt.Errorf("stdio MCP server %q must not set endpoint or auth", server.Name)
	}
	if server.Command == "" {
		return "", nil, fmt.Errorf("stdio MCP server %q missing command", server.Name)
	}
	args := append([]string(nil), server.Args...)
	return server.Command, args, nil
}

func HTTPTransportOptionsFromServerConfig(server ServerConfig, resolveToken TokenResolver) (string, HTTPTransportOptions, error) {
	if server.Command != "" || len(server.Args) != 0 {
		return "", HTTPTransportOptions{}, fmt.Errorf("streamable_http MCP server %q must set endpoint, not command/args", server.Name)
	}
	if server.Endpoint == "" {
		return "", HTTPTransportOptions{}, fmt.Errorf("streamable_http MCP server %q missing endpoint", server.Name)
	}
	if err := validateHTTPTransportEndpoint(server.Endpoint); err != nil {
		return "", HTTPTransportOptions{}, err
	}
	options := HTTPTransportOptions{}
	if server.RequestTimeoutMS > 0 {
		options.Client = &http.Client{Timeout: time.Duration(server.RequestTimeoutMS) * time.Millisecond}
	}
	if server.BodyCapBytes > 0 {
		options.BodyCap = int64(server.BodyCapBytes)
	}
	if server.SSEIdleTimeoutMS > 0 {
		options.SSEIdleTimeout = time.Duration(server.SSEIdleTimeoutMS) * time.Millisecond
	}
	if server.Reconnect != nil {
		if server.Reconnect.InitialMS > 0 {
			options.ReconnectInitialDelay = time.Duration(server.Reconnect.InitialMS) * time.Millisecond
		}
		if server.Reconnect.MaxMS > 0 {
			options.ReconnectMaxDelay = time.Duration(server.Reconnect.MaxMS) * time.Millisecond
		}
		options.ReconnectMaxAttempts = int(server.Reconnect.MaxAttempts)
	}
	if server.Auth != nil {
		if resolveToken == nil {
			return "", HTTPTransportOptions{}, fmt.Errorf("bearer auth requires token resolver")
		}
		auth, err := ResolveHTTPAuthFromStore(server.Auth, tokenResolverAuthStore{resolveToken: resolveToken})
		if err != nil {
			return "", HTTPTransportOptions{}, err
		}
		if header := auth.HeaderValue(); header != "" {
			options.Headers = map[string]string{"Authorization": header}
		}
	}
	return server.Endpoint, options, nil
}

type authStore interface {
	ResolveForProvider(provider string) (string, bool)
}

type tokenResolverAuthStore struct {
	resolveToken TokenResolver
}

func (store tokenResolverAuthStore) ResolveForProvider(provider string) (string, bool) {
	return store.resolveToken(provider)
}

func ResolveHTTPAuth(auth *HTTPAuthConfig) (HttpMcpAuth, error) {
	if auth == nil {
		return HttpMcpAuthNone, nil
	}
	store, err := config.LoadDefaultAuthStore()
	if err != nil {
		return HttpMcpAuthNone, fmt.Errorf("failed to load local credential store: %v; %s", err, HTTPAuthRecovery(auth))
	}
	return ResolveHTTPAuthFromStore(auth, store)
}

func ResolveHTTPAuthFromStore(auth *HTTPAuthConfig, store authStore) (HttpMcpAuth, error) {
	if auth == nil {
		return HttpMcpAuthNone, nil
	}
	if auth.Kind != "bearer" {
		return HttpMcpAuthNone, fmt.Errorf("unsupported streamable_http auth kind; expected bearer")
	}
	if auth.TokenKeychainRef == "" {
		return HttpMcpAuthNone, fmt.Errorf("bearer auth requires token_keychain_ref")
	}
	token, ok := store.ResolveForProvider(auth.TokenKeychainRef)
	if !ok || strings.TrimSpace(token) == "" {
		return HttpMcpAuthNone, fmt.Errorf("configured bearer credential was not found; %s", HTTPAuthRecovery(auth))
	}
	return HTTPMCPBearerAuth(token), nil
}

func HTTPAuthRecovery(auth *HTTPAuthConfig) string {
	return "run /login <configured-token-ref>"
}

func validateHTTPTransportEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid MCP HTTP endpoint: %v", err)
	}
	if parsed.Scheme != "https" && parsed.Hostname() != "127.0.0.1" {
		return fmt.Errorf("streamable_http endpoint must be https, except 127.0.0.1 test fixtures")
	}
	return nil
}

func ParseMCPConfig(data []byte) (MCPConfig, error) {
	var cfg MCPConfig
	var current *ServerConfig
	section := "unknown"
	var pendingKey string
	var pendingValue strings.Builder
	var pendingLine int
	var pendingMultilineStringDelimiter string
	seenKeys := map[string]bool{}
	seenSections := map[string]bool{}
	for lineNumber, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripTOMLComment(rawLine))
		if line == "" {
			continue
		}
		if pendingKey != "" {
			if pendingMultilineStringDelimiter != "" {
				pendingValue.WriteByte('\n')
				pendingValue.WriteString(rawLine)
			} else {
				pendingValue.WriteString(line)
			}
			if pendingMultilineStringDelimiter != "" && !tomlMultilineStringClosed(pendingValue.String(), pendingMultilineStringDelimiter) {
				continue
			}
			if pendingMultilineStringDelimiter == "" && strings.HasPrefix(strings.TrimSpace(pendingValue.String()), "[") && !tomlArrayClosed(pendingValue.String()) {
				continue
			}
			seenKey := section + "." + pendingKey
			if seenKeys[seenKey] {
				return MCPConfig{}, fmt.Errorf("line %d: duplicate field %s", pendingLine, pendingKey)
			}
			seenKeys[seenKey] = true
			if err := applyMCPValue(current, section, pendingKey, pendingValue.String()); err != nil {
				return MCPConfig{}, fmt.Errorf("line %d: %w", pendingLine, err)
			}
			pendingKey = ""
			pendingMultilineStringDelimiter = ""
			continue
		}
		header, err := normalizeTOMLHeader(line)
		if err != nil {
			return MCPConfig{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		switch header {
		case "[[server]]":
			server := ServerConfig{Kind: ServerKindStdio}
			cfg.Servers = append(cfg.Servers, server)
			current = &cfg.Servers[len(cfg.Servers)-1]
			section = "server"
			seenKeys = map[string]bool{}
			seenSections = map[string]bool{}
			continue
		case "[server.auth]":
			if current == nil {
				return MCPConfig{}, fmt.Errorf("line %d: auth section before server", lineNumber+1)
			}
			if seenSections["auth"] {
				return MCPConfig{}, fmt.Errorf("line %d: duplicate auth section", lineNumber+1)
			}
			seenSections["auth"] = true
			if current.Auth == nil {
				current.Auth = &HTTPAuthConfig{}
			}
			section = "auth"
			continue
		case "[server.reconnect]":
			if current == nil {
				return MCPConfig{}, fmt.Errorf("line %d: reconnect section before server", lineNumber+1)
			}
			if seenSections["reconnect"] {
				return MCPConfig{}, fmt.Errorf("line %d: duplicate reconnect section", lineNumber+1)
			}
			seenSections["reconnect"] = true
			if current.Reconnect == nil {
				current.Reconnect = &ReconnectConfig{}
			}
			section = "reconnect"
			continue
		}
		if strings.HasPrefix(header, "[server.") && strings.HasSuffix(header, "]") {
			if current == nil {
				return MCPConfig{}, fmt.Errorf("line %d: nested section before server", lineNumber+1)
			}
			section = "unknown"
			continue
		}
		if strings.HasPrefix(header, "[") && strings.HasSuffix(header, "]") {
			section = "unknown"
			current = nil
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return MCPConfig{}, fmt.Errorf("line %d: invalid TOML assignment %q", lineNumber+1, line)
		}
		if current == nil && section == "unknown" {
			continue
		}
		if current == nil {
			return MCPConfig{}, fmt.Errorf("line %d: field before server", lineNumber+1)
		}
		keyParts, err := parseTOMLKeySegments(key)
		if err != nil {
			return MCPConfig{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
		key = strings.Join(keyParts, ".")
		value = strings.TrimSpace(value)
		valueSection := section
		valueKey := key
		if section == "server" {
			if len(keyParts) == 2 && (keyParts[0] == "auth" || keyParts[0] == "reconnect") {
				valueSection = keyParts[0]
				valueKey = keyParts[1]
			}
		}
		seenKey := valueSection + "." + valueKey
		if seenKeys[seenKey] {
			return MCPConfig{}, fmt.Errorf("line %d: duplicate field %s", lineNumber+1, valueKey)
		}
		if strings.HasPrefix(value, "[") && !tomlArrayClosed(value) {
			pendingKey = valueKey
			pendingValue.Reset()
			pendingValue.WriteString(value)
			pendingLine = lineNumber + 1
			continue
		}
		if delimiter := tomlOpenMultilineStringDelimiter(value); delimiter != "" {
			pendingKey = valueKey
			pendingValue.Reset()
			pendingValue.WriteString(value)
			pendingLine = lineNumber + 1
			pendingMultilineStringDelimiter = delimiter
			continue
		}
		if section == "server" && (key == "auth" || key == "reconnect") {
			if seenSections[key] {
				return MCPConfig{}, fmt.Errorf("line %d: duplicate %s section", lineNumber+1, key)
			}
			seenSections[key] = true
		}
		seenKeys[seenKey] = true
		if err := applyMCPValue(current, valueSection, valueKey, value); err != nil {
			return MCPConfig{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
	}
	if pendingKey != "" {
		return MCPConfig{}, fmt.Errorf("line %d: unterminated TOML array", pendingLine)
	}
	for index, server := range cfg.Servers {
		if server.Name == "" {
			return MCPConfig{}, fmt.Errorf("server %d missing name", index+1)
		}
	}
	cfg.Server = append([]ServerConfig(nil), cfg.Servers...)
	return cfg, nil
}

func applyMCPValue(server *ServerConfig, section, key, raw string) error {
	switch section {
	case "server":
		switch key {
		case "name":
			value, err := parseTOMLString(raw)
			server.Name = value
			return err
		case "kind":
			value, err := parseTOMLString(raw)
			if err != nil {
				return err
			}
			kind := ServerKind(value)
			if kind != ServerKindStdio && kind != ServerKindStreamableHTTP {
				return fmt.Errorf("unknown server kind %q", kind)
			}
			server.Kind = kind
		case "command":
			value, err := parseTOMLString(raw)
			server.Command = value
			return err
		case "args":
			value, err := parseTOMLStringArray(raw)
			server.Args = value
			return err
		case "endpoint":
			value, err := parseTOMLString(raw)
			server.Endpoint = value
			return err
		case "request_timeout_ms":
			value, err := parseTOMLUint(raw)
			if err == nil && value == 0 {
				err = fmt.Errorf("request_timeout_ms must be positive")
			}
			server.RequestTimeoutMS = value
			return err
		case "sse_idle_timeout_ms":
			value, err := parseTOMLUint(raw)
			if err == nil && value == 0 {
				err = fmt.Errorf("sse_idle_timeout_ms must be positive")
			}
			server.SSEIdleTimeoutMS = value
			return err
		case "body_cap_bytes":
			value, err := parseTOMLUint(raw)
			if err == nil && value == 0 {
				err = fmt.Errorf("body_cap_bytes must be positive")
			}
			server.BodyCapBytes = value
			return err
		case "inject_summary":
			value, err := parseTOMLBool(raw)
			server.InjectSummary = value
			return err
		case "inject_and_run":
			value, err := parseTOMLBool(raw)
			server.InjectAndRun = value
			return err
		case "auth":
			auth, err := parseInlineHTTPAuth(raw)
			server.Auth = auth
			return err
		case "reconnect":
			reconnect, err := parseInlineReconnect(raw)
			server.Reconnect = reconnect
			return err
		}
	case "auth":
		if server.Auth == nil {
			server.Auth = &HTTPAuthConfig{}
		}
		switch key {
		case "kind":
			value, err := parseTOMLString(raw)
			server.Auth.Kind = value
			return err
		case "token_keychain_ref":
			value, err := parseTOMLString(raw)
			server.Auth.TokenKeychainRef = value
			return err
		}
	case "reconnect":
		if server.Reconnect == nil {
			server.Reconnect = &ReconnectConfig{}
		}
		value, err := parseTOMLUint(raw)
		if err != nil {
			return err
		}
		switch key {
		case "initial_ms":
			if value == 0 {
				return fmt.Errorf("reconnect delays must be positive")
			}
			server.Reconnect.InitialMS = value
		case "max_ms":
			if value == 0 {
				return fmt.Errorf("reconnect delays must be positive")
			}
			server.Reconnect.MaxMS = value
		case "max_attempts":
			server.Reconnect.MaxAttempts = value
		}
	}
	return nil
}

func parseInlineHTTPAuth(value string) (*HTTPAuthConfig, error) {
	auth := &HTTPAuthConfig{}
	fields, err := parseInlineTOMLTable(value)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return auth, nil
	}
	for key, raw := range fields {
		switch key {
		case "kind":
			parsed, err := parseTOMLString(raw)
			if err != nil {
				return nil, err
			}
			auth.Kind = parsed
		case "token_keychain_ref":
			parsed, err := parseTOMLString(raw)
			if err != nil {
				return nil, err
			}
			auth.TokenKeychainRef = parsed
		}
	}
	return auth, nil
}

func parseInlineReconnect(value string) (*ReconnectConfig, error) {
	fields, err := parseInlineTOMLTable(value)
	if err != nil {
		return nil, err
	}
	reconnect := &ReconnectConfig{}
	for key, raw := range fields {
		parsed, err := parseTOMLUint(raw)
		if err != nil {
			return nil, err
		}
		switch key {
		case "initial_ms":
			if parsed == 0 {
				return nil, fmt.Errorf("reconnect delays must be positive")
			}
			reconnect.InitialMS = parsed
		case "max_ms":
			if parsed == 0 {
				return nil, fmt.Errorf("reconnect delays must be positive")
			}
			reconnect.MaxMS = parsed
		case "max_attempts":
			reconnect.MaxAttempts = parsed
		}
	}
	return reconnect, nil
}

func parseInlineTOMLTable(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, fmt.Errorf("expected TOML inline table")
	}
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}"))
	fields := map[string]string{}
	if value == "" {
		return fields, nil
	}
	parts, err := splitTOMLCommaFields(value)
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		key, raw, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid TOML inline table field")
		}
		key, err := parseTOMLKey(key)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("invalid TOML inline table field")
		}
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("invalid TOML inline table field")
		}
		if fields[key] != "" {
			return nil, fmt.Errorf("duplicate field %s", key)
		}
		fields[key] = raw
	}
	return fields, nil
}

func parseTOMLKey(value string) (string, error) {
	parts, err := parseTOMLKeySegments(value)
	if err != nil {
		return "", err
	}
	return strings.Join(parts, "."), nil
}

func parseTOMLKeySegments(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("invalid TOML key")
	}
	parts, err := splitTOMLKeySegments(value)
	if err != nil {
		return nil, err
	}
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid TOML key")
		}
		if part[0] == '"' || part[0] == '\'' {
			key, err := parseTOMLString(part)
			if err != nil {
				return nil, err
			}
			part = key
		} else if !validTOMLBareKey(part) {
			return nil, fmt.Errorf("invalid TOML key")
		}
		if part == "" {
			return nil, fmt.Errorf("invalid TOML key")
		}
		parts[index] = part
	}
	return parts, nil
}

func splitTOMLKeySegments(value string) ([]string, error) {
	var parts []string
	var builder strings.Builder
	var quote rune
	escaped := false
	for _, char := range value {
		if escaped {
			escaped = false
			builder.WriteRune(char)
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			builder.WriteRune(char)
			continue
		}
		if char == '\'' || char == '"' {
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			}
		}
		if char == '.' && quote == 0 {
			parts = append(parts, builder.String())
			builder.Reset()
			continue
		}
		builder.WriteRune(char)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated TOML key string")
	}
	parts = append(parts, builder.String())
	return parts, nil
}

func validTOMLBareKey(value string) bool {
	for _, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func splitTOMLCommaFields(value string) ([]string, error) {
	var parts []string
	var builder strings.Builder
	var quote rune
	escaped := false
	for _, char := range value {
		if escaped {
			escaped = false
			builder.WriteRune(char)
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			builder.WriteRune(char)
			continue
		}
		if char == '\'' || char == '"' {
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			}
		}
		if char == ',' && quote == 0 {
			parts = append(parts, builder.String())
			builder.Reset()
			continue
		}
		builder.WriteRune(char)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated TOML inline table string")
	}
	parts = append(parts, builder.String())
	return parts, nil
}

func stripTOMLComment(line string) string {
	var quote rune
	escaped := false
	for index, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' || char == '\'' {
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			}
		}
		if char == '#' && quote == 0 {
			return line[:index]
		}
	}
	return line
}

func normalizeTOMLHeader(line string) (string, error) {
	if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
		name, err := normalizeTOMLHeaderName(line[2 : len(line)-2])
		if err != nil {
			return "", err
		}
		return "[[" + name + "]]", nil
	}
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		name, err := normalizeTOMLHeaderName(line[1 : len(line)-1])
		if err != nil {
			return "", err
		}
		return "[" + name + "]", nil
	}
	return line, nil
}

func normalizeTOMLHeaderName(value string) (string, error) {
	parts, err := parseTOMLKeySegments(value)
	if err != nil {
		return "", err
	}
	return strings.Join(parts, "."), nil
}

func parseTOMLString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"""`) && strings.HasSuffix(value, `"""`) && len(value) >= 6 {
		return unescapeBasicTOMLString(foldTOMLMultilineBasicString(trimTOMLMultilineInitialNewline(value[3 : len(value)-3])))
	}
	if strings.HasPrefix(value, `'''`) && strings.HasSuffix(value, `'''`) && len(value) >= 6 {
		return trimTOMLMultilineInitialNewline(value[3 : len(value)-3]), nil
	}
	if len(value) < 2 || (value[0] != '"' && value[0] != '\'') || value[len(value)-1] != value[0] {
		return "", fmt.Errorf("expected TOML string")
	}
	inner := value[1 : len(value)-1]
	if value[0] == '\'' {
		return inner, nil
	}
	return unescapeBasicTOMLString(inner)
}

func trimTOMLMultilineInitialNewline(value string) string {
	if strings.HasPrefix(value, "\r\n") {
		return value[2:]
	}
	return strings.TrimPrefix(value, "\n")
}

func foldTOMLMultilineBasicString(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) || (value[index+1] != '\n' && value[index+1] != '\r') {
			builder.WriteByte(value[index])
			continue
		}
		index++
		if value[index] == '\r' && index+1 < len(value) && value[index+1] == '\n' {
			index++
		}
		for index+1 < len(value) {
			next := value[index+1]
			if next != ' ' && next != '\t' && next != '\n' && next != '\r' {
				break
			}
			index++
			if next == '\r' && index+1 < len(value) && value[index+1] == '\n' {
				index++
			}
		}
	}
	return builder.String()
}

func tomlOpenMultilineStringDelimiter(value string) string {
	value = strings.TrimSpace(value)
	for _, delimiter := range []string{`"""`, `'''`} {
		if strings.HasPrefix(value, delimiter) && !tomlMultilineStringClosed(value, delimiter) {
			return delimiter
		}
	}
	return ""
}

func tomlMultilineStringClosed(value, delimiter string) bool {
	if !strings.HasPrefix(strings.TrimSpace(value), delimiter) {
		return false
	}
	start := strings.Index(value, delimiter)
	return strings.Contains(value[start+len(delimiter):], delimiter)
}

func unescapeBasicTOMLString(value string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(value); {
		char, size := utf8.DecodeRuneInString(value[index:])
		if char != '\\' {
			builder.WriteRune(char)
			index += size
			continue
		}
		index += size
		if index >= len(value) {
			return "", fmt.Errorf("unterminated TOML string escape")
		}
		escape, escapeSize := utf8.DecodeRuneInString(value[index:])
		switch escape {
		case '"':
			builder.WriteByte('"')
		case '\\':
			builder.WriteByte('\\')
		case 'b':
			builder.WriteByte('\b')
		case 't':
			builder.WriteByte('\t')
		case 'n':
			builder.WriteByte('\n')
		case 'f':
			builder.WriteByte('\f')
		case 'r':
			builder.WriteByte('\r')
		case 'u':
			r, err := parseTOMLUnicodeEscape(value[index+escapeSize:], 4)
			if err != nil {
				return "", err
			}
			builder.WriteRune(r)
			index += escapeSize + 4
			continue
		case 'U':
			r, err := parseTOMLUnicodeEscape(value[index+escapeSize:], 8)
			if err != nil {
				return "", err
			}
			builder.WriteRune(r)
			index += escapeSize + 8
			continue
		default:
			return "", fmt.Errorf("invalid TOML string escape")
		}
		index += escapeSize
	}
	return builder.String(), nil
}

func parseTOMLUnicodeEscape(value string, digits int) (rune, error) {
	if len(value) < digits {
		return 0, fmt.Errorf("invalid TOML unicode escape")
	}
	hexDigits := value[:digits]
	parsed, err := strconv.ParseUint(hexDigits, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid TOML unicode escape")
	}
	r := rune(parsed)
	if !utf8.ValidRune(r) {
		return 0, fmt.Errorf("invalid TOML unicode escape")
	}
	return r, nil
}

func parseTOMLStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected TOML string array")
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts, err := splitTOMLCommaFields(value)
	if err != nil {
		return nil, err
	}
	var out []string
	for index, part := range parts {
		if strings.TrimSpace(part) == "" {
			if index != len(parts)-1 {
				return nil, fmt.Errorf("empty TOML array element")
			}
			continue
		}
		item, err := parseTOMLString(part)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func tomlArrayClosed(value string) bool {
	var quote rune
	escaped := false
	for _, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if char == '\'' || char == '"' {
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			}
			continue
		}
		if char == ']' && quote == 0 {
			return true
		}
	}
	return false
}

func parseTOMLBool(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", value)
	}
}

func parseTOMLUint(value string) (uint64, error) {
	text := strings.TrimSpace(value)
	if hasInvalidTOMLIntegerUnderscore(text) {
		return 0, fmt.Errorf("invalid TOML integer underscore")
	}
	text = strings.ReplaceAll(text, "_", "")
	base := 10
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		base = 16
		text = text[2:]
	} else if strings.HasPrefix(text, "0o") || strings.HasPrefix(text, "0O") {
		base = 8
		text = text[2:]
	} else if strings.HasPrefix(text, "0b") || strings.HasPrefix(text, "0B") {
		base = 2
		text = text[2:]
	}
	parsed, err := strconv.ParseUint(text, base, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func hasInvalidTOMLIntegerUnderscore(value string) bool {
	if strings.HasPrefix(value, "_") || strings.HasSuffix(value, "_") || strings.Contains(value, "__") {
		return true
	}
	return strings.HasPrefix(value, "0x_") || strings.HasPrefix(value, "0X_") || strings.HasPrefix(value, "0o_") || strings.HasPrefix(value, "0O_") || strings.HasPrefix(value, "0b_") || strings.HasPrefix(value, "0B_")
}

func serverIndex(servers []ServerConfig, name string) int {
	for index, server := range servers {
		if server.Name == name {
			return index
		}
	}
	return -1
}
