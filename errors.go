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
	Title  string         `json:"title"`
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
	if detail == "" && len(e.Problem.Errors) > 0 {
		detail = e.Problem.Errors[0].Message
	}
	if detail == "" {
		detail = e.Code()
	}
	if detail == "" {
		detail = string(e.Body)
	}
	return fmt.Sprintf("Paymos API %d: %s", e.Status, detail)
}
func (e *APIError) Code() string {
	if len(e.Problem.Errors) > 0 {
		return e.Problem.Errors[0].Code
	}
	return e.Problem.Code
}
func (e *APIError) Field() *string {
	if len(e.Problem.Errors) > 0 {
		return e.Problem.Errors[0].Field
	}
	return e.Problem.Field
}
func (e *APIError) Kind() string {
	switch e.Status {
	case 400:
		return "validation"
	case 401, 403:
		return "authentication"
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
	_ = json.Unmarshal(body, &value.Problem)
	return value
}
