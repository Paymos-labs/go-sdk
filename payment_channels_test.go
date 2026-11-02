package paymos

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// One wire channel in the API's own spellings: a pc_ id, TRC20 as the rail code, fee percents as
// numbers, timestamps as Unix seconds. The second rail has no address and the second token has no
// minimum, because the API OMITS null members rather than sending them.
const channelJSON = `{"id":"pc_1","project_id":"prj_1","external_id":"customer-42",` +
	`"status":"active","is_accepting_payments":true,"is_fully_provisioned":false,"is_test":true,` +
	`"applied_fee_percent":1.5,"customer_fee_percent":0,` +
	`"networks":[{"network":"TRC20","status":"active","address":"TQ1",` +
	`"tokens":[{"symbol":"USDT","minimum_deposit":"1.000000"},{"symbol":"USDC"}]},` +
	`{"network":"BEP20","status":"provisioning","tokens":[]}],` +
	`"created_at":1700000000,"updated_at":1700000100}`

const blockedChannelJSON = `{"id":"pc_1","project_id":"prj_1","external_id":"customer-42",` +
	`"status":"blocked","is_accepting_payments":false,"is_fully_provisioned":true,"is_test":true,` +
	`"applied_fee_percent":1.5,"customer_fee_percent":0,"networks":[],` +
	`"created_at":1700000000,"updated_at":1700000200}`

const depositJSON = `{"id":"pcd_1","payment_channel_id":"pc_1","project_id":"prj_1",` +
	`"payment_channel_external_id":"customer-42","status":"confirmed","is_final":true,"is_test":true,` +
	`"currency":"USDT","network":"TRC20","chain_id":728126428,"gross":"100.000000","fee":"1.500000",` +
	`"net":"98.500000","applied_fee_percent":1.5,"customer_fee_percent":0,"tx_hash":"0xabc",` +
	`"transfer_id":"trf_1","destination_address":"TQ1","block_height":1234,` +
	`"created_at":1700000000,"updated_at":1700000100,"confirmed_at":1700000100}`

func stubClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	client, err := NewClient("pk_test_key", "sk_test_secret",
		agentWithOwnedHTTPClient(roundTripper(handler)),
		WithRetry(0, 0),
		WithClock(func() time.Time { return time.Unix(1700000000, 0) }))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// 1. Creating the same channel twice answers 201 and then 200 — both a channel. An SDK that treats
// the idempotent 200 as anything other than success breaks every merchant who calls create on each
// checkout, which is exactly what the contract invites them to do.
func TestPaymentChannelCreateIsIdempotentAndBothStatusesReturnTheChannel(t *testing.T) {
	statuses := []int{201, 200}
	call := 0
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		status := statuses[call]
		call++
		return jsonResponse(status, channelJSON), nil
	})
	ctx := context.Background()
	params := CreatePaymentChannelParams{ProjectID: "prj_1", ExternalID: "customer-42"}

	created, err := client.PaymentChannels.Create(ctx, params)
	if err != nil || created.ID != "pc_1" {
		t.Fatalf("201 must yield the channel: %#v %v", created, err)
	}
	replayed, err := client.PaymentChannels.Create(ctx, params)
	if err != nil || replayed.ID != "pc_1" {
		t.Fatalf("the idempotent 200 must yield the channel too, not an error: %#v %v", replayed, err)
	}

	if created.CreatedAt != 1700000000 || created.UpdatedAt != 1700000100 {
		t.Fatalf("timestamps are Unix seconds: %#v", created)
	}
	if created.Networks[0].Address == nil || *created.Networks[0].Address != "TQ1" {
		t.Fatalf("a provisioned rail carries its address: %#v", created.Networks[0])
	}
	if created.Networks[1].Address != nil {
		t.Fatalf("an unprovisioned rail has NO address, not an empty one: %#v", created.Networks[1])
	}
	if created.Networks[0].Tokens[1].MinimumDeposit != nil {
		t.Fatalf("an absent minimum stays absent — it never becomes zero: %#v", created.Networks[0].Tokens[1])
	}
	if created.Networks[0].Tokens[0].MinimumDeposit == nil || *created.Networks[0].Tokens[0].MinimumDeposit != "1.000000" {
		t.Fatalf("the minimum is a decimal string: %#v", created.Networks[0].Tokens[0])
	}
}

// 2. Block is not a void call: it answers with the updated channel.
func TestPaymentChannelBlockReturnsTheUpdatedChannel(t *testing.T) {
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, blockedChannelJSON), nil
	})
	channel, err := client.PaymentChannels.Block(context.Background(), "pc_1")
	if err != nil {
		t.Fatal(err)
	}
	if channel.IsAcceptingPayments {
		t.Fatalf("a blocked channel reports it stopped accepting payments: %#v", channel)
	}
	if channel.Status != PaymentChannelBlocked {
		t.Fatalf("expected the blocked status, got %q", channel.Status)
	}
}

// 8. The mirror of the above — both directions are wired, and asserting only one has let the other
// be a silent no-op before.
func TestPaymentChannelUnblockReturnsTheUpdatedChannel(t *testing.T) {
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, channelJSON), nil
	})
	channel, err := client.PaymentChannels.Unblock(context.Background(), "pc_1")
	if err != nil {
		t.Fatal(err)
	}
	if !channel.IsAcceptingPayments || channel.Status != PaymentChannelActive {
		t.Fatalf("an unblocked channel accepts payments again: %#v", channel)
	}
}

// 3. THE feed test. An empty page still hands back a cursor, because feed positions are global and
// this merchant's filter skipped the ones in between. Anyone who modelled the feed on the invoice
// list fails here — Page[T]'s cursor is a *string and invites `if cursor == nil { break }`.
func TestDepositFeedEmptyPageStillCarriesACursor(t *testing.T) {
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"items":[],"next_cursor":"cur_2"}`), nil
	})
	page, err := client.PaymentChannelDeposits.Read(context.Background(), PaymentChannelDepositFeedParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("expected an empty page, got %#v", page.Items)
	}
	if page.NextCursor != "cur_2" {
		t.Fatalf("an empty feed page must still advance the cursor, got %q", page.NextCursor)
	}
	if page.Blocked != nil {
		t.Fatalf("a quiet page is not a blocked page: %#v", page.Blocked)
	}
}

// 4. A stalled feed is byte-identical to a quiet one except for this field, so dropping it leaves a
// merchant unable to tell "no new payments" from "I am not seeing my money".
func TestDepositFeedSurfacesWhichDepositIsBlockedAndWhy(t *testing.T) {
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"items":[],"next_cursor":"cur_2","blocked":{"deposit_id":"pcd_9","reason":"not_confirmed"}}`), nil
	})
	page, err := client.PaymentChannelDeposits.Read(context.Background(), PaymentChannelDepositFeedParams{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Blocked == nil {
		t.Fatal("the blockage must reach the caller, not be dropped")
	}
	if page.Blocked.DepositID != "pcd_9" || page.Blocked.Reason != "not_confirmed" {
		t.Fatalf("both fields must survive: %#v", page.Blocked)
	}
	if page.NextCursor != "cur_2" {
		t.Fatalf("a blockage explains a stall, it never causes one: %q", page.NextCursor)
	}
}

// 5. Every filter reaches the wire in snake_case. The API rejects an unknown query field outright,
// so a camelCase slip is a 400 at a merchant and a green test here if left unasserted.
func TestPaymentChannelFiltersReachTheWireInSnakeCase(t *testing.T) {
	uri := ""
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		uri = r.URL.RequestURI()
		body := `{"items":[],"next_cursor":null}`
		if strings.HasPrefix(r.URL.Path, "/v1/payment-channel-deposits") {
			body = `{"items":[],"next_cursor":"cur_2"}`
		}
		return jsonResponse(200, body), nil
	})
	ctx := context.Background()
	cursor, externalID, projectID, channelID := "cur_1", "customer-42", "prj_1", "pc_1"
	confirmedFrom := int64(1700000000)

	if _, err := client.PaymentChannels.List(ctx, PaymentChannelListParams{
		Limit:      25,
		Cursor:     &cursor,
		Status:     []PaymentChannelStatus{PaymentChannelActive, PaymentChannelBlocked},
		ExternalID: &externalID,
		ProjectID:  &projectID,
	}); err != nil {
		t.Fatal(err)
	}
	const wantChannels = "/v1/payment-channels?cursor=cur_1&external_id=customer-42&limit=25&project_id=prj_1&status=active&status=blocked"
	if uri != wantChannels {
		t.Fatalf("channel filters mismatch:\nwant %s\n got %s", wantChannels, uri)
	}

	if _, err := client.PaymentChannelDeposits.Read(ctx, PaymentChannelDepositFeedParams{
		Limit:            25,
		Cursor:           &cursor,
		ProjectID:        &projectID,
		PaymentChannelID: &channelID,
		ConfirmedFrom:    &confirmedFrom,
	}); err != nil {
		t.Fatal(err)
	}
	const wantFeed = "/v1/payment-channel-deposits?confirmed_from=1700000000&cursor=cur_1&limit=25&payment_channel_id=pc_1&project_id=prj_1"
	if uri != wantFeed {
		t.Fatalf("feed filters mismatch:\nwant %s\n got %s", wantFeed, uri)
	}
	for _, camel := range []string{"externalId", "projectId", "paymentChannelId", "confirmedFrom"} {
		if strings.Contains(wantChannels+wantFeed, camel) {
			t.Fatalf("camelCase spelling %q must never reach the wire", camel)
		}
	}
}

// 6. Ids go through the path encoder, not the query encoder. A space is where the two disagree:
// the path form is %20, the query form is +, and a + in the path is a different resource.
func TestPaymentChannelIdsArePathEncoded(t *testing.T) {
	uri := ""
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		uri = r.URL.RequestURI()
		return jsonResponse(200, channelJSON), nil
	})
	ctx := context.Background()

	if _, err := client.PaymentChannels.Get(ctx, "pc 1"); err != nil {
		t.Fatal(err)
	}
	if uri != "/v1/payment-channels/pc%201" {
		t.Fatalf("channel id must be path-encoded, got %s", uri)
	}

	client = stubClient(t, func(r *http.Request) (*http.Response, error) {
		uri = r.URL.RequestURI()
		return jsonResponse(200, depositJSON), nil
	})
	if _, err := client.PaymentChannelDeposits.Get(ctx, "pcd 1"); err != nil {
		t.Fatal(err)
	}
	if uri != "/v1/payment-channel-deposits/pcd%201" {
		t.Fatalf("deposit id must be path-encoded, got %s", uri)
	}
	if strings.Contains(uri, "+") {
		t.Fatalf("a query-encoded space reached the path: %s", uri)
	}
}

// 7. Simulate always sends the rail, and an unset stage is absent from the body entirely — not
// null, not "". The server's stage is an enum of three, and an empty one is not one of them.
func TestSimulateDepositSendsNetworkAndOmitsAnUnsetStage(t *testing.T) {
	var body map[string]any
	uri := ""
	client := stubClient(t, func(r *http.Request) (*http.Response, error) {
		uri = r.URL.RequestURI()
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		body = nil
		if err = json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		return jsonResponse(200, depositJSON), nil
	})
	ctx := context.Background()

	deposit, err := client.PaymentChannels.SimulateDeposit(ctx, "pc_1", SimulateDepositParams{
		Amount: "100.00", Currency: "USDT", Network: "TRC20",
	})
	if err != nil {
		t.Fatal(err)
	}
	if uri != "/v1/sandbox/payment-channels/pc_1/simulate-deposit" {
		t.Fatalf("simulation is a sandbox route: %s", uri)
	}
	if body["network"] != "TRC20" {
		t.Fatalf("network is required and must be sent: %#v", body)
	}
	if body["amount"] != "100.00" {
		t.Fatalf("the amount stays a decimal string: %#v", body["amount"])
	}
	if _, present := body["stage"]; present {
		t.Fatalf("an unset stage must be omitted, not sent as %#v", body["stage"])
	}
	if deposit.ID != "pcd_1" || deposit.Gross != "100.000000" || !deposit.IsFinal {
		t.Fatalf("the simulated deposit must decode: %#v", deposit)
	}
	if deposit.ConfirmedAt == nil || *deposit.ConfirmedAt != 1700000100 {
		t.Fatalf("confirmed_at is Unix seconds: %#v", deposit.ConfirmedAt)
	}
	if deposit.ContractAddress != nil || deposit.SourceAddress != nil {
		t.Fatalf("omitted members stay nil: %#v", deposit)
	}

	if _, err = client.PaymentChannels.SimulateDeposit(ctx, "pc_1", SimulateDepositParams{
		Amount: "100.00", Currency: "USDT", Network: "TRC20", Stage: SimulateDepositConfirming,
	}); err != nil {
		t.Fatal(err)
	}
	if body["stage"] != "confirming" {
		t.Fatalf("a set stage must be sent verbatim: %#v", body["stage"])
	}

	requests := 0
	client = stubClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(200, depositJSON), nil
	})
	if _, err = client.PaymentChannels.SimulateDeposit(ctx, "pc_1", SimulateDepositParams{
		Amount: "1.00", Currency: "USDT", Network: "TRC20", Stage: "settled",
	}); err == nil {
		t.Fatal("a stage outside the three-value enum must be rejected before the request leaves")
	}
	if requests != 0 {
		t.Fatalf("the invalid stage still hit the network %d times", requests)
	}
}

// 9. The channel LIST ends, and its iterator stops on the null cursor. This assertion deliberately
// has no counterpart for the deposit feed, and the two living in one file is what keeps the
// difference visible to whoever reads this next.
func TestPaymentChannelListIteratorTerminates(t *testing.T) {
	requests := 0
	client := stubClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return jsonResponse(200, `{"items":[`+channelJSON+`],"next_cursor":"cur_2"}`), nil
		}
		return jsonResponse(200, `{"items":[`+blockedChannelJSON+`],"next_cursor":null}`), nil
	})
	iterator := NewPaymentChannelIterator(client.PaymentChannels, PaymentChannelListParams{Limit: 1}, 10)

	seen := 0
	for guard := 0; guard < 20; guard++ {
		channel, ok, err := iterator.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if channel.ID != "pc_1" {
			t.Fatalf("unexpected channel %q", channel.ID)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("expected both pages' items, got %d", seen)
	}
	if requests != 2 {
		t.Fatalf("a null cursor ends the channel list; the iterator made %d requests", requests)
	}
}
