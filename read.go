package smstunnel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// State is where a text is now.
type State string

// The states of a text. A state this version doesn't know is kept as SMSTunnel sent it.
const (
	// StatePending: waiting for the phone, for example while it is offline.
	StatePending State = "pending"
	// StateSent: the phone sent it.
	StateSent State = "sent"
	// StateDelivered: the recipient's network confirmed delivery.
	StateDelivered State = "delivered"
	// StateFailed: it could not be sent; MessageStatus.ErrorMessage says why.
	StateFailed State = "failed"
	// StateUnconfirmed: the phone handed it to the SIM but never got the confirmation. It most likely went out and
	// is never resent automatically. Don't resend it yourself: the recipient could get it twice.
	StateUnconfirmed State = "unconfirmed"
)

// Final reports whether the state won't change any more: delivered, failed or unconfirmed.
func (s State) Final() bool {
	return s == StateDelivered || s == StateFailed || s == StateUnconfirmed
}

// MessageStatus is where one text is now.
type MessageStatus struct {
	// ID is the text's ID.
	ID string `json:"id"`
	// PhoneNumber is the number the text went to, in international format.
	PhoneNumber string `json:"phoneNumber"`
	// State is where the text is now.
	State State `json:"state"`
	// SentAt is when the phone sent it, or nil.
	SentAt *time.Time `json:"sentAt,omitempty"`
	// DeliveredAt is when delivery was confirmed, or nil.
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
	// ErrorMessage says why it failed, or why it is unconfirmed.
	ErrorMessage string `json:"errorMessage,omitempty"`
	// Raw is SMSTunnel's whole answer, for fields this version doesn't map.
	Raw json.RawMessage `json:"-"`
}

// GetMessageStatus looks up where a text is now (GET /api/v1/sms/status/{id}). It needs a key with sms:read.
func (c *Client) GetMessageStatus(ctx context.Context, messageID string) (*MessageStatus, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, invalid("give the message ID that the send returned")
	}
	a, err := c.call(ctx, http.MethodGet, "api/v1/sms/status/"+url.PathEscape(messageID), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var wire struct {
		ID             string     `json:"id"`
		PhoneNumber    string     `json:"phoneNumber"`
		Status         string     `json:"status"`
		Unconfirmed    bool       `json:"unconfirmed"`
		DeliveryStatus string     `json:"deliveryStatus"`
		SentAt         *time.Time `json:"sentAt"`
		DeliveredAt    *time.Time `json:"deliveredAt"`
		ErrorMessage   *string    `json:"errorMessage"`
	}
	if err := a.decode(&wire); err != nil {
		return nil, err
	}
	// deliveryStatus holds the real state; "status" shows an unconfirmed text as failed, for older integrations.
	state := wire.DeliveryStatus
	if state == "" {
		state = wire.Status
		if wire.Unconfirmed {
			state = string(StateUnconfirmed)
		}
	}
	st := &MessageStatus{
		ID:          wire.ID,
		PhoneNumber: wire.PhoneNumber,
		State:       State(strings.ToLower(strings.TrimSpace(state))),
		SentAt:      wire.SentAt,
		DeliveredAt: wire.DeliveredAt,
		Raw:         a.body,
	}
	if st.ID == "" {
		st.ID = messageID
	}
	if wire.ErrorMessage != nil {
		st.ErrorMessage = *wire.ErrorMessage
	}
	return st, nil
}

// Phone is one of your phones.
type Phone struct {
	// ID identifies the phone: pass it to WithDeviceID to send from it.
	ID string `json:"id"`
	// Name is the phone's name, as shown under Phones in the dashboard.
	Name string `json:"name"`
	// Online is whether the SMSTunnel app on the phone is connected now. Texts for an offline phone wait until it is back.
	Online bool `json:"isOnline"`
	// LastSeenAt is when the phone was last connected, or nil.
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

// ListPhones lists the phones of the key's account and whether each is online (GET /api/v1/devices).
func (c *Client) ListPhones(ctx context.Context) ([]Phone, error) {
	a, err := c.call(ctx, http.MethodGet, "api/v1/devices", nil, nil, "")
	if err != nil {
		return nil, err
	}
	var wire struct {
		Devices []Phone `json:"devices"`
	}
	if err := a.decode(&wire); err != nil {
		return nil, err
	}
	if wire.Devices == nil {
		wire.Devices = []Phone{}
	}
	return wire.Devices, nil
}

// Usage is the plan, today's usage and what is left. Daily counts restart at 00:00 UTC, the month's count on the
// 1st. A client of a company account shares the company's allowance and sees it here. The field names are the API's.
type Usage struct {
	// Plan is the plan, for example "free", "starter", "standard", "pro" or "enterprise".
	Plan string `json:"plan"`
	// AccountType is the kind of account, for example "standard".
	AccountType string `json:"accountType"`
	// Used is what was used today, and this month.
	Used UsageCounts `json:"usage"`
	// Limits are the plan's limits.
	Limits UsageLimits `json:"limits"`
	// Remaining is what is left.
	Remaining UsageRemaining `json:"remaining"`
	// Enterprise is the company account whose allowance this account shares, or nil.
	Enterprise *UsageEnterprise `json:"enterprise,omitempty"`
	// Raw is SMSTunnel's whole answer, for fields this version doesn't map.
	Raw json.RawMessage `json:"-"`
}

// UsageCounts is what was used.
type UsageCounts struct {
	SMSClean      int64 `json:"smsClean"`      // texts sent today as written
	SMSBranded    int64 `json:"smsBranded"`    // texts sent today with the Free plan's added line
	TwoFAClean    int64 `json:"twoFaClean"`    // sign-in codes sent today
	SMSCleanMonth int64 `json:"smsCleanMonth"` // texts sent this month as written
}

// UsageLimits are the plan's limits.
type UsageLimits struct {
	SMSClean         Quota `json:"smsClean"`         // texts as written, per day
	TwoFAClean       Quota `json:"twoFaClean"`       // sign-in codes, per day
	SMSCleanPerMonth Quota `json:"smsCleanPerMonth"` // texts as written, per month
	SMSBrandedPerDay Quota `json:"smsBrandedPerDay"` // texts with the added line, per day (Free plan; 0 on paid plans)
}

// UsageRemaining is what is left.
type UsageRemaining struct {
	SMSClean      Quota `json:"smsClean"`      // texts that can still go out today as written
	TwoFAClean    Quota `json:"twoFaClean"`    // sign-in codes that can still go out today
	SMSCleanMonth Quota `json:"smsCleanMonth"` // texts as written left this month
	SMSBranded    Quota `json:"smsBranded"`    // texts with the added line left today
}

// UsageEnterprise is the company account a client account belongs to.
type UsageEnterprise struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetUsage reads the plan, today's usage and what is left (GET /api/v1/account/usage).
func (c *Client) GetUsage(ctx context.Context) (*Usage, error) {
	a, err := c.call(ctx, http.MethodGet, "api/v1/account/usage", nil, nil, "")
	if err != nil {
		return nil, err
	}
	var u Usage
	if err := a.decode(&u); err != nil {
		return nil, err
	}
	u.Raw = a.body
	return &u, nil
}

// ReceivedTextsQuery chooses which received texts to list. Every field is optional.
type ReceivedTextsQuery struct {
	// Limit is how many texts per page, 1 to 100; 0 means SMSTunnel's default (50).
	Limit int
	// Page is the page, starting at 1; 0 means the first.
	Page int
	// Since keeps only the texts that reached SMSTunnel at or after this moment (by SMSTunnel's clock).
	Since time.Time
	// Phone keeps only the texts from this sender (part of the number is enough).
	Phone string
	// DeviceID keeps only the texts received by this phone.
	DeviceID string
}

// ReceivedText is a text one of your phones received and synced to your account.
type ReceivedText struct {
	// ID is the text's ID.
	ID string `json:"id"`
	// Sender is the number that sent it.
	Sender string `json:"sender"`
	// Message is the words; "" when the phone synced the text encrypted (see EncryptedPayload).
	Message string `json:"message"`
	// Encrypted is whether the phone synced the text encrypted.
	Encrypted bool `json:"isEncrypted"`
	// EncryptedPayload is the encrypted text, for software that holds the key.
	EncryptedPayload string `json:"encryptedPayload,omitempty"`
	// ReceivedAt is when the phone received it (the phone's clock).
	ReceivedAt time.Time `json:"receivedAt"`
	// SIMSlot is the SIM slot it arrived on, when the phone says.
	SIMSlot *int `json:"simSlot,omitempty"`
	// SubscriptionID is Android's subscription ID of that SIM, when the phone says.
	SubscriptionID *int `json:"subscriptionId,omitempty"`
	// DeviceID is the ID of the phone that received it.
	DeviceID string `json:"deviceId,omitempty"`
	// DeviceName is the name of that phone.
	DeviceName string `json:"deviceName,omitempty"`
}

// ReceivedTextsPage is one page of received texts, newest first.
type ReceivedTextsPage struct {
	// Texts are the texts on this page, newest first.
	Texts []ReceivedText `json:"messages"`
	// Total is how many texts match, on every page.
	Total int `json:"total"`
	// Page is this page's number, starting at 1.
	Page int `json:"page"`
	// Limit is the number of texts per page.
	Limit int `json:"limit"`
	// HasMore is whether there is a next page.
	HasMore bool `json:"hasMore"`
}

// ListReceivedTexts lists texts your phones received, newest first (GET /api/v1/sms/received). It needs a key you
// allowed to read received texts ("Can also read received texts", the sms:inbox permission); other keys get a 403
// [*APIError] with [CodePermissionRequired]. To follow new texts as they arrive, use [Client.NewReceivedTextsCursor].
func (c *Client) ListReceivedTexts(ctx context.Context, q ReceivedTextsQuery) (*ReceivedTextsPage, error) {
	query, err := q.values()
	if err != nil {
		return nil, err
	}
	a, err := c.call(ctx, http.MethodGet, "api/v1/sms/received", query, nil, "")
	if err != nil {
		return nil, err
	}
	var page ReceivedTextsPage
	if err := a.decode(&page); err != nil {
		return nil, err
	}
	if page.Texts == nil {
		page.Texts = []ReceivedText{}
	}
	return &page, nil
}

func (q ReceivedTextsQuery) values() (url.Values, error) {
	if q.Limit < 0 || q.Limit > 100 {
		return nil, invalid("the limit must be between 1 and 100 (0 for the default)")
	}
	if q.Page < 0 {
		return nil, invalid("pages start at 1 (0 for the first)")
	}
	v := url.Values{}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Page > 0 {
		v.Set("page", strconv.Itoa(q.Page))
	}
	if !q.Since.IsZero() {
		v.Set("since", q.Since.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if phone := strings.TrimSpace(q.Phone); phone != "" {
		v.Set("phone", phone)
	}
	if id := strings.TrimSpace(q.DeviceID); id != "" {
		v.Set("deviceId", id)
	}
	return v, nil
}
