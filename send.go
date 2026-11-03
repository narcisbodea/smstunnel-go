package smstunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// A SendOption changes one text: the phone it goes out from, its country code, its idempotency key.
type SendOption func(*sendSettings)

type sendSettings struct {
	deviceID       string
	countryCode    string
	idempotencyKey string
}

// WithDeviceID sends the text from this phone: its ID from the dashboard (Phones, the Copy button) or [Phone.ID].
// Without it, SMSTunnel picks one of your phones.
func WithDeviceID(id string) SendOption {
	return func(s *sendSettings) { s.deviceID = strings.TrimSpace(id) }
}

// WithCountryCode sets the country code ("+49", "49" or "0049") that completes this text's number when it is written
// without + or 00. It wins over [WithDefaultCountryCode] and over the phone's own country.
func WithCountryCode(code string) SendOption {
	return func(s *sendSettings) { s.countryCode = code }
}

// WithIdempotencyKey makes a retry safe: a second call with the same key within 24 hours gets the first call's
// answer back ([SendResult.Replayed]) instead of sending a second text. Use a value that names this one text, such
// as "order-1042-shipped" or a UUID you store with it (1 to 255 printable ASCII characters). Reusing a key for a
// different text fails with status 422 and [CodeIdempotencyKeyReused].
func WithIdempotencyKey(key string) SendOption {
	return func(s *sendSettings) { s.idempotencyKey = key }
}

// SendResult is SMSTunnel's answer to a text it accepted.
type SendResult struct {
	// MessageID identifies the text: keep it to look up the status with GetMessageStatus.
	MessageID string `json:"messageId"`
	// Queued is true when no phone of yours was online: the text waits on your account and goes out as soon as a
	// phone is back (within 3 days, otherwise it is marked failed). Nothing to resend.
	Queued bool `json:"queued"`
	// Branded is true when the Free plan added its short SMSTunnel line to the text.
	Branded bool `json:"branded"`
	// Remaining is what is left today after this text, when SMSTunnel says.
	Remaining *Remaining `json:"remaining,omitempty"`
	// Replayed is true when this is the stored answer to an earlier call with the same idempotency key: no second
	// text was sent.
	Replayed bool `json:"replayed"`
	// Raw is SMSTunnel's whole answer, for fields this version doesn't map.
	Raw json.RawMessage `json:"-"`
}

// Remaining is today's allowance left after a text.
type Remaining struct {
	// Clean counts the texts (or, after SendSignInCode, the sign-in codes) that can still go out as written.
	Clean Quota `json:"clean"`
	// Total counts all that can still go out, including the ones with the Free plan's added line.
	Total Quota `json:"total"`
}

// Quota is a limit, or what is left of one. [Unlimited] means there is no limit. In JSON it is a number, or the
// string "unlimited".
type Quota int64

// Unlimited is the Quota of something without a limit.
const Unlimited Quota = -1

// IsUnlimited reports whether there is no limit.
func (q Quota) IsUnlimited() bool { return q < 0 }

// String is the number, or "unlimited".
func (q Quota) String() string {
	if q < 0 {
		return "unlimited"
	}
	return strconv.FormatInt(int64(q), 10)
}

// MarshalJSON writes the number, or "unlimited".
func (q Quota) MarshalJSON() ([]byte, error) {
	if q < 0 {
		return []byte(`"unlimited"`), nil
	}
	return strconv.AppendInt(nil, int64(q), 10), nil
}

// UnmarshalJSON reads a number (a negative one meaning no limit), "unlimited", or null (left as it is).
func (q *Quota) UnmarshalJSON(data []byte) error {
	text := string(bytes.TrimSpace(data))
	if text == "null" {
		return nil
	}
	if unquoted, err := strconv.Unquote(text); err == nil {
		if strings.EqualFold(strings.TrimSpace(unquoted), "unlimited") {
			*q = Unlimited
			return nil
		}
		text = strings.TrimSpace(unquoted)
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(number) {
		return fmt.Errorf("smstunnel: %s is not a number or \"unlimited\"", text)
	}
	if number < 0 || math.IsInf(number, 1) {
		*q = Unlimited
		return nil
	}
	*q = Quota(number)
	return nil
}

// SendSMS sends one text to one number (POST /api/v1/sms/send). To text several people, call it once per number.
//
// to is the recipient in international format (+14155550123); a local number needs a country code. message is the
// text, up to 1600 characters.
func (c *Client) SendSMS(ctx context.Context, to, message string, opts ...SendOption) (*SendResult, error) {
	return c.send(ctx, "api/v1/sms/send", to, message, opts)
}

// SendSignInCode sends a sign-in or verification code (POST /api/v1/sms/send-2fa). The text must contain the code as a
// number of its own, 4 to 10 digits ("Your code is 482913" or "123-456"), and be at most 320 characters; otherwise
// SMSTunnel answers 400. Codes have their own daily allowance. Use it for codes only.
func (c *Client) SendSignInCode(ctx context.Context, to, message string, opts ...SendOption) (*SendResult, error) {
	return c.send(ctx, "api/v1/sms/send-2fa", to, message, opts)
}

func (c *Client) send(ctx context.Context, path, to, message string, opts []SendOption) (*SendResult, error) {
	if err := checkRecipient(to); err != nil {
		return nil, err
	}
	if err := checkText(message); err != nil {
		return nil, err
	}
	var s sendSettings
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}
	countryCode, err := checkCountryCode(s.countryCode)
	if err != nil {
		return nil, err
	}
	if countryCode == "" {
		countryCode = c.defaultCountryCode
	}
	if err := checkIdempotencyKey(s.idempotencyKey); err != nil {
		return nil, err
	}

	body := struct {
		To                 string `json:"to"`
		Message            string `json:"message"`
		DeviceID           string `json:"deviceId,omitempty"`
		DefaultCountryCode string `json:"defaultCountryCode,omitempty"`
	}{strings.TrimSpace(to), message, s.deviceID, countryCode}

	a, err := c.call(ctx, http.MethodPost, path, nil, body, s.idempotencyKey)
	if err != nil {
		return nil, err
	}

	var wire struct {
		MessageID string `json:"messageId"`
		Queued    bool   `json:"queued"`
		Data      struct {
			MessageID string     `json:"messageId"`
			Branded   bool       `json:"branded"`
			Queued    bool       `json:"queued"`
			Remaining *Remaining `json:"remaining"`
		} `json:"data"`
	}
	if err := json.Unmarshal(a.body, &wire); err != nil {
		return nil, &APIError{StatusCode: a.status, Message: "SMSTunnel's answer could not be read: " + err.Error(), Body: a.body}
	}
	res := &SendResult{
		MessageID: wire.Data.MessageID,
		Queued:    wire.Queued || wire.Data.Queued,
		Branded:   wire.Data.Branded,
		Remaining: wire.Data.Remaining,
		Replayed:  a.replayed,
		Raw:       a.body,
	}
	if res.MessageID == "" {
		res.MessageID = wire.MessageID
	}
	return res, nil
}
