package smstunnel

import (
	"context"
	"sort"
	"time"
)

// DefaultCursorOverlap is how far back each poll of a [ReceivedTextsCursor] looks again, by default.
const DefaultCursorOverlap = 2 * time.Minute

// maxCursorPages bounds one Next call (100 pages of up to 100 texts); a longer backlog goes on with the next call.
const maxCursorPages = 100

// ReceivedTextsCursorOptions sets up a [ReceivedTextsCursor].
type ReceivedTextsCursorOptions struct {
	// Since is where to start: texts that reached SMSTunnel at or after this moment, by SMSTunnel's clock. Use
	// time.Now() to follow only new texts; the zero time starts with every text the account has.
	Since time.Time
	// Phone keeps only the texts from this sender (part of the number is enough).
	Phone string
	// DeviceID keeps only the texts received by this phone.
	DeviceID string
	// PageSize is how many texts each request asks for, 1 to 100; 0 means 100.
	PageSize int
	// Overlap is how far back each poll looks again, so that no text is missed because of a clock difference
	// between this computer and SMSTunnel. Texts seen in the overlap are not returned twice. 0 means
	// DefaultCursorOverlap.
	Overlap time.Duration
	// Seen are the IDs of texts already handled, for example Seen() saved by an earlier run along with Since():
	// they are not returned again.
	Seen []string
}

// ReceivedTextsCursor follows received texts as they arrive, for a watch loop: each call to Next returns the texts
// that reached SMSTunnel since the previous call, oldest first, each one once. It is not safe for concurrent use.
//
// SMSTunnel filters received texts by the time it got them from the phone, so a text the phone syncs late is still
// found. The cursor asks again for the last Overlap of every poll and remembers the IDs it returned in that window.
//
// It needs a key allowed to read received texts (sms:inbox).
type ReceivedTextsCursor struct {
	client  *Client
	query   ReceivedTextsQuery
	since   time.Time
	overlap time.Duration
	seen    map[string]time.Time // ID → when this cursor first returned it

	// A backlog longer than maxPages pages is read over several calls: resumePage is the next page to read, and
	// sequenceStart is when the first of those calls began.
	resumePage    int
	sequenceStart time.Time

	now      func() time.Time
	maxPages int
}

// NewReceivedTextsCursor returns a cursor over the texts your phones receive. See [ReceivedTextsCursor].
func (c *Client) NewReceivedTextsCursor(opts ReceivedTextsCursorOptions) (*ReceivedTextsCursor, error) {
	if opts.PageSize < 0 || opts.PageSize > 100 {
		return nil, invalid("the page size must be between 1 and 100 (0 for 100)")
	}
	if opts.Overlap < 0 {
		return nil, invalid("the overlap can't be negative")
	}
	rc := &ReceivedTextsCursor{
		client:   c,
		query:    ReceivedTextsQuery{Limit: opts.PageSize, Phone: opts.Phone, DeviceID: opts.DeviceID},
		since:    opts.Since,
		overlap:  opts.Overlap,
		seen:     make(map[string]time.Time, len(opts.Seen)),
		now:      time.Now,
		maxPages: maxCursorPages,
	}
	if rc.query.Limit == 0 {
		rc.query.Limit = 100
	}
	if rc.overlap == 0 {
		rc.overlap = DefaultCursorOverlap
	}
	created := rc.now()
	for _, id := range opts.Seen {
		if id != "" {
			rc.seen[id] = created
		}
	}
	return rc, nil
}

// Next returns the texts that arrived since the previous call (on the first call, since Options.Since), oldest
// first; none is an empty slice. After an error nothing is lost: the next call asks for the same texts again.
func (rc *ReceivedTextsCursor) Next(ctx context.Context) ([]ReceivedText, error) {
	pollStart := rc.now()
	first := rc.resumePage
	if first == 0 {
		first = 1
		rc.sequenceStart = pollStart
	}

	q := rc.query
	q.Since = rc.since
	var fresh []ReceivedText // newest first, as SMSTunnel lists them
	inThisCall := map[string]bool{}
	complete := false
	page := first
	for ; page < first+rc.maxPages; page++ {
		q.Page = page
		res, err := rc.client.ListReceivedTexts(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, text := range res.Texts {
			if _, old := rc.seen[text.ID]; old || inThisCall[text.ID] || text.ID == "" {
				continue // returned before, or pushed to this page by a newer text
			}
			inThisCall[text.ID] = true
			fresh = append(fresh, text)
		}
		if !res.HasMore || len(res.Texts) == 0 {
			complete = true
			break
		}
	}

	for _, text := range fresh {
		rc.seen[text.ID] = pollStart
	}
	if complete {
		// Next time: everything that reached SMSTunnel since this sequence of calls began, looking back by the overlap.
		if next := rc.sequenceStart.Add(-rc.overlap); next.After(rc.since) {
			rc.since = next
		}
		rc.resumePage = 0
	} else {
		rc.resumePage = page
	}
	// An ID first returned before since - overlap can't be listed again (unless the clocks differ by more than the overlap).
	forget := rc.since.Add(-rc.overlap)
	for id, at := range rc.seen {
		if at.Before(forget) {
			delete(rc.seen, id)
		}
	}

	for i, j := 0, len(fresh)-1; i < j; i, j = i+1, j-1 { // oldest first
		fresh[i], fresh[j] = fresh[j], fresh[i]
	}
	if fresh == nil {
		fresh = []ReceivedText{}
	}
	return fresh, nil
}

// Since is the moment the next poll asks from. Save it with Seen to go on after a restart.
func (rc *ReceivedTextsCursor) Since() time.Time { return rc.since }

// Seen lists, sorted, the IDs the cursor remembers, to pass as Options.Seen after a restart.
func (rc *ReceivedTextsCursor) Seen() []string {
	ids := make([]string, 0, len(rc.seen))
	for id := range rc.seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
