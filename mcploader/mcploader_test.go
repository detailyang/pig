package mcploader

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/mcp"
)

func TestMCPLoaderPackageParsesConfigAndConnectsFailures(t *testing.T) {
	cfg, err := ParseMCPConfig([]byte(`
[[server]]
name = "local"
kind = "stdio"
command = "node"
args = ["server.js"]
inject_summary = true
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Server) != 1 || cfg.Server[0].Name != "local" || cfg.Server[0].Kind != ServerKindStdio || !cfg.Server[0].InjectSummary {
		t.Fatalf("config mismatch: %#v", cfg)
	}
	command, args, err := StdioCommandFromServerConfig(cfg.Server[0])
	if err != nil {
		t.Fatal(err)
	}
	if command != "node" || !reflect.DeepEqual(args, []string{"server.js"}) {
		t.Fatalf("stdio command mismatch: %q %#v", command, args)
	}
	loaded := ConnectAll(context.Background(), []ServerConfig{{Name: "bad"}}, MCPConnectOptions{NewTransport: func(context.Context, ServerConfig) (mcp.Transport, error) {
		return nil, errors.New("boom")
	}})
	if loaded.ClientCount != 0 || len(loaded.Diagnostics) != 1 || !strings.Contains(loaded.Diagnostics[0], "boom") {
		t.Fatalf("connect all mismatch: %#v", loaded)
	}
}

func TestMCPLoaderPackageLoadConfigFilesAndHTTPAuth(t *testing.T) {
	loaded := LoadMCPConfigFiles("/path/that/does/not/exist")
	if len(loaded.Servers) != 0 || len(loaded.Diagnostics) != 0 {
		t.Fatalf("missing config should be ignored: %#v", loaded)
	}
	transport, err := DefaultMCPTransportFromServer(context.Background(), ServerConfig{Kind: ServerKindStreamableHTTP, Name: "remote", Endpoint: "https://example.test/mcp", Auth: &HttpAuthConfig{Kind: "bearer", TokenKeychainRef: "token"}}, TokenResolver(func(ref string) (string, bool) {
		if ref != "token" {
			t.Fatalf("token ref mismatch: %q", ref)
		}
		return "secret", true
	}))
	if err != nil {
		t.Fatal(err)
	}
	httpTransport, ok := transport.(*mcp.HTTPTransport)
	if !ok || httpTransport.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("http transport mismatch: %#v %T", transport, transport)
	}
}
