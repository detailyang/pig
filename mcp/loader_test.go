package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
)

func TestConnectMCPServersReturnsToolsAndSkipsFailuresLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	server := newMockServer(t, serverSide)
	go server.serve()
	runClientSide, runServerSide := newPipePair()
	runServer := newMockServer(t, runServerSide)
	go runServer.serve()

	loaded := ConnectMCPServers(context.Background(), []ServerConfig{{Name: "ok", InjectSummary: true}, {Name: "bad"}, {Name: "run", InjectSummary: true, InjectAndRun: true}}, MCPConnectOptions{
		ClientName: "pig-test",
		Timeout:    time.Second,
		NewTransport: func(ctx context.Context, server ServerConfig) (Transport, error) {
			if server.Name == "bad" {
				return nil, errors.New("boom")
			}
			if server.Name == "run" {
				return runClientSide, nil
			}
			return clientSide, nil
		},
	})

	if loaded.ClientCount != 2 || len(loaded.ServerNames) != 2 || loaded.ServerNames[0] != "ok" || loaded.ServerNames[1] != "run" {
		t.Fatalf("loaded server summary mismatch: %#v", loaded)
	}
	if len(loaded.NotificationSources) != 2 || loaded.NotificationSources[0].Label() != "mcp:ok" || loaded.NotificationSources[1].Label() != "mcp:run" {
		t.Fatalf("notification sources mismatch: %#v", loaded.NotificationSources)
	}
	if len(loaded.NotificationHooks) != 2 || loaded.NotificationHooks[0].Label() != loaded.NotificationSources[0].Label() || loaded.NotificationHooks[1].Label() != loaded.NotificationSources[1].Label() {
		t.Fatalf("notification hooks alias mismatch: %#v", loaded.NotificationHooks)
	}
	if !loaded.InjectSummaryServers["ok"] || !loaded.InjectSummaryServers["run"] || len(loaded.InjectSummaryServers) != 2 || !loaded.InjectAndRunServers["run"] || len(loaded.InjectAndRunServers) != 1 {
		t.Fatalf("inject server sets mismatch: summary=%#v run=%#v", loaded.InjectSummaryServers, loaded.InjectAndRunServers)
	}
	if len(loaded.Tools) != 2 || loaded.Tools[0].Name() != "echo" || loaded.Tools[1].Name() != "echo" {
		t.Fatalf("loaded tools mismatch: %#v", loaded.Tools)
	}
	if len(loaded.Diagnostics) != 1 || loaded.Diagnostics[0] != "mcp server 'bad' failed: boom" {
		t.Fatalf("diagnostics mismatch: %#v", loaded.Diagnostics)
	}
	result, err := loaded.Tools[0].Execute(context.Background(), testToolCall("echo", map[string]any{"text": "hi"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "hi" {
		t.Fatalf("adapter result mismatch: %#v", result)
	}
	if _, ok := loaded.Tools[0].(agent.Tool); !ok {
		t.Fatalf("loaded tool should satisfy agent.Tool")
	}
}

func TestLoadAllReadsConfigAndConnectsServersLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	clientSide, serverSide := newPipePair()
	server := newMockServer(t, serverSide)
	go server.serve()
	if err := os.MkdirAll(filepath.Join(cwd, ".pie"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".pie", "mcp.toml"), []byte(`[[server]]
name = "ok"
command = "ignored"
inject_summary = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := LoadAll(cwd, MCPConnectOptions{ClientName: "pig-test", Timeout: time.Second, NewTransport: func(context.Context, ServerConfig) (Transport, error) {
		return clientSide, nil
	}})
	if len(loaded.Diagnostics) != 0 || loaded.ClientCount != 1 || len(loaded.Tools) != 1 || loaded.Tools[0].Name() != "echo" || len(loaded.NotificationHooks) != 1 || !loaded.InjectSummaryServers["ok"] {
		t.Fatalf("load all should read config and connect like upstream: %#v", loaded)
	}
}

func TestConnectMCPServersDefaultClientNameMatchesUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	recording := &recordingTransport{Transport: clientSide}
	server := newMockServer(t, serverSide)
	go server.serve()

	loaded := ConnectMCPServers(context.Background(), []ServerConfig{{Name: "ok"}}, MCPConnectOptions{Timeout: time.Second, NewTransport: func(context.Context, ServerConfig) (Transport, error) {
		return recording, nil
	}})
	if len(loaded.Diagnostics) != 0 || loaded.ClientCount != 1 {
		t.Fatalf("connect mismatch: %#v", loaded)
	}
	initialize := recording.methodParams("initialize")
	clientInfo, _ := initialize["clientInfo"].(map[string]any)
	if clientInfo["name"] != "pie-coding-agent" {
		t.Fatalf("default MCP client name mismatch: %#v", clientInfo)
	}
}

type recordingTransport struct {
	Transport
	mu    sync.Mutex
	lines []string
}

func (transport *recordingTransport) SendLine(ctx context.Context, line string) error {
	transport.mu.Lock()
	transport.lines = append(transport.lines, line)
	transport.mu.Unlock()
	return transport.Transport.SendLine(ctx, line)
}

func (transport *recordingTransport) methodParams(method string) map[string]any {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	for _, line := range transport.lines {
		var request map[string]any
		if err := json.Unmarshal([]byte(line), &request); err != nil || request["method"] != method {
			continue
		}
		params, _ := request["params"].(map[string]any)
		return params
	}
	return nil
}

func TestDefaultMCPTransportFromServerUsesBearerTokenResolver(t *testing.T) {
	transport, err := DefaultMCPTransportFromServer(context.Background(), ServerConfig{
		Name:     "remote",
		Kind:     ServerKindStreamableHTTP,
		Endpoint: "https://example.test/mcp",
		Auth:     &HTTPAuthConfig{Kind: "bearer", TokenKeychainRef: "mcp-token"},
	}, TokenResolver(func(ref string) (string, bool) {
		if ref != "mcp-token" {
			t.Fatalf("token ref mismatch: %q", ref)
		}
		return "secret-token", true
	}))
	if err != nil {
		t.Fatal(err)
	}
	httpTransport, ok := transport.(*HTTPTransport)
	if !ok {
		t.Fatalf("transport type mismatch: %T", transport)
	}
	if httpTransport.Headers["Authorization"] != "Bearer secret-token" {
		t.Fatalf("authorization header mismatch: %#v", httpTransport.Headers)
	}
}

func TestMCPUpstreamConnectWrappers(t *testing.T) {
	ctx := context.Background()
	if _, err := ConnectStdio(ctx, ServerConfig{Name: "bad", Command: "node", Endpoint: "https://example.test/mcp"}, MCPConnectOptions{}); err == nil {
		t.Fatal("ConnectStdio should validate stdio server shape")
	}
	if _, err := ConnectStreamableHTTP(ctx, ServerConfig{Name: "bad", Kind: ServerKindStreamableHTTP, Command: "node", Endpoint: "https://example.test/mcp"}, MCPConnectOptions{}); err == nil {
		t.Fatal("ConnectStreamableHTTP should validate http server shape")
	}

	clientSide, serverSide := newPipePair()
	server := newMockServer(t, serverSide)
	go server.serve()
	tools, hook, err := ConnectOne(ctx, ServerConfig{Name: "ok"}, MCPConnectOptions{ClientName: "pig-test", Timeout: time.Second, NewTransport: func(context.Context, ServerConfig) (Transport, error) {
		return clientSide, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name() != "echo" || hook.Label() != "mcp:ok" {
		t.Fatalf("connect one mismatch tools=%#v hook=%#v", tools, hook)
	}

	loaded := ConnectAll(ctx, []ServerConfig{{Name: "bad"}}, MCPConnectOptions{NewTransport: func(context.Context, ServerConfig) (Transport, error) {
		return nil, errors.New("boom")
	}})
	if loaded.ClientCount != 0 || len(loaded.Diagnostics) != 1 || loaded.Diagnostics[0] != "mcp server 'bad' failed: boom" {
		t.Fatalf("connect all mismatch: %#v", loaded)
	}
}
