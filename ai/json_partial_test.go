package ai

import (
	"encoding/json"
	"testing"
)

func TestParsePartialJSONMatchesUpstreamCases(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want any
	}{
		{name: "empty", raw: ``, want: nil},
		{name: "object", raw: `{"a": 1`, want: json.Number("1")},
		{name: "string", raw: `{"a": "hello`, want: "hello"},
		{name: "trailing comma", raw: `{"a": 1,`, want: json.Number("1")},
		{name: "array", raw: `[{"a": 1}`, want: []any{map[string]any{"a": json.Number("1")}}},
	}
	for _, tc := range cases {
		value, err := ParsePartialJSON(tc.raw)
		if err != nil {
			t.Fatalf("%s parse failed: %v", tc.name, err)
		}
		if tc.name == "array" {
			array, ok := value.([]any)
			if !ok || len(array) != 1 || array[0].(map[string]any)["a"] != json.Number("1") {
				t.Fatalf("array mismatch: %#v", value)
			}
			continue
		}
		if tc.name == "empty" {
			if value != nil {
				t.Fatalf("empty should parse to nil, got %#v", value)
			}
			continue
		}
		object := value.(map[string]any)
		if object["a"] != tc.want {
			t.Fatalf("%s mismatch: %#v", tc.name, object)
		}
	}
}

func TestParsePartialJSONEmptyMarshalsAsJSONNullLikeUpstream(t *testing.T) {
	value, err := ParsePartialJSON("")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "null" {
		t.Fatalf("empty partial JSON should parse to JSON null like upstream, got %s", data)
	}
}

func TestParsePartialJSONPreservesLargeNumbersLikeSerdeValue(t *testing.T) {
	value, err := ParsePartialJSON(`{"id": 9007199254740993`)
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]any)
	if got := object["id"]; got != json.Number("9007199254740993") {
		t.Fatalf("large JSON number should be preserved like serde_json::Value, got %#v", got)
	}
}

func TestParsePartialJSONRejectsTrailingTokensLikeSerdeJSON(t *testing.T) {
	if value, err := ParsePartialJSON(`{"a": 1} {"b": 2}`); err == nil {
		t.Fatalf("expected trailing token parse error, got %#v", value)
	}
}

func TestParsePartialJSONObjectClosesMissingBrace(t *testing.T) {
	args, ok := parsePartialJSONObject(`{"path":"README.md"`)
	if !ok || args["path"] != "README.md" {
		t.Fatalf("args mismatch: %#v ok=%v", args, ok)
	}
}

func TestParsePartialJSONObjectRejectsInvalidObject(t *testing.T) {
	if args, ok := parsePartialJSONObject(`{"path":`); ok || args != nil {
		t.Fatalf("expected invalid partial object, got %#v ok=%v", args, ok)
	}
}
