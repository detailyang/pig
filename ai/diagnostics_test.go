package ai

import (
	"encoding/json"
	"testing"
)

func TestAssistantMessageDiagnosticJSONMatchesUpstream(t *testing.T) {
	diagnostic := AssistantMessageDiagnostic{Kind: "retry", Message: "succeeded"}
	data, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"kind":"retry","message":"succeeded"}` {
		t.Fatalf("diagnostic without data should omit data like upstream, got %s", data)
	}

	diagnostic.Data = map[string]any{"attempt": float64(2)}
	data, err = json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["kind"] != "retry" || object["message"] != "succeeded" || object["data"].(map[string]any)["attempt"] != float64(2) {
		t.Fatalf("diagnostic data mismatch: %#v", object)
	}
}
