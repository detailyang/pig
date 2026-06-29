package ai

import "net/http"

func UserAgent() string {
	return "pie-ai-rs/0.75.0"
}

func MergeHeaders(base map[string]string, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overrides))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overrides {
		out[key] = value
	}
	return out
}

func applyExtraHeaders(request *http.Request, headers map[string]string) {
	for key, value := range headers {
		request.Header.Set(key, value)
	}
}

func applyModelAndOptionHeaders(request *http.Request, model Model, options StreamOptions) {
	applyExtraHeaders(request, model.Headers)
	applyExtraHeaders(request, options.Headers)
}

func copyAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]any, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}
