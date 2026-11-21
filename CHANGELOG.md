# Changelog

All notable changes to `github.com/narcisbodea/smstunnel-go` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the module uses [Semantic Versioning](https://semver.org/).

## [1.1.1] - 2026-11-21

### Fixed

- `State.Final()` is now `true` for `StateSent`. The SMSTunnel app gets no delivery reports, so a text that went out
  stays sent, and code waiting "until final" never stopped. `StateSent` is the last good state today; `StateDelivered`
  is reserved and still counts as final. `StatePending` and states this version doesn't know are still not final.
- The docs no longer promise a delivery confirmation.

### Compatibility

- Only `Final()` changes. If your code waits for `Final()` and then looks for `StateDelivered`, treat `StateSent` as
  success too.

## [1.1.0] - 2026-11-15

### Added

- Scheduled texts. `WithSendAt(t)` makes `SendSMS` schedule the text instead of sending it now, 1 minute to 30 days
  ahead (`sendAt` in RFC 3339, whole seconds, with the time's own offset): `SendResult.Scheduled` holds the
  `ScheduledText` (`ID`, `SendAt`, `Status`, ...) and `MessageID` stays empty until the text goes out. At that time
  SMSTunnel sends it as if `SendSMS` were called then with the same key. `SendSignInCode` refuses `WithSendAt`.
- `ListScheduledTexts` (`ScheduledTextsQuery`: `ScheduledScopeWaiting` or `ScheduledScopeDone`, `Limit`, `Page`;
  `ScheduledTextsPage` with `Waiting` and the plan's `Max`), `GetScheduledText` and `CancelScheduledText`, for the
  texts the key scheduled.
- `ScheduledStatus` (`ScheduledWaiting`, `ScheduledSent`, `ScheduledFailed`, `ScheduledCanceled`, `Done()`) and the
  codes `CodeScheduleTooSoon`, `CodeScheduleTooFar`, `CodeScheduleInvalidInput`, `CodeScheduleTextTooLong`,
  `CodeScheduleNumberNotAllowed`, `CodeScheduleSealedOnlyPhone`, `CodeScheduleEncryptedText`,
  `CodeScheduleSignInCode`, `CodeSchedulePhoneNotFound`, `CodeScheduleLimitReached`, `CodeScheduleNotFound` and
  `CodeScheduleNotEditable`.

## [1.0.0] - 2026-11-03

### Added

- `smstunnel.Client` (`NewClient`) with `SendSMS`, `SendSignInCode`, `GetMessageStatus`, `ListPhones`, `GetUsage` and
  `ListReceivedTexts`, each taking a `context.Context`.
- Client options: `WithBaseURL`, `WithHTTPClient`, `WithDefaultCountryCode`, `WithTimeout` and `WithUserAgentSuffix`.
  Per-text options: `WithDeviceID`, `WithCountryCode` and `WithIdempotencyKey` (`SendResult.Replayed` tells a stored
  answer).
- `ReceivedTextsCursor` (`NewReceivedTextsCursor`): follows received texts as they arrive, each one once, oldest first,
  and can go on after a restart (`Since`, `Seen`).
- Errors: `*APIError` with the HTTP status, SMSTunnel's `Code`, `Message`, `Permission`, `RetryAfter`, headers and
  body, matched by `errors.Is` against `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrRateLimited` and
  `ErrServer`; `*ConnectionError` (with `Timeout()`) when no answer comes back; `ErrInvalidInput` for arguments
  refused before anything is sent.
- `Quota` for limits that can be "unlimited". JSON tags with the API's field names on every result.
- Country codes and idempotency keys are checked with the server's rules; the client never follows redirects and
  never retries by itself. No dependencies outside the standard library.
