# SMSTunnel for Go

`smstunnel`: send texts from your own Go programs through **your own Android phone**, from **your own number**, with **[SMSTunnel](https://smstunnel.io)**.

Your program hands the text to your SMSTunnel account and your phone sends it from its own SIM, so recipients see a number they know and can reply to. You can cut a program off at any time by deleting its API key in your dashboard.

Your phone needs the SMSTunnel app from Google Play:

<a href="https://play.google.com/store/apps/details?id=io.smstunnel.app"><img src="https://smstunnel.io/google-play-badge.png" alt="Get it on Google Play" height="56"></a>

[![Go Reference](https://pkg.go.dev/badge/github.com/narcisbodea/smstunnel-go.svg)](https://pkg.go.dev/github.com/narcisbodea/smstunnel-go) · Docs: [smstunnel.io/docs](https://smstunnel.io/docs/) · Keys: [dashboard.smstunnel.io/apps](https://dashboard.smstunnel.io/apps)

- Go 1.22 or newer. No dependencies: only the standard library.
- Every call takes a `context.Context`; errors keep the HTTP status, SMSTunnel's code and the Retry-After wait.

## Install

```bash
go get github.com/narcisbodea/smstunnel-go
```

## Get your key

1. Install the [SMSTunnel app from Google Play](https://play.google.com/store/apps/details?id=io.smstunnel.app) on your Android phone and sign in. Sign in at [smstunnel.io](https://smstunnel.io) on your computer with the same account: the phone appears under **Phones** (you can also link it with the QR code from **Phones → Add phone**).
2. In your [SMSTunnel dashboard](https://dashboard.smstunnel.io/apps), open **Apps**.
3. Click **Add an app** and copy its key (`sk_live_…`). It can send texts and look up whether they went out (`sms:send` and `sms:read`). Reading received texts is a separate opt-in for each key: **Can also read received texts** (`sms:inbox`).

Keep the key out of your code: in an environment variable such as `SMSTUNNEL_API_KEY`, or your secret store.

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/narcisbodea/smstunnel-go"
)

func main() {
	client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	res, err := client.SendSMS(context.Background(), "+14155550123", "Your order #1042 has shipped.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.MessageID) // keep it to look up the status later
}
```

Make one `Client` and reuse it: it is safe for concurrent use.

## Usage

### Send a text

```go
res, err := client.SendSMS(ctx,
	"+14155550123",      // international format; one number per call
	"See you at 10:30.", // up to 1600 characters
	smstunnel.WithDeviceID("6aea3248fedd7cccac9abf42"),       // the phone that sends it (Phones → Copy); leave it out to let SMSTunnel pick one of yours
	smstunnel.WithCountryCode("+40"),                         // completes numbers written without + or 00, like 0721 123 456
	smstunnel.WithIdempotencyKey("appointment-981-reminder"), // makes a retry safe, see below
)

res.MessageID // string
res.Queued    // true when no phone of yours was online: the text waits and goes out when one is back
res.Branded   // true when the Free plan added its short line to the text
res.Replayed  // true when this is the stored answer to an earlier call with the same idempotency key
```

One call sends one text to one number. To text several people, call `SendSMS` once for each.

### Send a sign-in or verification code

```go
_, err := client.SendSignInCode(ctx, "+14155550123", "Your sign-in code is 482913")
```

The text must contain the code as a number of its own, 4 to 10 digits (`482913` or `123-456`), and be at most 320 characters; otherwise SMSTunnel refuses it (400). Codes have their own daily allowance. Use it for codes only.

### Look up a status

```go
status, err := client.GetMessageStatus(ctx, res.MessageID) // needs sms:read

status.State         // smstunnel.StatePending, StateSent, StateDelivered, StateFailed or StateUnconfirmed
status.State.Final() // true once it won't change: delivered, failed or unconfirmed
status.SentAt        // *time.Time
status.ErrorMessage  // why it failed
```

`StateUnconfirmed` means the phone handed the text to the SIM but never got the confirmation: it most likely went out, and it is never resent automatically. Don't resend it yourself.

### Your phones and your usage

```go
phones, err := client.ListPhones(ctx)
for _, phone := range phones {
	fmt.Println(phone.ID, phone.Name, phone.Online)
}

usage, err := client.GetUsage(ctx)
usage.Plan                   // "free", "starter", "standard", "pro" or "enterprise"
usage.Used.SMSClean          // texts sent today
usage.Remaining.SMSClean     // texts left today: a Quota, smstunnel.Unlimited when there is no limit
usage.Remaining.TwoFAClean   // sign-in codes left today
```

### Read received texts

Needs a key you allowed to read received texts (**Can also read received texts**) and syncing turned on in the app. When it is on, only texts that arrive from then on are synced.

```go
page, err := client.ListReceivedTexts(ctx, smstunnel.ReceivedTextsQuery{
	Since: time.Now().Add(-24 * time.Hour),
	Phone: "+14155550123", // only from this sender
	Limit: 20,             // 1 to 100
	Page:  1,
})
for _, text := range page.Texts { // newest first
	fmt.Println(text.ReceivedAt.Format(time.Kitchen), text.Sender, text.Message)
}
page.HasMore // ask for the next page
```

To follow texts as they arrive, use a cursor. Each `Next` returns the texts that came in since the previous call, oldest first, each one once, reading as many pages as it needs:

```go
cursor, err := client.NewReceivedTextsCursor(smstunnel.ReceivedTextsCursorOptions{Since: time.Now()})
for {
	texts, err := cursor.Next(ctx)
	if err != nil {
		log.Print(err) // nothing is lost: the next call asks again
	}
	for _, text := range texts {
		fmt.Println(text.Sender, text.Message)
	}
	time.Sleep(10 * time.Second)
}
```

To go on after a restart, save `cursor.Since()` and `cursor.Seen()` and pass them back as `Since` and `Seen`. SMSTunnel can also post each received text to your own address instead: in the dashboard, **Apps** → the key → **Forward received texts**.

Every result struct has JSON tags with the API's field names, and the send, status and usage results have a `Raw` field with SMSTunnel's whole answer, for fields this version doesn't map yet.

## In a web server

```go
sms, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"), smstunnel.WithDefaultCountryCode("+40"))
if err != nil {
	log.Fatal(err)
}

http.HandleFunc("POST /orders/{id}/shipped", func(w http.ResponseWriter, r *http.Request) {
	order, err := orders.MarkShipped(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_, err = sms.SendSMS(r.Context(), order.Phone, "Your order #"+order.ID+" has shipped.",
		smstunnel.WithIdempotencyKey("order-"+order.ID+"-shipped"))
	if err != nil {
		log.Printf("the shipping text for order %s did not go out: %v", order.ID, err)
	}
	w.WriteHeader(http.StatusNoContent)
})
```

To keep requests fast, you can send from a goroutine or a job queue instead, with a context of its own (`context.WithoutCancel(r.Context())` on Go 1.21+).

## Errors

Every method returns an error when the call doesn't go through:

| Error | When |
| --- | --- |
| `*smstunnel.APIError` | SMSTunnel answered with an error. `StatusCode`, `Code` (such as `smstunnel.CodePermissionRequired`), `Message`, `Permission`, `RetryAfter`, `Header`, `Body`. |
| `errors.Is(err, smstunnel.ErrUnauthorized)` | 401: the API key is wrong, was deleted or turned off, or its account is suspended. |
| `errors.Is(err, smstunnel.ErrForbidden)` | 403: the key lacks a permission (`Code` is `PERMISSION_REQUIRED` and `Permission` names it, e.g. `sms:inbox`), or the plan doesn't include the feature. |
| `errors.Is(err, smstunnel.ErrRateLimited)` | 429: too many requests (`RetryAfter` says how long to wait), or the plan's limit for the day or month is reached (no `RetryAfter`; the message says when texts can go out again). |
| `errors.Is(err, smstunnel.ErrServer)` | 5xx: a problem on SMSTunnel's side. |
| `errors.Is(err, smstunnel.ErrNotFound)` | 404: no such message for this key. |
| `*smstunnel.ConnectionError` | No answer: SMSTunnel could not be reached, or `Timeout()` when it didn't answer in time. |
| `errors.Is(err, smstunnel.ErrInvalidInput)` | An argument was refused before anything was sent: an empty number, two numbers in one, a country code that isn't one. |
| `errors.Is(err, context.Canceled)` | The context was cancelled. |

Other statuses come as an `*APIError` too: 400 (a number SMSTunnel can't read, a code text without a code), 409 and 422 (an idempotency key still in progress, or reused for another text).

```go
_, err := client.SendSMS(ctx, number, text, smstunnel.WithIdempotencyKey(textID))

var apiErr *smstunnel.APIError
var connErr *smstunnel.ConnectionError
switch {
case err == nil:
case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests && apiErr.RetryAfter > 0:
	time.Sleep(apiErr.RetryAfter) // then try again, with the same idempotency key
case errors.As(err, &apiErr) && apiErr.Code == smstunnel.CodePermissionRequired:
	log.Printf("the SMSTunnel key lacks %s", apiErr.Permission)
case errors.As(err, &connErr):
	log.Printf("no answer (timeout: %v): retry only with the same idempotency key", connErr.Timeout())
default:
	log.Print(err)
}
```

A phone that is offline is not an error: the text waits on your account (`Queued` is true) and goes out when the phone is back, within 3 days (otherwise it is marked failed).

### Retrying a send safely

The client never retries on its own. After a timeout, a connection error or a 5xx answer, the text may already be on its way, so retry a send only with `WithIdempotencyKey`: a second call with the same key within 24 hours gets the first answer back (`Replayed` is true) instead of sending a second text. Use a key that names the text (`"order-" + id + "-shipped"`), or a UUID you store with it.

## Settings

```go
client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY"),
	smstunnel.WithDefaultCountryCode("+40"),       // for numbers written without + or 00; a text's WithCountryCode wins
	smstunnel.WithTimeout(15*time.Second),         // per call; the default is 30 seconds, 0 turns it off
	smstunnel.WithHTTPClient(myHTTPClient),        // your transport, proxy or TLS settings
	smstunnel.WithUserAgentSuffix("my-tool/2.1"),  // added to the User-Agent header
	smstunnel.WithBaseURL("http://127.0.0.1:3141"), // a test server; the default is https://smstunnel.io
)
```

The client doesn't follow redirects (unless your `http.Client` has its own `CheckRedirect`), so the API key is never sent to another address. The default transport reads the proxy from `HTTPS_PROXY`. In your own tests, point `WithBaseURL` at an `httptest.Server`.

## Notes and limits

- **Rate limit**: 30 sends a minute per key; the keys of one account together get three times that. Over it, an `*APIError` with status 429 and `RetryAfter`.
- **Plans**: the Free plan has a daily and a monthly allowance; see [smstunnel.io/pricing](https://smstunnel.io/pricing/).
- **Length**: up to 1600 characters (10 joined SMS); sign-in codes up to 320.
- **Numbers**: use the international format (`+14155550123`). For local numbers, set a country code (`+49`, `49` and `0049` all work): the text's own wins over the client's, and either wins over the phone's country.
- **Offline phone**: the text waits (`Queued`) and goes out when the phone is back online.

## Build it with an AI assistant

Paste this into Claude, ChatGPT, Cursor or Copilot, and replace the part in brackets. Never paste your real API key into a prompt.

```text
I want my Go program to send texts with SMSTunnel. SMSTunnel sends each text from my own Android phone, from my own number: the phone runs the SMSTunnel app and is linked to my SMSTunnel account.

Use the module github.com/narcisbodea/smstunnel-go (go get github.com/narcisbodea/smstunnel-go), package smstunnel: client, err := smstunnel.NewClient(os.Getenv("SMSTUNNEL_API_KEY")).
API facts:
- client.SendSMS(ctx, to, message, smstunnel.WithDeviceID(id), smstunnel.WithCountryCode("+40"), smstunnel.WithIdempotencyKey(key)) calls POST https://smstunnel.io/api/v1/sms/send with the header X-API-Key and the JSON body {"to", "message", "deviceId"?, "defaultCountryCode"?}. It returns MessageID and Queued.
- One recipient per call. To text several people, call it once per number.
- client.SendSignInCode (POST /api/v1/sms/send-2fa) is only for sign-in codes: the text must contain a code of 4 to 10 digits standing on its own (e.g. 482913 or 123-456), at most 320 characters; it uses a separate allowance.
- Numbers in international format (+14155550123). A country code (WithCountryCode, or WithDefaultCountryCode on the client) completes numbers written without + or 00.
- Limits: 30 sends a minute per key, 1600 characters a text.
- Errors: *smstunnel.APIError with StatusCode, Code (such as PERMISSION_REQUIRED), Message and RetryAfter; errors.Is with smstunnel.ErrUnauthorized (401, bad key), ErrForbidden (403, missing permission or plan), ErrRateLimited (429, wait RetryAfter), ErrServer (5xx); *smstunnel.ConnectionError when there is no answer; ErrInvalidInput for refused arguments.
- The client never retries. Retry a send only with the same WithIdempotencyKey: a repeat within 24 h returns the first answer instead of sending twice.
- If the phone is offline, the text waits on the account (Queued is true) and goes out when the phone is back.

What I want: [describe what should send a text, to whom, and when]

Rules:
- Read the API key from an environment variable or the platform's secret store. Never put it in source code, in the repository or in client-side code.
- Validate phone numbers before sending, and handle every error above: log it clearly, don't retry 401/403, wait RetryAfter on 429.
- Only send texts people expect (alerts to myself, reminders, sign-in codes, order updates).
```

## Responsible use

Send only texts that people expect from you: alerts to yourself, appointment reminders, sign-in codes, order updates. Unsolicited messages from a personal SIM break most carriers' terms of service. Please make sure your usage complies with your carrier's terms and with local regulations (in the EU, that includes GDPR and ePrivacy rules on electronic communications).

## Development

```bash
go vet ./...
go test -race ./...
```

The end-to-end tests (`e2e_test.go`) send real texts, so they run only against a test server, never smstunnel.io; without one they are skipped. Point them at it with a JSON file `{"serverUrl": "http://127.0.0.1:3141", "apiKey": "...", "deviceId": "..."}`, or with `SMSTUNNEL_E2E_URL` and `SMSTUNNEL_E2E_API_KEY`:

```bash
SMSTUNNEL_E2E_KEY_FILE=/path/to/test-key.json go test -run EndToEnd -v ./...
```

## Links

- Documentation: <https://smstunnel.io/docs/> · API reference: <https://smstunnel.io/api-reference/>
- Dashboard (phones, keys): <https://dashboard.smstunnel.io/apps>
- Package documentation: <https://pkg.go.dev/github.com/narcisbodea/smstunnel-go>
- Changelog: [CHANGELOG.md](CHANGELOG.md)

SMSTunnel is not affiliated with, endorsed by, or sponsored by Google or the Go project. Go is a trademark of Google LLC, and so are Android and Google Play.

## License

MIT, © NARBOWEB SRL. See [LICENSE](LICENSE).
