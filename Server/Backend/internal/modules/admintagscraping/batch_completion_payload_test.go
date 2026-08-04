package admintagscraping

import (
	"encoding/json"
	"testing"
)

func TestBatchCompletionPayloadUsesSQLColumnNames(t *testing.T) {
	tests := []struct {
		name    string
		payload any
	}{
		{
			name: "tag scraping",
			payload: batchCompletionPayload{
				ItemID: "item", AttemptID: "attempt", Status: ItemSucceeded, Message: "done",
			},
		},
		{
			name: "artist artwork",
			payload: artistArtworkBatchCompletionPayload{
				ItemID: "item", AttemptID: "attempt", Status: ItemSucceeded,
				Candidate: json.RawMessage(`{}`), Message: "done",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"item_id", "attempt_id"} {
				if _, ok := fields[key]; !ok {
					t.Fatalf("payload %s is missing %q: %s", test.name, key, encoded)
				}
			}
			for _, key := range []string{"itemId", "attemptId"} {
				if _, ok := fields[key]; ok {
					t.Fatalf("payload %s contains camel-case key %q: %s", test.name, key, encoded)
				}
			}
		})
	}
}
