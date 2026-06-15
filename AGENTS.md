# xget — Agent Instructions

Go CLI tool for parallel file downloads with S3 caching. Module path: `xget`, source in `src/`.

## Build & Verify

```bash
make build          # → bin/xget
make test           # go test ./...
make lint           # golangci-lint --timeout=5m (33 linters active)
```

**Required commit order:** `make test` → `make lint` → commit. CI runs both. Lint will fail on `wsl_v5` blank-line rules, `godot` missing periods on block comments, and `lll` line length.

**After changing imports:** always run `go mod tidy -v`.

**Single test:** `go test -race -run TestName ./path/to/package`

**Build path:** `go build` target is `xget/src` (not `./src`). The module root is `xget`, so all internal imports are `xget/src/...`.

## Architecture Gotchas

**HTTP/1.1-only enforcement** (`src/storage/http.go`): HTTPSource deliberately strips `h2` from `NextProtos` and clones the transport to prevent HTTP/2 negotiation. Cloudflare R2 resets multiplexed HTTP/2 streams under concurrent range load. `Transport.Clone()` of `http.DefaultTransport` carries "h2" in the cloned ALPN list — disabling `TLSNextProto` alone is NOT enough. Do not re-enable HTTP/2. Regression test: `TestHTTPSourceUsesHTTP1AgainstHTTP2Server`.

**S3Source uses the AWS SDK default HTTP client** (`src/storage/s3.go`) which offers h2 in ALPN. Works with R2 only because `r2.cloudflarestorage.com` forces http/1.1. If HTTP/2 errors appear on `s3://` aliases, force HTTP/1.1 in `createS3Client`. S3Source always uses path-style URLs (required for MinIO). If `no_sign_request` is set, anonymous credentials are used. Aliases support a `prefix` field that gets prepended to all keys.

**Config validation is strict** (`src/config/config.go`): every file entry requires `url`, `dest`, AND `sha256`. Test configs need a dummy `sha256` value or validation will reject them.

**Unset env vars are literal text:** `${VAR}` stays as the string `${VAR}` when unset (not emptied). A masked credential ending in `}` usually means its env var wasn't exported.

**Config merging** (`src/config/config.go`): `LoadMultiple` merges configs left-to-right. Aliases override per-name (later wins). Settings override per-field (zero values do NOT override — only non-zero values take effect). Files accumulate across all configs. `single_stream` is a string field, so it overrides when non-empty (not just non-zero). Validation runs only after all merging is complete.

**Segmented download state** (`src/segment/state.go`): interrupted downloads persist per-segment byte progress to a `.segments` JSON file (e.g., `file.partial.segments`). State is saved to disk every 1 second during transfer (throttled) plus a final flush. `sanitizeSegments()` repairs corrupt state (negative or out-of-range `Written` counters reset to 0; `Done` segments get `Written` set to full size). The state file is cleaned up by `downloader.go` after successful checksum verification.

**ProgressWriter.Abort()** (`src/progress.go`): must be deferred immediately after creating a ProgressWriter. Without it, `progress.Wait()` blocks forever on error paths because mpb never sees the bar complete. This applies to both `ProgressWriter` and `SharedProgressWriter`.

**Download flow** (`src/downloader.go`): segmented download is tried first (unless `single_stream` or `segments_per_file <= 1` or file < `segment_min_size` or source doesn't implement `RangeSource`). Falls back to single-stream. Partial files use `.partial` suffix; renamed to final dest after SHA256 verification passes. Checksum mismatch deletes the partial file and segment state.

**HTTP range fallback** (`src/storage/http.go:159`): if a CDN returns 200 instead of 206 for a range request (e.g., Cloudflare cache miss), `DownloadRange` skips the prefix bytes and caps the body with `io.LimitReader` so the caller still receives exactly the requested range.

## Test Coverage Gaps

These files have **no tests**: `src/downloader.go`, `src/cache.go`, `src/storage/s3.go`, `src/progress.go`, `src/checksum.go`. When touching these, consider adding tests.

Well-tested: `src/config/` (1146 lines, comprehensive merge/expansion/validation coverage), `src/segment/` (both state and download with resume/short-read/abort recovery), `src/generate.go` (table-driven), `src/storage/http_test.go` (HTTP/1.1 enforcement and range fallback).

**Test patterns:** config tests use `ParseMultiple()` with inline YAML strings and `t.Helper()` assertion functions. Segment tests use `httptest.NewServer` to simulate range servers (including short-read, abort, and full-body-without-range scenarios). Use `t.TempDir()` for test isolation.

## Key Constraints

- **Progress bars:** use `github.com/vbauerster/mpb/v8` only. Do NOT add `schollz/progressbar` (was removed).
- **Logging:** codebase uses `fmt.Printf`/`fmt.Fprintf` — no structured logger. Do NOT add `logrus` without a broader refactor plan, even though `.claude/rules/go-codestyle.md` mentions it.
- **Error style:** wrap with `fmt.Errorf("...: %w", err)`, compare with `errors.Is()`/`errors.As()`, don't use `failed` in wrap messages.
- **Code style:** `any` not `interface{}`, lowercase log messages, singular package names, split multi-expression `if` to separate lines. See `.claude/rules/go-codestyle.md`.
- **nolint annotations:** `//nolint:nilerr` is used intentionally where size/range probe errors are silently swallowed to fall back to single-stream download. Do not remove these without understanding the fallback behavior.

## Commit Format

`<type>: <subject>` — types: feat, fix, refactor, perf, test, docs, build, ci, chore. Imperative mood, lowercase, no period, max 50 chars. See `.claude/rules/commit-messages.md`.

## Lint Config

`.golangci.yml` — 33 linters enabled. Common surprises: `wsl_v5` (blank-line rules around blocks), `godot` (block comments need periods), `lll` (line length), `nonamedreturns` (no named returns), `errorlint` (must use `errors.Is`/`errors.As`).
