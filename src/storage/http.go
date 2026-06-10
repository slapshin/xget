package storage

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HTTPSource implements Source for HTTP/HTTPS URLs.
type HTTPSource struct {
	url              string
	http1Client      *http.Client
	fallbackClient   *http.Client
	http1Unsupported atomic.Bool
	rangeOnce        sync.Once
	acceptsRangesVal bool
	acceptsRangesErr error
}

// NewHTTPSource creates an HTTPSource for the given URL and timeout.
func NewHTTPSource(url string, timeout time.Duration) *HTTPSource {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		baseTransport = &http.Transport{}
	}

	// Force HTTP/1.1: some CDNs (e.g. Cloudflare) reset multiplexed HTTP/2
	// streams under concurrent range-request load (RST_STREAM INTERNAL_ERROR).
	// With HTTP/1.1 each range request gets its own connection.
	http1Transport := baseTransport.Clone()
	http1Transport.ForceAttemptHTTP2 = false
	http1Transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}

	return &HTTPSource{
		url: url,
		http1Client: &http.Client{
			Timeout:   timeout,
			Transport: http1Transport,
		},
		// Keeps protocol negotiation enabled for HTTP/2-only servers.
		fallbackClient: &http.Client{
			Timeout:   timeout,
			Transport: baseTransport.Clone(),
		},
	}
}

// doRequest sends the request with the HTTP/1.1 client and retries with the
// HTTP/2-capable client when the server cannot speak HTTP/1.1.
func (httpSource *HTTPSource) doRequest(req *http.Request) (*http.Response, error) {
	if httpSource.http1Unsupported.Load() {
		return httpSource.fallbackClient.Do(req)
	}

	// Clone for the retry: a request must not be reused after Do. The body is
	// shared by Clone, so callers must send bodyless requests (all do).
	retryReq := req.Clone(req.Context())

	resp, err := httpSource.http1Client.Do(req)
	if err == nil || !isHTTP2PrefaceError(err) {
		return resp, err
	}

	if httpSource.http1Unsupported.CompareAndSwap(false, true) {
		fmt.Fprintf(os.Stderr, "warning: %s: server does not support http/1.1, using single stream mode\n",
			httpSource.url)
	}

	return httpSource.fallbackClient.Do(retryReq)
}

// isHTTP2PrefaceError reports whether the error indicates a server that only
// speaks HTTP/2: either it answered an HTTP/1.x request with raw HTTP/2
// frames, or the TLS handshake failed because no common protocol was found.
// String matching is the only option here: net/http and crypto/tls return
// unexported error types with no sentinel for errors.Is/errors.As.
func isHTTP2PrefaceError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()

	return strings.Contains(message, "malformed HTTP response") ||
		strings.Contains(message, "no application protocol")
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

	resp, err := httpSource.doRequest(req)
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

	resp, err := httpSource.doRequest(req)
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

	resp, err := httpSource.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode == http.StatusPartialContent {
		return resp.Body, nil
	}

	// Some CDNs (e.g. Cloudflare on a cache miss) ignore the Range header and
	// return 200 with the full body. Skip the prefix and cap the body so the
	// caller still receives exactly the requested range.
	if resp.StatusCode == http.StatusOK {
		if start > 0 {
			_, err = io.CopyN(io.Discard, resp.Body, start)
			if err != nil {
				resp.Body.Close()

				return nil, fmt.Errorf("discarding %d bytes before range start: %w", start, err)
			}
		}

		return newLimitedReadCloser(resp.Body, end-start+1), nil
	}

	resp.Body.Close()

	return nil, fmt.Errorf("unexpected status code: %d (expected 206)", resp.StatusCode)
}

// limitedReadCloser reads at most a fixed number of bytes from the underlying
// body while closing the full response body on Close.
type limitedReadCloser struct {
	reader io.Reader
	body   io.Closer
}

func newLimitedReadCloser(body io.ReadCloser, limit int64) io.ReadCloser {
	return &limitedReadCloser{
		reader: io.LimitReader(body, limit),
		body:   body,
	}
}

// Read reads from the limited range body.
func (limited *limitedReadCloser) Read(p []byte) (int, error) {
	return limited.reader.Read(p)
}

// Close closes the underlying response body.
func (limited *limitedReadCloser) Close() error {
	return limited.body.Close()
}

// AcceptsRanges reports whether the server accepts Range requests.
func (httpSource *HTTPSource) AcceptsRanges(ctx context.Context) (bool, error) {
	httpSource.rangeOnce.Do(func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, httpSource.url, nil)
		if err != nil {
			httpSource.acceptsRangesErr = fmt.Errorf("creating HEAD request: %w", err)

			return
		}

		resp, err := httpSource.doRequest(req)
		if err != nil {
			httpSource.acceptsRangesErr = fmt.Errorf("executing HEAD request: %w", err)

			return
		}

		defer resp.Body.Close()

		acceptRanges := resp.Header.Get("Accept-Ranges")
		httpSource.acceptsRangesVal = strings.EqualFold(acceptRanges, "bytes")
	})

	// Parallel range requests need one HTTP/1.1 connection per segment; on an
	// HTTP/2-only server they would share a single multiplexed connection, so
	// report no range support to force single stream mode. Any probe error is
	// deliberately dropped: single stream is the decisive answer either way.
	if httpSource.http1Unsupported.Load() {
		return false, nil
	}

	return httpSource.acceptsRangesVal, httpSource.acceptsRangesErr
}
