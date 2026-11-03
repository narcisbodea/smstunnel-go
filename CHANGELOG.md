# Changelog

All notable changes to `github.com/narcisbodea/smstunnel-go` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the module uses [Semantic Versioning](https://semver.org/).

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
