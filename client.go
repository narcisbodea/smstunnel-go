package smstunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version is the version of this library. It is sent in the User-Agent header.
const Version = "1.0.0"

// DefaultBaseURL is the address of SMSTunnel's API.
const DefaultBaseURL = "https://smstunnel.io"

// DefaultTimeout is how long a call may take, unless [WithTimeout] changes it.
const DefaultTimeout = 30 * time.Second

// maxAnswerBytes bounds what is read from one answer (a page of 100 received texts is far smaller).
const maxAnswerBytes = 8 << 20

const userAgentBase = "smstunnel-go/" + Version + " (+https://smstunnel.io)"

// Client calls the SMSTunnel API. It is safe for concurrent use: make one and reuse it.
type Client struct {
	apiKey             string
	baseURL            string // always ends with "/"
	httpClient         *http.Client
	defaultCountryCode string
	timeout            time.Duration
	userAgent          string
}

// An Option changes a setting of [NewClient].
type Option func(*Client) error

// NewClient returns a client that sends with this API key, the key you create in your SMSTunnel dashboard (Apps →
// Add an app). Read it from an environment variable or a secret store, never from source code.
//
// The error matches [ErrInvalidInput] when the key is empty or an option is refused.
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	key, err := checkAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	c := &Client{
		apiKey:     key,
		baseURL:    DefaultBaseURL + "/",
		httpClient: &http.Client{CheckRedirect: noRedirects},
		timeout:    DefaultTimeout,
		userAgent:  userAgentBase,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// WithBaseURL points the client at another address than [DefaultBaseURL], such as a test server
// ("http://127.0.0.1:3141"). A path is kept: "https://example.com/smstunnel" calls
// "https://example.com/smstunnel/api/v1/...".
func WithBaseURL(rawURL string) Option {
	return func(c *Client) error {
		base, err := checkBaseURL(rawURL)
		if err != nil {
			return err
		}
		c.baseURL = base
		return nil
	}
}

// WithHTTPClient sends through your own http.Client, for its transport, proxy or TLS settings. The client keeps a copy
// of it: unless yours has its own CheckRedirect, the copy doesn't follow redirects, so the API key is never sent to
// another address. Its Timeout applies on top of [WithTimeout].
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return invalid("WithHTTPClient needs an http.Client")
		}
		own := *hc
		if own.CheckRedirect == nil {
			own.CheckRedirect = noRedirects
		}
		c.httpClient = &own
		return nil
	}
}

// WithDefaultCountryCode sets the country code ("+40", "40" or "0040") that completes numbers written without + or
// 00, for every text the client sends. A text's own [WithCountryCode] wins over it; when neither is set, SMSTunnel
// uses the phone's country. Numbers in international format (+14155550123) never need it.
func WithDefaultCountryCode(code string) Option {
	return func(c *Client) error {
		cc, err := checkCountryCode(code)
		if err != nil {
			return err
		}
		c.defaultCountryCode = cc
		return nil
	}
}

// WithTimeout sets how long one call may take ([DefaultTimeout] otherwise). 0 turns the client's own timeout off;
// the context's deadline still applies.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d < 0 {
			return invalid("the timeout can't be negative")
		}
		c.timeout = d
		return nil
	}
}

// WithUserAgentSuffix adds your program's name and version to the User-Agent header, after this library's:
// "smstunnel-go/1.0.0 (+https://smstunnel.io) my-tool/2.1".
func WithUserAgentSuffix(suffix string) Option {
	return func(c *Client) error {
		suffix = strings.TrimSpace(suffix)
		for i := 0; i < len(suffix); i++ {
			if suffix[i] < ' ' || suffix[i] > '~' {
				return invalid("the User-Agent suffix must be printable ASCII")
			}
		}
		c.userAgent = userAgentBase
		if suffix != "" {
			c.userAgent += " " + suffix
		}
		return nil
	}
}

// BaseURL is the address the client calls, ending with "/".
func (c *Client) BaseURL() string { return c.baseURL }

// UserAgent is the User-Agent header the client sends.
func (c *Client) UserAgent() string { return c.userAgent }

// String describes the client without its API key, so printing it (%v, %+v) never shows the key.
func (c *Client) String() string {
	if c == nil {
		return "smstunnel.Client(nil)"
	}
	return "smstunnel.Client(" + c.baseURL + ")"
}

// GoString is String, for %#v.
func (c *Client) GoString() string { return c.String() }

func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// answer is a successful (2xx) answer: the JSON object, and its "data" member when it has one.
type answer struct {
	status   int
	body     []byte
	data     json.RawMessage
	replayed bool
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, payload any, idempotencyKey string) (*answer, error) {
	if c == nil || c.apiKey == "" || c.httpClient == nil {
		return nil, invalid("create the client with NewClient")
	}
	if ctx == nil {
		return nil, invalid("the context is nil")
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	var body io.Reader
	if payload != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // the body is never put in a web page: "<", ">" and "&" stay as they are
		if err := enc.Encode(payload); err != nil {
			return nil, fmt.Errorf("smstunnel: %w", err)
		}
		body = bytes.NewReader(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("smstunnel: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	if err != nil {
		return nil, transportError(ctx, err)
	}

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode <= 399:
		where := ""
		if loc := resp.Header.Get("Location"); loc != "" {
			where = " to " + loc
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message: fmt.Sprintf("SMSTunnel answered with a redirect%s. The client doesn't follow redirects, so your API key is "+
				"never sent to another address: use the address the API answers on (WithBaseURL)", where),
			Header: resp.Header,
			Body:   raw,
		}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, newAPIError(resp.StatusCode, resp.Header, raw)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("SMSTunnel answered with something that is not JSON: check that %s is the SMSTunnel API", c.baseURL),
			Header:     resp.Header,
			Body:       raw,
		}
	}
	if success, ok := root["success"]; ok && string(bytes.TrimSpace(success)) == "false" {
		e := newAPIError(resp.StatusCode, resp.Header, raw)
		if e.Message == fallbackMessage(resp.StatusCode) {
			e.Message = "SMSTunnel did not accept the request"
		}
		return nil, e
	}

	a := &answer{status: resp.StatusCode, body: raw, data: raw, replayed: strings.EqualFold(strings.TrimSpace(resp.Header.Get("Idempotent-Replayed")), "true")}
	if data, ok := root["data"]; ok && len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{' {
		a.data = data
	}
	return a, nil
}

// decode reads an answer's data into v.
func (a *answer) decode(v any) error {
	if err := json.Unmarshal(a.data, v); err != nil {
		return &APIError{StatusCode: a.status, Message: "SMSTunnel's answer could not be read: " + err.Error(), Body: a.body}
	}
	return nil
}

// transportError wraps an error from the HTTP client: a cancelled context as it is, the rest as a *ConnectionError.
func transportError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("smstunnel: %w", err)
	}
	return &ConnectionError{Err: err}
}
