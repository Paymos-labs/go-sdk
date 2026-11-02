package paymos_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	paymos "github.com/Paymos-labs/go-sdk/v2"
)

type globalTransportProbe struct{ calls int }

func (p *globalTransportProbe) RoundTrip(req *http.Request) (*http.Response, error) {
	p.calls++
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
}

func TestRefundGlobalDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()

	for _, when := range []string{"before_client", "after_client"} {
		t.Run(when, func(t *testing.T) {
			probe := &globalTransportProbe{}
			http.DefaultTransport = original
			if when == "before_client" {
				http.DefaultTransport = probe
			}
			client, err := paymos.NewClient("test-key", "test-secret", paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithRetry(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if when == "after_client" {
				http.DefaultTransport = probe
			}
			_, err = client.Refunds.Create(context.Background(), "inv_probe", paymos.RefundFull, "order-probe")
			if probe.calls != 0 || err == nil {
				t.Errorf("refund used unverified global transport: calls=%d err=%v", probe.calls, err)
			}
		})
	}

	t.Run("ordinary_request_still_uses_global_transport", func(t *testing.T) {
		probe := &globalTransportProbe{}
		http.DefaultTransport = probe
		client, err := paymos.NewClient("test-key", "test-secret", paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithRetry(0, 0))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Invoices.Get(context.Background(), "inv_probe")
		if err != nil || probe.calls != 1 {
			t.Errorf("ordinary request changed: calls=%d err=%v", probe.calls, err)
		}
	})
}
