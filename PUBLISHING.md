# Publishing the Go module

A Go module has no registry to upload to: a version is a git tag on a public repository. The Go module proxy
(`proxy.golang.org`) fetches the tagged code the first time someone asks for it and keeps that copy forever, and
pkg.go.dev shows the documentation from there. So publishing needs only the GitHub account `narcisbodea` (no other
account, no token, no secret).

The module path is `github.com/narcisbodea/smstunnel-go`, so the repository must be exactly
`https://github.com/narcisbodea/smstunnel-go`. It was free on 3 November 2026 (the proxy answered 404 for
`https://proxy.golang.org/github.com/narcisbodea/smstunnel-go/@v/list`).

## 1. GitHub: the repository (once)

1. Create the **public** repository `narcisbodea/smstunnel-go` (the proxy can't fetch a private one). Description:
   "Send texts from your own Go programs through your own Android phone, from your own number". Topics, for example:
   `sms`, `go`, `golang`, `android`, `two-factor-authentication`, `smstunnel`.
2. Push this folder to its `main` branch:

   ```bash
   git remote add origin https://github.com/narcisbodea/smstunnel-go.git
   git push -u origin main
   ```

3. Wait for the **CI** workflow (Actions tab) to pass: tests on Go 1.22, the previous and the current Go release, on
   Linux, Windows and macOS, plus gofmt, staticcheck and govulncheck.

## 2. Release v1.0.0

1. Check that `const Version` in `client.go` and the top entry of `CHANGELOG.md` say `1.0.0`.
2. Tag the commit and push the tag:

   ```bash
   git tag -a v1.0.0 -m "smstunnel-go 1.0.0"
   git push origin v1.0.0
   ```

3. The **Release** workflow runs on the tag: it tests the tagged commit, checks that the tag matches `Version`, and
   asks the proxy for the version, which is what makes pkg.go.dev list it:

   ```bash
   curl https://proxy.golang.org/github.com/narcisbodea/smstunnel-go/@v/v1.0.0.info
   ```

   You can run that request yourself too; it answers with `{"Version":"v1.0.0","Time":"..."}`. The same happens with
   `GOPROXY=https://proxy.golang.org GO111MODULE=on go get github.com/narcisbodea/smstunnel-go@v1.0.0`.
4. pkg.go.dev adds new versions every few minutes:
   <https://pkg.go.dev/github.com/narcisbodea/smstunnel-go>. If the page says the package isn't there yet, click
   **Request** on it. The documentation is shown because the module has an MIT `LICENSE` file.
5. Optional: on GitHub, **Releases → Draft a new release**, choose the existing tag `v1.0.0`, paste the changelog
   entry and publish it, so the release notes show on GitHub.
6. Check it from an empty folder:

   ```bash
   mkdir try-smstunnel && cd try-smstunnel && go mod init example.com/try
   go get github.com/narcisbodea/smstunnel-go@v1.0.0
   ```

## Later versions

1. Change `const Version` in `client.go` and add a `CHANGELOG.md` entry; commit and push.
2. Tag `v<version>` and push the tag, as above.

A published version never changes: the proxy keeps its first copy even if the tag is moved or deleted, so never move
a tag, and fix a mistake with a new version. To warn people away from a broken version, publish a new one whose
`go.mod` has a `retract` line for it (for example `retract v1.0.1 // sends the wrong country code`).

Breaking changes need a new major version, and from v2 the module path changes: `go.mod` says
`module github.com/narcisbodea/smstunnel-go/v2` and people import that path.

## What stays manual

Creating the public repository, pushing `main`, and pushing each version tag. Nothing in this repository needs a
secret.
