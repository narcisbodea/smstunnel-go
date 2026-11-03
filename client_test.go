package smstunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientRefusesAMissingOrBrokenKey(t *testing.T) {
	for _, key := range []string{"", "   ", "sk_live_with space", "sk_live_ключ"} {
		if _, err := NewClient(key); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "key") {
			t.Errorf("NewClient(%q): err = %v", key, err)
		}
	}
}

func TestNewClientTrimsTheKey(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[]}}`)
	client, err := NewClient("  "+testKey+"\r\n", WithBaseURL(api.server.URL))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListPhones(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := api.only(t).Header.Get("X-API-Key"); got != testKey {
		t.Errorf("X-API-Key = %q", got)
	}
}

func TestNewClientRefusesBadOptions(t *testing.T) {
	cases := map[string]Option{
		"ftp base URL":         WithBaseURL("ftp://smstunnel.io"),
		"relative base URL":    WithBaseURL("/api"),
		"no host":              WithBaseURL("https://"),
		"base URL with query":  WithBaseURL("https://smstunnel.io/?x=1"),
		"base URL with user":   WithBaseURL("https://me:secret@smstunnel.io"),
		"negative timeout":     WithTimeout(-time.Second),
		"nil http client":      WithHTTPClient(nil),
		"country code":         WithDefaultCountryCode("Romania"),
		"user agent line feed": WithUserAgentSuffix("tool/1.0\r\nX-Evil: 1"),
	}
	for name, opt := range cases {
		if _, err := NewClient(testKey, opt); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestNewClientDefaults(t *testing.T) {
	client, err := NewClient(testKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL() != "https://smstunnel.io/" || client.UserAgent() != "smstunnel-go/"+Version+" (+https://smstunnel.io)" || client.timeout != DefaultTimeout {
		t.Errorf("client = %+v", client)
	}
}

func TestWithUserAgentSuffix(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[]}}`)

	if _, err := api.client(t, WithUserAgentSuffix(" smstunnel-cli/0.3.0 (windows) ")).ListPhones(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got, want := api.only(t).Header.Get("User-Agent"), "smstunnel-go/"+Version+" (+https://smstunnel.io) smstunnel-cli/0.3.0 (windows)"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}

func TestNeverFollowsARedirectSoTheKeyStaysHome(t *testing.T) {
	elsewhere := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[]}}`)
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.server.URL+"/api/v1/devices", http.StatusFound)
	})

	for name, client := range map[string]*Client{
		"own http client":  api.client(t),
		"your http client": api.client(t, WithHTTPClient(&http.Client{Timeout: 5 * time.Second})),
	} {
		_, err := client.ListPhones(context.Background())

		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound || !strings.Contains(apiErr.Message, elsewhere.server.URL) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n := len(elsewhere.requests()); n != 0 {
		t.Errorf("the key went to the other address %d times", n)
	}
}

func TestWithHTTPClientKeepsYourClientAsItIs(t *testing.T) {
	yours := &http.Client{Timeout: 5 * time.Second}

	if _, err := NewClient(testKey, WithHTTPClient(yours)); err != nil {
		t.Fatal(err)
	}

	if yours.CheckRedirect != nil {
		t.Error("NewClient changed your http.Client")
	}
}

func TestWithHTTPClientUsesYourTransport(t *testing.T) {
	api := answering(t, http.StatusOK, `{"success":true,"data":{"devices":[]}}`)
	used := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used = true
		return http.DefaultTransport.RoundTrip(r)
	})

	if _, err := api.client(t, WithHTTPClient(&http.Client{Transport: transport})).ListPhones(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Error("your transport was not used")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNoAnswerInTimeIsATimeout(t *testing.T) {
	release := make(chan struct{})
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)

	_, err := api.client(t, WithTimeout(100*time.Millisecond)).SendSMS(context.Background(), "+14155550123", "Hi")

	var connErr *ConnectionError
	if !errors.As(err, &connErr) || !connErr.Timeout() || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %#v", err)
	}
	if !strings.HasPrefix(err.Error(), "smstunnel: no answer in time: ") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestTheContextsDeadlineIsATimeoutToo(t *testing.T) {
	release := make(chan struct{})
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := api.client(t, WithTimeout(0)).ListPhones(ctx)

	var connErr *ConnectionError
	if !errors.As(err, &connErr) || !connErr.Timeout() {
		t.Errorf("err = %#v", err)
	}
}

func TestCancellingIsNotAConnectionError(t *testing.T) {
	release := make(chan struct{})
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	_, err := api.client(t).SendSMS(ctx, "+14155550123", "Hi")

	var connErr *ConnectionError
	if !errors.Is(err, context.Canceled) || errors.As(err, &connErr) {
		t.Errorf("err = %#v, want context.Canceled and no ConnectionError", err)
	}
}

func TestAnAddressNobodyAnswersOnIsAConnectionError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	client, err := NewClient(testKey, WithBaseURL("http://"+address))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GetUsage(context.Background())

	var connErr *ConnectionError
	if !errors.As(err, &connErr) || connErr.Timeout() {
		t.Errorf("err = %#v", err)
	}
	if !strings.HasPrefix(err.Error(), "smstunnel: SMSTunnel could not be reached: ") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestANilContextIsRefused(t *testing.T) {
	api := answering(t, http.StatusOK, `{}`)

	//lint:ignore SA1012 checking that a nil context is refused
	_, err := api.client(t).ListPhones(nil) //nolint:staticcheck

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v", err)
	}
}

func TestPrintingTheClientNeverShowsTheKey(t *testing.T) {
	client, err := NewClient(testKey, WithBaseURL("http://127.0.0.1:3141"))
	if err != nil {
		t.Fatal(err)
	}

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(format, client); strings.Contains(out, testKey) || !strings.Contains(out, "127.0.0.1:3141") {
			t.Errorf("%s printed %q", format, out)
		}
	}
}

func TestAClientNotMadeByNewClientSaysSo(t *testing.T) {
	var zero Client

	if _, err := zero.ListPhones(context.Background()); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v", err)
	}
}

func TestTheAnswerIsReadUpToALimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"devices":[],"padding":"`)
		_, _ = io.WriteString(w, strings.Repeat("x", maxAnswerBytes))
		_, _ = io.WriteString(w, `"}}`)
	}))
	defer server.Close()
	client, err := NewClient(testKey, WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListPhones(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "not JSON") {
		t.Errorf("err = %v", err)
	}
}
