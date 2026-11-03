package smstunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Errors that [*APIError] matches with errors.Is, by HTTP status.
var (
	// ErrUnauthorized (401): the API key is wrong, was deleted or turned off, or its account is suspended.
	ErrUnauthorized = errors.New("smstunnel: unauthorized")
	// ErrForbidden (403): the key lacks a permission (Code is CodePermissionRequired), or the plan doesn't include
	// the feature.
	ErrForbidden = errors.New("smstunnel: forbidden")
	// ErrNotFound (404): no such message for this key.
	ErrNotFound = errors.New("smstunnel: not found")
	// ErrRateLimited (429): too many requests (RetryAfter says how long to wait), or the plan's limit for the day
	// or month is reached (no RetryAfter; the message says when texts can go out again).
	ErrRateLimited = errors.New("smstunnel: rate limited")
	// ErrServer (5xx): a problem on SMSTunnel's side. A send may or may not have been accepted.
	ErrServer = errors.New("smstunnel: server error")
)

// ErrInvalidInput is matched (errors.Is) by the errors of arguments refused before anything is sent: an empty number,
// two numbers in one, a country code that isn't one, a bad option.
var ErrInvalidInput = errors.New("smstunnel: invalid input")

// Values of [APIError.Code] a program can act on.
const (
	// CodePermissionRequired (403): the API key lacks a permission; APIError.Permission names it, e.g. "sms:inbox".
	CodePermissionRequired = "PERMISSION_REQUIRED"
	// CodeAPIKeyFromWebPage (401): an API key sent from a web page on another site. Call SMSTunnel from a server.
	CodeAPIKeyFromWebPage = "API_KEY_FROM_WEB_PAGE"
	// CodeDisabledBySupport: SMSTunnel support turned the key or the phone off.
	CodeDisabledBySupport = "DISABLED_BY_SUPPORT"
	// CodeIdempotencyKeyInvalid (400): the idempotency key is not 1 to 255 printable ASCII characters.
	CodeIdempotencyKeyInvalid = "IDEMPOTENCY_KEY_INVALID"
	// CodeIdempotencyKeyReused (422): the idempotency key was used in the last 24 hours for a different text.
	CodeIdempotencyKeyReused = "IDEMPOTENCY_KEY_REUSED"
	// CodeIdempotencyKeyInProgress (409): a call with this idempotency key is still running; retry after RetryAfter.
	CodeIdempotencyKeyInProgress = "IDEMPOTENCY_KEY_IN_PROGRESS"
)

// APIError is returned when SMSTunnel answered with an error status.
//
// Besides the statuses with a sentinel (see [ErrUnauthorized] and the others), expect 400 (a number SMSTunnel can't
// read, a sign-in code text without a code, a refused field), 409 and 422 (an idempotency key still in progress, or
// reused for another text).
type APIError struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Code is SMSTunnel's own code for errors a program can act on (see CodePermissionRequired and the others),
	// or "".
	Code string
	// Message is what went wrong, in SMSTunnel's words.
	Message string
	// Permission is, with CodePermissionRequired, the permission the key lacks, such as "sms:inbox".
	Permission string
	// RetryAfter is how long to wait before trying again, from the Retry-After header; 0 when there was none.
	RetryAfter time.Duration
	// Header holds the answer's headers.
	Header http.Header
	// Body is the answer as SMSTunnel sent it (usually JSON).
	Body []byte
}

// Error is "smstunnel: <status>[ <code>]: <message>".
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("smstunnel: ")
	b.WriteString(strconv.Itoa(e.StatusCode))
	if e.Code != "" {
		b.WriteString(" ")
		b.WriteString(e.Code)
	}
	b.WriteString(": ")
	b.WriteString(e.Message)
	return b.String()
}

// Is matches the sentinel of the status: ErrUnauthorized, ErrForbidden, ErrNotFound, ErrRateLimited or ErrServer.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServer:
		return e.StatusCode >= 500 && e.StatusCode <= 599
	}
	return false
}

// ConnectionError is returned when no answer came back: SMSTunnel could not be reached (DNS, TLS, a refused or
// broken connection), or didn't answer in time. There is no HTTP status. For a send, the text may or may not have
// been accepted: retry it only with the same idempotency key.
type ConnectionError struct {
	// Err is the error of the HTTP client.
	Err error
}

func (e *ConnectionError) Error() string {
	if e.Timeout() {
		return "smstunnel: no answer in time: " + e.Err.Error()
	}
	return "smstunnel: SMSTunnel could not be reached: " + e.Err.Error()
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// Timeout reports whether the call ran out of time (the client's timeout or the context's deadline).
func (e *ConnectionError) Timeout() bool {
	if errors.Is(e.Err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(e.Err, &netErr) && netErr.Timeout()
}

func invalid(message string) error {
	return &inputError{message: message}
}

type inputError struct{ message string }

func (e *inputError) Error() string        { return "smstunnel: " + e.message }
func (e *inputError) Is(target error) bool { return target == ErrInvalidInput }

func newAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{StatusCode: status, Header: header, Body: body, RetryAfter: retryAfter(header.Get("Retry-After"), time.Now())}

	trimmed := bytes.TrimSpace(body)
	var fields struct {
		Message    json.RawMessage `json:"message"`
		Error      json.RawMessage `json:"error"`
		Code       json.RawMessage `json:"code"`
		Permission json.RawMessage `json:"permission"`
	}
	if len(trimmed) > 0 && trimmed[0] == '{' && json.Unmarshal(trimmed, &fields) == nil {
		e.Code = jsonString(fields.Code)
		e.Permission = jsonString(fields.Permission)
		e.Message = messageOf(fields.Message)
		if e.Message == "" {
			e.Message = jsonString(fields.Error)
		}
	} else if len(trimmed) > 0 {
		e.Message = plainText(string(trimmed)) // a proxy's error page, say
	}
	if e.Message == "" {
		e.Message = fallbackMessage(status)
	}
	return e
}

func fallbackMessage(status int) string {
	if text := http.StatusText(status); text != "" {
		return "SMSTunnel returned error: " + strconv.Itoa(status) + " " + text
	}
	return "SMSTunnel returned error: " + strconv.Itoa(status)
}

// messageOf reads "message": a string, or a list joined with "; ".
func messageOf(raw json.RawMessage) string {
	if s := jsonString(raw); s != "" {
		return s
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var words []string
	for _, part := range parts {
		if s := jsonString(part); s != "" {
			words = append(words, s)
		}
	}
	return strings.Join(words, "; ")
}

func jsonString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// plainText is a page's words without the markup, at most 300 characters.
func plainText(page string) string {
	var b strings.Builder
	inTag, space := false, false
	for _, r := range page {
		switch {
		case r == '<':
			inTag = true
		case inTag:
			if r == '>' {
				inTag = false
				space = b.Len() > 0
			}
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			space = b.Len() > 0
		default:
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
		}
		if b.Len() >= 300 {
			break
		}
	}
	return html.UnescapeString(b.String())
}

const maxRetryAfter = 24 * time.Hour

// retryAfter reads a Retry-After header: seconds, or an HTTP date from now.
func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if !(seconds > 0) || math.IsInf(seconds, 0) { // also NaN
			return 0
		}
		return time.Duration(math.Min(seconds, maxRetryAfter.Seconds()) * float64(time.Second))
	}
	if when, err := http.ParseTime(value); err == nil {
		if wait := when.Sub(now); wait > 0 {
			return wait
		}
	}
	return 0
}
