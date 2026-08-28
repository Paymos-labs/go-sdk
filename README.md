# Paymos Go SDK

Official dependency-free Go client for the Paymos Merchant API — invoices,
withdrawals, balances, and static per-customer wallets with their confirmed-deposit
feed. All network methods accept `context.Context`; signing, retries, cursor traversal and webhook
verification follow the shared Paymos SDK conformance contract.

```bash
go get github.com/Paymos-labs/go-sdk/v2@latest
```

```go
import paymos "github.com/Paymos-labs/go-sdk/v2"

client, _ := paymos.NewClient("pk_test_...", "sk_test_...")
invoice, err := client.Invoices.Create(ctx, paymos.CreateInvoiceParams{
	ProjectID: "prj_...", Amount: "10.00", Currency: "USD",
	ExternalOrderID: "order_123",
})
```

Use `NewInvoiceIterator` and `NewWithdrawalIterator` for bounded cursor
traversal. Non-success responses return `*paymos.APIError`, which preserves the
status, response body, problem details, error kind, field, and `Retry-After`.

A payment channel is one payer's reusable set of deposit addresses. Create it,
read its rails, then poll the confirmed-deposit feed and persist the cursor:

```go
channel, err := client.PaymentChannels.Create(ctx, paymos.CreatePaymentChannelParams{
	ProjectID: "prj_...", ExternalID: "customer_42",
})
for _, rail := range channel.Networks {
	if rail.Address == nil {
		continue // this rail has not provisioned yet
	}
	fmt.Println(rail.Network, rail.Status, *rail.Address)
}

feed, err := client.PaymentChannelDeposits.Read(ctx, paymos.PaymentChannelDepositFeedParams{
	Cursor: savedCursor, // *string, nil on the very first poll
	Limit:  100,
})
for _, deposit := range feed.Items {
	credit(deposit)
}
saveCursor(feed.NextCursor)
```

Four things that bite a caller who guesses. Repeating the same `ExternalID`
returns the **same** channel — `200` instead of `201`, and both bodies are a
channel, so calling this on every checkout is safe: a repeat is not a duplicate
and not an error. A rail's `Address` is nil until that rail finishes
provisioning and never changes once set, so nil means "not yet", not "no
address". A nil `MinimumDeposit` means "we cannot quote a minimum right now",
never "there is no minimum" — reading it as zero is how a merchant accepts a
deposit that lands below the live minimum and is never credited. And
`NextCursor` is never empty, not even on a page with no items: store it and
resume from it, and do not loop until it is empty the way `NewInvoiceIterator`
does, because that loop either spins forever or stops on the first quiet page
and leaves reconciliation silently behind. `ConfirmedFrom` is the first poll's
lower bound only; afterwards the stored cursor is the resume mechanism.

```go
verifier, _ := paymos.NewWebhookVerifier(os.Getenv("PAYMOS_WEBHOOK_SECRET"), 5*time.Minute)
if err := verifier.Verify(signatureHeader, rawBody, time.Now()); err != nil {
    // Return 401 without parsing or processing the payload.
}
```

A payment-channel event's `data` is always a full `PaymentChannelDeposit`, so
`WebhookEvent[PaymentChannelDeposit]` decodes any of the three
`payment_channel.deposit.*` events with no extra code:

```go
var event paymos.WebhookEvent[paymos.PaymentChannelDeposit]
if err := verifier.ConstructEvent(signatureHeader, rawBody, time.Now(), &event); err != nil {
    // Return 401 without processing the payload.
}
```

`confirming` and `reorged` are advisory and may arrive out of order — a
`confirming` can land after the `confirmed` for the same deposit, and a
`reorged` can be superseded by a later `confirmed`. Credit only on
`payment_channel.deposit.confirmed` with `IsFinal` true, and never let an
advisory event regress a deposit you already know is confirmed. Crediting on
`confirming` releases goods against money a reorg can still take back.

Never place the API secret in a browser or mobile application. Full
documentation: https://paymos.io/docs/server-sdks
