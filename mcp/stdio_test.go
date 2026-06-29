package mcp

import "testing"

func TestSpawnCompatSurface(t *testing.T) {
	transport, cmd, err := Spawn("definitely-missing-pig-mcp-command")
	if err == nil {
		_ = transport.Close()
		_ = cmd.Process.Kill()
		t.Fatal("expected spawn error for missing command")
	}
	if transport != nil || cmd != nil {
		t.Fatalf("failed spawn should not return transport or cmd: %#v %#v", transport, cmd)
	}
}
