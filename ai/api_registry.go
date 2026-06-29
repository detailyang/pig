package ai

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type APIProvider interface {
	API() Api
	Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream
	StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream
}

type ApiProvider = APIProvider

type registeredProvider struct {
	provider    APIProvider
	sourceID    string
	hasSourceID bool
}

var apiRegistry = struct {
	sync.RWMutex
	entries map[Api]registeredProvider
}{entries: map[Api]registeredProvider{}}

func RegisterAPIProvider(provider APIProvider, sourceID string) {
	apiRegistry.Lock()
	defer apiRegistry.Unlock()
	apiRegistry.entries[provider.API()] = registeredProvider{provider: provider, sourceID: sourceID, hasSourceID: sourceID != ""}
}

func RegisterApiProvider(provider APIProvider, sourceID string) {
	RegisterAPIProvider(provider, sourceID)
}

func GetAPIProvider(api Api) (RegisteredHandle, bool) {
	apiRegistry.RLock()
	defer apiRegistry.RUnlock()
	entry, ok := apiRegistry.entries[api]
	if !ok {
		return RegisteredHandle{}, false
	}
	return RegisteredHandle{provider: entry.provider}, true
}

func GetApiProvider(api Api) (RegisteredHandle, bool) { return GetAPIProvider(api) }

func UnregisterAPIProviders(sourceID string) {
	apiRegistry.Lock()
	defer apiRegistry.Unlock()
	for api, entry := range apiRegistry.entries {
		if entry.hasSourceID && entry.sourceID == sourceID {
			delete(apiRegistry.entries, api)
		}
	}
}

func UnregisterApiProviders(sourceID string) { UnregisterAPIProviders(sourceID) }

func ClearAPIProviders() {
	apiRegistry.Lock()
	defer apiRegistry.Unlock()
	apiRegistry.entries = map[Api]registeredProvider{}
}

func ClearApiProviders() { ClearAPIProviders() }

func ListAPIIDs() []string {
	apiRegistry.RLock()
	defer apiRegistry.RUnlock()
	ids := make([]string, 0, len(apiRegistry.entries))
	for api := range apiRegistry.entries {
		ids = append(ids, string(api))
	}
	sort.Strings(ids)
	return ids
}

func ListApiIds() []string { return ListAPIIDs() }

type RegisteredHandle struct {
	provider APIProvider
}

func (handle RegisteredHandle) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	if model.API != handle.provider.API() {
		return ErrorStream(fmt.Sprintf("Mismatched api: %s expected %s", model.API, handle.provider.API()))
	}
	return handle.provider.Stream(ctx, model, request, options)
}

func (handle RegisteredHandle) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	if model.API != handle.provider.API() {
		return ErrorStream(fmt.Sprintf("Mismatched api: %s expected %s", model.API, handle.provider.API()))
	}
	return handle.provider.StreamSimple(ctx, model, request, options)
}

func ErrorStream(message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	errorMessage := AssistantMessage{Role: AssistantRoleAssistant, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &errorMessage})
	return stream
}
