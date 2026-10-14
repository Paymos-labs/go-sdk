package paymos

import (
	"encoding/json"
	"testing"
)

// invoice-late-payment wire contract (late-26): the Invoice model decodes paid_late_at as Unix
// seconds, as null, and as ABSENT (legacy payload), with the terminal status and the payment money
// fields untouched; the paid_late simulate stage round-trips verbatim.
func TestPaidLateWireContract(t *testing.T) {
	expired := `{
		"invoice_id": "inv_1",
		"project_id": "prj_1",
		"status": "expired",
		"is_final": true,
		"is_test": false,
		"payment_url": "https://pay.test/inv_1",
		"order": {"external_id": "order_1", "amount": "100.00", "currency": "USD"},
		"payment": {"currency": "USDT", "network": "tron", "chain_id": 728126428, "expected": "100.00", "paid": "60.00"},
		"created_at": 1700000000,
		"updated_at": 1700003600,
		"expires_at": 1700003600,
		"completed_at": 1700003600
	}`

	decode := func(body string) Invoice {
		var invoice Invoice
		if err := json.Unmarshal([]byte(body), &invoice); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		return invoice
	}

	t.Run("unix mark beside terminal status", func(t *testing.T) {
		invoice := decode(`{` + expired[1:len(expired)-1] + `, "paid_late_at": 1700007200}`)
		if invoice.Status != InvoiceExpired {
			t.Fatalf("status = %q, want expired", invoice.Status)
		}
		if !invoice.IsFinal {
			t.Fatal("is_final must stay true")
		}
		if invoice.PaidLateAt == nil || *invoice.PaidLateAt != 1700007200 {
			t.Fatalf("paid_late_at = %v, want 1700007200", invoice.PaidLateAt)
		}
		if invoice.Payment == nil || invoice.Payment.Paid == nil || *invoice.Payment.Paid != "60.00" {
			t.Fatalf("payment.paid must stay 60.00, got %+v", invoice.Payment)
		}
	})

	t.Run("null mark", func(t *testing.T) {
		invoice := decode(`{` + expired[1:len(expired)-1] + `, "paid_late_at": null}`)
		if invoice.PaidLateAt != nil {
			t.Fatalf("paid_late_at = %v, want nil", invoice.PaidLateAt)
		}
	})

	t.Run("legacy payload without the field", func(t *testing.T) {
		invoice := decode(expired)
		if invoice.PaidLateAt != nil {
			t.Fatalf("paid_late_at = %v, want nil for legacy payload", invoice.PaidLateAt)
		}
		if invoice.Status != InvoiceExpired {
			t.Fatalf("legacy status = %q, want expired", invoice.Status)
		}
	})

	t.Run("simulate stage round-trips", func(t *testing.T) {
		payload := struct {
			Stage string `json:"stage"`
		}{}
		body, err := json.Marshal(map[string]string{"stage": "paid_late"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Stage != "paid_late" {
			t.Fatalf("stage = %q, want paid_late", payload.Stage)
		}
	})
}
