package ai

import (
	"context"
	"reflect"
	"testing"
)

type countingProvider struct {
	api         Api
	streamCalls int
	simpleCalls int
}

func (provider *countingProvider) API() Api { return provider.api }
func (provider *countingProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	provider.streamCalls++
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "stream:" + model.ID})
	stream.Close(DoneReasonStop)
	return stream
}
func (provider *countingProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	provider.simpleCalls++
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "simple:" + model.ID})
	stream.Close(DoneReasonStop)
	return stream
}

func TestAPIRegistryRegisterLookupAndUnregisterBySource(t *testing.T) {
	ClearAPIProviders()
	provider := &countingProvider{api: Api("race-api")}
	RegisterAPIProvider(provider, "test-source")

	ids := ListAPIIDs()
	if !reflect.DeepEqual(ids, []string{"race-api"}) {
		t.Fatalf("api ids mismatch: %#v", ids)
	}

	handle, ok := GetAPIProvider(Api("race-api"))
	if !ok {
		t.Fatal("expected provider handle")
	}
	message, ok := handle.Stream(context.Background(), Model{ID: "m", API: Api("race-api")}, Context{}, StreamOptions{}).Result()
	if !ok || message.Text() != "stream:m" || provider.streamCalls != 1 {
		t.Fatalf("stream mismatch message=%#v ok=%v calls=%d", message, ok, provider.streamCalls)
	}

	UnregisterAPIProviders("test-source")
	if _, ok := GetAPIProvider(Api("race-api")); ok {
		t.Fatal("provider should have been unregistered")
	}
}

func TestAPIRegistryUpstreamNameAliases(t *testing.T) {
	ClearAPIProviders()
	provider := &countingProvider{api: Api("alias-api")}
	RegisterApiProvider(provider, "alias-source")

	if ids := ListApiIds(); !reflect.DeepEqual(ids, []string{"alias-api"}) {
		t.Fatalf("api ids mismatch: %#v", ids)
	}

	handle, ok := GetApiProvider(Api("alias-api"))
	if !ok {
		t.Fatal("expected provider handle")
	}
	message, ok := handle.Stream(context.Background(), Model{ID: "m", API: Api("alias-api")}, Context{}, StreamOptions{}).Result()
	if !ok || message.Text() != "stream:m" || provider.streamCalls != 1 {
		t.Fatalf("stream mismatch message=%#v ok=%v calls=%d", message, ok, provider.streamCalls)
	}

	UnregisterApiProviders("alias-source")
	if _, ok := GetApiProvider(Api("alias-api")); ok {
		t.Fatal("provider should have been unregistered")
	}

	RegisterApiProvider(provider, "alias-source")
	ClearApiProviders()
	if ids := ListApiIds(); len(ids) != 0 {
		t.Fatalf("api ids should be empty after clear: %#v", ids)
	}
}

func TestAPIHandleSurvivesUnregisterAfterLookup(t *testing.T) {
	ClearAPIProviders()
	provider := &countingProvider{api: Api("race-api")}
	RegisterAPIProvider(provider, "race-source")
	handle, ok := GetAPIProvider(Api("race-api"))
	if !ok {
		t.Fatal("expected provider handle")
	}

	UnregisterAPIProviders("race-source")
	if _, ok := GetAPIProvider(Api("race-api")); ok {
		t.Fatal("provider should have been unregistered")
	}

	message, ok := handle.Stream(context.Background(), Model{ID: "m", API: Api("race-api")}, Context{}, StreamOptions{}).Result()
	if !ok || message.Text() != "stream:m" || provider.streamCalls != 1 {
		t.Fatalf("captured stream mismatch message=%#v ok=%v calls=%d", message, ok, provider.streamCalls)
	}
}

func TestAPIHandleSurvivesClearAfterLookupForSimpleStream(t *testing.T) {
	ClearAPIProviders()
	provider := &countingProvider{api: Api("race-api")}
	RegisterAPIProvider(provider, "")
	handle, ok := GetAPIProvider(Api("race-api"))
	if !ok {
		t.Fatal("expected provider handle")
	}

	ClearAPIProviders()
	if _, ok := GetAPIProvider(Api("race-api")); ok {
		t.Fatal("provider should have been cleared")
	}

	message, ok := handle.StreamSimple(context.Background(), Model{ID: "m", API: Api("race-api")}, Context{}, SimpleStreamOptions{}).Result()
	if !ok || message.Text() != "simple:m" || provider.simpleCalls != 1 {
		t.Fatalf("captured simple stream mismatch message=%#v ok=%v calls=%d", message, ok, provider.simpleCalls)
	}
}

func TestAPIRegistryUnregisterEmptySourceDoesNotRemoveUnscopedProviderLikeUpstream(t *testing.T) {
	ClearAPIProviders()
	RegisterAPIProvider(&countingProvider{api: Api("unscoped")}, "")

	UnregisterAPIProviders("")

	if _, ok := GetAPIProvider(Api("unscoped")); !ok {
		t.Fatal("unscoped provider should remain registered")
	}
}

func TestAPIHandleMismatchReturnsErrorStream(t *testing.T) {
	ClearAPIProviders()
	RegisterAPIProvider(&countingProvider{api: Api("expected")}, "test-source")
	handle, ok := GetAPIProvider(Api("expected"))
	if !ok {
		t.Fatal("expected provider handle")
	}

	message, ok := handle.Stream(context.Background(), Model{ID: "m", API: Api("wrong")}, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed error message")
	}
	if message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("expected error result, got %#v", message)
	}
	if message.ErrorMessage != "Mismatched api: wrong expected expected" {
		t.Fatalf("mismatch error should match upstream, got %q", message.ErrorMessage)
	}
	events := handle.Stream(context.Background(), Model{ID: "m", API: Api("wrong")}, Context{}, StreamOptions{}).Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Message == nil || events[0].Message.ErrorMessage == "" {
		t.Fatalf("error stream should contain single upstream-style error event, got %#v", events)
	}
}

func TestStreamAndCompleteUseRegisteredProvider(t *testing.T) {
	ClearAPIProviders()
	RegisterAPIProvider(&countingProvider{api: Api("fake")}, "test-source")
	model := Model{ID: "m", Provider: Provider("test"), API: Api("fake")}

	message, ok := Complete(context.Background(), model, Context{}, StreamOptions{})
	if !ok || message.Text() != "stream:m" {
		t.Fatalf("complete mismatch: %#v ok=%v", message, ok)
	}

	message, ok = CompleteSimple(context.Background(), model, Context{}, SimpleStreamOptions{})
	if !ok || message.Text() != "simple:m" {
		t.Fatalf("complete simple mismatch: %#v ok=%v", message, ok)
	}
}

func TestStreamWithoutProviderReturnsErrorStream(t *testing.T) {
	ClearAPIProviders()
	message, ok := Complete(context.Background(), Model{ID: "m", API: Api("missing")}, Context{}, StreamOptions{})
	if !ok {
		t.Fatal("expected completed error message")
	}
	if message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("expected missing-provider error, got %#v", message)
	}
	if message.ErrorMessage != "No API provider registered for api: missing" {
		t.Fatalf("missing provider error should match upstream, got %q", message.ErrorMessage)
	}
}

func TestStreamAutoRegistersBuiltinProviders(t *testing.T) {
	resetBuiltinProvidersEnsureForTest()
	ClearAPIProviders()
	ClearFauxResponses()
	t.Cleanup(func() { ClearAPIProviders(); ClearFauxResponses() })
	SetFauxResponses([]AssistantMessage{FauxAssistantMessage([]ContentBlock{FauxText("auto")})})

	message, ok := Complete(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{})
	if !ok || message.Text() != "auto" {
		t.Fatalf("auto-registered stream mismatch: %#v ok=%v", message, ok)
	}
}

func TestStreamAutoRegisteredBuiltinOverwritesExistingProviderLikeUpstream(t *testing.T) {
	resetBuiltinProvidersEnsureForTest()
	ClearAPIProviders()
	ClearFauxResponses()
	t.Cleanup(func() { ClearAPIProviders(); ClearFauxResponses() })
	RegisterAPIProvider(&countingProvider{api: ApiFaux}, "test-source")
	SetFauxResponses([]AssistantMessage{FauxAssistantMessage([]ContentBlock{FauxText("builtin")})})

	message, ok := Complete(context.Background(), Model{ID: "custom", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{})
	if !ok || message.Text() != "builtin" {
		t.Fatalf("builtin provider should overwrite existing provider like upstream: %#v ok=%v", message, ok)
	}
}
