package paymos

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type refundRequestVector struct {
	Method            string            `json:"method"`
	Path              string            `json:"path"`
	Body              map[string]string `json:"body"`
	NewStatus         int               `json:"new_status"`
	ReplayStatus      int               `json:"replay_status"`
	ChangedModeStatus int               `json:"changed_mode_status"`
	RejectedFields    []string          `json:"rejected_fields"`
}

func TestRefundScopedSimulatorAllTerminalStatuses(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			client := stubClient(t, func(r *http.Request) (*http.Response, error) {
				assertRefundRequest(t, r, "POST", "/v1/sandbox/refunds/wdr%20%2F%3F/simulate", map[string]string{"status": status})
				return jsonResponse(200, refundJSON("wdr /?", "inv_1", "key", RefundFull, "120", WithdrawalStatus(status))), nil
			})
			result, err := client.Refunds.Simulate(context.Background(), "wdr /?", status)
			if err != nil || string(result.Status) != status {
				t.Fatalf("terminal refund outcome: %#v %v", result, err)
			}
		})
	}
}

type refundVectors struct {
	Create      refundRequestVector `json:"create"`
	Overpayment refundRequestVector `json:"overpayment"`
	List        struct {
		Method              string `json:"method"`
		Path                string `json:"path"`
		PageSizes           []int  `json:"page_sizes"`
		InvalidCursorStatus int    `json:"invalid_cursor_status"`
	} `json:"list"`
	Get struct {
		Method                 string `json:"method"`
		Path                   string `json:"path"`
		ForeignOrMissingStatus int    `json:"foreign_or_missing_status"`
	} `json:"get"`
}

func refundContract(t *testing.T) refundVectors {
	t.Helper()
	raw, err := os.ReadFile("conformance/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Vectors struct {
			Refunds refundVectors `json:"refunds"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if value.Vectors.Refunds.Create.Path != "/v1/refunds" {
		t.Fatal("refund conformance vectors are missing")
	}
	return value.Vectors.Refunds
}

func refundJSON(id, invoiceID, key string, mode RefundMode, amount string, status WithdrawalStatus) string {
	return fmt.Sprintf(`{"type":"refund","project_id":"prj_test","withdrawal_id":%q,"invoice_id":%q,"external_order_id":%q,`+
		`"mode":%q,"amount":%q,"fee":"0.000001","currency":"USDT","network":"TRC20",`+
		`"status":%q,"is_final":%t,"is_test":true,"first_sender":"TFirstSender",`+
		`"destination_address":"TFirstSender","created_at":1700000000}`,
		id, invoiceID, key, mode, amount, status, status == WithdrawalCompleted)
}

func assertRefundRequest(t *testing.T, r *http.Request, method, uri string, payload map[string]string) {
	t.Helper()
	if r.Method != method || r.URL.RequestURI() != uri {
		t.Fatalf("want %s %s, got %s %s", method, uri, r.Method, r.URL.RequestURI())
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if payload == nil {
		if len(body) != 0 {
			t.Fatalf("expected no body, got %s", body)
		}
	} else {
		var got map[string]string
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, payload) {
			t.Fatalf("body mismatch: want %#v, got %#v", payload, got)
		}
	}
	path, query, hasQuery := strings.Cut(uri, "?")
	if hasQuery {
		query = "?" + query
	}
	bodyHash := ""
	if len(body) != 0 {
		sum := sha256.Sum256(body)
		bodyHash = hex.EncodeToString(sum[:])
	}
	const timestamp = "1700000000"
	mac := hmac.New(sha256.New, []byte("sk_test_secret"))
	_, _ = io.WriteString(mac, timestamp+"\n"+method+"\n"+path+"\n"+query+"\n"+bodyHash)
	want := "HMAC-SHA256 pk_test_key:" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if r.Header.Get("Authorization") != want || r.Header.Get("X-Request-Timestamp") != timestamp {
		t.Fatalf("request must sign its exact path, query and body: %#v", r.Header)
	}
	if r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
	}
}

func TestRefundCreateAndReplayUseOnlyModeAndCallerKey(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	v := refundContract(t)
	for _, tc := range []struct {
		vector refundRequestVector
		amount string
	}{
		{v.Create, "120"},
		{v.Overpayment, "20"},
	} {
		t.Run(tc.vector.Body["mode"], func(t *testing.T) {
			calls := 0
			client := stubClient(t, func(r *http.Request) (*http.Response, error) {
				assertRefundRequest(t, r, tc.vector.Method, tc.vector.Path, tc.vector.Body)
				calls++
				status := v.Create.NewStatus
				if calls == 2 {
					status = v.Create.ReplayStatus
				}
				return jsonResponse(status, refundJSON("wdr_refund", tc.vector.Body["invoice_id"],
					tc.vector.Body["external_order_id"], RefundMode(tc.vector.Body["mode"]), tc.amount, WithdrawalCreated)), nil
			})
			var create func(context.Context, string, RefundMode, string) (Refund, error) = client.Refunds.Create
			var first Refund
			for i := 0; i < 2; i++ {
				got, err := create(context.Background(), tc.vector.Body["invoice_id"],
					RefundMode(tc.vector.Body["mode"]), tc.vector.Body["external_order_id"])
				if err != nil {
					t.Fatal(err)
				}
				if got.WithdrawalID != "wdr_refund" || got.InvoiceID != tc.vector.Body["invoice_id"] ||
					got.Mode != RefundMode(tc.vector.Body["mode"]) || got.Amount != tc.amount ||
					got.ExternalOrderID != tc.vector.Body["external_order_id"] || got.Status != WithdrawalCreated ||
					got.FirstSender != "TFirstSender" || got.DestinationAddress != got.FirstSender ||
					got.Currency != "USDT" || got.Network != "TRC20" || got.CreatedAt != 1700000000 || !got.IsTest {
					t.Fatalf("server response must survive unchanged: %#v", got)
				}
				if i == 0 {
					first = got
				} else if !reflect.DeepEqual(first, got) {
					t.Fatalf("replay changed the refund: %#v / %#v", first, got)
				}
			}
			if calls != 2 {
				t.Fatalf("expected two requests, got %d", calls)
			}
		})
	}
}

func TestRefundOverpaymentThenFullUsesServerAmounts(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	v := refundContract(t)
	invoiceID := v.Create.Body["invoice_id"]
	over := refundJSON("wdr_over", invoiceID, "over-key", RefundOverpayment, "20", WithdrawalCreated)
	completed := refundJSON("wdr_over", invoiceID, "over-key", RefundOverpayment, "20", WithdrawalCompleted)
	full := refundJSON("wdr_full", invoiceID, "full-key", RefundFull, "100", WithdrawalCreated)
	steps := []struct {
		method, uri string
		body        map[string]string
		status      int
		response    string
	}{
		{"POST", "/v1/sandbox/invoices/" + invoiceID + "/simulate-payment", map[string]string{"stage": "overpaid"}, 200,
			`{"invoice_id":"` + invoiceID + `","status":"paid_over","is_test":true,"order":{"amount":"100","currency":"USD"},"payment":{"paid":"120","currency":"USDT","network":"TRC20"}}`},
		{"POST", "/v1/refunds", map[string]string{"invoice_id": invoiceID, "mode": "overpayment", "external_order_id": "over-key"}, 201, over},
		{"POST", "/v1/sandbox/refunds/wdr_over/simulate", map[string]string{"status": "completed"}, 200, completed},
		{"GET", "/v1/refunds/wdr_over", nil, 200, completed},
		{"POST", "/v1/refunds", map[string]string{"invoice_id": invoiceID, "mode": "full", "external_order_id": "full-key"}, 201, full},
	}
	calls := 0
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		if calls >= len(steps) {
			t.Fatal("unexpected extra request")
		}
		step := steps[calls]
		calls++
		assertRefundRequest(t, r, step.method, step.uri, step.body)
		return jsonResponse(step.status, step.response), nil
	})
	ctx := context.Background()
	invoice, err := client.Invoices.SimulatePayment(ctx, invoiceID, "overpaid")
	if err != nil || invoice.Order.Amount != "100" || invoice.Payment == nil || invoice.Payment.Paid == nil || *invoice.Payment.Paid != "120" {
		t.Fatalf("expected a 120/100 sandbox payment: %#v %v", invoice, err)
	}
	refund, err := client.Refunds.Create(ctx, invoiceID, RefundOverpayment, "over-key")
	if err != nil || refund.Amount != "20" || refund.WithdrawalID != "wdr_over" {
		t.Fatalf("overpayment must be the server's 20: %#v %v", refund, err)
	}
	withdrawal, err := client.Refunds.Simulate(ctx, refund.WithdrawalID, "completed")
	if err != nil || withdrawal.Status != WithdrawalCompleted || withdrawal.Amount != "20" {
		t.Fatalf("sandbox completes the same withdrawal: %#v %v", withdrawal, err)
	}
	refund, err = client.Refunds.Get(ctx, withdrawal.WithdrawalID)
	if err != nil || refund.Mode != RefundOverpayment || !refund.IsFinal {
		t.Fatalf("refund read must retain its mode: %#v %v", refund, err)
	}
	refund, err = client.Refunds.Create(ctx, invoiceID, RefundFull, "full-key")
	if err != nil || refund.Amount != "100" || refund.Mode != RefundFull || refund.WithdrawalID != "wdr_full" {
		t.Fatalf("full after overpayment must be the server's 100: %#v %v", refund, err)
	}
	if calls != len(steps) {
		t.Fatalf("expected %d requests, got %d", len(steps), calls)
	}
}

func TestRefundChangedModeReturnsStructuredConflict(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	v := refundContract(t)
	invoiceID, key := v.Create.Body["invoice_id"], v.Create.Body["external_order_id"]
	calls := 0
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		mode := RefundFull
		if calls == 2 {
			mode = RefundOverpayment
		}
		assertRefundRequest(t, r, "POST", "/v1/refunds", map[string]string{
			"invoice_id": invoiceID, "mode": string(mode), "external_order_id": key,
		})
		if calls == 1 {
			return jsonResponse(v.Create.NewStatus, refundJSON("wdr_1", invoiceID, key, mode, "120", WithdrawalCreated)), nil
		}
		return jsonResponse(v.Create.ChangedModeStatus, `{"type":"about:blank","title":"Conflict","status":409,"detail":"Refund mode differs.","code":"refund_idempotency_conflict","field":"external_order_id"}`), nil
	})
	ctx := context.Background()
	if _, err := client.Refunds.Create(ctx, invoiceID, RefundFull, key); err != nil {
		t.Fatal(err)
	}
	_, err := client.Refunds.Create(ctx, invoiceID, RefundOverpayment, key)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Status != 409 || apiError.Kind() != "conflict" ||
		apiError.Code() != "refund_idempotency_conflict" || apiError.Field() == nil ||
		*apiError.Field() != "external_order_id" || apiError.Problem.Detail != "Refund mode differs." {
		t.Fatalf("expected a structured mode conflict, got %#v", err)
	}
	if calls != 2 {
		t.Fatalf("conflict must not retry: %d requests", calls)
	}
}

func TestRefundListFiltersAndCursorPages(t *testing.T) {
	v := refundContract(t)
	invoiceID := v.Create.Body["invoice_id"]
	from, to := "2026-10-01T00:00:00Z", "2026-10-28T23:59:59+03:00"
	filters := RefundListParams{Limit: 100, InvoiceID: &invoiceID, Status: []WithdrawalStatus{WithdrawalCompleted, WithdrawalCreated}, CreatedFrom: &from, CreatedTo: &to}
	const period = "created_from=2026-10-01T00%3A00%3A00Z&created_to=2026-10-28T23%3A59%3A59%2B03%3A00"
	calls := 0
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		if calls >= len(v.List.PageSizes) {
			t.Fatal("iterator read past the last page")
		}
		query := period + "&invoice_id=" + invoiceID + "&limit=100&status=completed&status=created"
		if calls > 0 {
			query = period + fmt.Sprintf("&cursor=cur%%2F%d%%20%%2B&invoice_id=", calls) + invoiceID + "&limit=100&status=completed&status=created"
		}
		assertRefundRequest(t, r, v.List.Method, v.List.Path+"?"+query, nil)
		items := make([]string, v.List.PageSizes[calls])
		for i := range items {
			items[i] = refundJSON(fmt.Sprintf("wdr_%d_%d", calls, i), invoiceID, "key", RefundFull, "100", WithdrawalCompleted)
		}
		calls++
		cursor := "null"
		if calls < len(v.List.PageSizes) {
			cursor = fmt.Sprintf("%q", fmt.Sprintf("cur/%d +", calls))
		}
		return jsonResponse(200, `{"items":[`+strings.Join(items, ",")+`],"next_cursor":`+cursor+`}`), nil
	})
	iterator := NewRefundIterator(client.Refunds, filters, 5)
	count := 0
	for {
		refund, ok, err := iterator.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if refund.InvoiceID != invoiceID || refund.Amount != "100" || refund.Mode != RefundFull {
			t.Fatalf("unexpected list item: %#v", refund)
		}
		count++
	}
	if count != 205 || calls != 3 || filters.Cursor != nil {
		t.Fatalf("expected 100/100/5 without changing caller filters: %d items, %d pages, %#v", count, calls, filters)
	}
}

func TestRefundListDefaultsAndInvalidCursor(t *testing.T) {
	v := refundContract(t)
	calls := 0
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			assertRefundRequest(t, r, "GET", "/v1/refunds", nil)
			return jsonResponse(200, `{"items":[],"next_cursor":null}`), nil
		}
		assertRefundRequest(t, r, "GET", "/v1/refunds?cursor=invalid", nil)
		return jsonResponse(v.List.InvalidCursorStatus, `{"type":"about:blank","title":"Bad Request","status":400,"detail":"Invalid cursor.","code":"invalid_cursor","field":"cursor"}`), nil
	})
	page, err := client.Refunds.List(context.Background(), RefundListParams{})
	if err != nil || len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatalf("expected an empty final page: %#v %v", page, err)
	}
	cursor := "invalid"
	_, err = client.Refunds.List(context.Background(), RefundListParams{Cursor: &cursor})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code() != "invalid_cursor" || apiError.Kind() != "validation" ||
		apiError.Field() == nil || *apiError.Field() != "cursor" || calls != 2 {
		t.Fatalf("invalid cursor must remain a structured error: %#v", err)
	}
}

func TestRefundGetEncodesIDAndPreservesDecimalStrings(t *testing.T) {
	v := refundContract(t)
	const amount = "120.123456789012345678901234567890"
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		assertRefundRequest(t, r, v.Get.Method, strings.Replace(v.Get.Path, "{id}", "wdr%2Fone%20two", 1), nil)
		return jsonResponse(200, refundJSON("wdr/one two", v.Create.Body["invoice_id"], "key", RefundFull, amount, WithdrawalCompleted)), nil
	})
	refund, err := client.Refunds.Get(context.Background(), "wdr/one two")
	if err != nil || refund.WithdrawalID != "wdr/one two" || refund.Amount != amount ||
		refund.Fee == nil || *refund.Fee != "0.000001" || !refund.IsFinal || refund.FirstSender != "TFirstSender" {
		t.Fatalf("server money must remain exact strings: %#v %v", refund, err)
	}
}

func TestRefundForeignAndMissingIDsReturnStructuredNotFound(t *testing.T) {
	v := refundContract(t)
	for _, id := range []string{"wdr_foreign", "wdr_missing"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			client := stubClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				assertRefundRequest(t, r, v.Get.Method, strings.Replace(v.Get.Path, "{id}", id, 1), nil)
				return jsonResponse(v.Get.ForeignOrMissingStatus, `{"type":"about:blank","title":"Not Found","status":404,"detail":"Refund not found.","code":"not_found"}`), nil
			})
			_, err := client.Refunds.Get(context.Background(), id)
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.Status != 404 || apiError.Code() != "not_found" ||
				apiError.Kind() != "not_found" || apiError.Problem.Detail != "Refund not found." || calls != 1 {
				t.Fatalf("expected one structured 404: %#v (%d requests)", err, calls)
			}
		})
	}
}

func TestRefundUnknownOutcomeReplaysSameKey(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	for _, outcome := range []string{"transport", "503", "429"} {
		t.Run(outcome, func(t *testing.T) {
			v := refundContract(t)
			calls := 0
			transportError := errors.New("response lost")
			client, err := NewClient("pk_test_key", "sk_test_secret",
				WithClock(func() time.Time { return time.Unix(1700000000, 0) }), WithRetry(2, 0),
				agentWithOwnedHTTPClient(roundTripper(func(r *http.Request) (*http.Response, error) {
					assertRefundRequest(t, r, v.Create.Method, v.Create.Path, v.Create.Body)
					calls++
					if calls == 1 {
						switch outcome {
						case "transport":
							return nil, transportError
						case "503":
							return jsonResponse(503, `{"type":"about:blank","title":"Unavailable","status":503,"detail":"Try later.","code":"unavailable"}`), nil
						case "429":
							return jsonResponse(429, `{"type":"about:blank","title":"Rate limited","status":429,"detail":"Try later.","code":"rate_limited"}`), nil
						}
					}
					return jsonResponse(v.Create.ReplayStatus, refundJSON("wdr_accepted", v.Create.Body["invoice_id"], v.Create.Body["external_order_id"], RefundFull, "120", WithdrawalCreated)), nil
				})))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			refund, err := client.Refunds.Create(ctx, v.Create.Body["invoice_id"], RefundFull, v.Create.Body["external_order_id"])
			if calls != 1 || err == nil {
				t.Fatalf("unknown POST outcome must return without an automatic retry: %d %v", calls, err)
			}
			switch outcome {
			case "transport":
				if !errors.Is(err, transportError) {
					t.Fatalf("transport cause must survive: %v", err)
				}
			case "429", "503":
				wantStatus, wantKind, wantCode := 429, "rate_limit", "rate_limited"
				if outcome == "503" {
					wantStatus, wantKind, wantCode = 503, "unavailable", "unavailable"
				}
				var apiError *APIError
				if !errors.As(err, &apiError) || apiError.Status != wantStatus ||
					apiError.Kind() != wantKind || apiError.Code() != wantCode {
					t.Fatalf("original API error must survive: %#v", err)
				}
			}
			refund, err = client.Refunds.Create(ctx, v.Create.Body["invoice_id"], RefundFull, v.Create.Body["external_order_id"])
			if err != nil || calls != 2 || refund.WithdrawalID != "wdr_accepted" || refund.Amount != "120" {
				t.Fatalf("same-key replay must return the accepted refund: %#v %v (%d requests)", refund, err, calls)
			}
		})
	}
}

func TestRefundValidationKeepsTopLevelProblemAndFieldErrors(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(400, `{"type":"about:blank","title":"Bad Request","status":400,"detail":"Invalid request.","code":"validation_failed","errors":[{"code":"field_required","field":"invoice_id","message":"Required."},{"code":"field_required","field":"external_order_id","message":"Required."}]}`), nil
	})
	_, err := client.Refunds.Create(context.Background(), "", RefundFull, "")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code() != "validation_failed" || apiError.Field() != nil ||
		len(apiError.Problem.Errors) != 2 || apiError.Problem.Errors[0].Field == nil ||
		*apiError.Problem.Errors[0].Field != "invoice_id" || apiError.Problem.Errors[0].Code != "field_required" {
		t.Fatalf("top-level code and field errors must both survive: %#v", err)
	}
}

func TestRefundInvalidModeDoesNotSendARequest(t *testing.T) {
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid mode must fail before HTTP")
		return nil, nil
	})
	for _, mode := range []RefundMode{"", "partial", "FULL"} {
		if _, err := client.Refunds.Create(context.Background(), "inv_1", mode, "key"); err == nil {
			t.Fatalf("invalid mode %q must fail", mode)
		}
	}
}

type refundWireRoundTripper func(*http.Request) (*http.Response, error)

func (f refundWireRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func assertRefundCreateWire(t *testing.T, r *http.Request) {
	t.Helper()
	const wantBody = "{\"external_order_id\":\"key-1\",\"invoice_id\":\"inv-1\",\"mode\":\"full\"}"
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.URL.RequestURI() != "/v1/refunds" || string(body) != wantBody ||
		r.ContentLength != int64(len(wantBody)) || r.GetBody != nil {
		t.Fatalf("refund wire or rewind changed: method=%s uri=%s body=%q length=%d rewind=%t",
			r.Method, r.URL.RequestURI(), body, r.ContentLength, r.GetBody != nil)
	}
	sum := sha256.Sum256([]byte(wantBody))
	mac := hmac.New(sha256.New, []byte("sk_test_secret"))
	_, _ = io.WriteString(mac, "1700000000\nPOST\n/v1/refunds\n\n"+hex.EncodeToString(sum[:]))
	wantAuth := "HMAC-SHA256 pk_test_key:" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if r.Header.Get("Authorization") != wantAuth || r.Header.Get("X-Request-Timestamp") != "1700000000" {
		t.Fatalf("wrong refund signature: %#v", r.Header)
	}
}

func TestRefundCreatePreservesExactWire(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	const wantBody = "{\"external_order_id\":\"key-1\",\"invoice_id\":\"inv-1\",\"mode\":\"full\"}"
	calls := 0
	client, err := NewClient("pk_test_key", "sk_test_secret",
		WithClock(func() time.Time { return time.Unix(1700000000, 0) }),
		WithRetry(2, 0),
		agentWithOwnedHTTPClient(roundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || r.URL.RequestURI() != "/v1/refunds" || string(body) != wantBody ||
				r.ContentLength != int64(len(wantBody)) || r.GetBody != nil {
				t.Fatalf("refund wire or rewind changed: method=%s uri=%s body=%q length=%d rewind=%t",
					r.Method, r.URL.RequestURI(), body, r.ContentLength, r.GetBody != nil)
			}
			sum := sha256.Sum256([]byte(wantBody))
			mac := hmac.New(sha256.New, []byte("sk_test_secret"))
			_, _ = io.WriteString(mac, "1700000000\nPOST\n/v1/refunds\n\n"+hex.EncodeToString(sum[:]))
			wantAuth := "HMAC-SHA256 pk_test_key:" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
			if r.Header.Get("Authorization") != wantAuth || r.Header.Get("X-Request-Timestamp") != "1700000000" {
				t.Fatalf("wrong refund signature: %#v", r.Header)
			}
			return jsonResponse(201, "{}"), nil
		})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refunds.Create(context.Background(), "inv-1", RefundFull, "key-1"); err != nil || calls != 1 {
		t.Fatalf("refund create: calls=%d error=%v", calls, err)
	}
}

func TestRefundCreateStandardClientRedirects(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls, callbackCalls := 0, 0
			transport := refundWireRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					assertRefundCreateWire(t, r)
					_ = r.Body.Close()
					response := jsonResponse(status, "redirect response")
					response.Header.Set("Location", "/redirected")
					response.Header.Set("X-Original-Response", "retained")
					return response, nil
				}
				// A followed redirect would otherwise return an apparently successful refund.
				return jsonResponse(200, refundJSON("wdr_redirect", "inv-1", "key-1", RefundFull, "120", WithdrawalCreated)), nil
			})
			original := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
				callbackCalls++
				return nil
			}}
			client, err := NewClient("pk_test_key", "sk_test_secret",
				WithClock(func() time.Time { return time.Unix(1700000000, 0) }),
				WithRetry(2, 0), agentWithOwnedHTTPClient(original))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Refunds.Create(context.Background(), "inv-1", RefundFull, "key-1")
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.Status != status ||
				apiError.Header.Get("Location") != "/redirected" || apiError.Header.Get("X-Original-Response") != "retained" ||
				string(apiError.Body) != "redirect response" || calls != 1 || callbackCalls != 0 {
				t.Fatalf("redirect must return its original error without following: calls=%d callback=%d err=%#v", calls, callbackCalls, err)
			}
		})
	}
}

func TestRefundClientSettingsAndOrdinaryRedirects(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := http.NewRequest("GET", "https://api.paymos.io/v1/refunds", nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(seed.URL, []*http.Cookie{{Name: "before", Value: "yes", Path: "/"}})
	const timeout = 2 * time.Second
	var callbacks, calls []string
	var invoiceBody string
	transport := refundWireRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > timeout+200*time.Millisecond {
			t.Fatalf("configured timeout was not applied to %s %s", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/v1/refunds":
			if cookie, err := r.Cookie("before"); err != nil || cookie.Value != "yes" {
				t.Fatalf("refund lost original cookie jar: cookie=%v error=%v", cookie, err)
			}
			_ = r.Body.Close()
			response := jsonResponse(302, "redirect")
			response.Header.Set("Location", "/refund-target")
			response.Header.Add("Set-Cookie", "after=yes; Path=/")
			return response, nil
		case "/v1/time":
			response := jsonResponse(302, "redirect")
			response.Header.Set("Location", "/ordinary-get")
			return response, nil
		case "/ordinary-get":
			if r.Method != "GET" {
				t.Fatalf("ordinary GET changed method to %s", r.Method)
			}
			return jsonResponse(200, "{}"), nil
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
			response := jsonResponse(307, "redirect")
			response.Header.Set("Location", "/ordinary-post")
			return response, nil
		case "/ordinary-post":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || string(body) != invoiceBody {
				t.Fatalf("ordinary POST redirect lost method/body: %s %q", r.Method, body)
			}
			return jsonResponse(200, "{}"), nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})
	original := &http.Client{Transport: transport, Jar: jar, Timeout: timeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			callbacks = append(callbacks, via[0].URL.Path+" -> "+r.URL.Path)
			return nil
		}}
	client, err := NewClient("pk", "sk", agentWithOwnedHTTPClient(original), WithRetry(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Refunds.Create(ctx, "inv-1", RefundFull, "key-1")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Status != 302 {
		t.Fatalf("refund should return its redirect: %v", err)
	}
	if _, err = client.System.Time(ctx); err != nil {
		t.Fatalf("ordinary GET redirect: %v", err)
	}
	if _, err = client.Invoices.Create(ctx, CreateInvoiceParams{}); err != nil {
		t.Fatalf("ordinary POST redirect: %v", err)
	}
	wantCalls := []string{"POST /v1/refunds", "GET /v1/time", "GET /ordinary-get", "POST /v1/invoices", "POST /ordinary-post"}
	wantCallbacks := []string{"/v1/time -> /ordinary-get", "/v1/invoices -> /ordinary-post"}
	if !reflect.DeepEqual(calls, wantCalls) || !reflect.DeepEqual(callbacks, wantCallbacks) {
		t.Fatalf("unexpected requests/callbacks: %v %v", calls, callbacks)
	}
	if client.http != original || original.Jar != jar || original.Timeout != timeout {
		t.Fatal("refund changed the shared client or its settings")
	}
	foundCookie := false
	for _, cookie := range jar.Cookies(seed.URL) {
		foundCookie = foundCookie || cookie.Name == "after" && cookie.Value == "yes"
	}
	if !foundCookie {
		t.Fatal("refund response cookie was not stored in the original jar")
	}
}

type refundContextBody struct{ ctx context.Context }

func (body refundContextBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (refundContextBody) Close() error { return nil }

func TestRefundClientTimeoutCoversReadingBody(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	transport := refundWireRoundTripper(func(r *http.Request) (*http.Response, error) {
		_ = r.Body.Close()
		response := jsonResponse(200, "")
		response.Body = refundContextBody{ctx: r.Context()}
		return response, nil
	})
	original := &http.Client{Transport: transport, Timeout: 40 * time.Millisecond}
	client, err := NewClient("pk", "sk", agentWithOwnedHTTPClient(original))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	_, err = client.Refunds.Create(ctx, "inv-1", RefundFull, "key-1")
	if err == nil || time.Since(started) >= 2*time.Second {
		t.Fatalf("original client timeout must cancel reading the refund response body: %v", err)
	}
}

func TestRefundAndOrdinaryRedirectsCanOverlap(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	refundEntered, getEntered := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var callbacks []string
	transport := refundWireRoundTripper(func(r *http.Request) (*http.Response, error) {
		var other <-chan struct{}
		switch r.URL.Path {
		case "/v1/refunds":
			_ = r.Body.Close()
			close(refundEntered)
			other = getEntered
		case "/v1/time":
			close(getEntered)
			other = refundEntered
		case "/ordinary-target":
			return jsonResponse(200, "{}"), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.URL.Path)
		}
		select {
		case <-other:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		response := jsonResponse(302, "redirect")
		response.Header.Set("Location", "/ordinary-target")
		return response, nil
	})
	original := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		mu.Lock()
		callbacks = append(callbacks, via[0].URL.Path)
		mu.Unlock()
		return nil
	}}
	client, err := NewClient("pk", "sk", agentWithOwnedHTTPClient(original), WithRetry(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		kind string
		err  error
	}
	results := make(chan result, 2)
	go func() {
		_, err := client.Refunds.Create(ctx, "inv-1", RefundFull, "key-1")
		results <- result{"refund", err}
	}()
	go func() {
		_, err := client.System.Time(ctx)
		results <- result{"get", err}
	}()
	for i := 0; i < 2; i++ {
		got := <-results
		var apiError *APIError
		if got.kind == "refund" && (!errors.As(got.err, &apiError) || apiError.Status != 302) {
			t.Errorf("concurrent refund must return original 302: %v", got.err)
		}
		if got.kind == "get" && got.err != nil {
			t.Errorf("concurrent ordinary GET must follow redirect: %v", got.err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(callbacks, []string{"/v1/time"}) || client.http != original {
		t.Fatalf("original callback/client changed during overlapping requests: %v", callbacks)
	}
}

// Unknown refund transports are rejected before dispatch.
func TestRefundCustomTransportBoundary(t *testing.T) {
	for _, injection := range []string{"HTTPDoer", "RoundTripper"} {
		t.Run(injection, func(t *testing.T) {
			var requests []string
			leaf := refundWireRoundTripper(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				if len(requests) == 1 {
					_ = r.Body.Close()
					response := jsonResponse(302, "redirect")
					response.Header.Set("Location", "/handled-internally")
					return response, nil
				}
				return jsonResponse(200, refundJSON("wdr_redirect", "inv-1", "key-1", RefundFull, "120", WithdrawalCreated)), nil
			})
			inner := &http.Client{Transport: leaf}
			var injected HTTPDoer
			if injection == "HTTPDoer" {
				injected = roundTripper(inner.Do)
			} else {
				injected = &http.Client{Transport: refundWireRoundTripper(inner.Do)}
			}
			client, err := NewClient("pk", "sk", WithHTTPClient(injected), WithRetry(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Refunds.Create(context.Background(), "inv-1", RefundFull, "key-1")
			if err == nil || len(requests) != 0 {
				t.Fatalf("an injected %s must fail before transport: requests=%v err=%v", injection, requests, err)
			}
		})
	}
}

func TestRefundGuardPreservesGetAndOrdinaryPostRetry(t *testing.T) {
	for _, outcome := range []string{"429", "503", "transport"} {
		t.Run("get-"+outcome, func(t *testing.T) {
			calls := 0
			client, err := NewClient("pk", "sk", WithRetry(2, 0),
				WithHTTPClient(roundTripper(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != "GET" || r.URL.Path != "/v1/refunds" {
						t.Fatalf("unexpected GET: %s %s", r.Method, r.URL.Path)
					}
					if calls == 1 {
						switch outcome {
						case "429":
							return jsonResponse(429, "{}"), nil
						case "503":
							return jsonResponse(503, "{}"), nil
						default:
							return nil, errors.New("read failed")
						}
					}
					return jsonResponse(200, "{\"items\":[],\"next_cursor\":null}"), nil
				})))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Refunds.List(context.Background(), RefundListParams{})
			if err != nil || calls != 2 {
				t.Fatalf("safe GET must retain retry: calls=%d err=%v", calls, err)
			}
		})
	}
	t.Run("ordinary-post-429", func(t *testing.T) {
		calls := 0
		client, err := NewClient("pk", "sk", WithRetry(2, 0),
			WithHTTPClient(roundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v1/invoices" || r.GetBody == nil {
					t.Fatalf("ordinary POST changed: %s %s rewind=%t", r.Method, r.URL.Path, r.GetBody != nil)
				}
				if calls == 1 {
					return jsonResponse(429, "{}"), nil
				}
				return jsonResponse(200, "{}"), nil
			})))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Invoices.Create(context.Background(), CreateInvoiceParams{})
		if err != nil || calls != 2 {
			t.Fatalf("ordinary POST must retain 429 retry: calls=%d err=%v", calls, err)
		}
	})
}

// refundH2Frame builds only the frames needed by the in-memory HTTP/2 peer below.
func refundH2Frame(kind, flags byte, stream uint32, payload []byte) []byte {
	frame := make([]byte, 9+len(payload))
	frame[0] = byte(len(payload) >> 16)
	frame[1] = byte(len(payload) >> 8)
	frame[2] = byte(len(payload))
	frame[3], frame[4] = kind, flags
	binary.BigEndian.PutUint32(frame[5:9], stream&0x7fffffff)
	copy(frame[9:], payload)
	return frame
}

func serveRefundH2(conn net.Conn, certificate tls.Certificate, bodies chan<- string, failures chan<- error) {
	defer conn.Close()
	report := func(err error) {
		select {
		case failures <- err:
		default:
		}
	}
	peer := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2"}})
	if err := peer.Handshake(); err != nil {
		report(err)
		return
	}
	if peer.ConnectionState().NegotiatedProtocol != "h2" {
		report(errors.New("HTTP/2 ALPN was not negotiated"))
		return
	}
	const preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	greeting := make([]byte, len(preface))
	if _, err := io.ReadFull(peer, greeting); err != nil || string(greeting) != preface {
		report(fmt.Errorf("HTTP/2 preface: %q, %v", greeting, err))
		return
	}
	writes := make(chan []byte, 8)
	go func() {
		for frame := range writes {
			if _, err := peer.Write(frame); err != nil {
				report(err)
				return
			}
		}
	}()
	defer close(writes)
	writes <- refundH2Frame(4, 0, 0, nil) // SETTINGS
	streams := make(map[uint32][]byte)
	complete := 0
	for {
		var header [9]byte
		if _, err := io.ReadFull(peer, header[:]); err != nil {
			report(err)
			return
		}
		size := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
		if size > 1<<20 {
			report(fmt.Errorf("HTTP/2 frame too large: %d", size))
			return
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(peer, payload); err != nil {
			report(err)
			return
		}
		kind, flags := header[3], header[4]
		stream := binary.BigEndian.Uint32(header[5:9]) & 0x7fffffff
		switch kind {
		case 4: // SETTINGS
			if flags&1 == 0 {
				writes <- refundH2Frame(4, 1, 0, nil)
			}
		case 6: // PING
			if flags&1 == 0 {
				writes <- refundH2Frame(6, 1, 0, payload)
			}
		case 0: // DATA
			if flags&8 != 0 {
				if len(payload) == 0 || int(payload[0])+1 > len(payload) {
					report(errors.New("invalid padded HTTP/2 DATA"))
					return
				}
				payload = payload[1 : len(payload)-int(payload[0])]
			}
			streams[stream] = append(streams[stream], payload...)
			if flags&1 == 0 {
				continue
			}
			complete++
			bodies <- string(streams[stream])
			if complete == 1 {
				// REFUSED_STREAM is a retryable peer error, even after this peer read the body.
				writes <- refundH2Frame(3, 0, stream, []byte{0, 0, 0, 7})
			} else {
				// A forbidden replay would succeed, making the regression observable.
				writes <- refundH2Frame(1, 4, stream, []byte{0x88}) // indexed :status 200
				writes <- refundH2Frame(0, 1, stream, []byte("{}"))
			}
		}
	}
}

func TestRefundHTTP2RefusedAfterBodyDoesNotReplay(t *testing.T) {
	t.Skip("refund creation is OFF; preserve these assertions for a verified transport")
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		DNSNames:              []string{"refund.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
	bodies := make(chan string, 4)
	failures := make(chan error, 4)
	var mu sync.Mutex
	var connections []net.Conn
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			_ = clientConn.SetDeadline(time.Now().Add(10 * time.Second))
			_ = serverConn.SetDeadline(time.Now().Add(10 * time.Second))
			mu.Lock()
			connections = append(connections, clientConn, serverConn)
			mu.Unlock()
			go serveRefundH2(serverConn, certificate, bodies, failures)
			return tls.Client(clientConn, &tls.Config{
				RootCAs: roots, ServerName: "refund.test", NextProtos: []string{"h2"},
			}), nil
		},
	}
	defer func() {
		transport.CloseIdleConnections()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	client, err := NewClient("pk", "sk", WithBaseURL("https://refund.test"),
		WithRetry(2, 0), agentWithOwnedHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Refunds.Create(ctx, "inv-1", RefundFull, "key-1")
	if err == nil || !strings.Contains(err.Error(), "REFUSED_STREAM") {
		select {
		case peerErr := <-failures:
			t.Fatalf("expected refused-stream error, got %v; peer: %v", err, peerErr)
		default:
			t.Fatalf("expected refused-stream error, got %v", err)
		}
	}
	select {
	case body := <-bodies:
		const want = "{\"external_order_id\":\"key-1\",\"invoice_id\":\"inv-1\",\"mode\":\"full\"}"
		if body != want {
			t.Fatalf("wrong HTTP/2 body: %q", body)
		}
	default:
		t.Fatal("HTTP/2 peer did not observe the complete body")
	}
	select {
	case extra := <-bodies:
		t.Fatalf("HTTP/2 replayed a refund body: %q", extra)
	default:
	}
}

func TestRefundLegacyOptionalFieldsAndCurrentFields(t *testing.T) {
	var old Refund
	if err := json.Unmarshal([]byte(`{"withdrawal_id":"wdr_1","invoice_id":"inv_1","mode":"full","amount":"1"}`), &old); err != nil {
		t.Fatal(err)
	}
	var _ *string = old.Type
	var _ *string = old.ProjectID
	if old.Type != nil || old.ProjectID != nil || old.WithdrawalID != "wdr_1" || old.InvoiceID != "inv_1" {
		t.Fatalf("legacy refund changed: %#v", old)
	}
	var current Refund
	if err := json.Unmarshal([]byte(`{"type":"refund","project_id":"prj_1","withdrawal_id":"wdr_1"}`), &current); err != nil {
		t.Fatal(err)
	}
	if current.Type == nil || *current.Type != "refund" || current.ProjectID == nil || *current.ProjectID != "prj_1" {
		t.Fatalf("current refund fields missing: %#v", current)
	}
	payload, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["type"]; exists {
		t.Fatal("legacy absent type must remain absent")
	}
	if _, exists := fields["project_id"]; exists {
		t.Fatal("legacy absent project_id must remain absent")
	}
}

func TestRefundInvalidSimulatorInputDoesNotSend(t *testing.T) {
	calls := 0
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(200, `{}`), nil
	})
	for _, value := range [][2]string{{"", "completed"}, {"wdr_1", ""}, {"wdr_1", "created"}, {"wdr_1", "COMPLETED"}} {
		if _, err := client.Refunds.Simulate(context.Background(), value[0], value[1]); err == nil {
			t.Fatalf("invalid simulation %q/%q succeeded", value[0], value[1])
		}
	}
	if calls != 0 {
		t.Fatalf("invalid simulations made %d transport calls", calls)
	}
}
