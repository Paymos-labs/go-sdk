// Package paymos provides the official server-side Go client for the Paymos
// Merchant API. It includes canonical HMAC signing, typed API errors, bounded
// cursor traversal, safe retries, and raw-body webhook verification.
//
// Payment-channel deposit webhooks decode into WebhookEvent[PaymentChannelDeposit].
// confirming and reorged are advisory and may arrive out of order; credit only on
// confirmed with IsFinal true — see PaymentChannelDeposit for the full rule.
package paymos
