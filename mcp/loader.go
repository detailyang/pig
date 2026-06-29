package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/detailyang/pig/agent"
)

type LoadedMCP struct {
	Tools                []agent.Tool
	Diagnostics          []string
	ClientCount          int
	ServerNames          []string
	Clients              []*Client
	NotificationSources  []MCPNotificationSource
	NotificationHooks    []MCPNotificationSource
	InjectSummaryServers map[string]bool
	InjectAndRunServers  map[string]bool
}

type LoadedMcp = LoadedMCP

type MCPNotificationSource struct {
	ServerName    string
	Notifications <-chan ServerNotification
}

func (source MCPNotificationSource) Label() string { return "mcp:" + source.ServerName }

type MCPConnectOptions struct {
	ClientName    string
	Timeout       time.Duration
	TokenResolver TokenResolver
	NewTransport  func(context.Context, ServerConfig) (Transport, error)
}

func LoadAll(cwd string, options ...MCPConnectOptions) LoadedMcp {
	return LoadAllWithContext(context.Background(), cwd, options...)
}

func LoadAllWithContext(ctx context.Context, cwd string, options ...MCPConnectOptions) LoadedMcp {
	config := LoadMCPConfigAll(cwd)
	connectOptions := MCPConnectOptions{}
	if len(options) > 0 {
		connectOptions = options[0]
	}
	loaded := ConnectMCPServers(ctx, config.Servers, connectOptions)
	if len(config.Diagnostics) != 0 {
		loaded.Diagnostics = append(append([]string(nil), config.Diagnostics...), loaded.Diagnostics...)
	}
	return loaded
}

func ConnectMCPServers(ctx context.Context, servers []ServerConfig, options MCPConnectOptions) LoadedMCP {
	clientName := options.ClientName
	if clientName == "" {
		clientName = "pie-coding-agent"
	}
	newTransport := options.NewTransport
	if newTransport == nil {
		newTransport = func(ctx context.Context, server ServerConfig) (Transport, error) {
			return DefaultMCPTransportFromServer(ctx, server, options.TokenResolver)
		}
	}
	loaded := LoadedMCP{InjectSummaryServers: map[string]bool{}, InjectAndRunServers: map[string]bool{}}
	for _, server := range servers {
		if server.InjectAndRun {
			loaded.InjectAndRunServers[server.Name] = true
		}
		if server.InjectSummary {
			loaded.InjectSummaryServers[server.Name] = true
		}
	}
	for _, server := range servers {
		transport, err := newTransport(ctx, server)
		if err != nil {
			loaded.Diagnostics = append(loaded.Diagnostics, fmt.Sprintf("mcp server '%s' failed: %v", server.Name, err))
			continue
		}
		client := NewClient(transport)
		if options.Timeout > 0 {
			client.WithTimeout(options.Timeout)
		}
		if _, err := client.Initialize(ctx, clientName); err != nil {
			_ = client.Close()
			loaded.Diagnostics = append(loaded.Diagnostics, fmt.Sprintf("mcp server '%s' failed: initialize: %v", server.Name, err))
			continue
		}
		catalog, err := client.ToolsList(ctx)
		if err != nil {
			_ = client.Close()
			loaded.Diagnostics = append(loaded.Diagnostics, fmt.Sprintf("mcp server '%s' failed: tools/list: %v", server.Name, err))
			continue
		}
		for _, tool := range catalog {
			loaded.Tools = append(loaded.Tools, NewAgentToolAdapter(client, tool))
		}
		if notifications, ok := client.TakeNotifications(); ok {
			source := MCPNotificationSource{ServerName: server.Name, Notifications: notifications}
			loaded.NotificationSources = append(loaded.NotificationSources, source)
			loaded.NotificationHooks = append(loaded.NotificationHooks, source)
		}
		loaded.Clients = append(loaded.Clients, client)
		loaded.ServerNames = append(loaded.ServerNames, server.Name)
		loaded.ClientCount++
	}
	return loaded
}

func ConnectAll(ctx context.Context, servers []ServerConfig, options MCPConnectOptions) LoadedMCP {
	return ConnectMCPServers(ctx, servers, options)
}

func ConnectOne(ctx context.Context, server ServerConfig, options MCPConnectOptions) ([]agent.Tool, MCPNotificationSource, error) {
	loaded := ConnectMCPServers(ctx, []ServerConfig{server}, options)
	if len(loaded.Diagnostics) != 0 {
		return nil, MCPNotificationSource{}, errors.New(loaded.Diagnostics[0])
	}
	if len(loaded.NotificationSources) == 0 {
		return loaded.Tools, MCPNotificationSource{}, fmt.Errorf("mcp server %q did not provide a notification source", server.Name)
	}
	return loaded.Tools, loaded.NotificationSources[0], nil
}

func ConnectStdio(ctx context.Context, server ServerConfig, options MCPConnectOptions) (Transport, error) {
	if options.NewTransport != nil {
		return options.NewTransport(ctx, server)
	}
	command, args, err := StdioCommandFromServerConfig(server)
	if err != nil {
		return nil, err
	}
	transport, _, err := SpawnStdioTransport(command, args...)
	return transport, err
}

func ConnectStreamableHTTP(ctx context.Context, server ServerConfig, options MCPConnectOptions) (Transport, error) {
	if options.NewTransport != nil {
		return options.NewTransport(ctx, server)
	}
	endpoint, transportOptions, err := HTTPTransportOptionsFromServerConfig(server, options.TokenResolver)
	if err != nil {
		return nil, err
	}
	transport := NewHTTPTransport(endpoint, transportOptions)
	transport.StartSSE(ctx)
	return transport, nil
}

func DefaultMCPTransportFromServer(ctx context.Context, server ServerConfig, resolveToken TokenResolver) (Transport, error) {
	switch server.Kind {
	case "", ServerKindStdio:
		command, args, err := StdioCommandFromServerConfig(server)
		if err != nil {
			return nil, err
		}
		transport, _, err := SpawnStdioTransport(command, args...)
		return transport, err
	case ServerKindStreamableHTTP:
		endpoint, options, err := HTTPTransportOptionsFromServerConfig(server, resolveToken)
		if err != nil {
			return nil, err
		}
		transport := NewHTTPTransport(endpoint, options)
		transport.StartSSE(ctx)
		return transport, nil
	default:
		return nil, fmt.Errorf("unknown server kind %q", server.Kind)
	}
}
