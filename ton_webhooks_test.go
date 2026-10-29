package paymos

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestSignedTonWebhookRawAndFriendlyV1PreserveFields(t *testing.T) {
	raw, err := os.ReadFile("conformance/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Vectors struct {
			TonWebhooks struct {
				Secret  string `json:"secret"`
				Now     int64  `json:"now"`
				Vectors []struct {
					Name      string `json:"name"`
					EventID   string `json:"event_id"`
					EventType string `json:"event_type"`
					Version   int    `json:"version"`
					Header    string `json:"header"`
					RawBody   string `json:"raw_body"`
					Expect    []struct {
						Path  []string `json:"path"`
						Value any      `json:"value"`
					} `json:"expect"`
				} `json:"vectors"`
			} `json:"ton_webhooks"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	fixtures := contract.Vectors.TonWebhooks
	if len(fixtures.Vectors) != 8 {
		t.Fatalf("expected 8 signed raw and friendly schema 1 vectors, got %d", len(fixtures.Vectors))
	}
	verifier, err := NewWebhookVerifier(fixtures.Secret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, vector := range fixtures.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			if vector.Version != 1 {
				t.Fatalf("unexpected payload version: %d", vector.Version)
			}
			var event WebhookEvent[map[string]any]
			now := time.Unix(fixtures.Now, 0)
			if err := verifier.ConstructEvent(vector.Header, []byte(vector.RawBody), now, &event); err != nil {
				t.Fatal(err)
			}
			if event.EventID != vector.EventID || event.EventType != vector.EventType ||
				event.Version != vector.Version || event.OccurredAt != fixtures.Now {
				t.Fatalf("unexpected decoded envelope: %+v", event)
			}
			for _, expected := range vector.Expect {
				var value any = event.Data
				for _, key := range expected.Path {
					object, ok := value.(map[string]any)
					if !ok {
						t.Fatalf("missing object along path %v", expected.Path)
					}
					value, ok = object[key]
					if !ok {
						t.Fatalf("missing field along path %v", expected.Path)
					}
				}
				if value != expected.Value {
					t.Fatalf("field %v: got %v, want %v", expected.Path, value, expected.Value)
				}
			}
			if err := verifier.Verify(vector.Header, []byte(vector.RawBody+" "), now); err == nil {
				t.Fatal("JSON-valid body change must invalidate the signature")
			}
		})
	}
}
