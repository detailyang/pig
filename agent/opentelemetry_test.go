package agent

import (
	"context"
	"testing"

	"github.com/detailyang/pig/ai"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type telemetryContextTool struct {
	contextHasSpan bool
	result         string
}

func (tool *telemetryContextTool) Name() string { return "read" }

func (tool *telemetryContextTool) Description() string { return "read a file" }

func (tool *telemetryContextTool) Parameters() map[string]any { return map[string]any{} }

func (tool *telemetryContextTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.contextHasSpan = trace.SpanFromContext(ctx).SpanContext().IsValid()
	if tool.result == "" {
		tool.result = "ok"
	}
	return ToolResult{Content: tool.result}, nil
}

func TestAgentOpenTelemetryRecordsContentOnlyWhenEnabled(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())

	runtime := New(Options{
		Model: ai.Model{ID: "gpt-test", Provider: ai.Provider("openai"), API: ai.Api("fake")},
		Tools: []Tool{&telemetryContextTool{result: "tool secret"}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		OpenTelemetry: &OpenTelemetryOptions{TracerProvider: provider, RecordInputs: true, RecordOutputs: true},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "secret.txt"}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventMetadata, ResponseID: "resp-1"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventUsage, Usage: &ai.Usage{InputTokens: 12, OutputTokens: 3}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	if _, err := runtime.Run(context.Background(), []Message{NewUserMessage("user secret")}); err != nil {
		t.Fatal(err)
	}

	spans := spansByName(recorder.Ended())
	modelAttrs := spanAttributes(spans["chat gpt-test"])
	toolAttrs := spanAttributes(spans["execute_tool read"])
	if modelAttrs["gen_ai.response.id"].AsString() != "resp-1" || modelAttrs["gen_ai.usage.input_tokens"].AsInt64() != 12 || modelAttrs["gen_ai.usage.output_tokens"].AsInt64() != 3 {
		t.Fatalf("model response attributes mismatch: %#v", modelAttrs)
	}
	if modelAttrs["gen_ai.input.messages"].AsString() == "" || modelAttrs["gen_ai.output.messages"].AsString() == "" {
		t.Fatalf("enabled model content attributes missing: %#v", modelAttrs)
	}
	if toolAttrs["gen_ai.tool.call.arguments"].AsString() != `{"path":"secret.txt"}` || toolAttrs["gen_ai.tool.call.result"].AsString() == "" {
		t.Fatalf("enabled tool content attributes mismatch: %#v", toolAttrs)
	}
}

func TestAgentOpenTelemetryCreatesAgentModelAndToolHierarchy(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())

	tool := &telemetryContextTool{}
	modelContextHasSpan := false
	runtime := New(Options{
		Model: ai.Model{ID: "gpt-test", Provider: ai.Provider("openai"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		OpenTelemetry: &OpenTelemetryOptions{TracerProvider: provider},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			modelContextHasSpan = trace.SpanFromContext(ctx).SpanContext().IsValid()
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "secret.txt"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	if _, err := runtime.Run(context.Background(), []Message{NewUserMessage("read the secret")}); err != nil {
		t.Fatal(err)
	}
	if !modelContextHasSpan || !tool.contextHasSpan {
		t.Fatalf("active span was not propagated: model=%v tool=%v", modelContextHasSpan, tool.contextHasSpan)
	}

	spans := spansByName(recorder.Ended())
	agentSpan := spans["invoke_agent"]
	modelSpan := spans["chat gpt-test"]
	toolSpan := spans["execute_tool read"]
	if agentSpan == nil || modelSpan == nil || toolSpan == nil {
		t.Fatalf("missing spans: %#v", spans)
	}
	if modelSpan.Parent().SpanID() != agentSpan.SpanContext().SpanID() || toolSpan.Parent().SpanID() != agentSpan.SpanContext().SpanID() {
		t.Fatalf("unexpected hierarchy: agent=%s model_parent=%s tool_parent=%s", agentSpan.SpanContext().SpanID(), modelSpan.Parent().SpanID(), toolSpan.Parent().SpanID())
	}

	modelAttrs := spanAttributes(modelSpan)
	if modelAttrs["gen_ai.operation.name"].AsString() != "chat" || modelAttrs["gen_ai.request.model"].AsString() != "gpt-test" || modelAttrs["gen_ai.provider.name"].AsString() != "openai" {
		t.Fatalf("model attributes mismatch: %#v", modelAttrs)
	}
	toolAttrs := spanAttributes(toolSpan)
	if toolAttrs["gen_ai.operation.name"].AsString() != "execute_tool" || toolAttrs["gen_ai.tool.name"].AsString() != "read" || toolAttrs["gen_ai.tool.call.id"].AsString() != "call-1" {
		t.Fatalf("tool attributes mismatch: %#v", toolAttrs)
	}
	for _, key := range []string{"gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.tool.call.arguments", "gen_ai.tool.call.result"} {
		if _, ok := modelAttrs[key]; ok {
			t.Fatalf("sensitive attribute %q recorded by default", key)
		}
		if _, ok := toolAttrs[key]; ok {
			t.Fatalf("sensitive attribute %q recorded by default", key)
		}
	}
}

func TestAgentOpenTelemetryEndsPanickingToolSpanWithError(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())

	runtime := New(Options{
		Model:         ai.Model{ID: "gpt-test", Provider: ai.Provider("openai"), API: ai.Api("fake")},
		Tools:         []Tool{panickingTool{}},
		OpenTelemetry: &OpenTelemetryOptions{TracerProvider: provider},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "panic", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	if _, err := runtime.Run(context.Background(), []Message{NewUserMessage("panic")}); err != nil {
		t.Fatal(err)
	}

	toolSpan := spansByName(recorder.Ended())["execute_tool panic"]
	if toolSpan == nil || toolSpan.Status().Code != codes.Error {
		t.Fatalf("panicking tool span should end with error status: %#v", toolSpan)
	}
}

func spansByName(spans []sdktrace.ReadOnlySpan) map[string]sdktrace.ReadOnlySpan {
	result := make(map[string]sdktrace.ReadOnlySpan, len(spans))
	for _, span := range spans {
		result[span.Name()] = span
	}
	return result
}

func spanAttributes(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	result := make(map[string]attribute.Value, len(span.Attributes()))
	for _, attr := range span.Attributes() {
		result[string(attr.Key)] = attr.Value
	}
	return result
}
