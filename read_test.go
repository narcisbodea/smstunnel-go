package smstunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestGetMessageStatus(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"id":"m1","phoneNumber":"+14155550123","status":"sent","deliveryStatus":"sent",
		"sentAt":"2026-11-03T19:41:46.622Z","deliveredAt":null,"errorMessage":null}}`)

	st, err := api.client(t).GetMessageStatus(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if r.Method != http.MethodGet || r.RequestURI != "/api/v1/sms/status/m1" || r.Body != "" || r.Header.Get("Content-Type") != "" {
		t.Errorf("request = %s %s, body %q, content type %q", r.Method, r.RequestURI, r.Body, r.Header.Get("Content-Type"))
	}
	if st.ID != "m1" || st.PhoneNumber != "+14155550123" || st.State != StateSent || st.State.Final() {
		t.Errorf("status = %+v", st)
	}
	if want := time.Date(2026, 11, 3, 19, 41, 46, 622_000_000, time.UTC); st.SentAt == nil || !st.SentAt.Equal(want) {
		t.Errorf("SentAt = %v, want %v", st.SentAt, want)
	}
	if st.DeliveredAt != nil || st.ErrorMessage != "" {
		t.Errorf("DeliveredAt = %v, ErrorMessage = %q", st.DeliveredAt, st.ErrorMessage)
	}
}

func TestGetMessageStatusReadsTheRealState(t *testing.T) {
	cases := []struct {
		data  string
		state State
		final bool
	}{
		{`{"status":"failed","unconfirmed":true,"deliveryStatus":"unconfirmed","errorMessage":"Not confirmed by the phone"}`, StateUnconfirmed, true},
		{`{"status":"failed","unconfirmed":true}`, StateUnconfirmed, true},
		{`{"status":"failed","deliveryStatus":"failed"}`, StateFailed, true},
		{`{"status":"delivered"}`, StateDelivered, true},
		{`{"status":"pending","deliveryStatus":"pending"}`, StatePending, false},
		{`{"status":"sent","deliveryStatus":"read-by-martians"}`, State("read-by-martians"), false},
	}
	for _, tc := range cases {
		api := answering(t, http.StatusOK, `{"success":true,"data":`+tc.data+`}`)

		st, err := api.client(t).GetMessageStatus(context.Background(), "m1")
		if err != nil {
			t.Fatal(err)
		}
		if st.State != tc.state || st.State.Final() != tc.final || st.ID != "m1" {
			t.Errorf("%s: state %q final %v id %q, want %q %v", tc.data, st.State, st.State.Final(), st.ID, tc.state, tc.final)
		}
	}
}

func TestGetMessageStatusEscapesTheID(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"id":"a/b?c"}}`)
	client := api.client(t)

	if _, err := client.GetMessageStatus(context.Background(), " a/b?c "); err != nil {
		t.Fatal(err)
	}

	if got := api.only(t).RequestURI; got != "/api/v1/sms/status/a%2Fb%3Fc" {
		t.Errorf("RequestURI = %s", got)
	}
	if _, err := client.GetMessageStatus(context.Background(), " "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("blank ID: err = %v", err)
	}
}

func TestListPhones(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[
		{"id":"6aea3248fedd7cccac9abf42","name":"Test phone","isOnline":true,"lastSeenAt":"2026-11-03T19:13:38.656Z"},
		{"id":"p2","name":"Old phone","isOnline":false}],"online":1,"total":2}}`)

	phones, err := api.client(t).ListPhones(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got := api.only(t).RequestURI; got != "/api/v1/devices" {
		t.Errorf("RequestURI = %s", got)
	}
	if len(phones) != 2 || phones[0].ID != "6aea3248fedd7cccac9abf42" || phones[0].Name != "Test phone" || !phones[0].Online {
		t.Fatalf("phones = %+v", phones)
	}
	if want := time.Date(2026, 11, 3, 19, 13, 38, 656_000_000, time.UTC); phones[0].LastSeenAt == nil || !phones[0].LastSeenAt.Equal(want) {
		t.Errorf("LastSeenAt = %v", phones[0].LastSeenAt)
	}
	if phones[1].Online || phones[1].LastSeenAt != nil {
		t.Errorf("second phone = %+v", phones[1])
	}
}

func TestListPhonesWithoutPhones(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[],"online":0,"total":0}}`)

	phones, err := api.client(t).ListPhones(context.Background())
	if err != nil || phones == nil || len(phones) != 0 {
		t.Errorf("phones = %#v, err = %v; want an empty slice", phones, err)
	}
}

func TestGetUsage(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"plan":"free","accountType":"sub_account",
		"usage":{"smsClean":12,"smsBranded":3,"twoFaClean":4,"smsCleanMonth":120},
		"limits":{"smsClean":25,"twoFaClean":50,"smsCleanPerMonth":500,"smsBrandedPerDay":25},
		"remaining":{"smsClean":13,"twoFaClean":46,"smsCleanMonth":380,"smsBranded":22},
		"enterprise":{"id":"e1","name":"Acme"}}}`)

	u, err := api.client(t).GetUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got := api.only(t).RequestURI; got != "/api/v1/account/usage" {
		t.Errorf("RequestURI = %s", got)
	}
	if u.Plan != "free" || u.AccountType != "sub_account" {
		t.Errorf("plan %q, account %q", u.Plan, u.AccountType)
	}
	if want := (UsageCounts{SMSClean: 12, SMSBranded: 3, TwoFAClean: 4, SMSCleanMonth: 120}); u.Used != want {
		t.Errorf("Used = %+v", u.Used)
	}
	if want := (UsageLimits{SMSClean: 25, TwoFAClean: 50, SMSCleanPerMonth: 500, SMSBrandedPerDay: 25}); u.Limits != want {
		t.Errorf("Limits = %+v", u.Limits)
	}
	if want := (UsageRemaining{SMSClean: 13, TwoFAClean: 46, SMSCleanMonth: 380, SMSBranded: 22}); u.Remaining != want {
		t.Errorf("Remaining = %+v", u.Remaining)
	}
	if u.Enterprise == nil || *u.Enterprise != (UsageEnterprise{ID: "e1", Name: "Acme"}) {
		t.Errorf("Enterprise = %+v", u.Enterprise)
	}
	if len(u.Raw) == 0 {
		t.Error("Raw is empty")
	}
}

func TestGetUsageReadsUnlimited(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"plan":"pro","accountType":"standard",
		"usage":{"smsClean":2,"smsBranded":0,"twoFaClean":0,"smsCleanMonth":2},
		"limits":{"smsClean":"unlimited","twoFaClean":"unlimited","smsCleanPerMonth":"unlimited","smsBrandedPerDay":0},
		"remaining":{"smsClean":"unlimited","twoFaClean":"unlimited","smsCleanMonth":"unlimited","smsBranded":0}}}`)

	u, err := api.client(t).GetUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if u.Limits.SMSClean != Unlimited || u.Limits.SMSBrandedPerDay != 0 || u.Remaining.TwoFAClean != Unlimited || u.Enterprise != nil {
		t.Errorf("usage = %+v", u)
	}
	out, err := json.Marshal(u.Remaining)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"smsClean":"unlimited","twoFaClean":"unlimited","smsCleanMonth":"unlimited","smsBranded":0}`; string(out) != want {
		t.Errorf("JSON = %s\nwant   %s", out, want)
	}
}

func TestQuotaReadsNumbersAndUnlimited(t *testing.T) {
	cases := map[string]Quota{`25`: 25, `"25"`: 25, `-1`: Unlimited, `"unlimited"`: Unlimited, `"Unlimited"`: Unlimited, `25.0`: 25}
	for in, want := range cases {
		var q Quota
		if err := json.Unmarshal([]byte(in), &q); err != nil || q != want {
			t.Errorf("%s → %v (%v), want %v", in, q, err, want)
		}
	}
	var q Quota = 7
	if err := json.Unmarshal([]byte(`null`), &q); err != nil || q != 7 {
		t.Errorf("null changed the quota: %v %v", q, err)
	}
	if err := json.Unmarshal([]byte(`"lots"`), &q); err == nil {
		t.Error(`"lots" was read as a quota`)
	}
}

func TestListReceivedTextsWithTheFilters(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"messages":[
		{"id":"r2","sender":"+14155550199","message":"Yes, see you then","isEncrypted":false,"receivedAt":"2026-11-03T10:00:00.000Z",
		 "simSlot":1,"subscriptionId":3,"deviceId":"6aea3248fedd7cccac9abf42","deviceName":"Test phone"},
		{"id":"r1","sender":"+14155550199","message":null,"isEncrypted":true,"encryptedPayload":"abc==","receivedAt":"2026-11-03T09:00:00.000Z",
		 "deviceId":"6aea3248fedd7cccac9abf42","deviceName":"Test phone"}],
		"total":7,"page":2,"limit":2,"hasMore":true}}`)

	page, err := api.client(t).ListReceivedTexts(context.Background(), ReceivedTextsQuery{
		Limit:    2,
		Page:     2,
		Since:    time.Date(2026, 11, 3, 12, 30, 0, 0, time.FixedZone("EET", 2*60*60)),
		Phone:    "+1 415",
		DeviceID: "6aea3248fedd7cccac9abf42",
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := "/api/v1/sms/received?deviceId=6aea3248fedd7cccac9abf42&limit=2&page=2&phone=%2B1+415&since=2026-11-03T10%3A30%3A00.000Z"; api.only(t).RequestURI != want {
		t.Errorf("RequestURI = %s\nwant         %s", api.only(t).RequestURI, want)
	}
	if page.Total != 7 || page.Page != 2 || page.Limit != 2 || !page.HasMore || len(page.Texts) != 2 {
		t.Fatalf("page = %+v", page)
	}
	first := page.Texts[0]
	if first.ID != "r2" || first.Sender != "+14155550199" || first.Message != "Yes, see you then" || first.Encrypted ||
		first.SIMSlot == nil || *first.SIMSlot != 1 || first.SubscriptionID == nil || *first.SubscriptionID != 3 || first.DeviceName != "Test phone" {
		t.Errorf("first = %+v", first)
	}
	if !first.ReceivedAt.Equal(time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("ReceivedAt = %v", first.ReceivedAt)
	}
	second := page.Texts[1]
	if second.Message != "" || !second.Encrypted || second.EncryptedPayload != "abc==" || second.SIMSlot != nil {
		t.Errorf("second = %+v", second)
	}
}

func TestListReceivedTextsWithoutFilters(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"messages":[],"total":0,"page":1,"limit":50,"hasMore":false}}`)

	page, err := api.client(t).ListReceivedTexts(context.Background(), ReceivedTextsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if got := api.only(t).RequestURI; got != "/api/v1/sms/received" {
		t.Errorf("RequestURI = %s", got)
	}
	if page.Texts == nil || len(page.Texts) != 0 || page.Limit != 50 || page.HasMore {
		t.Errorf("page = %#v", page)
	}
}

func TestListReceivedTextsRefusesALimitOrPageOutOfRange(t *testing.T) {
	api := answering(t, http.StatusOK, `{}`)
	client := api.client(t)

	for _, q := range []ReceivedTextsQuery{{Limit: 101}, {Limit: -1}, {Page: -1}} {
		if _, err := client.ListReceivedTexts(context.Background(), q); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: err = %v", q, err)
		}
	}
	if n := len(api.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestABaseURLWithAPathIsKept(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[]}}`)
	client, err := NewClient(testKey, WithBaseURL(api.server.URL+"/smstunnel"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListPhones(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := api.only(t).RequestURI; got != "/smstunnel/api/v1/devices" {
		t.Errorf("RequestURI = %s", got)
	}
	if got := client.BaseURL(); got != api.server.URL+"/smstunnel/" {
		t.Errorf("BaseURL = %s", got)
	}
}
