package paymos

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCancellationReasonUsesServerUTF16Limit(t *testing.T) {
	for _, resource := range []string{"invoices", "withdrawals"} {
		for _, sample := range []struct {
			name, reason string
			allowed      bool
		}{
			{"cyrillic_boundary", strings.Repeat("я", 500), true},
			{"cyrillic_over", strings.Repeat("я", 501), false},
			{"astral_boundary", strings.Repeat("😀", 250), true},
			{"astral_over", strings.Repeat("😀", 250) + "я", false},
			{"ascii_boundary", strings.Repeat("a", 500), true},
			{"ascii_over", strings.Repeat("a", 501), false},
		} {
			t.Run(resource+"/"+sample.name, func(t *testing.T) {
				calls := 0
				client, err := NewClient("pk_test_key", "sk_test_secret", WithHTTPClient(roundTripper(func(r *http.Request) (*http.Response, error) {
					calls++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["reason"] != sample.reason {
						t.Fatal("reason changed on the wire")
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"cancelled"}`))}, nil
				})))
				if err != nil {
					t.Fatal(err)
				}
				if resource == "invoices" {
					_, err = client.Invoices.Cancel(context.Background(), "inv_fixture", sample.reason)
				} else {
					_, err = client.Withdrawals.Cancel(context.Background(), "wdl_fixture", sample.reason)
				}
				if sample.allowed && (err != nil || calls != 1) {
					t.Fatalf("valid reason rejected: calls=%d err=%v", calls, err)
				}
				if !sample.allowed && (err == nil || calls != 0) {
					t.Fatalf("over-limit reason sent: calls=%d err=%v", calls, err)
				}
			})
		}
	}
}
