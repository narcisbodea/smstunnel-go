package smstunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeInbox answers GET /api/v1/sms/received like SMSTunnel: the texts that reached it at or after `since`, newest
// first, in pages.
type fakeInbox struct {
	mu      sync.Mutex
	texts   map[string]time.Time // ID → when it reached SMSTunnel
	queries []url.Values
	failAt  map[int]bool // the numbers (1, 2, …) of the requests that answer 500
}

func newFakeInbox(t *testing.T) (*fakeInbox, *Client) {
	t.Helper()
	inbox := &fakeInbox{texts: map[string]time.Time{}, failAt: map[int]bool{}}
	server := httptest.NewServer(http.HandlerFunc(inbox.serve))
	t.Cleanup(server.Close)
	client, err := NewClient(testKey, WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	return inbox, client
}

func (f *fakeInbox) add(id string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts[id] = at
}

// failRequest makes the n-th request from now (1 for the next one) answer 500.
func (f *fakeInbox) failRequest(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAt[len(f.queries)+n] = true
}

func (f *fakeInbox) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.queries = append(f.queries, q)
	if f.failAt[len(f.queries)] {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"statusCode":500,"message":"Internal server error"}`))
		return
	}
	since, _ := time.Parse(time.RFC3339Nano, q.Get("since"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, _ := strconv.Atoi(q.Get("page"))
	if limit == 0 {
		limit = 50
	}
	if page == 0 {
		page = 1
	}
	type row struct {
		id string
		at time.Time
	}
	var rows []row
	for id, at := range f.texts {
		if !at.Before(since) {
			rows = append(rows, row{id, at})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
	messages := []map[string]any{}
	for i := (page - 1) * limit; i < len(rows) && i < page*limit; i++ {
		messages = append(messages, map[string]any{"id": rows[i].id, "sender": "+14155550199", "message": "text " + rows[i].id,
			"isEncrypted": false, "receivedAt": rows[i].at.UTC().Format(time.RFC3339Nano)})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
		"messages": messages, "total": len(rows), "page": page, "limit": limit, "hasMore": page*limit < len(rows)}})
}

func (f *fakeInbox) lastQuery() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}

// clock is a time.Now the test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func ids(texts []ReceivedText) []string {
	out := []string{}
	for _, text := range texts {
		out = append(out, text.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var t0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func newTestCursor(t *testing.T, client *Client, clk *clock, opts ReceivedTextsCursorOptions) *ReceivedTextsCursor {
	t.Helper()
	cursor, err := client.NewReceivedTextsCursor(opts)
	if err != nil {
		t.Fatal(err)
	}
	cursor.now = clk.now
	return cursor
}

func next(t *testing.T, cursor *ReceivedTextsCursor) []string {
	t.Helper()
	texts, err := cursor.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if texts == nil {
		t.Fatal("Next returned nil, want a slice")
	}
	return ids(texts)
}

func TestCursorReturnsNewTextsOnceOldestFirst(t *testing.T) {
	inbox, client := newFakeInbox(t)
	clk := &clock{t0.Add(5 * time.Second)}
	inbox.add("before", t0.Add(-10*time.Minute))
	inbox.add("b", t0.Add(1*time.Second))
	inbox.add("c", t0.Add(2*time.Second))
	cursor := newTestCursor(t, client, clk, ReceivedTextsCursorOptions{Since: t0})

	if got := next(t, cursor); !equal(got, []string{"b", "c"}) {
		t.Errorf("first poll = %v, want [b c]", got)
	}
	if got := inbox.lastQuery().Get("since"); got != "2026-11-03T12:00:00.000Z" {
		t.Errorf("first poll asked since %s", got)
	}

	inbox.add("d", t0.Add(6*time.Second))
	clk.t = t0.Add(10 * time.Second)
	if got := next(t, cursor); !equal(got, []string{"d"}) {
		t.Errorf("second poll = %v, want [d]", got)
	}
	// The poll's start minus the overlap is before Since: it never asks for less than you asked for.
	if got := inbox.lastQuery().Get("since"); got != "2026-11-03T12:00:00.000Z" {
		t.Errorf("second poll asked since %s", got)
	}

	clk.t = t0.Add(5 * time.Minute)
	if got := next(t, cursor); len(got) != 0 {
		t.Errorf("third poll = %v, want nothing", got)
	}
	if want := t0.Add(5*time.Minute - DefaultCursorOverlap); !cursor.Since().Equal(want) {
		t.Errorf("Since = %v, want %v", cursor.Since(), want)
	}

	inbox.add("e", t0.Add(5*time.Minute+time.Second))
	clk.t = t0.Add(5*time.Minute + 3*time.Second)
	if got := next(t, cursor); !equal(got, []string{"e"}) {
		t.Errorf("fourth poll = %v, want [e]", got)
	}
	if got := inbox.lastQuery().Get("since"); got != "2026-11-03T12:03:00.000Z" {
		t.Errorf("fourth poll asked since %s, want the third poll's start minus the overlap", got)
	}
}

func TestCursorReadsEveryPageAndGoesOnAfterThePageLimit(t *testing.T) {
	inbox, client := newFakeInbox(t)
	clk := &clock{t0.Add(10 * time.Minute)}
	for i := 1; i <= 7; i++ {
		inbox.add("t"+strconv.Itoa(i), t0.Add(time.Duration(i)*time.Second))
	}
	cursor := newTestCursor(t, client, clk, ReceivedTextsCursorOptions{Since: t0, PageSize: 2})
	cursor.maxPages = 2

	if got := next(t, cursor); !equal(got, []string{"t4", "t5", "t6", "t7"}) {
		t.Errorf("first call = %v", got)
	}
	if !cursor.Since().Equal(t0) {
		t.Errorf("Since moved to %v before the backlog was read", cursor.Since())
	}

	inbox.add("t8", t0.Add(10*time.Minute+30*time.Second)) // arrives while the backlog is being read
	clk.t = t0.Add(11 * time.Minute)
	if got := next(t, cursor); !equal(got, []string{"t1", "t2", "t3"}) {
		t.Errorf("second call = %v, want [t1 t2 t3]", got)
	}
	if got := inbox.lastQuery().Get("page"); got != "4" {
		t.Errorf("the second call ended on page %s, want 4", got)
	}
	if want := t0.Add(10*time.Minute - DefaultCursorOverlap); !cursor.Since().Equal(want) {
		t.Errorf("Since = %v, want %v (the backlog's first call minus the overlap)", cursor.Since(), want)
	}

	clk.t = t0.Add(12 * time.Minute)
	if got := next(t, cursor); !equal(got, []string{"t8"}) {
		t.Errorf("third call = %v, want [t8]", got)
	}
}

func TestCursorLosesNothingAfterAnError(t *testing.T) {
	inbox, client := newFakeInbox(t)
	clk := &clock{t0.Add(time.Minute)}
	inbox.add("a", t0.Add(time.Second))
	inbox.add("b", t0.Add(2*time.Second))
	inbox.add("c", t0.Add(3*time.Second))
	cursor := newTestCursor(t, client, clk, ReceivedTextsCursorOptions{Since: t0, PageSize: 2})

	inbox.failRequest(1) // the first page
	if _, err := cursor.Next(context.Background()); !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v, want ErrServer", err)
	}
	if !cursor.Since().Equal(t0) || len(cursor.Seen()) != 0 {
		t.Errorf("the failed call moved the cursor: since %v, seen %v", cursor.Since(), cursor.Seen())
	}

	inbox.failRequest(2) // the second page, after the first was read
	if _, err := cursor.Next(context.Background()); !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v, want ErrServer", err)
	}

	if got := next(t, cursor); !equal(got, []string{"a", "b", "c"}) {
		t.Errorf("after the errors = %v, want [a b c]", got)
	}
}

func TestCursorPassesTheFilters(t *testing.T) {
	inbox, client := newFakeInbox(t)
	cursor := newTestCursor(t, client, &clock{t0}, ReceivedTextsCursorOptions{Since: t0, Phone: "+1415", DeviceID: "6aea3248fedd7cccac9abf42", PageSize: 20})

	next(t, cursor)

	q := inbox.lastQuery()
	if q.Get("phone") != "+1415" || q.Get("deviceId") != "6aea3248fedd7cccac9abf42" || q.Get("limit") != "20" || q.Get("page") != "1" {
		t.Errorf("query = %v", q)
	}
}

func TestCursorGoesOnAfterARestart(t *testing.T) {
	inbox, client := newFakeInbox(t)
	inbox.add("a", t0.Add(time.Second))
	cursor := newTestCursor(t, client, &clock{t0.Add(10 * time.Second)}, ReceivedTextsCursorOptions{Since: t0})
	next(t, cursor)
	saved := ReceivedTextsCursorOptions{Since: cursor.Since(), Seen: cursor.Seen()}

	inbox.add("b", t0.Add(15*time.Second))
	restarted := newTestCursor(t, client, &clock{t0.Add(20 * time.Second)}, saved)

	if got := next(t, restarted); !equal(got, []string{"b"}) {
		t.Errorf("after the restart = %v, want [b]", got)
	}
	if !equal(saved.Seen, []string{"a"}) {
		t.Errorf("Seen = %v", saved.Seen)
	}
}

func TestCursorForgetsIDsThatCantComeBack(t *testing.T) {
	inbox, client := newFakeInbox(t)
	clk := &clock{t0.Add(time.Second)}
	inbox.add("a", t0)
	cursor := newTestCursor(t, client, clk, ReceivedTextsCursorOptions{Since: t0, Overlap: time.Minute})

	for _, at := range []time.Duration{time.Second, time.Minute, 2 * time.Minute, 3*time.Minute + 2*time.Second} {
		clk.t = t0.Add(at)
		next(t, cursor)
	}

	if seen := cursor.Seen(); len(seen) != 0 {
		t.Errorf("Seen = %v, want nothing after more than twice the overlap", seen)
	}
}

func TestNewReceivedTextsCursorRefusesBadOptions(t *testing.T) {
	client, err := NewClient(testKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []ReceivedTextsCursorOptions{{PageSize: 101}, {PageSize: -1}, {Overlap: -time.Second}} {
		if _, err := client.NewReceivedTextsCursor(opts); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: err = %v", opts, err)
		}
	}
}
