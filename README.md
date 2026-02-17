# xget

A high-performance parallel file downloader with caching capabilities for HTTP/HTTPS and S3-compatible storage.

## Features

- **Parallel Downloads** - Configurable concurrent downloads with worker pool pattern
- **Download Resumption** - Automatically resume interrupted downloads from partial files
- **SHA256 Verification** - Built-in checksum validation for integrity assurance
- **S3 Caching Layer** - Content-addressable cache to deduplicate downloads
- **Retry Mechanism** - Exponential backoff with configurable retry attempts
- **Multi-Source Support** - Download from HTTP/HTTPS and S3/MinIO endpoints
- **Progress Tracking** - Real-time progress bars for visual feedback
- **Graceful Shutdown** - Signal handling (SIGINT/SIGTERM) for clean interruption
- **Environment Variables** - Support for credential management via environment variables

## Installation

### Download Pre-built Binaries

Download the latest release for your platform from the [releases page](https://github.com/slapshin/xget/releases):

- **Linux**: `xget-linux-amd64.tar.gz`, `xget-linux-arm64.tar.gz`, `xget-linux-arm.tar.gz`
- **macOS**: `xget-darwin-amd64.tar.gz`, `xget-darwin-arm64.tar.gz`
- **Windows**: `xget-windows-amd64.exe.zip`, `xget-windows-arm64.exe.zip`

Extract and run:

```bash
# Linux/macOS
tar xzf xget-linux-amd64.tar.gz
chmod +x xget-linux-amd64
./xget-linux-amd64 config.yaml

# Windows (PowerShell)
Expand-Archive xget-windows-amd64.exe.zip
.\xget-windows-amd64.exe config.yaml
```

### From Source

```bash
# Clone the repository
git clone <repository-url>
cd xget

# Build the binary
make build

# Binary will be available at ./bin/xget
./bin/xget config.yaml
```

### Using Docker

```bash
# Build the image
docker build -t xget .

# Run with config file
docker run -v $(pwd)/config.yaml:/config.yaml xget /config.yaml
```

### Requirements

- Go 1.26.0 or higher

## Usage

```bash
xget <config.yaml>
```

The tool takes a single argument - the path to a YAML configuration file that defines:

- Storage endpoints (aliases)
- Cache configuration
- Download settings
- List of files to download

## Configuration

Create a configuration file (see `config.yaml.template` for a complete example):

```yaml
# Storage aliases - define S3/MinIO endpoints
aliases:
  # AWS S3 example
  mycloud:
    endpoint: https://s3.amazonaws.com
    region: us-east-1
    bucket: my-bucket
    access_key: ""      # optional, falls back to AWS_ACCESS_KEY_ID env var
    secret_key: ""      # optional, falls back to AWS_SECRET_ACCESS_KEY env var

  # MinIO example with environment variable substitution
  minio:
    endpoint: https://minio.company.com
    bucket: artifacts
    access_key: ${MINIO_ACCESS_KEY}
    secret_key: ${MINIO_SECRET_KEY}

  # Cache storage
  cache:
    endpoint: https://s3.amazonaws.com
    region: us-east-1
    bucket: download-cache
    prefix: files/      # optional key prefix

# Cache configuration
cache:
  alias: cache          # reference to alias defined above
  enabled: true

# Download settings
settings:
  parallel: 4           # max concurrent downloads (default: 4)
  retries: 3            # retry attempts on failure (default: 3)
  retry_delay: 5s       # delay between retries (default: 5s)

# Files to download
files:
  # Download from S3 using alias
  - url: s3://mycloud/path/to/file1.tar.gz
    dest: ./downloads/file1.tar.gz
    sha256: abc123def456...

  # Download from MinIO
  - url: s3://minio/tools/file2.zip
    dest: /opt/tools/file2.zip
    sha256: def456ghi789...

  # Download from HTTP
  - url: https://example.com/file3.bin
    dest: ./downloads/file3.bin
    sha256: ghi789jkl012...
```

### Environment Variables

The configuration supports environment variable expansion using `${VAR_NAME}` syntax in alias credentials and file destination paths:

```yaml
aliases:
  minio:
    access_key: ${MINIO_ACCESS_KEY}
    secret_key: ${MINIO_SECRET_KEY}

files:
  - url: https://example.com/file.tar.gz
    dest: ${DOWNLOAD_DIR}/file.tar.gz
    sha256: abc123...
```

You can also omit `access_key` and `secret_key` fields to use standard AWS environment variables:

- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`

### URL Formats

**HTTP/HTTPS URLs:**

```yaml
url: https://example.com/path/to/file.tar.gz
```

**S3 URLs:**

```yaml
url: s3://alias/path/to/file.tar.gz
```

Where `alias` references a storage endpoint defined in the `aliases` section.

## How It Works

### Download Pipeline

1. **Check Existing File** - Verify if destination file exists with correct SHA256 hash (skip if valid)
2. **Try Cache** - Attempt to retrieve from cache by content hash (if cache enabled)
3. **Download from Source** - Download with retry logic and exponential backoff
4. **Verify Checksum** - Validate SHA256 hash against expected value
5. **Update Cache** - Upload to cache on successful download (if cache enabled)

### Partial Downloads

Downloads are saved with a `.partial` suffix during transfer:

- Existing partial files are automatically resumed using HTTP Range requests
- Only renamed to final destination after successful checksum verification
- Failed downloads leave partial file intact for next retry attempt

### Caching Strategy

The S3-based cache uses SHA256 hash as the key for content-addressable storage:

- Prevents redundant downloads across different configurations
- Deduplicates files with identical content
- Transparently handles cache misses by falling back to source

## Examples

### Basic HTTP Download

```yaml
settings:
  parallel: 2

files:
  - url: https://releases.example.com/tool-v1.2.3.tar.gz
    dest: ./downloads/tool.tar.gz
    sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

### S3 Downloads with Cache

```yaml
aliases:
  artifacts:
    endpoint: https://s3.amazonaws.com
    region: us-west-2
    bucket: build-artifacts

  cache:
    endpoint: https://s3.amazonaws.com
    region: us-west-2
    bucket: download-cache

cache:
  alias: cache
  enabled: true

settings:
  parallel: 4
  retries: 5
  retry_delay: 10s

files:
  - url: s3://artifacts/releases/app-v2.0.0.tar.gz
    dest: ./releases/app.tar.gz
    sha256: d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2
```

### MinIO with Environment Variables

```bash
# Set environment variables
export MINIO_ACCESS_KEY=myaccesskey
export MINIO_SECRET_KEY=mysecretkey

# Config file
cat > config.yaml <<EOF
aliases:
  minio:
    endpoint: https://minio.company.com
    bucket: artifacts
    access_key: ${MINIO_ACCESS_KEY}
    secret_key: ${MINIO_SECRET_KEY}

files:
  - url: s3://minio/binaries/tool.bin
    dest: ./tool.bin
    sha256: a1b2c3d4e5f6...
EOF

# Run xget
./bin/xget config.yaml
```

## Development

### Building

```bash
# Build binary
make build

# Run linter
make lint

# Update dependencies
make go-update
make go-tidy
```

### Project Structure

```
xget/
├── src/
│   ├── main.go              # Application entry point
│   ├── downloader.go        # Core download orchestration
│   ├── cache.go             # S3-based caching layer
│   ├── checksum.go          # SHA256 verification
│   ├── progress.go          # Progress bar wrapper
│   ├── config/              # Configuration management
│   │   ├── config.go        # YAML loading and validation
│   │   ├── types.go         # Config structures
│   │   └── env.go           # Environment variable expansion
│   └── storage/             # Download source abstractions
│       ├── storage.go       # Source interface
│       ├── http.go          # HTTP/HTTPS implementation
│       └── s3.go            # S3/MinIO implementation
├── Makefile                 # Build commands
├── Dockerfile               # Docker build
├── config.yaml.template     # Configuration example
└── .golangci.yml           # Linter configuration
```

### Testing

```bash
# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Code Style

The project follows strict Go coding standards. See `.claude/rules/go-codestyle.md` for detailed guidelines:

- Use `errors.Is()` and `errors.As()` for error comparison
- Wrap errors with context using `fmt.Errorf("...: %w", err)`
- Accept `context.Context` as first parameter where applicable
- Use lowercase in log messages
- Prefer singular package names
- Always use `any` instead of `interface{}`

### Commit Messages

Follow conventional commit format. See `.claude/rules/commit-messages.md` for guidelines:

```
<type>: <subject>

Types: feat, fix, refactor, perf, test, docs, build, ci, chore
```

Examples:

```
feat: add retry mechanism for failed downloads
fix: handle nil pointer in download manager
refactor: simplify error handling in S3 source
```

## Architecture

### Storage Abstraction

The `Source` interface abstracts download sources:

```go
type Source interface {
    Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error)
    GetSize(ctx context.Context) (int64, error)
}
```

**Implementations:**

- **HTTPSource** - Downloads via HTTP/HTTPS with Range request support
- **S3Source** - Downloads from S3/MinIO using AWS SDK v2

### Download Manager

The downloader uses a worker pool pattern with semaphore channel to limit concurrency:

- Processes files in parallel up to configured limit
- Handles partial file resume with `.partial` suffix
- Implements retry logic with exponential backoff
- Propagates context cancellation for graceful shutdown

### Cache Layer

S3-based content-addressable cache:

- `Get(hash)` - Retrieves file from cache by SHA256 hash
- `Put(hash, file)` - Uploads successfully downloaded file to cache
- Transparent fallback to source on cache miss

## Dependencies

- **AWS SDK for Go v2** - S3/MinIO operations
- **progressbar** - Terminal progress visualization
- **yaml.v3** - Configuration parsing

Full dependency list in `go.mod`.

## Exit Codes

- `0` - All downloads completed successfully
- `1` - One or more downloads failed or configuration error

## Contributing

Contributions are welcome. Please:

1. Follow the code style guidelines in `.claude/rules/go-codestyle.md`
2. Use conventional commit messages per `.claude/rules/commit-messages.md`
3. Ensure all tests pass and linter is clean
4. Add tests for new functionality

## License

[Add license information here]
