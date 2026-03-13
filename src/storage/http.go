package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTPSource implements Source for HTTP/HTTPS URLs.
type HTTPSource struct {
	url               string
	client            *http.Client
	rangeOnce         sync.Once
	acceptsRangesVal  bool
	acceptsRangesErr  error
}

// NewHTTPSource creates an HTTPSource for the given URL and timeout.
func NewHTTPSource(url string, timeout time.Duration) *HTTPSource {
	return &HTTPSource{
		url:    url,
		client: &http.Client{Timeout: timeout},
	}
}

// Download retrieves the file content starting from the given offset.
func (httpSource *HTTPSource) Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpSource.url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}

	// Set Range header for resume support.
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := httpSource.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}

	// Check for successful response.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()

		return nil, 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	totalSize := parseTotalSize(resp, offset)

	return resp.Body, totalSize, nil
}

func parseTotalSize(resp *http.Response, offset int64) int64 {
	if resp.StatusCode != http.StatusPartialContent {
		return resp.ContentLength
	}

	contentRange := resp.Header.Get("Content-Range")
	if contentRange == "" {
		return offset + resp.ContentLength
	}

	// Format: bytes start-end/total.
	var start, end, total int64

	_, err := fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
	if err != nil {
		return offset + resp.ContentLength
	}

	return total
}

// GetSize returns the total size of the file using HEAD request.
func (httpSource *HTTPSource) GetSize(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, httpSource.url, nil)
	if err != nil {
		return 0, fmt.Errorf("creating HEAD request: %w", err)
	}

	resp, err := httpSource.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("executing HEAD request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	contentLength := resp.Header.Get("Content-Length")
	if contentLength == "" {
		return 0, fmt.Errorf("content-length header not present")
	}

	size, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing content-length: %w", err)
	}

	return size, nil
}

// DownloadRange downloads bytes [start, end] inclusive.
func (httpSource *HTTPSource) DownloadRange(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpSource.url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := httpSource.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()

		return nil, fmt.Errorf("unexpected status code: %d (expected 206)", resp.StatusCode)
	}

	return resp.Body, nil
}

// AcceptsRanges reports whether the server accepts Range requests.
func (httpSource *HTTPSource) AcceptsRanges(ctx context.Context) (bool, error) {
	httpSource.rangeOnce.Do(func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, httpSource.url, nil)
		if err != nil {
			httpSource.acceptsRangesErr = fmt.Errorf("creating HEAD request: %w", err)

			return
		}

		resp, err := httpSource.client.Do(req)
		if err != nil {
			httpSource.acceptsRangesErr = fmt.Errorf("executing HEAD request: %w", err)

			return
		}

		defer resp.Body.Close()

		acceptRanges := resp.Header.Get("Accept-Ranges")
		httpSource.acceptsRangesVal = strings.EqualFold(acceptRanges, "bytes")
	})

	return httpSource.acceptsRangesVal, httpSource.acceptsRangesErr
}
