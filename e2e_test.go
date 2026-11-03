package smstunnel_test

// End-to-end tests: they send real texts, so they run only against an SMSTunnel test server named by the
// environment, and are skipped otherwise. They never run against smstunnel.io.
//
//	SMSTUNNEL_E2E_KEY_FILE   a JSON file {"serverUrl", "apiKey", "deviceId"}; a "keys" object is read too, with
//	SMSTUNNEL_E2E_KEY_NAME   choosing the key
//	SMSTUNNEL_E2E_URL, SMSTUNNEL_E2E_API_KEY, SMSTUNNEL_E2E_DEVICE_ID   the same, and they win over the file
//	SMSTUNNEL_E2E_RUN_ID     a word put in every text, to find them in the server's database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/narcisbodea/smstunnel-go"
)

type testServer struct {
	url, apiKey, deviceID, runID string
}

var (
	serverOnce sync.Once
	server     *testServer
	serverErr  error
)

func loadTestServer() (*testServer, error) {
	s := &testServer{}
	if file := os.Getenv("SMSTUNNEL_E2E_KEY_FILE"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var f struct {
			ServerURL string            `json:"serverUrl"`
			APIKey    string            `json:"apiKey"`
			DeviceID  string            `json:"deviceId"`
			Keys      map[string]string `json:"keys"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, err
		}
		s.url, s.apiKey, s.deviceID = f.ServerURL, f.APIKey, f.DeviceID
		if name := os.Getenv("SMSTUNNEL_E2E_KEY_NAME"); name != "" {
			if s.apiKey = f.Keys[name]; s.apiKey == "" {
				return nil, fmt.Errorf("the key file has no keys.%s", name)
			}
		}
	}
	for env, field := range map[string]*string{"SMSTUNNEL_E2E_URL": &s.url, "SMSTUNNEL_E2E_API_KEY": &s.apiKey, "SMSTUNNEL_E2E_DEVICE_ID": &s.deviceID} {
		if v := os.Getenv(env); v != "" {
			*field = v
		}
	}
	if s.url == "" || s.apiKey == "" {
		return nil, nil
	}
	u, err := url.Parse(s.url)
	if err != nil {
		return nil, err
	}
	if host := strings.ToLower(u.Hostname()); host == "smstunnel.io" || strings.HasSuffix(host, ".smstunnel.io") {
		return nil, errors.New("the end-to-end tests send real texts: never run them against smstunnel.io")
	}
	if s.runID = os.Getenv("SMSTUNNEL_E2E_RUN_ID"); s.runID == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		s.runID = hex.EncodeToString(b)
	}
	return s, nil
}

// e2e is the test server's client, or skips the test when there is no test server.
func e2e(t *testing.T, opts ...smstunnel.Option) (*smstunnel.Client, *testServer) {
	t.Helper()
	serverOnce.Do(func() { server, serverErr = loadTestServer() })
	if serverErr != nil {
		t.Fatal(serverErr)
	}
	if server == nil {
		t.Skip("no test server: set SMSTUNNEL_E2E_KEY_FILE, or SMSTUNNEL_E2E_URL and SMSTUNNEL_E2E_API_KEY")
	}
	client, err := smstunnel.NewClient(server.apiKey, append([]smstunnel.Option{smstunnel.WithBaseURL(server.url), smstunnel.WithUserAgentSuffix("e2e/" + server.runID)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func (s *testServer) text(what string) string { return "SMSTunnel Go e2e " + s.runID + ": " + what }

// recipient is a test number, +1415555xxxx.
func recipient() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(9000))
	return fmt.Sprintf("+1415555%04d", 1000+n.Int64())
}

func waitUntilSent(t *testing.T, client *smstunnel.Client, messageID string) *smstunnel.MessageStatus {
	t.Helper()
	giveUp := time.Now().Add(45 * time.Second)
	for {
		status, err := client.GetMessageStatus(context.Background(), messageID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State != smstunnel.StatePending || time.Now().After(giveUp) {
			return status
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestEndToEndListPhones(t *testing.T) {
	client, s := e2e(t)

	phones, err := client.ListPhones(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(phones) == 0 {
		t.Fatal("no phones")
	}
	if s.deviceID != "" {
		found := false
		for _, phone := range phones {
			if phone.ID == s.deviceID {
				found = true
				if !phone.Online || phone.LastSeenAt == nil {
					t.Errorf("the test phone is %+v, want online", phone)
				}
			}
		}
		if !found {
			t.Errorf("the test phone %s is not in %+v", s.deviceID, phones)
		}
	}
}

func TestEndToEndGetUsage(t *testing.T) {
	client, _ := e2e(t)

	usage, err := client.GetUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if usage.Plan == "" || usage.Used.SMSClean < 0 || len(usage.Raw) == 0 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestEndToEndSendSMS(t *testing.T) {
	client, s := e2e(t)
	to := recipient()

	res, err := client.SendSMS(context.Background(), to, s.text("a plain text"))
	if err != nil {
		t.Fatal(err)
	}
	status := waitUntilSent(t, client, res.MessageID)

	if res.MessageID == "" || res.Replayed || status.ID != res.MessageID || status.PhoneNumber != to {
		t.Errorf("result %+v, status %+v", res, status)
	}
	if (status.State != smstunnel.StateSent && status.State != smstunnel.StateDelivered) || status.SentAt == nil {
		t.Errorf("state %q, sent at %v", status.State, status.SentAt)
	}
}

func TestEndToEndLocalNumberWithTheTextsCountryCodeFromTheChosenPhone(t *testing.T) {
	client, s := e2e(t)
	to := recipient()

	res, err := client.SendSMS(context.Background(), strings.TrimPrefix(to, "+1"), s.text("a local number"),
		smstunnel.WithCountryCode("1"), smstunnel.WithDeviceID(s.deviceID))
	if err != nil {
		t.Fatal(err)
	}

	if status := waitUntilSent(t, client, res.MessageID); status.PhoneNumber != to || status.State != smstunnel.StateSent {
		t.Errorf("status = %+v, want sent to %s", status, to)
	}
}

func TestEndToEndLocalNumberWithTheClientsCountryCode(t *testing.T) {
	client, s := e2e(t, smstunnel.WithDefaultCountryCode("+1"))
	to := recipient()

	res, err := client.SendSMS(context.Background(), strings.TrimPrefix(to, "+1"), s.text("the client's country code"))
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.GetMessageStatus(context.Background(), res.MessageID)
	if err != nil {
		t.Fatal(err)
	}

	if status.PhoneNumber != to {
		t.Errorf("sent to %s, want %s", status.PhoneNumber, to)
	}
}

func TestEndToEndIdempotencyKeyAnswersARetryWithoutASecondText(t *testing.T) {
	client, s := e2e(t)
	to := recipient()
	key := smstunnel.WithIdempotencyKey("go-e2e-" + s.runID + "-" + to)

	first, err := client.SendSMS(context.Background(), to, s.text("sent once, asked twice"), key)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := client.SendSMS(context.Background(), to, s.text("sent once, asked twice"), key)
	if err != nil {
		t.Fatal(err)
	}
	_, other := client.SendSMS(context.Background(), to, s.text("another text, same key"), key)

	if first.Replayed || !retry.Replayed || first.MessageID != retry.MessageID {
		t.Errorf("first %+v, retry %+v", first, retry)
	}
	var apiErr *smstunnel.APIError
	if !errors.As(other, &apiErr) || apiErr.StatusCode != 422 || apiErr.Code != smstunnel.CodeIdempotencyKeyReused {
		t.Errorf("another text with the same key: %v", other)
	}
}

func TestEndToEndSendSignInCode(t *testing.T) {
	client, s := e2e(t)
	to := recipient()

	res, err := client.SendSignInCode(context.Background(), to, s.text("your sign-in code is 482913"))
	if err != nil {
		t.Fatal(err)
	}

	if status := waitUntilSent(t, client, res.MessageID); status.PhoneNumber != to || status.State != smstunnel.StateSent {
		t.Errorf("status = %+v", status)
	}
}

func TestEndToEndASignInCodeTextWithoutACodeIsRefused(t *testing.T) {
	client, _ := e2e(t)

	_, err := client.SendSignInCode(context.Background(), recipient(), "Welcome aboard!")

	var apiErr *smstunnel.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || !strings.Contains(apiErr.Message, "No valid verification code found") {
		t.Errorf("err = %v", err)
	}
}

func TestEndToEndListReceivedTexts(t *testing.T) {
	client, s := e2e(t)

	page, err := client.ListReceivedTexts(context.Background(), smstunnel.ReceivedTextsQuery{
		Limit: 5, Page: 1, Since: time.Now().Add(-7 * 24 * time.Hour), DeviceID: s.deviceID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if page.Page != 1 || page.Limit != 5 || page.Total < len(page.Texts) {
		t.Errorf("page = %+v", page)
	}
}

func TestEndToEndReceivedTextsCursor(t *testing.T) {
	client, _ := e2e(t)
	cursor, err := client.NewReceivedTextsCursor(smstunnel.ReceivedTextsCursorOptions{Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		texts, err := cursor.Next(context.Background())
		if err != nil || texts == nil {
			t.Fatalf("poll %d: %v, %v", i, texts, err)
		}
	}
}

func TestEndToEndAWrongKeyIsRefused(t *testing.T) {
	_, s := e2e(t)
	client, err := smstunnel.NewClient("sk_live_"+strings.Repeat("0", 32), smstunnel.WithBaseURL(s.url))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListPhones(context.Background())

	var apiErr *smstunnel.APIError
	if !errors.Is(err, smstunnel.ErrUnauthorized) || !errors.As(err, &apiErr) || apiErr.Message != "Invalid or expired API key" {
		t.Errorf("err = %v", err)
	}
}

func TestEndToEndAnUnknownMessageIsNotFound(t *testing.T) {
	client, _ := e2e(t)

	_, err := client.GetMessageStatus(context.Background(), "000000000000000000000000")

	if !errors.Is(err, smstunnel.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}
