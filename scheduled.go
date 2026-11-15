package smstunnel

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ScheduledStatus is where a scheduled text is now.
type ScheduledStatus string

// The statuses of a scheduled text. A status this version doesn't know is kept as SMSTunnel sent it.
const (
	// ScheduledWaiting ("scheduled"): waiting for its time. It can still be cancelled.
	ScheduledWaiting ScheduledStatus = "scheduled"
	// ScheduledSent: at its time SMSTunnel sent it as if SendSMS had been called then. ScheduledText.MessageID
	// identifies that text: GetMessageStatus says whether the phone sent it and whether it was delivered.
	ScheduledSent ScheduledStatus = "sent"
	// ScheduledFailed: it could not be sent at its time; ScheduledText.Error says why.
	ScheduledFailed ScheduledStatus = "failed"
	// ScheduledCanceled: it was cancelled before its time.
	ScheduledCanceled ScheduledStatus = "canceled"
)

// Done reports whether the scheduled text won't change any more: sent, failed or cancelled.
func (s ScheduledStatus) Done() bool {
	return s == ScheduledSent || s == ScheduledFailed || s == ScheduledCanceled
}

// ScheduledText is a text scheduled with [WithSendAt]: SMSTunnel keeps it and sends it at SendAt, as if SendSMS were
// called then with the same key.
type ScheduledText struct {
	// ID identifies the scheduled text: pass it to GetScheduledText or CancelScheduledText.
	ID string `json:"id"`
	// To is the recipient's number.
	To string `json:"to"`
	// Message is the text.
	Message string `json:"message"`
	// SendAt is when SMSTunnel sends it.
	SendAt time.Time `json:"sendAt"`
	// Status is where it is now: ScheduledWaiting, ScheduledSent, ScheduledFailed or ScheduledCanceled.
	Status ScheduledStatus `json:"status"`
	// DeviceID is the phone chosen with WithDeviceID, or "" when SMSTunnel picks one of yours at SendAt.
	DeviceID string `json:"deviceId,omitempty"`
	// MessageID is set once the text was sent: look it up with GetMessageStatus.
	MessageID string `json:"messageId,omitempty"`
	// Error says why it failed: "key_not_active" (the key was turned off, deleted or expired, or lost sms:send),
	// "limit_reached" (the plan's allowance was used up at that moment), "invalid_number", "phone_not_found",
	// "phone_unlinked", "sealed_only_phone", "number_not_allowed", "account_not_active", "interrupted" or "failed".
	// It is "" otherwise.
	Error string `json:"error,omitempty"`
	// CreatedAt is when the text was scheduled, or nil.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// ScheduledScope chooses which scheduled texts [Client.ListScheduledTexts] lists.
type ScheduledScope string

const (
	// ScheduledScopeWaiting lists the texts still waiting for their time, soonest first. It is the default.
	ScheduledScopeWaiting ScheduledScope = "waiting"
	// ScheduledScopeDone lists the texts that were sent, failed or were cancelled, newest first. SMSTunnel keeps
	// them 30 days.
	ScheduledScopeDone ScheduledScope = "done"
)

// ScheduledTextsQuery chooses which scheduled texts to list. Every field is optional.
type ScheduledTextsQuery struct {
	// Scope is ScheduledScopeWaiting (the default, also for "") or ScheduledScopeDone.
	Scope ScheduledScope
	// Limit is how many texts per page, 1 to 100; 0 means SMSTunnel's default (50).
	Limit int
	// Page is the page, starting at 1; 0 means the first.
	Page int
}

// ScheduledTextsPage is one page of the texts this key scheduled.
type ScheduledTextsPage struct {
	// Texts are the texts on this page: waiting ones soonest first, done ones newest first.
	Texts []ScheduledText `json:"items"`
	// Total is how many texts of this scope the key has, on every page.
	Total int `json:"total"`
	// Page is this page's number, starting at 1.
	Page int `json:"page"`
	// Limit is the number of texts per page.
	Limit int `json:"limit"`
	// HasMore is whether there is a next page, from Total, Page and Limit.
	HasMore bool `json:"hasMore"`
	// Waiting is how many texts wait in the whole account now: scheduled with any key, or in the dashboard.
	Waiting int `json:"waiting"`
	// Max is how many texts the plan lets wait at once: Free 10, Starter 50, Standard 200, Professional 500,
	// Enterprise 1000.
	Max int `json:"max"`
}

// ListScheduledTexts lists the texts this key scheduled with [WithSendAt] (GET /api/v1/sms/scheduled): by default
// the ones still waiting, soonest first. The texts of your other keys and of the dashboard are not listed, but
// count in Waiting. It needs a key with sms:read.
func (c *Client) ListScheduledTexts(ctx context.Context, q ScheduledTextsQuery) (*ScheduledTextsPage, error) {
	query, err := q.values()
	if err != nil {
		return nil, err
	}
	a, err := c.call(ctx, http.MethodGet, "api/v1/sms/scheduled", query, nil, "")
	if err != nil {
		return nil, err
	}
	var page ScheduledTextsPage
	if err := a.decode(&page); err != nil {
		return nil, err
	}
	if page.Texts == nil {
		page.Texts = []ScheduledText{}
	}
	page.HasMore = page.Page > 0 && page.Limit > 0 && page.Page*page.Limit < page.Total
	return &page, nil
}

// GetScheduledText reads one text this key scheduled (GET /api/v1/sms/scheduled/{id}). It needs a key with sms:read.
// A text another key scheduled, or one done more than 30 days ago, is a 404 [*APIError] with [CodeScheduleNotFound].
func (c *Client) GetScheduledText(ctx context.Context, id string) (*ScheduledText, error) {
	return c.scheduledText(ctx, http.MethodGet, id)
}

// CancelScheduledText cancels a text this key scheduled while it still waits (DELETE /api/v1/sms/scheduled/{id}),
// and returns it with ScheduledCanceled. It needs a key with sms:send. A text that already went out, failed, was
// cancelled or is being sent right now is a 409 [*APIError] with [CodeScheduleNotEditable]; one this key didn't
// schedule is a 404 with [CodeScheduleNotFound].
func (c *Client) CancelScheduledText(ctx context.Context, id string) (*ScheduledText, error) {
	return c.scheduledText(ctx, http.MethodDelete, id)
}

func (c *Client) scheduledText(ctx context.Context, method, id string) (*ScheduledText, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, invalid("give the scheduled text's ID (SendResult.Scheduled.ID)")
	}
	a, err := c.call(ctx, method, "api/v1/sms/scheduled/"+url.PathEscape(id), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var st ScheduledText
	if err := a.decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (q ScheduledTextsQuery) values() (url.Values, error) {
	switch q.Scope {
	case "", ScheduledScopeWaiting, ScheduledScopeDone:
	default:
		return nil, invalid(`the scope must be ScheduledScopeWaiting ("waiting") or ScheduledScopeDone ("done")`)
	}
	if q.Limit < 0 || q.Limit > 100 {
		return nil, invalid("the limit must be between 1 and 100 (0 for the default)")
	}
	if q.Page < 0 {
		return nil, invalid("pages start at 1 (0 for the first)")
	}
	v := url.Values{}
	if q.Scope != "" {
		v.Set("scope", string(q.Scope))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Page > 0 {
		v.Set("page", strconv.Itoa(q.Page))
	}
	return v, nil
}
