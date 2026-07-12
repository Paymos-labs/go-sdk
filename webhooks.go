package paymos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrWebhookSignature = errors.New("Paymos webhook signature mismatch")
var ErrWebhookTimestamp = errors.New("Paymos webhook timestamp outside tolerance")

type WebhookVerifier struct {
	secret    []byte
	tolerance time.Duration
}

func NewWebhookVerifier(secret string, tolerance time.Duration) (*WebhookVerifier, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("webhook secret is required")
	}
	if tolerance < 0 {
		return nil, errors.New("webhook tolerance cannot be negative")
	}
	return &WebhookVerifier{secret: []byte(secret), tolerance: tolerance}, nil
}
func (v *WebhookVerifier) Verify(header string, body []byte, now time.Time) error {
	timestamp, signatures, err := parseWebhookHeader(header)
	if err != nil {
		return err
	}
	if delta := now.Sub(time.Unix(timestamp, 0)); delta > v.tolerance || delta < -v.tolerance {
		return ErrWebhookTimestamp
	}
	mac := hmac.New(sha256.New, v.secret)
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	for _, raw := range signatures {
		candidate, err := hex.DecodeString(raw)
		if err == nil && hmac.Equal(expected, candidate) {
			return nil
		}
	}
	return ErrWebhookSignature
}
func (v *WebhookVerifier) ConstructEvent(header string, body []byte, now time.Time, target any) error {
	if err := v.Verify(header, body, now); err != nil {
		return err
	}
	if target == nil {
		return errors.New("webhook event target is required")
	}
	var envelope struct {
		EventID    string          `json:"event_id"`
		EventType  string          `json:"event_type"`
		Version    int             `json:"version"`
		OccurredAt int64           `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("invalid Paymos webhook JSON: %w", err)
	}
	if envelope.EventID == "" || envelope.EventType == "" || envelope.Version < 1 || envelope.OccurredAt < 0 || len(envelope.Data) == 0 || envelope.Data[0] != '{' {
		return errors.New("invalid Paymos webhook event envelope")
	}
	return json.Unmarshal(body, target)
}
func parseWebhookHeader(header string) (int64, []string, error) {
	var timestamp int64
	found := false
	signatures := []string{}
	for _, part := range strings.Split(header, ",") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		if pair[0] == "t" {
			if found {
				return 0, nil, ErrWebhookSignature
			}
			value, err := strconv.ParseInt(pair[1], 10, 64)
			if err != nil || value < 0 {
				return 0, nil, ErrWebhookSignature
			}
			timestamp = value
			found = true
		} else if pair[0] == "v1" && pair[1] != "" {
			signatures = append(signatures, pair[1])
		}
	}
	if !found || len(signatures) == 0 {
		return 0, nil, ErrWebhookSignature
	}
	return timestamp, signatures, nil
}
