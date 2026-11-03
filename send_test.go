package smstunnel

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSendSMSPostsOneTextAsJSON(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)

	if _, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Your order #1042 has shipped."); err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if r.Method != http.MethodPost || r.RequestURI != "/api/v1/sms/send" {
		t.Errorf("request = %s %s, want POST /api/v1/sms/send", r.Method, r.RequestURI)
	}
	for name, want := range map[string]string{
		"X-API-Key":    testKey,
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"User-Agent":   "smstunnel-go/" + Version + " (+https://smstunnel.io)",
	} {
		if got := r.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"Idempotency-Key", "Authorization"} {
		if got := r.Header.Get(name); got != "" {
			t.Errorf("%s = %q, want none", name, got)
		}
	}
	if want := `{"to":"+14155550123","message":"Your order #1042 has shipped."}`; r.Body != want {
		t.Errorf("body = %s\nwant   %s", r.Body, want)
	}
}

func TestSendSMSReadsTheAnswer(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi")
	if err != nil {
		t.Fatal(err)
	}

	if res.MessageID != "6aea38f9b38ed3bed4f8569f" || res.Queued || res.Branded || res.Replayed {
		t.Errorf("result = %+v", res)
	}
	if res.Remaining == nil || res.Remaining.Clean != Unlimited || res.Remaining.Total != Unlimited {
		t.Errorf("remaining = %+v, want unlimited", res.Remaining)
	}
	if !res.Remaining.Clean.IsUnlimited() || res.Remaining.Total.String() != "unlimited" {
		t.Errorf("Quota helpers: %v %s", res.Remaining.Clean.IsUnlimited(), res.Remaining.Total)
	}
	if string(res.Raw) != sendAnswer {
		t.Errorf("Raw = %s", res.Raw)
	}
}

func TestSendSMSSaysWhenTheTextWaitsForAnOfflinePhone(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"messageId":"m1","queued":true,"data":{"messageId":"m1","branded":true,"queued":true,"remaining":{"clean":0,"total":24}}}`)

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi")
	if err != nil {
		t.Fatal(err)
	}

	if !res.Queued || !res.Branded || res.Remaining.Clean != 0 || res.Remaining.Total != 24 {
		t.Errorf("result = %+v, remaining = %+v", res, res.Remaining)
	}
}

func TestSendSMSSendsTheOptions(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)

	_, err := api.client(t).SendSMS(context.Background(), " 0721 123 456 ", "Hi",
		WithDeviceID(" 6aea3248fedd7cccac9abf42 "), WithCountryCode("0040"), WithIdempotencyKey("order-1042-shipped"))
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if want := `{"to":"0721 123 456","message":"Hi","deviceId":"6aea3248fedd7cccac9abf42","defaultCountryCode":"+40"}`; r.Body != want {
		t.Errorf("body = %s\nwant   %s", r.Body, want)
	}
	if got := r.Header.Get("Idempotency-Key"); got != "order-1042-shipped" {
		t.Errorf("Idempotency-Key = %q", got)
	}
}

func TestTheTextsCountryCodeWinsOverTheClients(t *testing.T) {
	cases := []struct{ client, text, want string }{
		{"", "", ""},
		{"49", "", "+49"},
		{"+49", "+40", "+40"},
		{"", "1", "+1"},
		{"0049", " ", "+49"},
	}
	for _, tc := range cases {
		api := answering(t, http.StatusOK, sendAnswer)
		var opts []Option
		if tc.client != "" {
			opts = append(opts, WithDefaultCountryCode(tc.client))
		}

		if _, err := api.client(t, opts...).SendSMS(context.Background(), "4155550123", "Hi", WithCountryCode(tc.text)); err != nil {
			t.Fatal(err)
		}

		got, _ := decodeBody(t, api.only(t).Body)["defaultCountryCode"].(string)
		if got != tc.want {
			t.Errorf("client %q, text %q: sent %q, want %q", tc.client, tc.text, got, tc.want)
		}
	}
}

func TestSendSignInCodeGoesToSend2FA(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)

	res, err := api.client(t).SendSignInCode(context.Background(), "+14155550123", "Your sign-in code is 482913", WithIdempotencyKey("login-77"))
	if err != nil {
		t.Fatal(err)
	}

	r := api.only(t)
	if r.RequestURI != "/api/v1/sms/send-2fa" || r.Header.Get("Idempotency-Key") != "login-77" {
		t.Errorf("request = %s, key %q", r.RequestURI, r.Header.Get("Idempotency-Key"))
	}
	if want := `{"to":"+14155550123","message":"Your sign-in code is 482913"}`; r.Body != want {
		t.Errorf("body = %s", r.Body)
	}
	if res.MessageID != "6aea38f9b38ed3bed4f8569f" {
		t.Errorf("MessageID = %q", res.MessageID)
	}
}

func TestSendSMSSaysWhenTheAnswerIsAReplay(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer, "Idempotent-Replayed", "true")

	res, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi", WithIdempotencyKey("k-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replayed {
		t.Error("Replayed = false, want true")
	}
}

func TestSendSMSKeepsTheTextAsWritten(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)
	text := "  Ți-am scris: \"ok\" <b>&</b> 👍\nLinia 2  "

	if _, err := api.client(t).SendSMS(context.Background(), "+14155550123", text); err != nil {
		t.Fatal(err)
	}

	body := api.only(t).Body
	if got := decodeBody(t, body)["message"]; got != text {
		t.Errorf("message = %q, want %q", got, text)
	}
	if !strings.Contains(body, "<b>&</b>") {
		t.Errorf("HTML characters were escaped: %s", body)
	}
}

func TestSendRefusesBadInputBeforeSending(t *testing.T) {
	cases := []struct {
		name, to, message string
		opts              []SendOption
	}{
		{"no number", "", "Hi", nil},
		{"blank number", "   ", "Hi", nil},
		{"two numbers", "+14155550123, +14155550124", "Hi", nil},
		{"two numbers with ;", "+14155550123;+14155550124", "Hi", nil},
		{"no text", "+14155550123", "", nil},
		{"blank text", "+14155550123", "  ", nil},
		{"letters", "0721123456", "Hi", []SendOption{WithCountryCode("abc")}},
		{"zero", "0721123456", "Hi", []SendOption{WithCountryCode("+0")}},
		{"five digits", "0721123456", "Hi", []SendOption{WithCountryCode("12345")}},
		{"letter O", "0721123456", "Hi", []SendOption{WithCountryCode("+4O")}},
		{"two plus", "0721123456", "Hi", []SendOption{WithCountryCode("++40")}},
		{"other digits", "0721123456", "Hi", []SendOption{WithCountryCode("٤٠")}},
		{"key not ASCII", "+14155550123", "Hi", []SendOption{WithIdempotencyKey("ключ")}},
		{"key with a line break", "+14155550123", "Hi", []SendOption{WithIdempotencyKey("line\nbreak")}},
		{"blank key", "+14155550123", "Hi", []SendOption{WithIdempotencyKey("   ")}},
		{"long key", "+14155550123", "Hi", []SendOption{WithIdempotencyKey(strings.Repeat("k", 256))}},
	}
	api := answering(t, http.StatusOK, sendAnswer)
	client := api.client(t)
	for _, tc := range cases {
		_, err := client.SendSMS(context.Background(), tc.to, tc.message, tc.opts...)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", tc.name, err)
		}
		_, err = client.SendSignInCode(context.Background(), tc.to, tc.message, tc.opts...)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s (sign-in code): err = %v, want ErrInvalidInput", tc.name, err)
		}
	}
	if n := len(api.requests()); n != 0 {
		t.Errorf("%d requests were sent, want none", n)
	}
}

func TestAnEmptyIdempotencyKeyMeansNone(t *testing.T) {
	api := answering(t, http.StatusOK, sendAnswer)
	client := api.client(t)

	for _, key := range []string{"", strings.Repeat("k", 255)} {
		if _, err := client.SendSMS(context.Background(), "+14155550123", "Hi", WithIdempotencyKey(key), nil); err != nil {
			t.Fatal(err)
		}
	}

	reqs := api.requests()
	if got := reqs[0].Header.Get("Idempotency-Key"); got != "" {
		t.Errorf("empty key sent as %q", got)
	}
	if got := reqs[1].Header.Get("Idempotency-Key"); got != strings.Repeat("k", 255) {
		t.Errorf("255-character key sent as %q", got)
	}
}
