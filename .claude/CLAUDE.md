# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

xget is a parallel file downloader with caching capabilities. It downloads files from HTTP/HTTPS and S3-compatible storage (including MinIO), with support for:

- Parallel downloads with configurable concurrency
- Download resumption from partial files
- SHA256 checksum verification
- S3-based caching layer
- Retry mechanism with exponential backoff

## Building and Development

```bash
# Build binary (outputs to bin/xget)
make build

# Run tests (with race detector)
make test

# Run linter (golangci-lint, 5m timeout)
make lint

# Update dependencies
go get -u ./...
go mod tidy -v

# Run directly
./bin/xget config.yaml

# Build Docker image
docker build -t xget .
```

**Before committing:** always run `make lint` — 33 linters are active (see `.golangci.yml`). Common violations: `wsl_v5` blank-line rules, `godot` missing period on block comments, `lll` line length.

**After changing imports:** run `go mod tidy` to keep direct/indirect dependency sections correct.

## Configuration

The application uses YAML config files (see `config.yaml.template` for full example):

- **aliases**: Storage endpoint configurations (S3/MinIO)
- **cache**: Optional S3-based caching layer
- **settings**: Download behavior (parallel, retries, retry_delay, debug, debug_interval)
- **files**: List of files to download with URLs, destinations, and SHA256 checksums

Every file entry requires `url`, `dest`, and `sha256` — config validation (`src/config/config.go`) rejects entries missing any of them, so test configs need a dummy `sha256`.

Config supports environment variable expansion using `${VAR_NAME}` syntax in:

- Alias credentials and configuration fields
- Cache enabled flag
- File destination paths

Unset `${VAR}` references are left as the literal `${VAR}` text (not emptied) — see `expandEnvVars` in `src/config/env.go`. A masked credential showing a `}` tail usually means its env var wasn't exported.

## Architecture

### Storage Abstraction (`src/storage/`)

The `Source` interface abstracts download sources:

- **HTTPSource**: Downloads from HTTP/HTTPS URLs with Range request support.
  Deliberately HTTP/1.1-only (like `curl --http1.1`): Cloudflare R2 resets
  multiplexed HTTP/2 streams under concurrent range load. Do not re-enable
  HTTP/2 or remove the `TLSClientConfig.NextProtos = ["http/1.1"]` scrub —
  `Transport.Clone()` of `http.DefaultTransport` leaves "h2" in the cloned
  ALPN list, so disabling `TLSNextProto` alone is NOT enough (servers still
  negotiate h2 → "malformed HTTP response"). Regression test:
  `TestHTTPSourceUsesHTTP1AgainstHTTP2Server`.
- **S3Source**: Downloads from S3/MinIO using AWS SDK v2
  - Parses URLs as `s3://alias/path` where alias references a configured storage endpoint
  - Supports path-style URLs (required for MinIO)
  - Handles optional key prefixes from alias configuration
  - Uses the AWS SDK default HTTP client, which offers h2 in ALPN. Works with
    R2 today only because `r2.cloudflarestorage.com` offers http/1.1-only; if
    HTTP/2 errors ever appear on `s3://` aliases, force HTTP/1.1 in
    `createS3Client` the same way.

### Download Manager (`src/downloader.go`)

Core download orchestration:

1. Check if destination file exists with correct hash (skip if valid)
2. Attempt cache retrieval (if cache enabled)
3. Download from source with retry logic
4. Verify SHA256 checksum
5. Upload to cache on successful download

Worker pool pattern using semaphore channel limits concurrent downloads.

### Segmented Download (`src/segment/`)

Splits a single large file into N byte-range segments downloaded in parallel
(controlled by `settings.segments_per_file` and `settings.segment_min_size`;
disabled entirely by `settings.single_stream: true`):

- **download.go**: `NewDownloader(...)` / `Download()` — orchestrates per-segment range requests; invoked from `src/downloader.go`.
- **state.go**: persistent resume state in a `.segments` file alongside `.partial` (`StatePath()`, `LoadState()`, `SaveState()`); tracks per-segment byte progress (`Written`, saved on a 1s throttle during transfer plus a final flush) so interrupted downloads resume mid-segment, not just at completed-segment boundaries.

### Output Layer (`src/output/`)

All user-facing output goes through the `output.Reporter` interface — nothing in the
download path writes to stdout/stderr directly:

- **reporter.go**: `Reporter` (`NewTracker`, `Logf`, `Debugf`, `Errorf`, `Wait`) and `Tracker` (`io.Writer` + `SetCurrent`/`Finish`/`Abort`) interfaces.
- **bar.go**: `NewBarReporter(ctx, out)` — mpb progress bars; `Debugf` is a no-op. `Logf` routes through the mpb container only while bars are active, because a container with no bars silently discards writes.
- **debug.go** / **stats.go**: `NewDebugReporter(out, errOut, interval)` — no bars, timestamped lines only, plus a stats line per running transfer (and a totals line for several) every `interval`.

Debug mode is resolved in `applyDebugSettings` (`src/main.go`) from three sources, in
increasing order of precedence: `settings.debug` in the config, the `XGET_DEBUG` env var
(which also disables debug when set to a falsy value) and the `-debug`/`--debug` flag.
`settings.debug_interval` (default 5s, floored at 100ms) sets the stats cadence. `Tracker.Abort()` must be
deferred right after creating a tracker, or the bar container's `Wait` blocks forever on
error paths.

### Redaction (`src/redact/`)

`redact.Secret()` masks a credential (keeps the last 4 chars) and `redact.URL()` masks
any `user:password` embedded in a URL. Every place that prints a file URL — config dump,
debug lines, retry errors, the final failure summary and `storage.NewSource`'s
unsupported-scheme error — must go through `redact.URL()`, so output stays safe to paste
into a log.

### Cache Layer (`src/cache.go`)

S3-based caching using SHA256 hash as the key:

- `Get()`: Retrieves file from cache by hash
- `Put()`: Uploads successfully downloaded file to cache
- Deduplicates downloads across configurations by content hash

### Config System (`src/config/`)

- **types.go**: Config structure definitions
- **config.go**: YAML parsing, validation, defaults
- **env.go**: Environment variable expansion in alias credentials and file destination paths

### Partial Download Support

Files download to `.partial` suffix during transfer:

- Existing partial files are resumed using Range requests
- Only renamed to final destination after successful checksum verification
- Failed downloads leave partial file for next retry

## Key Dependencies

- **Progress bars**: `github.com/vbauerster/mpb/v8` — used in `src/output/bar.go` only. Do NOT add `schollz/progressbar` (removed).
- **S3 client**: `github.com/aws/aws-sdk-go-v2` family.
- **YAML parsing**: `gopkg.in/yaml.v3`.

## Test Coverage

Tested:

- `src/config/` — comprehensive (config parsing, merging, env expansion)
- `src/segment/` — state and download tests (`state_test.go`, `download_test.go`)
- `src/output/` — bar/debug reporter behaviour, stats formatting
- `src/redact/` — credential and URL masking
- `src/main.go` — debug mode resolution and argument parsing (`main_test.go`)
- `src/generate.go` — full table-driven tests

**No tests exist for:**

- `src/downloader.go`
- `src/cache.go`
- `src/storage/` (http.go, s3.go)
- `src/checksum.go`

When touching those files, consider adding tests.

## Output / Logging

Download-path output goes through `output.Reporter` (see Output Layer above): `Logf` for
messages the user always needs, `Debugf` for source/segment detail shown only in debug mode,
`Errorf` for failures. Startup banner, config dump and the final summary in `src/main.go` /
`src/configprint.go` still use plain `fmt.Printf` / `fmt.Fprintf(os.Stderr, ...)`; there is no
structured logger. The `go-codestyle.md` rule about `logrus.FieldLogger` describes the desired
direction but is not yet implemented. Do not add logrus to new code without a broader refactor plan.

## Code Style

See `.claude/rules/go-codestyle.md` for detailed guidelines. Key points:

- Wrap errors with context using `fmt.Errorf("...: %w", err)`
- Use `errors.Is()` and `errors.As()` for error comparison
- Accept `context.Context` as first parameter where applicable
- Use lowercase in log messages
- Prefer singular package names
- Always use `any` instead of `interface{}`

## Commit Guidelines

See `.claude/rules/commit-messages.md` for format. Summary:

- Format: `<type>: <subject>` (types: feat, fix, refactor, perf, test, docs, build, ci, chore)
- Imperative mood, lowercase, no period, max 50 chars
- Example: `feat: add retry mechanism for failed downloads`

## Subagents

Use these subagents automatically when the situation matches — no need to ask.

| Agent | When to use |
|---|---|
| `go-reviewer` | After writing or modifying any Go file — review against codestyle rules |
| `security-auditor` | When auth, key handling, command execution, or config parsing is touched |
| `test-runner` | When asked to run tests or when a test failure needs to be diagnosed and fixed |
| `lint-runner` | After writing or modifying any Go file — run `make lint` and fix any reported issues |
