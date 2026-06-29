package ai

import (
	"sort"
	"sync"
)

var modelRegistry = struct {
	sync.RWMutex
	builtins           map[string]Model
	builtinOrder       []string
	custom             map[string]Model
	customOrder        []string
	builtinsLoaded     bool
	disableBuiltinLoad bool
}{builtins: map[string]Model{}, custom: map[string]Model{}}

func modelKey(provider Provider, id string) string {
	return string(provider) + "/" + id
}

func RegisterBuiltinModel(model Model) {
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	modelRegistry.builtinsLoaded = true
	key := modelKey(model.Provider, model.ID)
	if _, ok := modelRegistry.builtins[key]; !ok {
		modelRegistry.builtinOrder = append(modelRegistry.builtinOrder, key)
	}
	modelRegistry.builtins[key] = model
}

func RegisterCustomModel(model Model) {
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	key := modelKey(model.Provider, model.ID)
	if _, ok := modelRegistry.custom[key]; !ok {
		modelRegistry.customOrder = append(modelRegistry.customOrder, key)
	}
	modelRegistry.custom[key] = model
}

func UnregisterCustomModel(provider Provider, id string) {
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	key := modelKey(provider, id)
	delete(modelRegistry.custom, key)
	modelRegistry.customOrder = removeModelKey(modelRegistry.customOrder, key)
}

func ClearCustomModels() {
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	modelRegistry.custom = map[string]Model{}
	modelRegistry.customOrder = nil
}

func ClearBuiltinModels() {
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	modelRegistry.builtins = map[string]Model{}
	modelRegistry.builtinOrder = nil
	modelRegistry.builtinsLoaded = true
	modelRegistry.disableBuiltinLoad = true
}

func GetModel(provider Provider, id string) (Model, bool) {
	ensureBuiltinModelsLoaded()
	modelRegistry.RLock()
	defer modelRegistry.RUnlock()
	if model, ok := modelRegistry.custom[modelKey(provider, id)]; ok {
		return model, true
	}
	model, ok := modelRegistry.builtins[modelKey(provider, id)]
	return model, ok
}

func ListModels() []Model {
	ensureBuiltinModelsLoaded()
	modelRegistry.RLock()
	defer modelRegistry.RUnlock()
	models := make([]Model, 0, len(modelRegistry.builtins)+len(modelRegistry.custom))
	for _, key := range modelRegistry.builtinOrder {
		if model, ok := modelRegistry.builtins[key]; ok {
			models = append(models, model)
		}
	}
	for _, key := range modelRegistry.customOrder {
		if model, ok := modelRegistry.custom[key]; ok {
			models = append(models, model)
		}
	}
	return models
}

func removeModelKey(keys []string, key string) []string {
	for index, candidate := range keys {
		if candidate == key {
			return append(keys[:index], keys[index+1:]...)
		}
	}
	return keys
}

func ListAPIs() []Api {
	ensureBuiltinModelsLoaded()
	modelRegistry.RLock()
	defer modelRegistry.RUnlock()
	seen := map[Api]bool{}
	for _, model := range modelRegistry.builtins {
		seen[model.API] = true
	}
	apis := make([]Api, 0, len(seen))
	for api := range seen {
		apis = append(apis, api)
	}
	sort.Slice(apis, func(i, j int) bool { return apis[i] < apis[j] })
	return apis
}

func ListApis() []Api { return ListAPIs() }

func ensureBuiltinModelsLoaded() {
	modelRegistry.RLock()
	loaded := modelRegistry.builtinsLoaded || modelRegistry.disableBuiltinLoad
	modelRegistry.RUnlock()
	if loaded {
		return
	}
	models, err := ParseVendoredGeneratedModelCatalog()
	if err != nil {
		return
	}
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	if modelRegistry.builtinsLoaded || modelRegistry.disableBuiltinLoad {
		return
	}
	for _, model := range models {
		key := modelKey(model.Provider, model.ID)
		if _, ok := modelRegistry.builtins[key]; !ok {
			modelRegistry.builtinOrder = append(modelRegistry.builtinOrder, key)
		}
		modelRegistry.builtins[key] = model
	}
	modelRegistry.builtinsLoaded = true
}
