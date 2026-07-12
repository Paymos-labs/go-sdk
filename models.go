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
