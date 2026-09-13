package paymos

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type ProblemError struct {
	Code    string  `json:"code"`
	Field   *string `json:"field"`
	Message string  `json:"message"`
}
type Problem struct {
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Status int            `json:"status"`
	Detail string         `json:"detail"`
	Code   string         `json:"code"`
	Field  *string        `json:"field"`
	Errors []ProblemError `json:"errors"`
}

type APIError struct {
	Status  int
	Body    []byte
	Header  http.Header
	Problem Problem
}

func (e *APIError) Error() string {
	detail := e.Problem.Detail
	if detail == "" {
		detail = e.Code()
	}
	if detail == "" {
		detail = string(e.Body)
	}
	return fmt.Sprintf("Paymos API %d: %s", e.Status, detail)
}
func (e *APIError) Code() string {
	return e.Problem.Code
}
func (e *APIError) Field() *string {
	return e.Problem.Field
}
func (e *APIError) Kind() string {
	switch e.Status {
	case 400:
		return "validation"
	case 401:
		return "authentication"
	// 403 is its own category: the credential was accepted and something else refused. It carries
	// wrong-key-type (payout_key_required, payment_key_required, forbidden) next to plain business state
	// (whitelist_required, merchant_suspended, widget_inactive). Reporting an unwhitelisted payout
	// address as "authentication" sends the on-call engineer to rotate perfectly good credentials.
	// Switch on Code() for the specific remedy.
	case 403:
		return "permission"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 410:
		return "gone"
	case 429:
		return "rate_limit"
	case 503:
		return "unavailable"
	default:
		if e.Status >= 500 {
			return "server"
		}
		return "api"
	}
}
func (e *APIError) RetryAfter() (time.Duration, bool) {
	value := e.Header.Get("Retry-After")
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
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

func newAPIError(status int, body []byte, header http.Header) *APIError {
	value := &APIError{Status: status, Body: append([]byte(nil), body...), Header: header.Clone()}
	var wire struct {
		Type   *string        `json:"type"`
		Title  *string        `json:"title"`
		Status *int           `json:"status"`
		Detail *string        `json:"detail"`
		Code   *string        `json:"code"`
		Field  *string        `json:"field"`
		Errors []ProblemError `json:"errors"`
	}
	if json.Unmarshal(body, &wire) == nil && wire.Status != nil && *wire.Status == status &&
		wire.Type != nil && wire.Title != nil && wire.Detail != nil && wire.Code != nil {
		value.Problem = Problem{
			Type: *wire.Type, Title: *wire.Title, Status: *wire.Status,
			Detail: *wire.Detail, Code: *wire.Code, Field: wire.Field, Errors: wire.Errors,
		}
	}
	return value
}
