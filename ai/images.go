package ai

import (
	"context"
	"fmt"
	"sync"
)

var BUILTIN_IMAGE_MODELS = []ImagesModel{}

var BUILTINIMAGEMODELS = BUILTIN_IMAGE_MODELS

func GetImageModel(provider ImagesProvider, id string) (ImagesModel, bool) {
	for _, model := range BUILTIN_IMAGE_MODELS {
		if model.Provider == provider && model.ID == id {
			return model, true
		}
	}
	return ImagesModel{}, false
}

func ListImageModels() []ImagesModel {
	return append([]ImagesModel{}, BUILTIN_IMAGE_MODELS...)
}

type ImagesAPIProvider interface {
	Generate(ctx context.Context, model ImagesModel, request ImagesContext) (AssistantImages, error)
}

type ImagesApiProvider = ImagesAPIProvider

type ImagesEntry struct{}

func (entry ImagesEntry) Generate(ctx context.Context, model ImagesModel, request ImagesContext) (AssistantImages, error) {
	_ = entry
	_ = ctx
	_ = model
	_ = request
	return AssistantImages{}, fmt.Errorf("openrouter-images not yet implemented")
}

func EnsureImagesBuiltinProviders() {}

var imagesAPIRegistry sync.Map

func RegisterImagesAPIProvider(api ImagesApi, provider ImagesAPIProvider) {
	if provider == nil {
		imagesAPIRegistry.Delete(api)
		return
	}
	imagesAPIRegistry.Store(api, provider)
}

func RegisterImagesApiProvider(api ImagesApi, provider ImagesAPIProvider) {
	RegisterImagesAPIProvider(api, provider)
}

func GetImagesAPIProvider(api ImagesApi) (*ImagesEntryHandle, bool) {
	provider, ok := imagesAPIRegistry.Load(api)
	if !ok {
		return nil, false
	}
	return &ImagesEntryHandle{provider: provider.(ImagesAPIProvider)}, true
}

func GetImagesApiProvider(api ImagesApi) (*ImagesEntryHandle, bool) { return GetImagesAPIProvider(api) }

type ImagesEntryHandle struct {
	provider ImagesAPIProvider
}

func (handle ImagesEntryHandle) Generate(ctx context.Context, model ImagesModel, request ImagesContext) (AssistantImages, error) {
	if handle.provider != nil {
		return handle.provider.Generate(ctx, model, request)
	}
	return AssistantImages{}, fmt.Errorf("images registry handle is a stub")
}

func Images(ctx context.Context, model ImagesModel, request ImagesContext) (AssistantImages, error) {
	handle, ok := GetImagesAPIProvider(model.API)
	if !ok {
		return AssistantImages{}, fmt.Errorf("No images API registered for: %s", model.API)
	}
	return handle.Generate(ctx, model, request)
}
