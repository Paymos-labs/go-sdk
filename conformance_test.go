package paymos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) Do(r *http.Request) (*http.Response, error) { return f(r) }

type contractValue struct {
	Vectors struct {
		Post struct {
			APIKey        string `json:"api_key"`
			APISecret     string `json:"api_secret"`
			Timestamp     string `json:"timestamp"`
			Method        string `json:"method"`
			Path          string `json:"path"`
			Query         string `json:"query"`
			Body          string `json:"body"`
			Authorization string `json:"authorization"`
		} `json:"post_signing"`
		PostEmptyBody struct {
			APIKey        string `json:"api_key"`
			APISecret     string `json:"api_secret"`
			Timestamp     string `json:"timestamp"`
			Method        string `json:"method"`
			Path          string `json:"path"`
			Query         string `json:"query"`
			Body          string `json:"body"`
			Authorization string `json:"authorization"`
		} `json:"post_empty_body_signing"`
		Get struct {
			APIKey    string `json:"api_key"`
			APISecret string `json:"api_secret"`
			Timestamp string `json:"timestamp"`
			Method    string `json:"method"`
			Path      string `json:"path"`
			Query     string `json:"query"`
			Body      string `json:"body"`
			Signature string `json:"signature"`
		} `json:"get_query_signing"`
		Webhook struct {
			Secret    string `json:"secret"`
			Header    string `json:"header"`
			RawBody   string `json:"raw_body"`
			Timestamp int64  `json:"timestamp"`
			Now       int64  `json:"now"`
			Tolerance int64  `json:"tolerance_seconds"`
		} `json:"webhook"`
		ProblemDetails struct {
			Flat  map[string]any `json:"flat"`
			Multi map[string]any `json:"multi"`
		} `json:"problem_details"`
	} `json:"vectors"`
}

func contract(t *testing.T) contractValue {
	t.Helper()
	raw, err := os.ReadFile("conformance/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var value contractValue
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestConformanceSigning(t *testing.T) {
	value := contract(t)
	v := value.Vectors.Post
	got := AuthorizationHeader(v.APIKey, v.APISecret, v.Timestamp, v.Method, v.Path, v.Query, []byte(v.Body))
	if got != v.Authorization {
		t.Fatalf("authorization mismatch: %s", got)
	}
	// An absent body signs an EMPTY last segment, never sha256(""). This vector is a POST, so an
	// SDK that shortcuts on the method ("only GET is empty") fails here.
	e := value.Vectors.PostEmptyBody
	if got := AuthorizationHeader(e.APIKey, e.APISecret, e.Timestamp, e.Method, e.Path, e.Query, []byte(e.Body)); got != e.Authorization {
		t.Fatalf("empty-body authorization mismatch: %s", got)
	}
	g := value.Vectors.Get
	if Sign(g.APISecret, StringToSign(g.Timestamp, g.Method, g.Path, g.Query, []byte(g.Body))) != g.Signature {
		t.Fatal("GET signature mismatch")
	}
	query, err := BuildQuery(map[string]any{"status": []string{"paid_over", "paid"}, "project_id": "prj/a", "limit": 50})
	if err != nil || query != g.Query {
		t.Fatalf("query mismatch: %s %v", query, err)
	}
}
func TestConformanceWebhook(t *testing.T) {
	v := contract(t).Vectors.Webhook
	verifier, err := NewWebhookVerifier(v.Secret, time.Duration(v.Tolerance)*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifier.Verify(v.Header, []byte(v.RawBody), time.Unix(v.Now, 0)); err != nil {
		t.Fatal(err)
	}
}

// A verifier not built by NewWebhookVerifier has no secret. A body signed with the empty key at
// the exact current second passes both the zero tolerance and the HMAC comparison, so the
// verifier itself has to refuse before it compares anything.
func TestZeroValueWebhookVerifierRejectsUnconfiguredSecret(t *testing.T) {
	body := []byte(`{"event_id":"evt_1","event_type":"invoice.paid","version":1,"occurred_at":1700000000,"data":{}}`)
	mac := hmac.New(sha256.New, nil)
	_, _ = mac.Write([]byte("1700000000."))
	_, _ = mac.Write(body)
	header := "t=1700000000,v1=" + hex.EncodeToString(mac.Sum(nil))
	now := time.Unix(1700000000, 0)

	var zero WebhookVerifier
	if err := zero.Verify(header, body, now); !errors.Is(err, ErrWebhookNotConfigured) {
		t.Fatalf("zero-value verifier must refuse to verify: %v", err)
	}
	var event WebhookEvent[map[string]any]
	if err := zero.ConstructEvent(header, body, now, &event); !errors.Is(err, ErrWebhookNotConfigured) {
		t.Fatalf("zero-value verifier must refuse to construct an event: %v", err)
	}
	var missing *WebhookVerifier
	if err := missing.Verify(header, body, now); !errors.Is(err, ErrWebhookNotConfigured) {
		t.Fatalf("nil verifier must return an error instead of panicking: %v", err)
	}
}

func TestProblemDetailsUsesTopLevelCode(t *testing.T) {
	vector := contract(t).Vectors.ProblemDetails.Multi
	body, err := json.Marshal(vector)
	if err != nil {
		t.Fatal(err)
	}
	apiError := newAPIError(400, body, make(http.Header))
	if apiError.Code() != "validation_failed" {
		t.Fatalf("top-level code must be authoritative, got %q", apiError.Code())
	}
	if apiError.Field() != nil {
		t.Fatalf("request-level field must come only from the top level, got %q", *apiError.Field())
	}
	if len(apiError.Problem.Errors) != 1 || apiError.Problem.Errors[0].Code != "field_required" {
		t.Fatalf("field errors must remain available: %#v", apiError.Problem.Errors)
	}
}
func TestCompleteResourceRoutes(t *testing.T) {
	calls := []string{}
	transport := roundTripper(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		body := "{}"
		if strings.Contains(r.URL.Path, "balances") {
			body = "[]"
		}
		if r.Method == "GET" && (r.URL.Path == "/v1/invoices" || r.URL.Path == "/v1/withdrawals" || r.URL.Path == "/v1/payment-channels") {
			body = "{\"items\":[],\"next_cursor\":null}"
		}
		if r.Method == "GET" && r.URL.Path == "/v1/payment-channel-deposits" {
			body = "{\"items\":[],\"next_cursor\":\"cur_1\"}"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	client, err := NewClient("pk_test_key", "sk_test_secret", WithHTTPClient(transport), WithRetry(0, 0), WithClock(func() time.Time { return time.Unix(1700000000, 0) }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	client.System.Time(ctx)
	client.Invoices.Create(ctx, CreateInvoiceParams{})
	client.Invoices.Get(ctx, "inv/1")
	client.Invoices.List(ctx, InvoiceListParams{Limit: 1})
	client.Invoices.Cancel(ctx, "inv_1", "reason")
	client.Invoices.ConfirmPayment(ctx, "inv_1", "USDT", "tron")
	client.Invoices.SimulatePayment(ctx, "inv_1", "paid")
	client.Withdrawals.Create(ctx, CreateWithdrawalParams{})
	client.Withdrawals.Get(ctx, "wdr_1")
	client.Withdrawals.List(ctx, WithdrawalListParams{Limit: 1})
	client.Withdrawals.Cancel(ctx, "wdr_1", "reason")
	client.Withdrawals.SimulateCompletion(ctx, "wdr_1")
	client.PaymentChannels.Create(ctx, CreatePaymentChannelParams{})
	client.PaymentChannels.Get(ctx, "pc_1")
	client.PaymentChannels.List(ctx, PaymentChannelListParams{Limit: 1})
	client.PaymentChannels.Block(ctx, "pc_1")
	client.PaymentChannels.Unblock(ctx, "pc_1")
	client.PaymentChannels.SimulateDeposit(ctx, "pc_1", SimulateDepositParams{Amount: "1.00", Currency: "USDT", Network: "TRC20"})
	client.PaymentChannelDeposits.Read(ctx, PaymentChannelDepositFeedParams{Limit: 1})
	client.PaymentChannelDeposits.Get(ctx, "pcd_1")
	client.Balances.Get(ctx)
	expected := []string{
		"GET /v1/time",
		"POST /v1/invoices",
		"GET /v1/invoices/inv%2F1",
		"GET /v1/invoices?limit=1",
		"POST /v1/invoices/inv_1/cancel",
		"POST /v1/invoices/inv_1/confirm-payment",
		"POST /v1/sandbox/invoices/inv_1/simulate-payment",
		"POST /v1/withdrawals",
		"GET /v1/withdrawals/wdr_1",
		"GET /v1/withdrawals?limit=1",
		"POST /v1/withdrawals/wdr_1/cancel",
		"POST /v1/sandbox/withdrawals/wdr_1/simulate-completion",
		"POST /v1/payment-channels",
		"GET /v1/payment-channels/pc_1",
		"GET /v1/payment-channels?limit=1",
		"POST /v1/payment-channels/pc_1/block",
		"POST /v1/payment-channels/pc_1/unblock",
		"POST /v1/sandbox/payment-channels/pc_1/simulate-deposit",
		"GET /v1/payment-channel-deposits?limit=1",
		"GET /v1/payment-channel-deposits/pcd_1",
		"GET /v1/balances",
	}
	if !reflect.DeepEqual(expected, calls) {
		t.Fatalf("route mismatch:\nwant %#v\n got %#v", expected, calls)
	}
}

func TestRetryAndErrorContract(t *testing.T) {
	attempts := 0
	transport := roundTripper(func(r *http.Request) (*http.Response, error) {
		attempts++
		status, body := 429, `{"detail":"slow down"}`
		if attempts == 2 {
			status, body = 200, `{}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	client, err := NewClient("pk", "sk", WithHTTPClient(transport), WithRetry(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Invoices.Create(context.Background(), CreateInvoiceParams{Amount: "10.00"}); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("429 POST should retry once, got %d attempts", attempts)
	}

	attempts = 0
	transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"detail":"retry later"}`)), Header: make(http.Header)}, nil
	})
	client, _ = NewClient("pk", "sk", WithHTTPClient(transport), WithRetry(2, 0))
	_, err = client.Invoices.Create(context.Background(), CreateInvoiceParams{})
	apiError, ok := err.(*APIError)
	if !ok || apiError.Kind() != "unavailable" {
		t.Fatalf("expected unavailable APIError, got %#v", err)
	}
	if attempts != 1 {
		t.Fatalf("POST must not retry generic 503, got %d attempts", attempts)
	}
}

func TestRepeatedInitialCursorIsRejected(t *testing.T) {
	same := "same"
	iterator := newIterator(
		func(context.Context, InvoiceListParams) (Page[InvoiceListItem], error) {
			return Page[InvoiceListItem]{Items: nil, NextCursor: &same}, nil
		},
		InvoiceListParams{Cursor: &same},
		3,
		func(value InvoiceListParams) *string { return value.Cursor },
		func(value *InvoiceListParams, cursor string) { value.Cursor = &cursor },
	)
	if _, _, err := iterator.Next(context.Background()); err == nil {
		t.Fatal("expected repeated cursor error")
	}
}

func TestTypedListQueryAndWebhookEnvelope(t *testing.T) {
	projectID := "prj/a"
	query, err := BuildQuery(InvoiceListParams{
		Limit:     50,
		ProjectID: &projectID,
		Status:    []InvoiceStatus{InvoicePaidOver, InvoicePaid},
	})
	if err != nil || query != "?limit=50&project_id=prj%2Fa&status=paid&status=paid_over" {
		t.Fatalf("typed query mismatch: %s %v", query, err)
	}

	v := contract(t).Vectors.Webhook
	verifier, err := NewWebhookVerifier(v.Secret, time.Duration(v.Tolerance)*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var event WebhookEvent[struct {
		InvoiceID string `json:"invoice_id"`
	}]
	if err = verifier.ConstructEvent(v.Header, []byte(v.RawBody), time.Unix(v.Now, 0), &event); err != nil {
		t.Fatal(err)
	}
	if event.EventID != "evt_123" || event.Data.InvoiceID != "inv_123" {
		t.Fatalf("unexpected typed event: %#v", event)
	}
	duplicateTimestamp := v.Header + ",t=" + strconv.FormatInt(v.Timestamp, 10)
	if err = verifier.Verify(duplicateTimestamp, []byte(v.RawBody), time.Unix(v.Now, 0)); err == nil {
		t.Fatal("duplicate webhook timestamp must be rejected")
	}
}
