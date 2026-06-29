package mcploader

import (
	"context"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/mcp"
)

type MCPConfig = mcp.MCPConfig
type McpConfig = mcp.McpConfig
type ServerConfig = mcp.ServerConfig
type ServerKind = mcp.ServerKind
type HTTPAuthConfig = mcp.HTTPAuthConfig
type HttpAuthConfig = mcp.HttpAuthConfig
type ReconnectConfig = mcp.ReconnectConfig
type LoadedMCPConfig = mcp.LoadedMCPConfig
type LoadedMCP = mcp.LoadedMCP
type LoadedMcp = mcp.LoadedMcp
type MCPNotificationSource = mcp.MCPNotificationSource
type MCPConnectOptions = mcp.MCPConnectOptions
type TokenResolver = mcp.TokenResolver
type Transport = mcp.Transport

const ServerKindStdio = mcp.ServerKindStdio
const ServerKindStreamableHTTP = mcp.ServerKindStreamableHTTP

func LoadAll(cwd string, options ...MCPConnectOptions) LoadedMcp {
	return mcp.LoadAll(cwd, options...)
}

func LoadAllWithContext(ctx context.Context, cwd string, options ...MCPConnectOptions) LoadedMcp {
	return mcp.LoadAllWithContext(ctx, cwd, options...)
}

func LoadMCPConfigAll(cwd string) LoadedMCPConfig {
	return mcp.LoadMCPConfigAll(cwd)
}

func LoadMCPConfigFiles(paths ...string) LoadedMCPConfig {
	return mcp.LoadMCPConfigFiles(paths...)
}

func ReadConfig(path string, diagnostics *[]string, label string) (MCPConfig, bool) {
	return mcp.ReadConfig(path, diagnostics, label)
}

func ParseMCPConfig(data []byte) (MCPConfig, error) {
	return mcp.ParseMCPConfig(data)
}

func ConnectMCPServers(ctx context.Context, servers []ServerConfig, options MCPConnectOptions) LoadedMCP {
	return mcp.ConnectMCPServers(ctx, servers, options)
}

func ConnectAll(ctx context.Context, servers []ServerConfig, options MCPConnectOptions) LoadedMCP {
	return mcp.ConnectAll(ctx, servers, options)
}

func ConnectOne(ctx context.Context, server ServerConfig, options MCPConnectOptions) ([]agent.Tool, MCPNotificationSource, error) {
	return mcp.ConnectOne(ctx, server, options)
}

func ConnectStdio(ctx context.Context, server ServerConfig, options MCPConnectOptions) (Transport, error) {
	return mcp.ConnectStdio(ctx, server, options)
}

func ConnectStreamableHTTP(ctx context.Context, server ServerConfig, options MCPConnectOptions) (Transport, error) {
	return mcp.ConnectStreamableHTTP(ctx, server, options)
}

func DefaultMCPTransportFromServer(ctx context.Context, server ServerConfig, resolveToken TokenResolver) (Transport, error) {
	return mcp.DefaultMCPTransportFromServer(ctx, server, resolveToken)
}

func StdioCommandFromServerConfig(server ServerConfig) (string, []string, error) {
	return mcp.StdioCommandFromServerConfig(server)
}

func HTTPTransportOptionsFromServerConfig(server ServerConfig, resolveToken TokenResolver) (string, mcp.HTTPTransportOptions, error) {
	return mcp.HTTPTransportOptionsFromServerConfig(server, resolveToken)
}

func ResolveHTTPAuth(auth *HTTPAuthConfig) (mcp.HttpMcpAuth, error) {
	return mcp.ResolveHTTPAuth(auth)
}

func HTTPAuthRecovery(auth *HTTPAuthConfig) string {
	return mcp.HTTPAuthRecovery(auth)
}
