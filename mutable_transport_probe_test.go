package paymos_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	paymos "github.com/Paymos-labs/go-sdk/v2"
)

// The tests are deliberately external-package tests: only public runtime and
// SDK APIs are used. They are run in a network-none container against fake data.
type mutableTransportFake struct{ calls atomic.Int32 }

func (f *mutableTransportFake) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if req.Body != nil {
		_ = req.Body.Close()
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
}

func TestRefundGlobalMutableTransport(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()

	// In every case the global value is a genuine *http.Transport and the
	// pointer remains identical throughout the refund request. Type/identity
	// checks alone therefore cannot establish its protocol handlers are safe.
	for _, when := range []string{"before_client", "after_client", "during_clock"} {
		t.Run("registered_protocol_"+when, func(t *testing.T) {
			fake := &mutableTransportFake{}
			transport := &http.Transport{}
			http.DefaultTransport = transport
			register := func() { transport.RegisterProtocol("http", fake) }
			if when == "before_client" {
				register()
			}
			var options []paymos.Option
			options = append(options, paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithRetry(0, 0))
			if when == "during_clock" {
				options = append(options, paymos.WithClock(func() time.Time {
					register()
					return time.Unix(1700000000, 0)
				}))
			}
			client, err := paymos.NewClient("fixture-key", "fixture-secret", options...)
			if err != nil {
				t.Fatal(err)
			}
			if when == "after_client" {
				register()
			}
			_, err = client.Refunds.Create(context.Background(), "inv_probe", paymos.RefundFull, "order-probe")
			if fake.calls.Load() != 0 || err == nil {
				t.Errorf("refund used registered unverified handler: calls=%d err=%v", fake.calls.Load(), err)
			}
		})
	}

	t.Run("custom_dial_context", func(t *testing.T) {
		var calls atomic.Int32
		transport := &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			calls.Add(1)
			clientSide, serverSide := net.Pipe() // in-memory pipe: no socket or external service
			go func() {
				defer serverSide.Close()
				req, err := http.ReadRequest(bufio.NewReader(serverSide))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, req.Body)
				_ = req.Body.Close()
				_, _ = fmt.Fprint(serverSide, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
			}()
			return clientSide, nil
		}}
		http.DefaultTransport = transport
		client, err := paymos.NewClient("fixture-key", "fixture-secret", paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithRetry(0, 0))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Refunds.Create(context.Background(), "inv_probe", paymos.RefundFull, "order-probe")
		if calls.Load() != 0 || err == nil {
			t.Errorf("refund used unverified dial hook: calls=%d err=%v", calls.Load(), err)
		}
	})

	for _, operation := range []string{"invoice", "history"} {
		t.Run("ordinary_"+operation+"_keeps_global_transport", func(t *testing.T) {
			fake := &mutableTransportFake{}
			transport := &http.Transport{}
			transport.RegisterProtocol("http", fake)
			http.DefaultTransport = transport
			client, err := paymos.NewClient("fixture-key", "fixture-secret", paymos.WithBaseURL("http://127.0.0.1:1"), paymos.WithRetry(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if operation == "invoice" {
				_, err = client.Invoices.Get(context.Background(), "inv_probe")
			} else {
				_, err = client.Refunds.List(context.Background(), paymos.RefundListParams{})
			}
			if err != nil || fake.calls.Load() != 1 {
				t.Errorf("ordinary read changed: calls=%d err=%v", fake.calls.Load(), err)
			}
		})
	}

	t.Run("clone_drops_registered_protocol_but_retains_dial_hook", func(t *testing.T) {
		fake := &mutableTransportFake{}
		var dials atomic.Int32
		sentinel := errors.New("dial-hook-sentinel")
		transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, sentinel
		}}
		transport.RegisterProtocol("http", fake)
		original := &http.Client{Transport: transport}
		clone := &http.Client{Transport: transport.Clone()}
		response, err := original.Get("http://127.0.0.1:1/")
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		_, cloneErr := clone.Get("http://127.0.0.1:1/")
		if fake.calls.Load() != 1 || dials.Load() != 1 || !errors.Is(cloneErr, sentinel) {
			t.Errorf("unexpected clone semantics: protocol=%d dial=%d clone error=%v", fake.calls.Load(), dials.Load(), cloneErr)
		}
	})
}
