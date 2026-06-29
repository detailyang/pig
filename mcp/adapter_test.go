package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type fakeToolClient struct {
	name   string
	args   any
	result ToolCallResult
	err    error
}

func (client *fakeToolClient) ToolsCall(ctx context.Context, name string, arguments any) (ToolCallResult, error) {
	client.name = name
	client.args = arguments
	return client.result, client.err
}

func TestAgentToolAdapterMetadataAndExecute(t *testing.T) {
	client := &fakeToolClient{result: ToolCallResult{Content: []ToolContent{{Type: "text", Text: "hello"}, {Type: "resource", Resource: map[string]any{"uri": "file:///x"}}}}}
	description := "Echo text"
	adapter := NewAgentToolAdapter(client, Tool{Name: "echo", Description: &description, InputSchema: map[string]any{"type": "object"}})
	parameters, _ := adapter.Parameters().(map[string]any)
	if adapter.Name() != "echo" || adapter.Description() != "Echo text" || parameters["type"] != "object" {
		t.Fatalf("metadata mismatch")
	}
	result, err := adapter.Execute(context.Background(), testToolCall("echo", map[string]any{"text": "hi"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.name != "echo" || client.args.(map[string]any)["text"] != "hi" {
		t.Fatalf("client call mismatch name=%s args=%#v", client.name, client.args)
	}
	if !strings.Contains(result.Content, "hello") || !strings.Contains(result.Content, "<resource>") {
		t.Fatalf("content mismatch: %q", result.Content)
	}
	if result.Details["name"] != "echo" || result.Details["isError"] != false {
		t.Fatalf("details mismatch: %#v", result.Details)
	}
}

func TestAgentToolAdapterPreservesImageContentBlocksLikeUpstream(t *testing.T) {
	client := &fakeToolClient{result: ToolCallResult{Content: []ToolContent{{Type: "text", Text: "see image"}, {Type: "image", MimeType: "image/png", Data: "abc"}}}}
	adapter := NewAgentToolAdapter(client, Tool{Name: "screenshot"})

	result, err := adapter.Execute(context.Background(), testToolCall("screenshot", nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ContentBlocks) != 2 {
		t.Fatalf("content block count mismatch: %#v", result.ContentBlocks)
	}
	if result.ContentBlocks[0].Type != ai.ContentText || result.ContentBlocks[0].Text != "see image" {
		t.Fatalf("text content block mismatch: %#v", result.ContentBlocks[0])
	}
	if result.ContentBlocks[1].Type != ai.ContentImage || result.ContentBlocks[1].MimeType != "image/png" || result.ContentBlocks[1].Data != "abc" {
		t.Fatalf("image content block mismatch: %#v", result.ContentBlocks[1])
	}
}

func TestAgentToolAdapterErrorResult(t *testing.T) {
	client := &fakeToolClient{result: ToolCallResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "server failed"}}}}
	adapter := NewAgentToolAdapter(client, Tool{Name: "fail"})
	_, err := adapter.Execute(context.Background(), testToolCall("fail", nil), nil)
	if err == nil || !strings.Contains(err.Error(), "server failed") {
		t.Fatalf("expected server failed error, got %v", err)
	}
}

func TestAgentToolAdapterErrorResultOnlyIncludesTextualBlocks(t *testing.T) {
	client := &fakeToolClient{result: ToolCallResult{IsError: true, Content: []ToolContent{
		{Type: "text", Text: "server failed"},
		{Type: "image", MimeType: "image/png", Data: "abc"},
		{Type: "resource", Resource: map[string]any{"uri": "file:///x"}},
	}}}
	adapter := NewAgentToolAdapter(client, Tool{Name: "fail"})
	_, err := adapter.Execute(context.Background(), testToolCall("fail", nil), nil)
	if err == nil || err.Error() != "server failed\n<resource>{\"uri\":\"file:///x\"}</resource>" {
		t.Fatalf("error content mismatch: %v", err)
	}
}

func TestAgentToolAdapterResourceJSONDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	client := &fakeToolClient{result: ToolCallResult{Content: []ToolContent{{Type: "resource", Resource: map[string]any{"text": "a < b && c > d"}}}}}
	adapter := NewAgentToolAdapter(client, Tool{Name: "resource"})

	result, err := adapter.Execute(context.Background(), testToolCall("resource", nil), nil)
	if err != nil {
		t.Fatal(err)
	}

	want := `<resource>{"text":"a < b && c > d"}</resource>`
	if result.Content != want || len(result.ContentBlocks) != 1 || result.ContentBlocks[0].Text != want {
		t.Fatalf("resource JSON should match upstream serde_json formatting, content=%q blocks=%#v", result.Content, result.ContentBlocks)
	}
}

func TestAgentToolAdapterCancelledErrorMatchesUpstream(t *testing.T) {
	client := &fakeToolClient{err: ErrCancelled}
	adapter := NewAgentToolAdapter(client, Tool{Name: "slow"})
	_, err := adapter.Execute(context.Background(), testToolCall("slow", nil), nil)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("cancelled error mismatch: %v", err)
	}
}

func TestAgentToolAdapterExecutionModeMatchesUpstream(t *testing.T) {
	adapter := NewAgentToolAdapter(&fakeToolClient{}, Tool{Name: "echo"})
	if got := adapter.ExecutionMode(); got != agent.ToolExecutionParallel {
		t.Fatalf("execution mode mismatch: got %q want %q", got, agent.ToolExecutionParallel)
	}
}

func TestMCPAdapterRunsInsideAgentLoop(t *testing.T) {
	clientSide, serverSide := newPipePair()
	server := newMockServer(t, serverSide)
	go server.serve()

	client := NewClient(clientSide).WithTimeout(time.Second)
	if _, err := client.Initialize(context.Background(), "pig-test"); err != nil {
		t.Fatal(err)
	}
	catalog, err := client.ToolsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].Name != "echo" {
		t.Fatalf("catalog mismatch: %#v", catalog)
	}

	streamCalls := 0
	runner := agent.New(agent.Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []agent.Tool{NewAgentToolAdapter(client, catalog[0])},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				if len(tools) != 1 || tools[0].Name != "echo" {
					t.Fatalf("LLM should receive MCP tool spec, got %#v", tools)
				}
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello mcp"}}})
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			if len(messages) < 3 || messages[len(messages)-1].Role != ai.RoleTool || messages[len(messages)-1].ToolCallID != "call-1" || messages[len(messages)-1].Content[0].Text != "hello mcp" {
				t.Fatalf("second LLM call should include MCP tool result, got %#v", messages)
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	state, err := runner.Run(context.Background(), []agent.Message{agent.NewUserMessage("echo through mcp")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 {
		t.Fatalf("expected agent to continue after MCP tool result, got %d stream calls", streamCalls)
	}
	if len(state.Messages) != 4 {
		t.Fatalf("expected user, assistant tool call, MCP tool result, final assistant; got %#v", state.Messages)
	}
	toolResult := state.Messages[2].ToolResult
	if toolResult == nil || toolResult.CallID != "call-1" || toolResult.Name != "echo" || toolResult.Content != "hello mcp" {
		t.Fatalf("MCP tool result mismatch: %#v", toolResult)
	}
	if state.Messages[3].LLM == nil || state.Messages[3].LLM.Content[0].Text != "done" {
		t.Fatalf("final assistant mismatch: %#v", state.Messages[3])
	}
}

func TestAgentToolAdaptersFromCatalog(t *testing.T) {
	client := &fakeToolClient{}
	adapters := AgentToolsFromCatalog(client, []Tool{{Name: "a"}, {Name: "b"}})
	if len(adapters) != 2 || adapters[0].Name() != "a" || adapters[1].Name() != "b" {
		t.Fatalf("adapters mismatch: %#v", adapters)
	}
}

func TestMcpAgentToolUpstreamNameAndConstructor(t *testing.T) {
	client := &fakeToolClient{result: ToolCallResult{Content: []ToolContent{{Type: "text", Text: "ok"}}}}
	description := "Echo text"
	tool := NewMcpAgentTool(client, &McpTool{Name: "echo", Description: &description, InputSchema: map[string]any{"type": "object"}})
	var _ agent.Tool = tool
	var _ agent.ToolDefinition = tool
	var _ agent.ToolExecutionModeOverride = tool

	parameters, _ := tool.Parameters().(map[string]any)
	if tool.Name() != "echo" || tool.Description() != "Echo text" || parameters["type"] != "object" {
		t.Fatalf("metadata mismatch")
	}
	result, err := tool.Execute(context.Background(), testToolCall("echo", map[string]any{"text": "hi"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" || client.name != "echo" || client.args.(map[string]any)["text"] != "hi" {
		t.Fatalf("execute mismatch result=%#v client=%#v", result, client)
	}
}
