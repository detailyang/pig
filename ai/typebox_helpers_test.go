package ai

import (
	"reflect"
	"testing"
)

func TestTypeboxHelperConstructorsMatchUpstreamShapes(t *testing.T) {
	if got := TypeboxString("path to file"); got["type"] != "string" || got["description"] != "path to file" {
		t.Fatalf("string schema mismatch: %#v", got)
	}
	if got := String("path to file"); got["type"] != "string" || got["description"] != "path to file" {
		t.Fatalf("upstream string schema mismatch: %#v", got)
	}
	if got := TypeboxBoolean("confirm"); got["type"] != "boolean" || got["description"] != "confirm" {
		t.Fatalf("boolean schema mismatch: %#v", got)
	}
	if got := Boolean("confirm"); got["type"] != "boolean" || got["description"] != "confirm" {
		t.Fatalf("upstream boolean schema mismatch: %#v", got)
	}
	if got := TypeboxNumber("count"); got["type"] != "number" || got["description"] != "count" {
		t.Fatalf("number schema mismatch: %#v", got)
	}
	if got := Number("count"); got["type"] != "number" || got["description"] != "count" {
		t.Fatalf("upstream number schema mismatch: %#v", got)
	}

	properties := map[string]any{"path": TypeboxString("path")}
	object := TypeboxObject(properties, []string{"path"})
	if object["type"] != "object" || object["properties"] == nil {
		t.Fatalf("object schema mismatch: %#v", object)
	}
	required, ok := object["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "path" {
		t.Fatalf("required schema mismatch: %#v", object["required"])
	}
	if got := Object(properties, []string{"path"}); !reflect.DeepEqual(got, object) {
		t.Fatalf("upstream object schema mismatch: %#v want %#v", got, object)
	}
}
