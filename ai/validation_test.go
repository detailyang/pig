package ai

import "testing"

func TestValidateAcceptsAnyInputLikeUpstreamStub(t *testing.T) {
	result := Validate(map[string]any{"count": 1}, map[string]any{"type": "object", "required": []string{"missing"}})
	if !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("validation should accept everything for upstream stub parity: %#v", result)
	}
}

func TestValidationErrorShape(t *testing.T) {
	errorValue := ValidationError{Path: "/field", Message: "bad value"}
	if errorValue.Path != "/field" || errorValue.Message != "bad value" {
		t.Fatalf("validation error shape mismatch: %#v", errorValue)
	}
}
