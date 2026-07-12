# Paymos Go SDK

Official dependency-free Go client for the Paymos Merchant API. All network
methods accept `context.Context`; signing, retries, cursor traversal and webhook
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

```go
verifier, _ := paymos.NewWebhookVerifier(os.Getenv("PAYMOS_WEBHOOK_SECRET"), 5*time.Minute)
if err := verifier.Verify(signatureHeader, rawBody, time.Now()); err != nil {
    // Return 401 without parsing or processing the payload.
}
```

Never place the API secret in a browser or mobile application. Full
documentation: https://paymos.io/docs/server-sdks
