package paymos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type Option func(*Client) error

func WithBaseURL(value string) Option {
	return func(c *Client) error {
		value = strings.TrimRight(value, "/")
		if value == "" {
			return errors.New("base URL is required")
		}
		// The API secret signs every request, so the scheme here decides whether that credential
		// is readable in transit. HTTPS in production; plain HTTP only against a loopback host,
		// which is what a local integration test points at. Mirrors the Rust SDK's rule.
		parsed, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("base URL is invalid: %w", err)
		}
		host := strings.ToLower(parsed.Hostname())
		loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
			return errors.New("base URL must use HTTPS (HTTP is allowed only for loopback tests)")
		}
		c.baseURL = value
		return nil
	}
}
func WithHTTPClient(value HTTPDoer) Option {
	return func(c *Client) error {
		if value == nil {
			return errors.New("HTTP client is required")
		}
		c.http = value
		return nil
	}
}
func WithRetry(max int, baseDelay time.Duration) Option {
	return func(c *Client) error {
		if max < 0 || baseDelay < 0 {
			return errors.New("invalid retry options")
		}
		c.maxRetries = max
		c.baseDelay = baseDelay
		return nil
	}
}
func WithClock(value func() time.Time) Option {
	return func(c *Client) error { c.clock = value; return nil }
}

type Client struct {
	apiKey, apiSecret, baseURL string
	http                       HTTPDoer
	maxRetries                 int
	baseDelay                  time.Duration
	clock                      func() time.Time
	Invoices                   *InvoicesService
	Withdrawals                *WithdrawalsService
	Refunds                    *RefundsService
	PaymentChannels            *PaymentChannelsService
	PaymentChannelDeposits     *PaymentChannelDepositsService
	Balances                   *BalancesService
	System                     *SystemService
}

func NewClient(apiKey, apiSecret string, options ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(apiSecret) == "" {
		return nil, errors.New("api key and secret are required")
	}
	c := &Client{apiKey: apiKey, apiSecret: apiSecret, baseURL: "https://api.paymos.io", http: &http.Client{Timeout: 30 * time.Second}, maxRetries: 2, baseDelay: 150 * time.Millisecond, clock: time.Now}
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}
	c.Invoices = &InvoicesService{c}
	c.Withdrawals = &WithdrawalsService{c}
	c.Refunds = &RefundsService{c}
	c.PaymentChannels = &PaymentChannelsService{c}
	c.PaymentChannelDeposits = &PaymentChannelDepositsService{c}
	c.Balances = &BalancesService{c}
	c.System = &SystemService{c}
	return c, nil
}

func (c *Client) request(ctx context.Context, method, path, query string, payload any, target any) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		timestamp := strconv.FormatInt(c.clock().Unix(), 10)
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path+query, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", AuthorizationHeader(c.apiKey, c.apiSecret, timestamp, method, path, query, body))
		req.Header.Set("X-Request-Timestamp", timestamp)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "paymos-go/"+Version)
		response, doErr := c.http.Do(req)
		if doErr != nil {
			if attempt < c.maxRetries && isSafe(method) {
				if err = wait(ctx, c.baseDelay<<attempt); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("Paymos request failed: %w", doErr)
		}
		responseBody, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if attempt < c.maxRetries && (response.StatusCode == 429 || (response.StatusCode >= 500 && isSafe(method))) {
			delay := c.baseDelay << attempt
			if parsed, ok := parseRetryAfter(response.Header.Get("Retry-After")); ok && parsed > delay {
				delay = parsed
			}
			if err = wait(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return newAPIError(response.StatusCode, responseBody, response.Header)
		}
		if target == nil || len(responseBody) == 0 {
			return nil
		}
		if err = json.Unmarshal(responseBody, target); err != nil {
			return fmt.Errorf("Paymos API returned invalid JSON: %w", err)
		}
		return nil
	}
	return errors.New("unreachable")
}
func isSafe(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS":
		return true
	}
	return false
}
func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func parseRetryAfter(value string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second, true
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := time.Until(date)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

type InvoicesService struct{ client *Client }

func (s *InvoicesService) Create(ctx context.Context, payload CreateInvoiceParams) (Invoice, error) {
	return object[Invoice](ctx, s.client, "POST", "/v1/invoices", "", payload)
}
func (s *InvoicesService) Get(ctx context.Context, id string) (Invoice, error) {
	return object[Invoice](ctx, s.client, "GET", "/v1/invoices/"+EncodePathSegment(id), "", nil)
}
func (s *InvoicesService) List(ctx context.Context, filters InvoiceListParams) (Page[InvoiceListItem], error) {
	query, err := BuildQuery(filters)
	if err != nil {
		return Page[InvoiceListItem]{}, err
	}
	var value Page[InvoiceListItem]
	err = s.client.request(ctx, "GET", "/v1/invoices", query, nil, &value)
	return value, err
}
func (s *InvoicesService) Cancel(ctx context.Context, id, reason string) (Invoice, error) {
	if err := validReason(reason); err != nil {
		return Invoice{}, err
	}
	return object[Invoice](ctx, s.client, "POST", "/v1/invoices/"+EncodePathSegment(id)+"/cancel", "", map[string]string{"reason": reason})
}
func (s *InvoicesService) ConfirmPayment(ctx context.Context, id, currency, network string) (Invoice, error) {
	return object[Invoice](ctx, s.client, "POST", "/v1/invoices/"+EncodePathSegment(id)+"/confirm-payment", "", map[string]string{"currency": currency, "network": network})
}
func (s *InvoicesService) SimulatePayment(ctx context.Context, id, stage string) (Invoice, error) {
	switch stage {
	case "paid", "overpaid", "underpay", "cancel", "paid_late":
	default:
		return Invoice{}, errors.New("invalid simulation stage")
	}
	return object[Invoice](ctx, s.client, "POST", "/v1/sandbox/invoices/"+EncodePathSegment(id)+"/simulate-payment", "", map[string]string{"stage": stage})
}

type WithdrawalsService struct{ client *Client }

func (s *WithdrawalsService) Create(ctx context.Context, payload CreateWithdrawalParams) (Withdrawal, error) {
	return object[Withdrawal](ctx, s.client, "POST", "/v1/withdrawals", "", payload)
}
func (s *WithdrawalsService) Get(ctx context.Context, id string) (Withdrawal, error) {
	return object[Withdrawal](ctx, s.client, "GET", "/v1/withdrawals/"+EncodePathSegment(id), "", nil)
}
func (s *WithdrawalsService) List(ctx context.Context, filters WithdrawalListParams) (Page[Withdrawal], error) {
	query, err := BuildQuery(filters)
	if err != nil {
		return Page[Withdrawal]{}, err
	}
	var value Page[Withdrawal]
	err = s.client.request(ctx, "GET", "/v1/withdrawals", query, nil, &value)
	return value, err
}
func (s *WithdrawalsService) Cancel(ctx context.Context, id, reason string) (Withdrawal, error) {
	if err := validReason(reason); err != nil {
		return Withdrawal{}, err
	}
	return object[Withdrawal](ctx, s.client, "POST", "/v1/withdrawals/"+EncodePathSegment(id)+"/cancel", "", map[string]string{"reason": reason})
}
func (s *WithdrawalsService) SimulateCompletion(ctx context.Context, id string) (Withdrawal, error) {
	return object[Withdrawal](ctx, s.client, "POST", "/v1/sandbox/withdrawals/"+EncodePathSegment(id)+"/simulate-completion", "", nil)
}

type RefundsService struct{ client *Client }

// Simulate sets a terminal Sandbox refund status using the scoped Payment credential.
func (s *RefundsService) Simulate(ctx context.Context, id, status string) (Refund, error) {
	if strings.TrimSpace(id) == "" || (status != "completed" && status != "failed" && status != "cancelled") {
		return Refund{}, errors.New("a refund ID and terminal simulation status are required")
	}
	return object[Refund](ctx, s.client, "POST", "/v1/sandbox/refunds/"+EncodePathSegment(id)+"/simulate", "", map[string]string{"status": status})
}

// Create uses a caller-owned idempotency key; amounts and the recipient come from the server.
func (s *RefundsService) Create(ctx context.Context, invoiceID string, mode RefundMode, externalOrderID string) (Refund, error) {
	switch mode {
	case RefundFull, RefundOverpayment:
	default:
		return Refund{}, errors.New("invalid refund mode")
	}
	return object[Refund](ctx, s.client, "POST", "/v1/refunds", "", map[string]string{
		"invoice_id": invoiceID, "mode": string(mode), "external_order_id": externalOrderID,
	})
}
func (s *RefundsService) Get(ctx context.Context, id string) (Refund, error) {
	return object[Refund](ctx, s.client, "GET", "/v1/refunds/"+EncodePathSegment(id), "", nil)
}
func (s *RefundsService) List(ctx context.Context, filters RefundListParams) (Page[Refund], error) {
	query, err := BuildQuery(filters)
	if err != nil {
		return Page[Refund]{}, err
	}
	var value Page[Refund]
	err = s.client.request(ctx, "GET", "/v1/refunds", query, nil, &value)
	return value, err
}

type PaymentChannelsService struct{ client *Client }

// Create makes a channel for a payer — or hands back the one that already exists. The pair
// project_id + external_id is the idempotency key, so a replay answers 200 with the same channel
// instead of 201 with a duplicate. Both are a channel; a caller never inspects the status.
func (s *PaymentChannelsService) Create(ctx context.Context, payload CreatePaymentChannelParams) (PaymentChannel, error) {
	return object[PaymentChannel](ctx, s.client, "POST", "/v1/payment-channels", "", payload)
}
func (s *PaymentChannelsService) Get(ctx context.Context, id string) (PaymentChannel, error) {
	return object[PaymentChannel](ctx, s.client, "GET", "/v1/payment-channels/"+EncodePathSegment(id), "", nil)
}

// List is an ordinary keyset page: NextCursor is nil on the last one. Contrast
// PaymentChannelDepositsService.Read, whose cursor never terminates.
func (s *PaymentChannelsService) List(ctx context.Context, filters PaymentChannelListParams) (Page[PaymentChannel], error) {
	query, err := BuildQuery(filters)
	if err != nil {
		return Page[PaymentChannel]{}, err
	}
	var value Page[PaymentChannel]
	err = s.client.request(ctx, "GET", "/v1/payment-channels", query, nil, &value)
	return value, err
}

// Block stops the channel accepting payments and returns the updated channel — it is not a void call.
func (s *PaymentChannelsService) Block(ctx context.Context, id string) (PaymentChannel, error) {
	return object[PaymentChannel](ctx, s.client, "POST", "/v1/payment-channels/"+EncodePathSegment(id)+"/block", "", nil)
}
func (s *PaymentChannelsService) Unblock(ctx context.Context, id string) (PaymentChannel, error) {
	return object[PaymentChannel](ctx, s.client, "POST", "/v1/payment-channels/"+EncodePathSegment(id)+"/unblock", "", nil)
}

// SimulateDeposit is sandbox only — a production credential gets an error, not a deposit.
func (s *PaymentChannelsService) SimulateDeposit(ctx context.Context, id string, payload SimulateDepositParams) (PaymentChannelDeposit, error) {
	switch payload.Stage {
	case "", SimulateDepositConfirming, SimulateDepositReorged, SimulateDepositConfirmed:
	default:
		return PaymentChannelDeposit{}, errors.New("invalid simulation stage")
	}
	return object[PaymentChannelDeposit](ctx, s.client, "POST", "/v1/sandbox/payment-channels/"+EncodePathSegment(id)+"/simulate-deposit", "", payload)
}

type PaymentChannelDepositsService struct{ client *Client }

func (s *PaymentChannelDepositsService) Get(ctx context.Context, id string) (PaymentChannelDeposit, error) {
	return object[PaymentChannelDeposit](ctx, s.client, "GET", "/v1/payment-channel-deposits/"+EncodePathSegment(id), "", nil)
}

// Read reads ONE page of the confirmed-deposit feed. It is deliberately not called List and
// deliberately has no iterator: the feed never ends, so a helper that looped until the cursor was
// empty would either loop forever or — worse — stop on the first quiet page and leave a merchant's
// reconciliation silently behind. Store the returned cursor and pass it back on the next poll.
func (s *PaymentChannelDepositsService) Read(ctx context.Context, filters PaymentChannelDepositFeedParams) (PaymentChannelDepositFeedPage, error) {
	query, err := BuildQuery(filters)
	if err != nil {
		return PaymentChannelDepositFeedPage{}, err
	}
	var value PaymentChannelDepositFeedPage
	err = s.client.request(ctx, "GET", "/v1/payment-channel-deposits", query, nil, &value)
	return value, err
}

type BalancesService struct{ client *Client }

func (s *BalancesService) Get(ctx context.Context) ([]Balance, error) {
	var value []Balance
	err := s.client.request(ctx, "GET", "/v1/balances", "", nil, &value)
	return value, err
}

type SystemService struct{ client *Client }

func (s *SystemService) Time(ctx context.Context) (ServerTime, error) {
	return object[ServerTime](ctx, s.client, "GET", "/v1/time", "", nil)
}
func object[T any](ctx context.Context, c *Client, method, path, query string, payload any) (T, error) {
	var value T
	err := c.request(ctx, method, path, query, payload, &value)
	return value, err
}
func validReason(reason string) error {
	if strings.TrimSpace(reason) == "" || len(reason) > 500 {
		return errors.New("cancellation reason must contain 1 to 500 characters")
	}
	return nil
}

type Iterator[T any, F any] struct {
	fetch                  func(context.Context, F) (Page[T], error)
	filters                F
	readCursor             func(F) *string
	writeCursor            func(*F, string)
	maxPages, pages, index int
	page                   Page[T]
	cursor                 *string
	done                   bool
}

func NewInvoiceIterator(service *InvoicesService, filters InvoiceListParams, maxPages int) *Iterator[InvoiceListItem, InvoiceListParams] {
	return newIterator(service.List, filters, maxPages,
		func(value InvoiceListParams) *string { return value.Cursor },
		func(value *InvoiceListParams, cursor string) { value.Cursor = &cursor },
	)
}
func NewWithdrawalIterator(service *WithdrawalsService, filters WithdrawalListParams, maxPages int) *Iterator[Withdrawal, WithdrawalListParams] {
	return newIterator(service.List, filters, maxPages,
		func(value WithdrawalListParams) *string { return value.Cursor },
		func(value *WithdrawalListParams, cursor string) { value.Cursor = &cursor },
	)
}
func NewRefundIterator(service *RefundsService, filters RefundListParams, maxPages int) *Iterator[Refund, RefundListParams] {
	return newIterator(service.List, filters, maxPages,
		func(value RefundListParams) *string { return value.Cursor },
		func(value *RefundListParams, cursor string) { value.Cursor = &cursor },
	)
}

// NewPaymentChannelIterator walks the channel LIST, which ends: it stops when the page hands back a
// null cursor. There is deliberately no counterpart for the deposit feed — see
// PaymentChannelDepositsService.Read.
func NewPaymentChannelIterator(service *PaymentChannelsService, filters PaymentChannelListParams, maxPages int) *Iterator[PaymentChannel, PaymentChannelListParams] {
	return newIterator(service.List, filters, maxPages,
		func(value PaymentChannelListParams) *string { return value.Cursor },
		func(value *PaymentChannelListParams, cursor string) { value.Cursor = &cursor },
	)
}
func newIterator[T any, F any](fetch func(context.Context, F) (Page[T], error), filters F, maxPages int, readCursor func(F) *string, writeCursor func(*F, string)) *Iterator[T, F] {
	iterator := &Iterator[T, F]{fetch: fetch, filters: filters, maxPages: maxPages, readCursor: readCursor, writeCursor: writeCursor}
	if value := readCursor(filters); value != nil && *value != "" {
		cursor := *value
		iterator.cursor = &cursor
	}
	return iterator
}
func (i *Iterator[T, F]) Next(ctx context.Context) (T, bool, error) {
	var zero T
	if i.maxPages < 1 {
		return zero, false, errors.New("maxPages must be positive")
	}
	for {
		if i.index < len(i.page.Items) {
			value := i.page.Items[i.index]
			i.index++
			return value, true, nil
		}
		if i.done || i.pages >= i.maxPages {
			return zero, false, nil
		}
		page, err := i.fetch(ctx, i.filters)
		if err != nil {
			return zero, false, err
		}
		i.pages++
		i.page = page
		i.index = 0
		if page.NextCursor == nil || *page.NextCursor == "" {
			i.done = true
		} else {
			if i.cursor != nil && *page.NextCursor == *i.cursor {
				return zero, false, errors.New("Paymos API returned the same pagination cursor twice")
			}
			cursor := *page.NextCursor
			i.cursor = &cursor
			i.writeCursor(&i.filters, cursor)
		}
	}
}
