package smstunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

const scheduledAnswer = `{"success":true,"scheduled":true,"scheduledId":"6738c0ffee0123456789abcd","sendAt":"2026-11-16T07:00:00.000Z",` +
	`"data":{"id":"6738c0ffee0123456789abcd","to":"+14155550123","message":"Your appointment is tomorrow at 10:30.",` +
	`"sendAt":"2026-11-16T07:00:00.000Z","status":"scheduled","deviceId":null,"messageId":null,"error":null,` +
	`"createdAt":"2026-11-15T10:00:00.000Z"}}`

func TestWithSendAtSchedulesTheText(t *testing.T) {
	api := answering(t, http.StatusOK, scheduledAnswer)
	at := time.Date(2026, 11, 16, 9, 0, 0, 750_000_000, time.FixedZone("EET", 2*60*60))

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Your appointment is tomorrow at 10:30.", WithSendAt(at))
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if r.Method != http.MethodPost || r.RequestURI != "/api/v1/sms/send" {
		t.Errorf("request = %s %s, want POST /api/v1/sms/send", r.Method, r.RequestURI)
	}
	if want := `{"to":"+14155550123","message":"Your appointment is tomorrow at 10:30.","sendAt":"2026-11-16T09:00:00+02:00"}`; r.Body != want {
		t.Errorf("body = %s\nwant   %s", r.Body, want)
	}
	if res.MessageID != "" || res.Queued || res.Branded || res.Remaining != nil || res.Replayed {
		t.Errorf("result = %+v", res)
	}
	st := res.Scheduled
	if st == nil {
		t.Fatal("Scheduled = nil, want the scheduled text")
	}
	if st.ID != "6738c0ffee0123456789abcd" || st.To != "+14155550123" || st.Message != "Your appointment is tomorrow at 10:30." ||
		st.Status != ScheduledWaiting || st.Status.Done() || st.DeviceID != "" || st.MessageID != "" || st.Error != "" {
		t.Errorf("Scheduled = %+v", st)
	}
	if !st.SendAt.Equal(at.Truncate(time.Second)) {
		t.Errorf("SendAt = %v, want %v", st.SendAt, at.Truncate(time.Second))
	}
	if want := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC); st.CreatedAt == nil || !st.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", st.CreatedAt, want)
	}
	if string(res.Raw) != scheduledAnswer {
		t.Errorf("Raw = %s", res.Raw)
	}
}

func TestWithSendAtWritesRFC3339InWholeSeconds(t *testing.T) {
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC), "2026-11-16T07:00:00Z"},
		{time.Date(2026, 11, 16, 9, 30, 59, 999_999_999, time.FixedZone("", 2*60*60)), "2026-11-16T09:30:59+02:00"},
		{time.Date(2026, 11, 16, 2, 0, 0, 0, time.FixedZone("EST", -5*60*60)), "2026-11-16T02:00:00-05:00"},
		{time.Date(2026, 11, 16, 9, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60)), "2026-11-16T09:00:00+05:30"},
		// An offset with seconds can't be written in RFC 3339: the same moment goes in UTC.
		{time.Date(2026, 11, 16, 9, 0, 0, 0, time.FixedZone("LMT", 1*60*60+44*60+24)), "2026-11-16T07:15:36Z"},
	}
	for _, tc := range cases {
		api := answering(t, http.StatusOK, scheduledAnswer)

		if _, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi", WithSendAt(tc.at)); err != nil {
			t.Fatal(err)
		}

		if got, _ := decodeBody(t, api.only(t).Body)["sendAt"].(string); got != tc.want {
			t.Errorf("%v: sendAt = %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestWithSendAtKeepsTheOtherOptions(t *testing.T) {
	api := answering(t, http.StatusOK, scheduledAnswer)
	at := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)

	_, err := api.client(t, WithDefaultCountryCode("40")).SendSMS(context.Background(), "0721 123 456", "Hi",
		WithSendAt(at), WithDeviceID("6aea3248fedd7cccac9abf42"), WithIdempotencyKey("appointment-981-reminder"))
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if want := `{"to":"0721 123 456","message":"Hi","deviceId":"6aea3248fedd7cccac9abf42","defaultCountryCode":"+40","sendAt":"2026-11-16T07:00:00Z"}`; r.Body != want {
		t.Errorf("body = %s\nwant   %s", r.Body, want)
	}
	if got := r.Header.Get("Idempotency-Key"); got != "appointment-981-reminder" {
		t.Errorf("Idempotency-Key = %q", got)
	}
}

func TestAScheduledAnswerCanBeAReplay(t *testing.T) {
	api := answering(t, http.StatusOK, scheduledAnswer, "Idempotent-Replayed", "true")

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi",
		WithSendAt(time.Now().Add(time.Hour)), WithIdempotencyKey("k-1"))
	if err != nil {
		t.Fatal(err)
	}

	if !res.Replayed || res.Scheduled == nil || res.Scheduled.ID != "6738c0ffee0123456789abcd" {
		t.Errorf("result = %+v", res)
	}
}

func TestTheScheduledIDIsReadFromTheAnswerToo(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"scheduled":true,"scheduledId":"s1","sendAt":"2026-11-16T07:00:00.000Z",
		"data":{"status":"scheduled","sendAt":"2026-11-16T07:00:00.000Z"}}`)

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi", WithSendAt(time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}

	if res.Scheduled == nil || res.Scheduled.ID != "s1" || res.Scheduled.Status != ScheduledWaiting {
		t.Errorf("Scheduled = %+v", res.Scheduled)
	}
}

func TestATextSentNowHasNoScheduledText(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi")
	if err != nil {
		t.Fatal(err)
	}

	if res.Scheduled != nil || res.MessageID == "" {
		t.Errorf("result = %+v, Scheduled = %+v", res, res.Scheduled)
	}
	if body := api.only(t).Body; decodeBody(t, body)["sendAt"] != nil {
		t.Errorf("body = %s, want no sendAt", body)
	}
}

func TestWithSendAtRefusesAZeroTimeAndSignInCodes(t *testing.T) {
	api := answering(t, http.StatusOK, scheduledAnswer)
	client := api.client(t)

	_, zero := client.SendSMS(context.Background(), "+14155550123", "Hi", WithSendAt(time.Time{}))
	_, code := client.SendSignInCode(context.Background(), "+14155550123", "Your sign-in code is 482913", WithSendAt(time.Now().Add(time.Hour)))

	if !errors.Is(zero, ErrInvalidInput) {
		t.Errorf("zero time: err = %v, want ErrInvalidInput", zero)
	}
	if !errors.Is(code, ErrInvalidInput) {
		t.Errorf("sign-in code: err = %v, want ErrInvalidInput", code)
	}
	if n := len(api.requests()); n != 0 {
		t.Errorf("%d requests were sent, want none", n)
	}
}

func TestListScheduledTexts(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"items":[
		{"id":"s2","to":"+14155550123","message":"See you at 10:30.","sendAt":"2026-11-14T08:00:00.000Z","status":"sent",
		 "deviceId":"6aea3248fedd7cccac9abf42","messageId":"6aea38f9b38ed3bed4f8569f","error":null,"createdAt":"2026-11-13T08:00:00.000Z"},
		{"id":"s1","to":"+14155550124","message":"See you at 11:00.","sendAt":"2026-11-13T08:00:00.000Z","status":"failed",
		 "deviceId":null,"messageId":null,"error":"limit_reached","createdAt":null}],
		"total":3,"page":1,"limit":2,"waiting":4,"max":50}}`)

	page, err := api.client(t).ListScheduledTexts(context.Background(), ScheduledTextsQuery{Scope: ScheduledScopeDone, Limit: 2, Page: 1})
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if want := "/api/v1/sms/scheduled?limit=2&page=1&scope=done"; r.Method != http.MethodGet || r.RequestURI != want {
		t.Errorf("request = %s %s\nwant          GET %s", r.Method, r.RequestURI, want)
	}
	if page.Total != 3 || page.Page != 1 || page.Limit != 2 || !page.HasMore || page.Waiting != 4 || page.Max != 50 || len(page.Texts) != 2 {
		t.Fatalf("page = %+v", page)
	}
	sent := page.Texts[0]
	if sent.ID != "s2" || sent.Status != ScheduledSent || !sent.Status.Done() || sent.MessageID != "6aea38f9b38ed3bed4f8569f" ||
		sent.DeviceID != "6aea3248fedd7cccac9abf42" || sent.Error != "" || sent.CreatedAt == nil {
		t.Errorf("sent = %+v", sent)
	}
	if !sent.SendAt.Equal(time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("SendAt = %v", sent.SendAt)
	}
	failed := page.Texts[1]
	if failed.Status != ScheduledFailed || failed.Error != "limit_reached" || failed.DeviceID != "" || failed.MessageID != "" || failed.CreatedAt != nil {
		t.Errorf("failed = %+v", failed)
	}
}

func TestListScheduledTextsWithoutAQuery(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"items":[],"total":0,"page":1,"limit":50,"waiting":0,"max":10}}`)
	client := api.client(t)

	page, err := client.ListScheduledTexts(context.Background(), ScheduledTextsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListScheduledTexts(context.Background(), ScheduledTextsQuery{Scope: ScheduledScopeWaiting}); err != nil {
		t.Fatal(err)
	}

	reqs := api.requests()
	if reqs[0].RequestURI != "/api/v1/sms/scheduled" || reqs[1].RequestURI != "/api/v1/sms/scheduled?scope=waiting" {
		t.Errorf("RequestURIs = %s, %s", reqs[0].RequestURI, reqs[1].RequestURI)
	}
	if page.Texts == nil || len(page.Texts) != 0 || page.HasMore || page.Max != 10 {
		t.Errorf("page = %#v", page)
	}
}

func TestListScheduledTextsRefusesABadQuery(t *testing.T) {
	api := answering(t, http.StatusOK, `{}`)
	client := api.client(t)

	for _, q := range []ScheduledTextsQuery{{Scope: "all"}, {Scope: "Done"}, {Limit: 101}, {Limit: -1}, {Page: -1}} {
		if _, err := client.ListScheduledTexts(context.Background(), q); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: err = %v, want ErrInvalidInput", q, err)
		}
	}
	if n := len(api.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestGetScheduledText(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"id":"6738c0ffee0123456789abcd","to":"+14155550123","message":"Hi",
		"sendAt":"2026-11-16T07:00:00.000Z","status":"scheduled","deviceId":null,"messageId":null,"error":null,"createdAt":"2026-11-15T10:00:00.000Z"}}`)

	st, err := api.client(t).GetScheduledText(context.Background(), " 6738c0ffee0123456789abcd ")
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if r.Method != http.MethodGet || r.RequestURI != "/api/v1/sms/scheduled/6738c0ffee0123456789abcd" || r.Body != "" {
		t.Errorf("request = %s %s, body %q", r.Method, r.RequestURI, r.Body)
	}
	if st.ID != "6738c0ffee0123456789abcd" || st.To != "+14155550123" || st.Status != ScheduledWaiting || st.Status.Done() {
		t.Errorf("scheduled text = %+v", st)
	}
}

func TestCancelScheduledText(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"id":"s1","to":"+14155550123","message":"Hi",
		"sendAt":"2026-11-16T07:00:00.000Z","status":"canceled","deviceId":null,"messageId":null,"error":null,"createdAt":"2026-11-15T10:00:00.000Z"}}`)

	st, err := api.client(t).CancelScheduledText(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if r.Method != http.MethodDelete || r.RequestURI != "/api/v1/sms/scheduled/s1" || r.Body != "" || r.Header.Get("Content-Type") != "" {
		t.Errorf("request = %s %s, body %q, content type %q", r.Method, r.RequestURI, r.Body, r.Header.Get("Content-Type"))
	}
	if r.Header.Get("X-API-Key") != testKey || r.Header.Get("Idempotency-Key") != "" {
		t.Errorf("X-API-Key %q, Idempotency-Key %q", r.Header.Get("X-API-Key"), r.Header.Get("Idempotency-Key"))
	}
	if st.ID != "s1" || st.Status != ScheduledCanceled || !st.Status.Done() {
		t.Errorf("scheduled text = %+v", st)
	}
}

func TestScheduledTextIDsAreEscapedAndRequired(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"id":"a/b?c","status":"canceled"}}`)
	client := api.client(t)

	if _, err := client.CancelScheduledText(context.Background(), "a/b?c"); err != nil {
		t.Fatal(err)
	}
	if got := api.only(t).RequestURI; got != "/api/v1/sms/scheduled/a%2Fb%3Fc" {
		t.Errorf("RequestURI = %s", got)
	}
	for _, id := range []string{"", "  "} {
		if _, err := client.GetScheduledText(context.Background(), id); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("GetScheduledText(%q): err = %v", id, err)
		}
		if _, err := client.CancelScheduledText(context.Background(), id); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("CancelScheduledText(%q): err = %v", id, err)
		}
	}
	if n := len(api.requests()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestScheduledTextErrorsKeepTheirCode(t *testing.T) {
	cases := []struct {
		status   int
		code     string
		sentinel error
	}{
		{400, CodeScheduleTooSoon, nil},
		{400, CodeScheduleTooFar, nil},
		{400, CodeScheduleInvalidInput, nil},
		{400, CodeScheduleTextTooLong, nil},
		{400, CodeScheduleNumberNotAllowed, nil},
		{400, CodeScheduleSealedOnlyPhone, nil},
		{400, CodeScheduleEncryptedText, nil},
		{400, CodeScheduleSignInCode, nil},
		{404, CodeSchedulePhoneNotFound, ErrNotFound},
		{403, CodeScheduleLimitReached, ErrForbidden},
		{404, CodeScheduleNotFound, ErrNotFound},
		{409, CodeScheduleNotEditable, nil},
	}
	for _, tc := range cases {
		api := answering(t, tc.status, fmt.Sprintf(`{"statusCode":%d,"error":"Refused","code":%q,"message":"Words for %s"}`, tc.status, tc.code, tc.code))

		_, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi", WithSendAt(time.Now().Add(time.Hour)))

		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status || apiErr.Code != tc.code || apiErr.Message != "Words for "+tc.code {
			t.Errorf("%s: err = %#v", tc.code, err)
			continue
		}
		if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
			t.Errorf("%s: err doesn't match %v", tc.code, tc.sentinel)
		}
		if want := fmt.Sprintf("smstunnel: %d %s: Words for %s", tc.status, tc.code, tc.code); err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	}
}

func TestCancellingTooLateIsAConflict(t *testing.T) {
	api := answering(t, http.StatusConflict, `{"statusCode":409,"error":"Conflict","code":"SCHEDULE_NOT_EDITABLE",
		"message":"This text has already gone out or was canceled, or was scheduled through the API (cancel it instead)."}`)

	_, err := api.client(t).CancelScheduledText(context.Background(), "6738c0ffee0123456789abcd")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict || apiErr.Code != CodeScheduleNotEditable {
		t.Errorf("err = %#v", err)
	}
}

func TestAnUnknownScheduledTextIsNotFound(t *testing.T) {
	api := answering(t, http.StatusNotFound, `{"statusCode":404,"error":"Not Found","code":"SCHEDULE_NOT_FOUND","message":"This scheduled text no longer exists."}`)

	_, err := api.client(t).GetScheduledText(context.Background(), "6738c0ffee0123456789abcd")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrNotFound) || apiErr.Code != CodeScheduleNotFound {
		t.Errorf("err = %#v", err)
	}
}

func TestTheLimitOfWaitingTextsSaysTheMax(t *testing.T) {
	api := answering(t, http.StatusForbidden, `{"statusCode":403,"error":"Forbidden","code":"SCHEDULE_LIMIT_REACHED",
		"message":"You have as many scheduled texts as your plan allows. Wait until some go out, or cancel one.","max":10}`)

	_, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi", WithSendAt(time.Now().Add(time.Hour)))

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrForbidden) || apiErr.Code != CodeScheduleLimitReached {
		t.Fatalf("err = %#v", err)
	}
	var body struct {
		Max int `json:"max"`
	}
	if err := json.Unmarshal(apiErr.Body, &body); err != nil || body.Max != 10 {
		t.Errorf("Body = %s (%v), want max 10", apiErr.Body, err)
	}
}

func TestAServerWithoutScheduledTextsRefusesSendAt(t *testing.T) {
	api := answering(t, http.StatusBadRequest, `{"message":["property sendAt should not exist"],"error":"Bad Request","statusCode":400}`)

	_, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi", WithSendAt(time.Now().Add(time.Hour)))

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || apiErr.Code != "" || apiErr.Message != "property sendAt should not exist" {
		t.Errorf("err = %#v", err)
	}
}

func TestListingScheduledTextsTooOftenSaysHowLongToWait(t *testing.T) {
	api := answering(t, http.StatusTooManyRequests, `{"statusCode":429,"message":"ThrottlerException: Too Many Requests"}`, "Retry-After", "5")

	_, err := api.client(t).ListScheduledTexts(context.Background(), ScheduledTextsQuery{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrRateLimited) || apiErr.RetryAfter != 5*time.Second {
		t.Errorf("err = %#v", err)
	}
}
