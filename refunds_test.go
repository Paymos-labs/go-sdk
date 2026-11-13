package paymos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
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
	for _, outcome := range []string{"transport", "503", "429"} {
		t.Run(outcome, func(t *testing.T) {
			v := refundContract(t)
			calls := 0
			transportError := errors.New("response lost")
			client, err := NewClient("pk_test_key", "sk_test_secret",
				WithClock(func() time.Time { return time.Unix(1700000000, 0) }), WithRetry(2, 0),
				WithHTTPClient(roundTripper(func(r *http.Request) (*http.Response, error) {
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
			if outcome != "429" {
				if calls != 1 || err == nil {
					t.Fatalf("unknown POST outcome must return without an automatic retry: %d %v", calls, err)
				}
				if outcome == "transport" && !errors.Is(err, transportError) {
					t.Fatalf("transport cause must survive: %v", err)
				}
				refund, err = client.Refunds.Create(ctx, v.Create.Body["invoice_id"], RefundFull, v.Create.Body["external_order_id"])
			}
			if err != nil || calls != 2 || refund.WithdrawalID != "wdr_accepted" || refund.Amount != "120" {
				t.Fatalf("same-key replay must return the accepted refund: %#v %v (%d requests)", refund, err, calls)
			}
		})
	}
}

// A 2xx without a refund ID does not show whether the refund exists. It must reach the caller as
// an error after one request, so the caller replays the same key instead of recording a success.
func TestRefundCreateWithoutIDIsAnUnknownOutcome(t *testing.T) {
	v := refundContract(t)
	for _, body := range []string{"", "{}", "null", `{"withdrawal_id":"  "}`, `{"withdrawal_id":null}`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			client := stubClient(t, func(r *http.Request) (*http.Response, error) {
				assertRefundRequest(t, r, v.Create.Method, v.Create.Path, v.Create.Body)
				calls++
				return jsonResponse(v.Create.NewStatus, body), nil
			})
			refund, err := client.Refunds.Create(context.Background(), v.Create.Body["invoice_id"],
				RefundMode(v.Create.Body["mode"]), v.Create.Body["external_order_id"])
			if err == nil || calls != 1 || !reflect.DeepEqual(refund, Refund{}) {
				t.Fatalf("refund without an ID must be an error after one request: %#v %v (%d requests)", refund, err, calls)
			}
		})
	}
}

func TestRefundValidationKeepsTopLevelProblemAndFieldErrors(t *testing.T) {
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

func TestRefundCreateDoesNotFollowARedirect(t *testing.T) {
	// D44-02: a 307 must not resend the signed refund request to another host.
	elsewhere := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere++
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"withdrawal_id":"wdr_other"}`)
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/refunds", http.StatusTemporaryRedirect)
	}))
	defer api.Close()

	client, err := NewClient("pk_test_x", "sk_test_y", WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Refunds.Create(context.Background(), "inv_1", RefundFull, "order-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTemporaryRedirect {
		t.Fatalf("expected the 307 as an APIError, got %v", err)
	}
	if elsewhere != 0 {
		t.Fatalf("the refund request was resent to the redirect target %d time(s)", elsewhere)
	}
	if _, ok := client.http.(*http.Client); !ok || client.http.(*http.Client).CheckRedirect != nil {
		t.Fatal("the shared client must keep its own redirect policy")
	}
}
