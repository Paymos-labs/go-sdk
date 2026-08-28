package paymos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

// The same TRX-native deposit fixture the other SDKs' webhook round-trip test uses: no contract
// (native coin, not a token), no attributable payer address, no explorer link minted yet, and no
// first-included-block timestamp. All four are OMITTED from the JSON below, never sent as null —
// PaymentChannelDeposit's pointer optionals must decode them as nil, not as a zero value.
const paymentChannelDepositWebhookBody = `{"event_id":"evt_2","event_type":"payment_channel.deposit.confirmed",` +
	`"version":1,"occurred_at":1700000000,"data":{"id":"pcd_9","payment_channel_id":"pc_1",` +
	`"project_id":"prj_1","payment_channel_external_id":"customer-42","status":"confirmed",` +
	`"is_final":true,"is_test":false,"currency":"TRX","network":"TRC20","chain_id":728126428,` +
	`"gross":"100.00","fee":"1.00","net":"99.00","applied_fee_percent":1,"customer_fee_percent":0,` +
	`"tx_hash":"9a2f1c","transfer_id":"trf_1","destination_address":"TChannelAddress",` +
	`"block_height":60000000,"created_at":1700000000,"updated_at":1700000100,"confirmed_at":1700000100}}`

// TestPaymentChannelDepositWebhookRoundTrips proves WebhookEvent[PaymentChannelDeposit] needs no
// new decoding code: the existing generic envelope plus the existing deposit model is the whole
// payload. It goes through the real verifier — NewWebhookVerifier + ConstructEvent — the same way
// TestTypedListQueryAndWebhookEnvelope in conformance_test.go exercises WebhookEvent[struct{...}],
// so this proves the signed-and-verified path, not just json.Unmarshal. Unlike that test, this
// fixture is not one of the vectors in conformance/contract.json (it is a
// payment_channel.deposit.confirmed body, not the shared invoice.paid vector), so the signature is
// computed inline here using the same "timestamp.body" HMAC construction webhooks.go signs with.
func TestPaymentChannelDepositWebhookRoundTrips(t *testing.T) {
	const secret = "whsec_test"
	const timestamp int64 = 1700000000
	body := []byte(paymentChannelDepositWebhookBody)

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(body)
	header := fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))

	verifier, err := NewWebhookVerifier(secret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	var event WebhookEvent[PaymentChannelDeposit]
	if err = verifier.ConstructEvent(header, body, time.Unix(timestamp, 0), &event); err != nil {
		t.Fatal(err)
	}

	if event.EventType != "payment_channel.deposit.confirmed" {
		t.Fatalf("unexpected event type: %q", event.EventType)
	}

	deposit := event.Data
	if deposit.ID != "pcd_9" {
		t.Fatalf("unexpected deposit id: %q", deposit.ID)
	}
	if !deposit.IsFinal {
		t.Fatal("a payment_channel.deposit.confirmed webhook must carry is_final true")
	}

	// Money survives as decimal strings, never a float. Gross/Fee/Net are typed `string` on
	// PaymentChannelDeposit, so this comparison is also the type proof: had a decoder turned them
	// into float64, comparing the result against a string literal would not compile.
	if deposit.Gross != "100.00" || deposit.Fee != "1.00" || deposit.Net != "99.00" {
		t.Fatalf("money must survive as decimal strings: gross=%q fee=%q net=%q", deposit.Gross, deposit.Fee, deposit.Net)
	}

	// A native-coin deposit has no contract, no attributable source address, no explorer link
	// yet, and no first-included-block timestamp. All four are omitted from the JSON above, not
	// sent as null, so the pointer optionals must decode to nil, never to a pointer at a zero value.
	if deposit.ContractAddress != nil {
		t.Fatalf("contract_address must stay absent for a native-coin deposit, got %q", *deposit.ContractAddress)
	}
	if deposit.SourceAddress != nil {
		t.Fatalf("source_address must stay absent, got %q", *deposit.SourceAddress)
	}
	if deposit.ExplorerURL != nil {
		t.Fatalf("explorer_url must stay absent, got %q", *deposit.ExplorerURL)
	}
	if deposit.FirstIncludedBlockTimestamp != nil {
		t.Fatalf("first_included_block_timestamp must stay absent, got %d", *deposit.FirstIncludedBlockTimestamp)
	}
}
