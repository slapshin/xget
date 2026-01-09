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

# Run linter
make lint

# Update dependencies
go get -u ./...
go mod tidy -v

# Run directly
./bin/xget config.yaml

# Build Docker image
docker build -t xget .
```

## Configuration

The application uses YAML config files (see `config.yaml.template` for full example):
- **aliases**: Storage endpoint configurations (S3/MinIO)
- **cache**: Optional S3-based caching layer
- **settings**: Download behavior (parallel, retries, retry_delay)
- **files**: List of files to download with URLs, destinations, and SHA256 checksums

Config supports environment variable expansion using `${VAR_NAME}` syntax.

## Architecture

### Storage Abstraction (`src/storage/`)
The `Source` interface abstracts download sources:
- **HTTPSource**: Downloads from HTTP/HTTPS URLs with Range request support
- **S3Source**: Downloads from S3/MinIO using AWS SDK v2
  - Parses URLs as `s3://alias/path` where alias references a configured storage endpoint
  - Supports path-style URLs (required for MinIO)
  - Handles optional key prefixes from alias configuration

### Download Manager (`src/downloader.go`)
Core download orchestration:
1. Check if destination file exists with correct hash (skip if valid)
2. Attempt cache retrieval (if cache enabled)
3. Download from source with retry logic
4. Verify SHA256 checksum
5. Upload to cache on successful download

Worker pool pattern using semaphore channel limits concurrent downloads.

### Cache Layer (`src/cache.go`)
S3-based caching using SHA256 hash as the key:
- `Get()`: Retrieves file from cache by hash
- `Put()`: Uploads successfully downloaded file to cache
- Deduplicates downloads across configurations by content hash

### Config System (`src/config/`)
- **types.go**: Config structure definitions
- **config.go**: YAML parsing, validation, defaults
- **env.go**: Environment variable expansion in alias credentials

### Partial Download Support
Files download to `.partial` suffix during transfer:
- Existing partial files are resumed using Range requests
- Only renamed to final destination after successful checksum verification
- Failed downloads leave partial file for next retry

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
