package ai

import (
	"context"
	"strings"
	"testing"
)

func TestUpstreamEnumAliasesAreAvailable(t *testing.T) {
	thinking := []ThinkingLevel{ThinkingLevelMinimal, ThinkingLevelLow, ThinkingLevelMedium, ThinkingLevelHigh, ThinkingLevelXhigh}
	if thinking[0] != ThinkingMinimal || thinking[4] != ThinkingXHigh {
		t.Fatalf("thinking aliases mismatch: %#v", thinking)
	}

	modelThinking := []ModelThinkingLevel{ModelThinkingLevelOff, ModelThinkingLevelMinimal, ModelThinkingLevelLow, ModelThinkingLevelMedium, ModelThinkingLevelHigh, ModelThinkingLevelXhigh}
	if modelThinking[0] != ModelThinkingOff || modelThinking[5] != ModelThinkingXHigh {
		t.Fatalf("model thinking aliases mismatch: %#v", modelThinking)
	}

	if StopReasonStop != StopReasonEndTurn || StopReasonLength != StopReasonMaxTokens || StopReasonToolUse != StopReasonToolCalls {
		t.Fatalf("stop reason aliases mismatch")
	}
	if InputModalityText != InputText || InputModalityImage != InputImage {
		t.Fatalf("input modality aliases mismatch")
	}

	text := UserContentTextValue("hello")
	blocks := UserContentBlocksValue([]UserContentBlock{{Type: UserContentText, Text: "hello"}})
	if text.Text != "hello" || len(blocks.Blocks) != 1 {
		t.Fatalf("user content constructors mismatch: %#v %#v", text, blocks)
	}
}

func TestUpstreamFunctionAliasesAreAvailable(t *testing.T) {
	if value, err := ParsePartialJson(`{"a":1`); err != nil || value == nil {
		t.Fatalf("ParsePartialJson alias mismatch: value=%#v err=%v", value, err)
	}

	events, err := ParseSSE(strings.NewReader("event: done\ndata: ok\n\n"))
	if err != nil || len(events) != 1 || events[0].Event != "done" || events[0].Data != "ok" {
		t.Fatalf("ParseSSE mismatch: %#v err=%v", events, err)
	}
	stream := NewSseStream(strings.NewReader("data: hi\n\n"))
	if event, ok, err := stream.Next(); err != nil || !ok || event.Data != "hi" {
		t.Fatalf("NewSseStream alias mismatch: %#v ok=%v err=%v", event, ok, err)
	}

	sanitized := SanitizeSurrogatesU16([]uint16{'o', 'k'})
	if sanitized != "ok" {
		t.Fatalf("SanitizeSurrogatesU16 mismatch: %q", sanitized)
	}

	block := NewTextContentBlock("hello")
	if block.Type != ContentText || block.Text != "hello" {
		t.Fatalf("NewTextContentBlock mismatch: %#v", block)
	}

	if ApiOpenAIResponses.AsStr() != "openai-responses" || Provider("openai").AsStr() != "openai" || ImagesProvider("openai").AsStr() != "openai" {
		t.Fatalf("as_str aliases mismatch")
	}
	if EventDone.AsStr() != "done" || DoneReasonAbort.AsStr() != "abort" || ErrorReasonAbort.AsStr() != "aborted" {
		t.Fatalf("event/reason as_str aliases mismatch")
	}
	if !(AssistantMessageEvent{Type: EventDone}).IsTerminal() || !(AssistantMessageEvent{Type: EventError}).IsTerminal() || (AssistantMessageEvent{Type: EventTextDelta}).IsTerminal() {
		t.Fatalf("assistant event terminal helper mismatch")
	}
}

func TestUpstreamProviderHelperAliasesAreAvailable(t *testing.T) {
	converter := Converter{}.New()
	if converter == nil {
		t.Fatalf("converter New alias returned nil")
	}
	if _, ok := (BedrockCreds{}).FromEnv(); ok {
		t.Fatalf("bedrock env should be absent in test")
	}
	if _, ok := (VertexCreds{}).FromEnv(); ok {
		t.Fatalf("vertex env should be absent in test")
	}
	message := EventMessage{Headers: map[string]HeaderValue{":message-type": HeaderValueString("event"), ":event-type": HeaderValueString("chunk")}}
	if message.MessageType() != "event" || message.EventType() != "chunk" {
		t.Fatalf("event message method aliases mismatch")
	}
}

func TestUpstreamProviderInterfaceAliasesAreAvailable(t *testing.T) {
	var _ ApiProvider = &countingProvider{api: ApiFaux}
	var _ ImagesApiProvider = imagesAPIFakeProvider{output: "ok"}
}

func TestUpstreamStreamFnAliasIsAvailable(t *testing.T) {
	var streamFn StreamFn = Stream
	stream := streamFn(context.Background(), Model{ID: "missing", API: Api("missing-api")}, Context{}, StreamOptions{})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.Contains(message.ErrorMessage, "No API provider registered") {
		t.Fatalf("stream fn alias mismatch message=%#v ok=%v", message, ok)
	}
}

func TestUpstreamBuiltinAndCloudflareAliasesAreAvailable(t *testing.T) {
	if string(BUILTINMODELS) != string(BUILTIN_MODELS) {
		t.Fatalf("builtin models alias mismatch")
	}
	if len(BUILTINIMAGEMODELS) != len(BUILTIN_IMAGE_MODELS) {
		t.Fatalf("builtin image models alias mismatch")
	}
	if CLOUDFLAREWORKERSAIBASEURL != CLOUDFLARE_WORKERS_AI_BASE_URL {
		t.Fatalf("workers ai base url alias mismatch")
	}
	if CLOUDFLAREAIGATEWAYCOMPATBASEURL != CLOUDFLARE_AI_GATEWAY_COMPAT_BASE_URL {
		t.Fatalf("gateway compat base url alias mismatch")
	}
	if CLOUDFLAREAIGATEWAYOPENAIBASEURL != CLOUDFLARE_AI_GATEWAY_OPENAI_BASE_URL {
		t.Fatalf("gateway openai base url alias mismatch")
	}
	if CLOUDFLAREAIGATEWAYANTHROPICBASEURL != CLOUDFLARE_AI_GATEWAY_ANTHROPIC_BASE_URL {
		t.Fatalf("gateway anthropic base url alias mismatch")
	}
}
