package idempotency

import (
	"testing"
)

func TestCanonicalJSONSortsNestedObjectKeys(t *testing.T) {
	value := map[string]any{
		"z":     1,
		"a":     map[string]any{"second": true, "first": "value"},
		"items": []any{map[string]any{"b": 2, "a": 1}},
	}
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"first":"value","second":true},"items":[{"a":1,"b":2}],"z":1}`
	if string(encoded) != want {
		t.Fatalf("unexpected canonical JSON:\nwant %s\n got %s", want, encoded)
	}
}
