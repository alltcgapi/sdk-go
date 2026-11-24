package alltcgapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// APIError is the error envelope the API returns on every 4xx and 5xx. The
// typed wrappers in this file (NotFoundError, RateLimitedError, ...) embed it,
// so errors.As works both on the wrapper and on *APIError itself.
//
// Three errors look alike and are handled in opposite ways. RateLimitedError
// (429 RATE_LIMITED, CONCURRENCY_LIMIT) passes by waiting Retry-After, and the
// client retries it. QuotaExceededError (429 QUOTA_EXCEEDED) carries a
// Retry-After that points to midnight UTC or to the renewal, and the client
// never retries it. TrialExpiredError (403 TRIAL_EXPIRED) never passes: trial
// credits do not come back, a plan is needed.
type APIError struct {
	// Code is the stable code of the error taxonomy, e.g. CARD_NOT_FOUND.
	// Branch on it, not on the message.
	Code    string
	Status  int
	Message string
	// RequestID is set whenever the response came from the API. Quote it in a
	// support message: it is the only thing that can be looked up in the logs.
	RequestID string
	Details   map[string]any
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Param is details.param: the parameter the API refused, when it names one.
func (e *APIError) Param() string {
	s, _ := e.Details["param"].(string)
	return s
}

// AuthenticationError is a 401: the key is missing, malformed, revoked or expired.
type AuthenticationError struct{ *APIError }

func (e *AuthenticationError) Unwrap() error { return e.APIError }

// PermissionDeniedError is a 403: the key is valid but may not do this (INSUFFICIENT_SCOPE).
type PermissionDeniedError struct{ *APIError }

func (e *PermissionDeniedError) Unwrap() error { return e.APIError }

// TrialExpiredError is 403 TRIAL_EXPIRED: the 800 trial credits are used up
// and do not renew. Waiting and retrying do not help; a plan does
// (client.Plans, or Details["plans"]). It unwraps to *PermissionDeniedError.
type TrialExpiredError struct{ *PermissionDeniedError }

func (e *TrialExpiredError) Unwrap() error { return e.PermissionDeniedError }

// NotFoundError is a 404, or any code ending in _NOT_FOUND. Not a network
// error: it is never retried.
type NotFoundError struct{ *APIError }

func (e *NotFoundError) Unwrap() error { return e.APIError }

// InvalidRequestError is a 400: an unknown parameter, a repeated one, a value
// outside its vocabulary. Param names it when the API says which one.
type InvalidRequestError struct{ *APIError }

func (e *InvalidRequestError) Unwrap() error { return e.APIError }

// RateLimitedError is a 429 RATE_LIMITED or CONCURRENCY_LIMIT: it passes by waiting.
type RateLimitedError struct {
	*APIError
	// RetryAfter is the wait the API asked for, valid when HasRetryAfter is true.
	RetryAfter    time.Duration
	HasRetryAfter bool
}

func (e *RateLimitedError) Unwrap() error { return e.APIError }

// QuotaExceededError is a 429 QUOTA_EXCEEDED: the daily cap is reached or the
// period's credits are used up. The client never retries it.
type QuotaExceededError struct{ *APIError }

func (e *QuotaExceededError) Unwrap() error { return e.APIError }

// ServerError is any 5xx, including 503 METER_UNAVAILABLE, which is transient and retried.
type ServerError struct{ *APIError }

func (e *ServerError) Unwrap() error { return e.APIError }

// ConnectionError means the request never got an answer: DNS, TLS, socket.
type ConnectionError struct {
	URL string
	Err error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("alltcgapi: request to %s failed: %v", e.URL, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// TimeoutError means the request was abandoned by the client after Timeout.
// It unwraps to *ConnectionError.
type TimeoutError struct {
	*ConnectionError
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("alltcgapi: request to %s timed out after %s", e.URL, e.Timeout)
}

func (e *TimeoutError) Unwrap() error { return e.ConnectionError }

type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	RequestID string         `json:"request_id"`
}

// fallbackErrorBody is used when the body is not the API envelope (an HTML
// 502 from a proxy, say): the status still becomes an error instead of a
// decoding failure that hides it.
func fallbackErrorBody(status int) errorBody {
	message := http.StatusText(status)
	if message == "" {
		message = fmt.Sprintf("HTTP %d", status)
	}
	return errorBody{Code: fmt.Sprintf("HTTP_%d", status), Message: message}
}

// toAPIError picks the class from the body and the status: the code first,
// then the status. The status says the family, the code says the case, and
// the case that matters most (rate versus quota) shares a status.
func toAPIError(status int, body errorBody, retryAfter *time.Duration) error {
	base := &APIError{Code: body.Code, Status: status, Message: body.Message, RequestID: body.RequestID, Details: body.Details}
	switch {
	case body.Code == "QUOTA_EXCEEDED":
		return &QuotaExceededError{base}
	case body.Code == "TRIAL_EXPIRED":
		return &TrialExpiredError{&PermissionDeniedError{base}}
	case status == http.StatusTooManyRequests:
		e := &RateLimitedError{APIError: base}
		if retryAfter != nil {
			e.RetryAfter, e.HasRetryAfter = *retryAfter, true
		}
		return e
	case status == http.StatusUnauthorized:
		return &AuthenticationError{base}
	case status == http.StatusForbidden:
		return &PermissionDeniedError{base}
	case status == http.StatusNotFound || strings.HasSuffix(body.Code, "_NOT_FOUND"):
		return &NotFoundError{base}
	case status >= 500:
		return &ServerError{base}
	case status >= 400:
		return &InvalidRequestError{base}
	}
	return base
}
