package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"xget/src/config"
)

// Source represents a download source that can provide file content.
type Source interface {
	// Download retrieves the file content starting from the given offset.
	// Returns the reader, total file size, and any error.
	Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error)

	// GetSize returns the total size of the file.
	GetSize(ctx context.Context) (int64, error)
}

// RangeSource is implemented by sources that support byte-range requests.
type RangeSource interface {
	Source

	// DownloadRange downloads bytes [start, end] inclusive.
	DownloadRange(ctx context.Context, start, end int64) (io.ReadCloser, error)

	// AcceptsRanges reports whether the server accepts Range requests.
	AcceptsRanges(ctx context.Context) (bool, error)
}

// NewSource creates a Source based on the URL scheme.
func NewSource(url string, aliases map[string]config.Alias, timeout time.Duration) (Source, error) {
	switch {
	case strings.HasPrefix(url, "s3://"):
		return newS3Source(url, aliases)
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		return NewHTTPSource(url, timeout), nil
	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", url)
	}
}
