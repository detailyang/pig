package ai

type AssistantMessageDiagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
