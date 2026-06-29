package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltInImageModelsAreEmpty(t *testing.T) {
	if BUILTIN_IMAGE_MODELS == nil {
		t.Fatal("builtin image models alias should be an initialized empty slice")
	}
	old := BUILTIN_IMAGE_MODELS
	t.Cleanup(func() { BUILTIN_IMAGE_MODELS = old })
	BUILTIN_IMAGE_MODELS = []ImagesModel{{ID: "img-1", API: ImagesApi("openrouter-images"), Provider: ImagesProvider("openrouter")}}
	found, ok := GetImageModel(ImagesProvider("openrouter"), "img-1")
	if !ok || found.ID != "img-1" || found.Provider != ImagesProvider("openrouter") {
		t.Fatalf("expected GetImageModel to scan builtin image models like upstream, got %#v ok=%v", found, ok)
	}
	BUILTIN_IMAGE_MODELS = []ImagesModel{}

	models := ListImageModels()
	if len(models) != 0 {
		t.Fatalf("expected no builtin image models, got %#v", models)
	}
	if _, ok := GetImageModel(ImagesProvider("openrouter"), "test"); ok {
		t.Fatal("unexpected image model")
	}
	data, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[]" {
		t.Fatalf("empty builtin image models should marshal like upstream Vec::new(), got %s", data)
	}
}

func TestImagesErrorsWhenAPIProviderMissing(t *testing.T) {
	_, err := Images(context.Background(), ImagesModel{ID: "test", API: ImagesApi("openrouter"), Provider: ImagesProvider("openrouter")}, ImagesContext{})
	if err == nil || !strings.Contains(err.Error(), "No images API registered for: openrouter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImagesAPIRegistryStubMatchesUpstream(t *testing.T) {
	EnsureImagesBuiltinProviders()
	if handle, ok := GetImagesAPIProvider(ImagesApi("openrouter")); ok || handle != nil {
		t.Fatalf("expected no images API provider, got handle=%#v ok=%v", handle, ok)
	}

	entry := ImagesEntry{}
	_, err := entry.Generate(context.Background(), ImagesModel{ID: "test", API: ImagesApi("openrouter-images"), Provider: ImagesProvider("openrouter")}, ImagesContext{})
	if err == nil || err.Error() != "openrouter-images not yet implemented" {
		t.Fatalf("unexpected openrouter stub error: %v", err)
	}

	handle := ImagesEntryHandle{}
	_, err = handle.Generate(context.Background(), ImagesModel{ID: "test", API: ImagesApi("openrouter"), Provider: ImagesProvider("openrouter")}, ImagesContext{})
	if err == nil || err.Error() != "images registry handle is a stub" {
		t.Fatalf("unexpected stub error: %v", err)
	}
}

func TestRegisterImagesAPIProviderMakesImagesUseProvider(t *testing.T) {
	api := ImagesApi("test-images")
	RegisterImagesAPIProvider(api, imagesAPIFakeProvider{output: "generated"})
	RegisterImagesApiProvider(api, imagesAPIFakeProvider{output: "generated"})

	handle, ok := GetImagesAPIProvider(api)
	if !ok || handle == nil {
		t.Fatalf("expected registered images provider, got handle=%#v ok=%v", handle, ok)
	}
	aliasHandle, aliasOK := GetImagesApiProvider(api)
	if !aliasOK || aliasHandle == nil {
		t.Fatalf("expected registered images provider via alias, got handle=%#v ok=%v", aliasHandle, aliasOK)
	}

	images, err := Images(context.Background(), ImagesModel{ID: "img-test", API: api, Provider: ImagesProvider("test")}, ImagesContext{Input: []UserContentBlock{{Type: UserContentText, Text: "draw"}}})
	if err != nil {
		t.Fatal(err)
	}
	if images.API != api || images.Model != "img-test" || images.Output[0].Text != "generated" {
		t.Fatalf("unexpected generated images: %#v", images)
	}
}

type imagesAPIFakeProvider struct {
	output string
}

func (provider imagesAPIFakeProvider) Generate(ctx context.Context, model ImagesModel, request ImagesContext) (AssistantImages, error) {
	return AssistantImages{API: model.API, Provider: model.Provider, Model: model.ID, Output: []ContentBlock{{Type: ContentText, Text: provider.output}}}, nil
}
