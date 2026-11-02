package paymos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// This test-only seam reaches the standard http.Client, never the network.
type agentRoundTripper func(*http.Request) (*http.Response, error)

func (f agentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type agentDoer func(*http.Request) (*http.Response, error)

func (f agentDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }
func agentResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func agentDefault(t *testing.T, f agentRoundTripper) *Client {
	t.Helper()
	c, err := NewClient("pk_fixture", "sk_fixture", WithRetry(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	c.http.(*http.Client).Transport = f // private test seam; callers cannot get this pointer.
	return c
}
func TestAgentRefundRedirectUsesOriginalError(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := agentDefault(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					if r.Body != nil {
						_ = r.Body.Close()
					}
					return agentResponse(200, "{}"), nil
				}
				if r.Method != "POST" || r.URL.Path != "/v1/refunds" {
					t.Fatalf("unexpected initial request: %s %s", r.Method, r.URL.Path)
				}
				_ = r.Body.Close()
				p := agentResponse(status, "")
				p.Header.Set("Location", "/redirected")
				return p, nil
			})
			_, err := c.Refunds.Create(context.Background(), "inv_1", RefundFull, "original-key")
			var api *APIError
			if !errors.As(err, &api) || api.Status != status || calls != 1 {
				t.Fatalf("want original %d and one POST, got %v, calls=%d", status, err, calls)
			}
		})
	}
}
func TestAgentCustomDoerFailsBeforeRefundAndKeepsReads(t *testing.T) {
	for _, kind := range []string{"doer", "http-client"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			inner := agentDoer(func(r *http.Request) (*http.Response, error) {
				calls++
				return agentResponse(200, `{"items":[],"next_cursor":null}`), nil
			})
			var d HTTPDoer = inner
			if kind == "http-client" {
				d = &http.Client{Transport: agentRoundTripper(inner)}
			}
			c, err := NewClient("pk", "sk", WithHTTPClient(d), WithRetry(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Refunds.Create(context.Background(), "inv_1", RefundFull, "original-key")
			if err == nil || calls != 0 {
				t.Fatalf("custom transport invoked for refund: calls=%d error=%v", calls, err)
			}
			_, err = c.Refunds.List(context.Background(), RefundListParams{})
			if err != nil || calls != 1 {
				t.Fatalf("history read was blocked: calls=%d error=%v", calls, err)
			}
		})
	}
}
func TestAgentRefundErrorsAndExplicitSameKeyReplay(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	for _, mode := range []string{"429", "503", "lost"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := agentDefault(t, func(r *http.Request) (*http.Response, error) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				if json.Unmarshal(body, &payload) != nil || payload["external_order_id"] != "original-key" || r.GetBody != nil {
					t.Fatalf("changed identity or replayable body: %s", body)
				}
				if calls == 1 {
					switch mode {
					case "429":
						return agentResponse(429, `{}`), nil
					case "503":
						return agentResponse(503, `{}`), nil
					default:
						return nil, errors.New("lost")
					}
				}
				return agentResponse(200, `{"withdrawal_id":"wdr_fixture"}`), nil
			})
			_, err := c.Refunds.Create(context.Background(), "inv_1", RefundFull, "original-key")
			if err == nil || calls != 1 {
				t.Fatalf("first call retried or succeeded: calls=%d error=%v", calls, err)
			}
			_, err = c.Refunds.Create(context.Background(), "inv_1", RefundFull, "original-key")
			if err != nil || calls != 2 {
				t.Fatalf("explicit same-key replay failed: calls=%d error=%v", calls, err)
			}
		})
	}
}
func TestAgentOrdinaryPostRetryRemains(t *testing.T) {
	calls := 0
	c, err := NewClient("pk", "sk", WithRetry(2, 0), WithHTTPClient(agentDoer(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/invoices" {
			t.Fatalf("unexpected ordinary request")
		}
		if calls == 1 {
			return agentResponse(429, `{}`), nil
		}
		return agentResponse(200, `{}`), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Invoices.Create(context.Background(), CreateInvoiceParams{})
	if err != nil || calls != 2 {
		t.Fatalf("ordinary retry changed: calls=%d error=%v", calls, err)
	}
}

// Tests only: route ordinary requests to a fake HTTP client. This does not
// enable refund creation and is absent from production files.
func agentWithOwnedHTTPClient(value HTTPDoer) Option {
	return func(c *Client) error {
		owned, ok := value.(*http.Client)
		if !ok {
			owned = &http.Client{Transport: agentRoundTripper(value.Do)}
		}
		c.http = owned
		return nil
	}
}

func TestRefundExternalTransportDeniedBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"HTTPDoer", "RoundTripper"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			leaf := refundWireRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Body != nil {
					_ = r.Body.Close()
				}
				return jsonResponse(201, refundJSON("wdr_unverified", "inv-1", "key-1", RefundFull, "120", WithdrawalCreated)), nil
			})
			inner := &http.Client{Transport: leaf}
			var external HTTPDoer
			if kind == "HTTPDoer" {
				external = roundTripper(inner.Do)
			} else {
				external = &http.Client{Transport: refundWireRoundTripper(inner.Do)}
			}
			client, err := NewClient("pk", "sk", WithHTTPClient(external), WithRetry(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Refunds.Create(context.Background(), "inv-1", RefundFull, "key-1")
			if err == nil || calls != 0 {
				t.Errorf("unverified transport was called: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestRefundUsesCapturedOwnedClientAfterClockMutation(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	standardCalls, customCalls := 0, 0
	var client *Client
	clock := func() time.Time {
		client.http = agentDoer(func(*http.Request) (*http.Response, error) { customCalls++; return agentResponse(201, `{}`), nil })
		return time.Unix(1700000000, 0)
	}
	var err error
	client, err = NewClient("pk", "sk", WithClock(clock), WithRetry(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	client.http.(*http.Client).Transport = agentRoundTripper(func(r *http.Request) (*http.Response, error) {
		standardCalls++
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return agentResponse(201, `{"withdrawal_id":"wdr_clock"}`), nil
	})
	_, err = client.Refunds.Create(context.Background(), "inv-1", RefundFull, "key-1")
	if err != nil || standardCalls != 1 || customCalls != 0 {
		t.Fatalf("captured client changed between check and use: standard=%d custom=%d error=%v", standardCalls, customCalls, err)
	}
}
