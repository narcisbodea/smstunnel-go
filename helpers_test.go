package smstunnel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Split so that secret scanners do not take this fake key for a real one.
const testKey = "sk_" + "live_" + "0123456789abcdef" + "0123456789abcdef"

const sendAnswer = `{"success":true,"messageId":"6aea38f9b38ed3bed4f8569f","data":{"messageId":"6aea38f9b38ed3bed4f8569f","branded":false,"remaining":{"clean":-1,"total":"unlimited"}}}`

// recorded is a request as it reached the fake API.
type recorded struct {
	Method     string
	RequestURI string
	Path       string
	Query      string
	Header     http.Header
	Body       string
}

type fakeAPI struct {
	server *httptest.Server
	mu     sync.Mutex
	reqs   []recorded
}

// newFakeAPI starts an httptest server that keeps every request and answers with respond.
func newFakeAPI(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, recorded{r.Method, r.RequestURI, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(body)})
		f.mu.Unlock()
		respond(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// answering is a fake API that always answers status with this JSON.
func answering(t *testing.T, status int, body string, headers ...string) *fakeAPI {
	t.Helper()
	return newFakeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func (f *fakeAPI) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

// only is the one request the fake API got.
func (f *fakeAPI) only(t *testing.T) recorded {
	t.Helper()
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	return reqs[0]
}

// client is a Client for the fake API.
func (f *fakeAPI) client(t *testing.T, opts ...Option) *Client {
	t.Helper()
	c, err := NewClient(testKey, append([]Option{WithBaseURL(f.server.URL)}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("the body is not JSON: %v\n%s", err, body)
	}
	return m
}
