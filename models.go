package paymos

type InvoiceStatus string

const (
	InvoiceAwaitingClient   InvoiceStatus = "awaiting_client"
	InvoiceAwaitingPayment  InvoiceStatus = "awaiting_payment"
	InvoiceConfirming       InvoiceStatus = "confirming"
	InvoiceUnderpaidWaiting InvoiceStatus = "underpaid_waiting"
	InvoicePaid             InvoiceStatus = "paid"
	InvoicePaidOver         InvoiceStatus = "paid_over"
	InvoiceUnderpaid        InvoiceStatus = "underpaid"
	InvoiceExpired          InvoiceStatus = "expired"
	InvoiceCancelled        InvoiceStatus = "cancelled"
)

type WithdrawalStatus string

const (
	WithdrawalCreated       WithdrawalStatus = "created"
	WithdrawalPendingReview WithdrawalStatus = "pending_review"
	WithdrawalSigned        WithdrawalStatus = "signed"
	WithdrawalCancelling    WithdrawalStatus = "cancelling"
	WithdrawalCompleted     WithdrawalStatus = "completed"
	WithdrawalFailed        WithdrawalStatus = "failed"
	WithdrawalCancelled     WithdrawalStatus = "cancelled"
)

type PaymentChannelStatus string

const (
	PaymentChannelActive       PaymentChannelStatus = "active"
	PaymentChannelBlocked      PaymentChannelStatus = "blocked"
	PaymentChannelProvisioning PaymentChannelStatus = "provisioning"
)

type PaymentChannelNetworkStatus string

const (
	PaymentChannelNetworkProvisioning PaymentChannelNetworkStatus = "provisioning"
	PaymentChannelNetworkActive       PaymentChannelNetworkStatus = "active"
)

type PaymentChannelDepositStatus string

const (
	PaymentChannelDepositConfirming PaymentChannelDepositStatus = "confirming"
	PaymentChannelDepositReorged    PaymentChannelDepositStatus = "reorged"
	PaymentChannelDepositConfirmed  PaymentChannelDepositStatus = "confirmed"
)

// SimulateDepositStage is the lifecycle stage the sandbox drives a simulated deposit to. Exactly
// three values exist; anything else is rejected before the request leaves this process.
type SimulateDepositStage string

const (
	SimulateDepositConfirming SimulateDepositStage = "confirming"
	SimulateDepositReorged    SimulateDepositStage = "reorged"
	SimulateDepositConfirmed  SimulateDepositStage = "confirmed"
)

type CreateInvoiceParams struct {
	ProjectID             string  `json:"project_id"`
	Amount                string  `json:"amount"`
	Currency              string  `json:"currency"`
	ExternalOrderID       string  `json:"external_order_id"`
	Network               *string `json:"network,omitempty"`
	AllowMultiplePayments *bool   `json:"allow_multiple_payments,omitempty"`
	CustomerFeePercent    *int    `json:"customer_fee_percent,omitempty"`
	ClientID              *string `json:"client_id,omitempty"`
}

type InvoiceListParams struct {
	Limit           int             `json:"limit,omitempty"`
	Cursor          *string         `json:"cursor,omitempty"`
	Status          []InvoiceStatus `json:"status,omitempty"`
	ExternalOrderID *string         `json:"external_order_id,omitempty"`
	ProjectID       *string         `json:"project_id,omitempty"`
	CreatedFrom     *int64          `json:"created_from,omitempty"`
	CreatedTo       *int64          `json:"created_to,omitempty"`
}

type Order struct {
	ExternalID string  `json:"external_id"`
	ClientID   *string `json:"client_id,omitempty"`
	Amount     string  `json:"amount"`
	Currency   string  `json:"currency"`
	Network    *string `json:"network,omitempty"`
}

type Transfer struct {
	TxHash                  string  `json:"tx_hash"`
	Amount                  string  `json:"amount"`
	Status                  string  `json:"status"`
	CreatedAt               int64   `json:"created_at"`
	ConfirmedAt             *int64  `json:"confirmed_at,omitempty"`
	RequiredConfirmations   *int    `json:"required_confirmations,omitempty"`
	EstimatedConfirmationAt *int64  `json:"estimated_confirmation_at,omitempty"`
	ExplorerURL             *string `json:"explorer_url,omitempty"`
}

type Payment struct {
	Currency        string     `json:"currency"`
	Network         string     `json:"network"`
	ChainID         int64      `json:"chain_id"`
	ContractAddress *string    `json:"contract_address,omitempty"`
	Expected        string     `json:"expected"`
	Address         *string    `json:"address,omitempty"`
	ExchangeRate    *string    `json:"exchange_rate,omitempty"`
	Paid            *string    `json:"paid,omitempty"`
	Remaining       *string    `json:"remaining,omitempty"`
	Fee             *string    `json:"fee,omitempty"`
	Net             *string    `json:"net,omitempty"`
	Transfers       []Transfer `json:"transfers,omitempty"`
}

type Invoice struct {
	InvoiceID   string        `json:"invoice_id"`
	ProjectID   string        `json:"project_id"`
	Status      InvoiceStatus `json:"status"`
	IsFinal     bool          `json:"is_final"`
	IsTest      bool          `json:"is_test"`
	PaymentURL  string        `json:"payment_url"`
	Order       Order         `json:"order"`
	Payment     *Payment      `json:"payment,omitempty"`
	CreatedAt   int64         `json:"created_at"`
	UpdatedAt   int64         `json:"updated_at"`
	ExpiresAt   *int64        `json:"expires_at,omitempty"`
	CompletedAt *int64        `json:"completed_at,omitempty"`
	// PaidLateAt is the invoice-late-payment fact: money crossed the invoice's total after it had
	// already closed as expired/underpaid. Unix seconds; nil while the invoice has
	// no late mark — the server omits the field then. Status keeps its terminal meaning — this marker travels beside it.
	PaidLateAt *int64 `json:"paid_late_at,omitempty"`
}

type InvoiceListItem struct {
	InvoiceID       string        `json:"invoice_id"`
	ProjectID       string        `json:"project_id"`
	ExternalOrderID string        `json:"external_order_id"`
	ClientID        *string       `json:"client_id,omitempty"`
	Status          InvoiceStatus `json:"status"`
	IsFinal         bool          `json:"is_final"`
	IsTest          bool          `json:"is_test"`
	Amount          string        `json:"amount"`
	Currency        string        `json:"currency"`
	Network         *string       `json:"network,omitempty"`
	CreatedAt       int64         `json:"created_at"`
	ExpiresAt       *int64        `json:"expires_at,omitempty"`
	CompletedAt     *int64        `json:"completed_at,omitempty"`
	// PaidLateAt: additive invoice-late-payment mark, Unix seconds; nil while there is no mark (field omitted).
	PaidLateAt *int64 `json:"paid_late_at,omitempty"`
}

type CreateWithdrawalParams struct {
	DestinationAddress string `json:"destination_address"`
	Network            string `json:"network"`
	Currency           string `json:"currency"`
	Amount             string `json:"amount"`
	ExternalOrderID    string `json:"external_order_id"`
}

type WithdrawalListParams struct {
	Limit           int                `json:"limit,omitempty"`
	Cursor          *string            `json:"cursor,omitempty"`
	Status          []WithdrawalStatus `json:"status,omitempty"`
	ExternalOrderID *string            `json:"external_order_id,omitempty"`
	CreatedFrom     *int64             `json:"created_from,omitempty"`
	CreatedTo       *int64             `json:"created_to,omitempty"`
}

type Withdrawal struct {
	WithdrawalID       string           `json:"withdrawal_id"`
	ExternalOrderID    string           `json:"external_order_id"`
	Status             WithdrawalStatus `json:"status"`
	IsFinal            bool             `json:"is_final"`
	IsTest             bool             `json:"is_test"`
	Amount             string           `json:"amount"`
	Fee                *string          `json:"fee,omitempty"`
	Currency           string           `json:"currency"`
	Network            string           `json:"network"`
	DestinationAddress string           `json:"destination_address"`
	TxHash             *string          `json:"tx_hash,omitempty"`
	ExplorerURL        *string          `json:"explorer_url,omitempty"`
	CreatedAt          int64            `json:"created_at"`
	CompletedAt        *int64           `json:"completed_at,omitempty"`
	FailedAt           *int64           `json:"failed_at,omitempty"`
	CancelledAt        *int64           `json:"cancelled_at,omitempty"`
}

type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// CreatePaymentChannelParams is the whole of channel creation. The pair is the idempotency key:
// repeating it answers 200 with the channel that already exists instead of 201 with a duplicate.
type CreatePaymentChannelParams struct {
	ProjectID  string `json:"project_id"`
	ExternalID string `json:"external_id"`
}

// SimulateDepositParams drives one sandbox deposit. Network is REQUIRED — it is the rail the
// simulated money lands on. Stage is optional and defaults to confirmed server-side, so an unset
// stage is omitted from the body entirely rather than sent as an empty string.
type SimulateDepositParams struct {
	Amount   string               `json:"amount"`
	Currency string               `json:"currency"`
	Network  string               `json:"network"`
	Stage    SimulateDepositStage `json:"stage,omitempty"`
}

// PaymentChannelListParams is the complete, closed filter set of the channel list — the API rejects
// an unknown query field outright instead of ignoring it. Status repeats.
type PaymentChannelListParams struct {
	Limit      int                    `json:"limit,omitempty"`
	Cursor     *string                `json:"cursor,omitempty"`
	Status     []PaymentChannelStatus `json:"status,omitempty"`
	ExternalID *string                `json:"external_id,omitempty"`
	ProjectID  *string                `json:"project_id,omitempty"`
}

// PaymentChannelDepositFeedParams is the complete, closed filter set of the deposit feed.
// ConfirmedFrom is an initial-sync lower bound in Unix seconds — it answers "start me here" on the
// very first poll only. Afterwards resume from the cursor the feed hands back, never from a time.
type PaymentChannelDepositFeedParams struct {
	Limit            int     `json:"limit,omitempty"`
	Cursor           *string `json:"cursor,omitempty"`
	ProjectID        *string `json:"project_id,omitempty"`
	PaymentChannelID *string `json:"payment_channel_id,omitempty"`
	ConfirmedFrom    *int64  `json:"confirmed_from,omitempty"`
}

type PaymentChannelToken struct {
	Symbol string `json:"symbol"`
	// MinimumDeposit absent means the rail cannot quote a minimum right now. It never means
	// "no minimum" — a deposit below the live minimum is skipped outright.
	MinimumDeposit *string `json:"minimum_deposit,omitempty"`
}

type PaymentChannelNetwork struct {
	// Network is a rail code, e.g. "TRC20", not a chain nickname.
	Network string                      `json:"network"`
	Status  PaymentChannelNetworkStatus `json:"status"`
	// Address is absent until this rail provisions, and never changes afterwards.
	Address *string               `json:"address,omitempty"`
	Tokens  []PaymentChannelToken `json:"tokens"`
}

type PaymentChannel struct {
	ID                  string                  `json:"id"`
	ProjectID           string                  `json:"project_id"`
	ExternalID          string                  `json:"external_id"`
	Status              PaymentChannelStatus    `json:"status"`
	IsAcceptingPayments bool                    `json:"is_accepting_payments"`
	IsFullyProvisioned  bool                    `json:"is_fully_provisioned"`
	IsTest              bool                    `json:"is_test"`
	AppliedFeePercent   float64                 `json:"applied_fee_percent"`
	CustomerFeePercent  int                     `json:"customer_fee_percent"`
	Networks            []PaymentChannelNetwork `json:"networks"`
	CreatedAt           int64                   `json:"created_at"`
	UpdatedAt           int64                   `json:"updated_at"`
}

// PaymentChannelDeposit is also the full `data` payload of all three payment-channel deposit
// webhook events (payment_channel.deposit.confirming, .reorged, .confirmed), so
// WebhookEvent[PaymentChannelDeposit] decodes any of them with no extra code.
//
// ORDERING — confirming and reorged are ADVISORY and may arrive out of order: a confirming
// can land after the confirmed for the same deposit, and a reorged can be superseded by a
// later confirmed. Credit the merchant only on payment_channel.deposit.confirmed with IsFinal
// true, and never let an advisory event regress a deposit already known to be confirmed.
// Crediting on confirming releases goods against money a reorg can still take back.
type PaymentChannelDeposit struct {
	ID                          string                      `json:"id"`
	PaymentChannelID            string                      `json:"payment_channel_id"`
	ProjectID                   string                      `json:"project_id"`
	PaymentChannelExternalID    string                      `json:"payment_channel_external_id"`
	Status                      PaymentChannelDepositStatus `json:"status"`
	IsFinal                     bool                        `json:"is_final"`
	IsTest                      bool                        `json:"is_test"`
	Currency                    string                      `json:"currency"`
	Network                     string                      `json:"network"`
	ChainID                     int64                       `json:"chain_id"`
	ContractAddress             *string                     `json:"contract_address,omitempty"`
	Gross                       string                      `json:"gross"`
	Fee                         string                      `json:"fee"`
	Net                         string                      `json:"net"`
	AppliedFeePercent           float64                     `json:"applied_fee_percent"`
	CustomerFeePercent          int                         `json:"customer_fee_percent"`
	TxHash                      string                      `json:"tx_hash"`
	TransferID                  string                      `json:"transfer_id"`
	SourceAddress               *string                     `json:"source_address,omitempty"`
	DestinationAddress          string                      `json:"destination_address"`
	BlockHeight                 int64                       `json:"block_height"`
	FirstIncludedBlockTimestamp *int64                      `json:"first_included_block_timestamp,omitempty"`
	ExplorerURL                 *string                     `json:"explorer_url,omitempty"`
	CreatedAt                   int64                       `json:"created_at"`
	UpdatedAt                   int64                       `json:"updated_at"`
	ConfirmedAt                 *int64                      `json:"confirmed_at,omitempty"`
}

// PaymentChannelDepositFeedBlockage names the one publication position the feed could not render,
// and why. Surface it: without it a stuck feed is byte-identical to a quiet day, and a merchant
// cannot tell "no new payments" from "I am not seeing my money".
type PaymentChannelDepositFeedBlockage struct {
	DepositID string `json:"deposit_id"`
	// Reason is a stable code: booked_auth_missing, inconsistent, deposit_missing,
	// channel_missing, transfer_missing or not_confirmed.
	Reason string `json:"reason"`
}

// PaymentChannelDepositFeedPage is deliberately NOT Page[T]. The feed has no last page — money
// keeps arriving — so NextCursor is a plain string that is always present, including on an empty
// page: feed positions are global across all merchants, and an empty page still advances past the
// ones this merchant's filter skipped. Store it and resume from it; never break on it.
type PaymentChannelDepositFeedPage struct {
	Items      []PaymentChannelDeposit            `json:"items"`
	NextCursor string                             `json:"next_cursor"`
	Blocked    *PaymentChannelDepositFeedBlockage `json:"blocked,omitempty"`
}

type Balance struct {
	Currency  string `json:"currency"`
	Available string `json:"available"`
}

type ServerTime struct {
	ServerTime int64 `json:"server_time"`
}

type WebhookEvent[T any] struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	Version    int    `json:"version"`
	OccurredAt int64  `json:"occurred_at"`
	Data       T      `json:"data"`
}
