package smstunnel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAWrongKeyIsUnauthorized(t *testing.T) {
	api := answering(t, http.StatusUnauthorized, `{"message":"Invalid or expired API key","error":"Unauthorized","statusCode":401}`)

	_, err := api.client(t).ListPhones(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %#v", err)
	}
	if apiErr.StatusCode != 401 || apiErr.Message != "Invalid or expired API key" || apiErr.Code != "" || apiErr.RetryAfter != 0 {
		t.Errorf("APIError = %+v", apiErr)
	}
	if got := err.Error(); got != "smstunnel: 401: Invalid or expired API key" {
		t.Errorf("Error() = %q", got)
	}
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrServer) || errors.Is(err, ErrInvalidInput) {
		t.Error("a 401 matches another sentinel")
	}
}

func TestAMissingPermissionHasItsCodeAndName(t *testing.T) {
	api := answering(t, http.StatusForbidden, `{"statusCode":403,"error":"Forbidden","code":"PERMISSION_REQUIRED","permission":"sms:inbox",
		"message":"This API key may not read received texts. Allow it in the dashboard."}`)

	_, err := api.client(t).ListReceivedTexts(context.Background(), ReceivedTextsQuery{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %#v", err)
	}
	if apiErr.Code != CodePermissionRequired || apiErr.Permission != "sms:inbox" {
		t.Errorf("code %q, permission %q", apiErr.Code, apiErr.Permission)
	}
	if got := err.Error(); got != "smstunnel: 403 PERMISSION_REQUIRED: This API key may not read received texts. Allow it in the dashboard." {
		t.Errorf("Error() = %q", got)
	}
}

func TestTooManyRequestsSaysHowLongToWait(t *testing.T) {
	api := answering(t, http.StatusTooManyRequests, `{"statusCode":429,"message":"ThrottlerException: Too Many Requests"}`, "Retry-After", "17")

	_, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %#v", err)
	}
	if apiErr.RetryAfter != 17*time.Second || apiErr.Message != "ThrottlerException: Too Many Requests" {
		t.Errorf("APIError = %+v", apiErr)
	}
}

func TestRetryAfterCanBeADate(t *testing.T) {
	api := answering(t, http.StatusTooManyRequests, `{}`, "Retry-After", time.Now().Add(90*time.Second).UTC().Format(http.TimeFormat))

	_, err := api.client(t).GetUsage(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %#v", err)
	}
	if apiErr.RetryAfter < 80*time.Second || apiErr.RetryAfter > 91*time.Second {
		t.Errorf("RetryAfter = %v, want about 90s", apiErr.RetryAfter)
	}
	if apiErr.Message != "SMSTunnel returned error: 429 Too Many Requests" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

func TestRetryAfterIgnoresWhatIsntAWait(t *testing.T) {
	now := time.Now()
	for value, want := range map[string]time.Duration{
		"": 0, "0": 0, "-5": 0, "NaN": 0, "Inf": 0, "soon": 0, "1.5": 1500 * time.Millisecond,
		"999999999": 24 * time.Hour, now.Add(-time.Minute).UTC().Format(http.TimeFormat): 0,
	} {
		if got := retryAfter(value, now); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestTheDailyLimitHasNoRetryAfter(t *testing.T) {
	api := answering(t, http.StatusTooManyRequests, `{"statusCode":429,"message":"You reached today's limit of 300 texts on the Starter plan. More can go out tomorrow, or on a higher plan."}`)

	_, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != 0 || !strings.HasPrefix(apiErr.Message, "You reached today's limit") {
		t.Errorf("err = %#v", err)
	}
}

func TestAServerProblemIsAServerError(t *testing.T) {
	for _, status := range []int{500, 502, 503} {
		api := answering(t, status, `{"statusCode":500,"message":"Internal server error"}`)

		_, err := api.client(t).SendSMS(context.Background(), "+14155550123", "Hi")

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !errors.Is(err, ErrServer) || apiErr.StatusCode != status || apiErr.Message != "Internal server error" {
			t.Errorf("%d: err = %#v", status, err)
		}
	}
}

func TestAProxyErrorPageReadsAsPlainWords(t *testing.T) {
	page := "<html><head><title>503 Service Temporarily Unavailable</title></head><body><center><h1>503 Service Temporarily Unavailable</h1></center><hr><center>nginx</center></body></html>"
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, page)
	})

	_, err := api.client(t).ListPhones(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %#v", err)
	}
	if want := "503 Service Temporarily Unavailable 503 Service Temporarily Unavailable nginx"; apiErr.Message != want {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if string(apiErr.Body) != page {
		t.Errorf("Body = %s", apiErr.Body)
	}
}

func TestRefusedFieldsAreListed(t *testing.T) {
	api := answering(t, http.StatusBadRequest, `{"message":["Phone number must contain 7-25 characters (digits, spaces, dots, dashes, + allowed)",
		"to must be a string in international format, e.g. \"+14155550123\""],"error":"Bad Request","statusCode":400}`)

	_, err := api.client(t).SendSMS(context.Background(), "+1", "Hi")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("err = %#v", err)
	}
	if want := `Phone number must contain 7-25 characters (digits, spaces, dots, dashes, + allowed); to must be a string in international format, e.g. "+14155550123"`; apiErr.Message != want {
		t.Errorf("Message = %q", apiErr.Message)
	}
	for _, sentinel := range []error{ErrUnauthorized, ErrForbidden, ErrNotFound, ErrRateLimited, ErrServer} {
		if errors.Is(err, sentinel) {
			t.Errorf("a 400 matches %v", sentinel)
		}
	}
}

func TestASignInCodeTextWithoutACodeIsRefused(t *testing.T) {
	api := answering(t, http.StatusBadRequest, `{"message":"Invalid 2FA message: No valid verification code found. Put the code in the text as a number of its own.","error":"Bad Request","statusCode":400}`)

	_, err := api.client(t).SendSignInCode(context.Background(), "+14155550123", "Welcome!")

	if err == nil || !strings.Contains(err.Error(), "No valid verification code found") {
		t.Errorf("err = %v", err)
	}
}

func TestIdempotencyErrorsKeepTheirCode(t *testing.T) {
	calls := 0
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"statusCode":409,"error":"Conflict","code":"IDEMPOTENCY_KEY_IN_PROGRESS","message":"A request with this Idempotency-Key is still being processed."}`)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"statusCode":422,"error":"Unprocessable Entity","code":"IDEMPOTENCY_KEY_REUSED","message":"This Idempotency-Key was already used."}`)
	})
	client := api.client(t)

	_, inProgress := client.SendSMS(context.Background(), "+14155550123", "Hi", WithIdempotencyKey("k-1"))
	_, reused := client.SendSMS(context.Background(), "+14155550123", "Hi", WithIdempotencyKey("k-1"))

	var first, second *APIError
	if !errors.As(inProgress, &first) || first.StatusCode != 409 || first.Code != CodeIdempotencyKeyInProgress || first.RetryAfter != time.Second {
		t.Errorf("in progress: %#v", inProgress)
	}
	if !errors.As(reused, &second) || second.StatusCode != 422 || second.Code != CodeIdempotencyKeyReused {
		t.Errorf("reused: %#v", reused)
	}
}

func TestAnUnknownMessageIsNotFound(t *testing.T) {
	api := answering(t, http.StatusNotFound, `{"message":"Message not found","error":"Not Found","statusCode":404}`)

	_, err := api.client(t).GetMessageStatus(context.Background(), "000000000000000000000000")

	if !errors.Is(err, ErrNotFound) || err.Error() != "smstunnel: 404: Message not found" {
		t.Errorf("err = %v", err)
	}
}

func TestAnEmptyErrorBodyStillSaysTheStatus(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })

	_, err := api.client(t).GetUsage(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "SMSTunnel returned error: 403 Forbidden" {
		t.Errorf("err = %v", err)
	}
}

func TestASuccessStatusWithoutJSONIsAnError(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>Welcome</html>")
	})

	_, err := api.client(t).ListPhones(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 200 || !strings.Contains(apiErr.Message, "not JSON") {
		t.Errorf("err = %v", err)
	}
}

func TestASuccessStatusWithSuccessFalseIsAnError(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":false,"error":"Device not found"}`)

	_, err := api.client(t).ListPhones(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "Device not found" {
		t.Errorf("err = %v", err)
	}
}

func TestAnAnswerOfTheWrongShapeIsAnError(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":"none"}}`)

	_, err := api.client(t).ListPhones(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "could not be read") {
		t.Errorf("err = %v", err)
	}
}
