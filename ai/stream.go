package ai

import (
	"context"
	"fmt"
)

type StreamFn func(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream

func Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	EnsureBuiltinProviders()
	handle, ok := GetAPIProvider(model.API)
	if !ok {
		return ErrorStream(fmt.Sprintf("No API provider registered for api: %s", model.API))
	}
	return handle.Stream(ctx, model, request, options)
}

func Complete(ctx context.Context, model Model, request Context, options StreamOptions) (AssistantMessage, bool) {
	return Stream(ctx, model, request, options).Result()
}

func StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	EnsureBuiltinProviders()
	handle, ok := GetAPIProvider(model.API)
	if !ok {
		return ErrorStream(fmt.Sprintf("No API provider registered for api: %s", model.API))
	}
	return handle.StreamSimple(ctx, model, request, options)
}

func CompleteSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) (AssistantMessage, bool) {
	return StreamSimple(ctx, model, request, options).Result()
}
