package paymos_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	paymos "github.com/Paymos-labs/go-sdk/v2"
)

type offFake struct{ paths []string }

func (f *offFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	if r.Body != nil {
		_ = r.Body.Close()
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"items":[]}`)), Request: r}, nil
}

type offFakeDoer struct{ fake *offFake }

func (d *offFakeDoer) Do(r *http.Request) (*http.Response, error) { return d.fake.RoundTrip(r) }

func isRefundOff(err error) bool {
	return err != nil && strings.Contains(err.Error(), "refund creation is disabled")
}

func TestRefundOffPublicBoundary(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, when := range []string{"before_client", "after_client"} {
		t.Run("default_"+when, func(t *testing.T) {
			fake := &offFake{}
			clockCalls := 0
			http.DefaultTransport = original
			if when == "before_client" {
				http.DefaultTransport = fake
			}
			client, err := paymos.NewClient("fixture-key", "fixture-secret",
				paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithRetry(2, 0),
				paymos.WithClock(func() time.Time { clockCalls++; return time.Unix(1700000000, 0) }))
			if err != nil {
				t.Fatal(err)
			}
			if when == "after_client" {
				http.DefaultTransport = fake
			}
			for _, mode := range []paymos.RefundMode{paymos.RefundFull, paymos.RefundOverpayment} {
				for i := 0; i < 2; i++ { // same-key replay is OFF too
					_, err = client.Refunds.Create(context.Background(), "inv_probe", mode, "order-probe")
					if !isRefundOff(err) {
						t.Fatalf("want explicit OFF for %s: %v", mode, err)
					}
				}
			}
			if len(fake.paths) != 0 || clockCalls != 0 {
				t.Fatalf("refund performed work before OFF: calls=%v clock=%d", fake.paths, clockCalls)
			}
			if _, err := client.Invoices.Get(context.Background(), "inv_probe"); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Refunds.List(context.Background(), paymos.RefundListParams{}); err != nil {
				t.Fatal(err)
			}
			if strings.Join(fake.paths, ",") != "GET /v1/invoices/inv_probe,GET /v1/refunds" || clockCalls != 2 {
				t.Fatalf("ordinary/history changed: calls=%v clock=%d", fake.paths, clockCalls)
			}
		})
	}

	for _, kind := range []string{"doer", "http_client"} {
		t.Run("custom_"+kind, func(t *testing.T) {
			fake := &offFake{}
			var doer paymos.HTTPDoer = &offFakeDoer{fake}
			if kind == "http_client" {
				doer = &http.Client{Transport: fake}
			}
			client, err := paymos.NewClient("fixture-key", "fixture-secret", paymos.WithHTTPClient(doer), paymos.WithRetry(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Refunds.Create(context.Background(), "inv_probe", paymos.RefundFull, "order-probe")
			if !isRefundOff(err) || len(fake.paths) != 0 {
				t.Fatalf("custom refund was not OFF before dispatch: calls=%v err=%v", fake.paths, err)
			}
			if _, err := client.Refunds.List(context.Background(), paymos.RefundListParams{}); err != nil || len(fake.paths) != 1 {
				t.Fatalf("custom history changed: calls=%v err=%v", fake.paths, err)
			}
		})
	}

	t.Run("proxy_tls_and_dial_hooks_never_called", func(t *testing.T) {
		calls := 0
		transport := &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { calls++; return nil, nil },
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				calls++
				return nil, errors.New("dial must not run")
			},
			TLSClientConfig: &tls.Config{VerifyConnection: func(tls.ConnectionState) error { calls++; return nil }},
		}
		client, err := paymos.NewClient("fixture-key", "fixture-secret", paymos.WithHTTPClient(&http.Client{Transport: transport}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Refunds.Create(context.Background(), "inv_probe", paymos.RefundFull, "order-probe")
		if !isRefundOff(err) || calls != 0 {
			t.Fatalf("refund must fail before proxy/TLS/dial: calls=%d err=%v", calls, err)
		}
	})
}

type offRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f offRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// This retains the ordinary redirect, cookie, timeout and body assertions from
// the mixed refund/ordinary test, whose refund assertions are deferred while OFF.
func TestOrdinaryClientSettingsAndRedirectsWithRefundOff(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := url.Parse("http://127.0.0.1:1/")
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(seed, []*http.Cookie{{Name: "before", Value: "yes", Path: "/"}})
	const timeout = 2 * time.Second
	var callbacks, calls []string
	var invoiceBody string
	transport := offRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > timeout+200*time.Millisecond {
			t.Fatalf("configured timeout was not applied to %s %s", r.Method, r.URL.Path)
		}
		if cookie, err := r.Cookie("before"); err != nil || cookie.Value != "yes" {
			t.Fatalf("ordinary request lost cookie jar: cookie=%v error=%v", cookie, err)
		}
		response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}
		switch r.URL.Path {
		case "/v1/time":
			response.StatusCode = 302
			response.Header.Set("Location", "/ordinary-get")
			response.Header.Add("Set-Cookie", "after=yes; Path=/")
		case "/ordinary-get":
			if r.Method != "GET" {
				t.Fatalf("ordinary GET changed method: %s", r.Method)
			}
		case "/v1/invoices":
			if r.Method != "POST" || r.GetBody == nil {
				t.Fatalf("ordinary POST lost replay capability: method=%s", r.Method)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			_ = r.Body.Close()
			invoiceBody = string(body)
			response.StatusCode = 307
			response.Header.Set("Location", "/ordinary-post")
		case "/ordinary-post":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || string(body) != invoiceBody {
				t.Fatalf("ordinary POST redirect lost method/body: %s %q", r.Method, body)
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return response, nil
	})
	original := &http.Client{Transport: transport, Jar: jar, Timeout: timeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			callbacks = append(callbacks, via[0].URL.Path+" -> "+r.URL.Path)
			return nil
		}}
	client, err := paymos.NewClient("fixture-key", "fixture-secret", paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithHTTPClient(original), paymos.WithRetry(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = client.Refunds.Create(ctx, "inv_probe", paymos.RefundFull, "order-probe"); !isRefundOff(err) || len(calls) != 0 {
		t.Fatalf("refund must be OFF: calls=%v err=%v", calls, err)
	}
	if _, err = client.System.Time(ctx); err != nil {
		t.Fatalf("ordinary GET redirect: %v", err)
	}
	if _, err = client.Invoices.Create(ctx, paymos.CreateInvoiceParams{}); err != nil {
		t.Fatalf("ordinary POST redirect: %v", err)
	}
	if strings.Join(calls, ",") != "GET /v1/time,GET /ordinary-get,POST /v1/invoices,POST /ordinary-post" ||
		strings.Join(callbacks, ",") != "/v1/time -> /ordinary-get,/v1/invoices -> /ordinary-post" {
		t.Fatalf("unexpected requests/callbacks: %v %v", calls, callbacks)
	}
	if original.Jar != jar || original.Timeout != timeout {
		t.Fatal("refund changed the shared client or its settings")
	}
	foundCookie := false
	for _, cookie := range jar.Cookies(seed) {
		foundCookie = foundCookie || cookie.Name == "after" && cookie.Value == "yes"
	}
	if !foundCookie {
		t.Fatal("ordinary response cookie was not stored in the original jar")
	}
}

func TestRefundOffConcurrentOrdinaryRead(t *testing.T) {
	const count = 24
	var calls atomic.Int32
	transport := offRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/invoices/inv_probe" {
			return nil, errors.New("unexpected transport request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	client, err := paymos.NewClient("fixture-key", "fixture-secret", paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithHTTPClient(&http.Client{Transport: transport}), paymos.WithRetry(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2*count)
	var wait sync.WaitGroup
	for i := 0; i < count; i++ {
		wait.Add(2)
		go func() {
			defer wait.Done()
			_, err := client.Refunds.Create(context.Background(), "inv_probe", paymos.RefundFull, "order-probe")
			if !isRefundOff(err) {
				results <- errors.New("refund did not return OFF")
				return
			}
			results <- nil
		}()
		go func() {
			defer wait.Done()
			_, err := client.Invoices.Get(context.Background(), "inv_probe")
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != count {
		t.Fatalf("want %d ordinary calls and no refund calls, got %d", count, calls.Load())
	}
}
