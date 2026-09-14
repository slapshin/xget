package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xget/src/config"
	"xget/src/output"
	"xget/src/segment"
	"xget/src/storage"
)

// newRangeServer serves content with Range support, mimicking the cache's S3
// endpoint closely enough to exercise the shared transfer pipeline.
func newRangeServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(newRangeServerHandler(t, content))
}

func newRangeServerHandler(t *testing.T, content []byte) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		start, end, ok := parseTestRange(r.Header.Get("Range"), int64(len(content)))
		if !ok {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)

			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[start : end+1])
	}
}

func parseTestRange(header string, size int64) (int64, int64, bool) {
	if header == "" {
		return 0, 0, false
	}

	var start, end int64

	_, err := fmt.Sscanf(header, "bytes=%d-%d", &start, &end)
	if err != nil {
		_, err = fmt.Sscanf(header, "bytes=%d-", &start)
		if err != nil {
			return 0, 0, false
		}

		end = size - 1
	}

	if start < 0 || end >= size || start > end {
		return 0, 0, false
	}

	return start, end, true
}

func testSettings(segmentsPerFile int) config.Settings {
	return config.Settings{
		Parallel:        1,
		Retries:         3,
		RetryDelay:      time.Millisecond,
		Timeout:         30 * time.Second,
		SegmentsPerFile: segmentsPerFile,
		SegmentMinSize:  1,
	}
}

// syncBuffer collects reporter output; the debug reporter writes from its stats
// goroutine as well as from the transfer, so it needs its own lock.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (buffer *syncBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.buf.Write(data)
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.buf.String()
}

// newTestDownloader returns a Downloader whose reporter renders debug lines into
// the returned buffer, so tests can assert which transfer path was taken.
func newTestDownloader(t *testing.T, settings config.Settings) (*Downloader, *syncBuffer) {
	t.Helper()

	cfg := &config.Config{Settings: settings}
	logs := &syncBuffer{}
	reporter := output.NewDebugReporter(context.Background(), logs, logs, time.Hour)

	t.Cleanup(reporter.Wait)

	return NewDownloader(cfg, nil, reporter), logs
}

func hashOf(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

func TestTransferJobPartialPath(t *testing.T) {
	job := transferJob{dest: "/tmp/models/file.safetensors"}

	if got := job.partialPath(); got != "/tmp/models/file.safetensors.partial" {
		t.Errorf("partialPath() = %q, want %q", got, "/tmp/models/file.safetensors.partial")
	}
}

func TestTransferWithRetry(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 200))

	tests := []struct {
		name            string
		segmentsPerFile int
		singleStream    string
		wantSegmented   bool
	}{
		{
			name:            "segmented",
			segmentsPerFile: 4,
			wantSegmented:   true,
		},
		{
			name:            "single stream when segments disabled",
			segmentsPerFile: 1,
		},
		{
			name:            "single stream when forced by settings",
			segmentsPerFile: 4,
			singleStream:    "true",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newRangeServer(t, content)
			defer server.Close()

			settings := testSettings(test.segmentsPerFile)
			settings.SingleStream = test.singleStream

			downloader, logs := newTestDownloader(t, settings)
			dest := filepath.Join(t.TempDir(), "file.bin")

			job := transferJob{
				dest:   dest,
				sha256: hashOf(content),
				label:  cacheLabelPrefix + dest,
				origin: "cache",
				newSource: func(_ context.Context) (storage.Source, error) {
					return storage.NewHTTPSource(server.URL, settings.Timeout), nil
				},
			}

			err := downloader.transferWithRetry(context.Background(), job)
			if err != nil {
				t.Fatalf("transferWithRetry: %v", err)
			}

			assertFileContent(t, dest, content)
			assertNotExists(t, job.partialPath())
			assertNotExists(t, segment.StatePath(job.partialPath()))

			marker := "(single stream"
			if test.wantSegmented {
				marker = "(segmented,"
			}

			if !strings.Contains(logs.String(), marker) {
				t.Errorf("debug output %q does not report %q", logs.String(), marker)
			}
		})
	}
}

func TestTransferWithRetryResumesPartialFile(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 200))

	server := newRangeServer(t, content)
	defer server.Close()

	settings := testSettings(1)
	downloader, _ := newTestDownloader(t, settings)
	dest := filepath.Join(t.TempDir(), "file.bin")

	// Simulate an interrupted transfer that already wrote the first half.
	half := len(content) / 2

	err := os.WriteFile(dest+".partial", content[:half], 0o600)
	if err != nil {
		t.Fatalf("writing partial file: %v", err)
	}

	var served atomic.Int64

	job := transferJob{
		dest:   dest,
		sha256: hashOf(content),
		label:  cacheLabelPrefix + dest,
		origin: "cache",
		newSource: func(_ context.Context) (storage.Source, error) {
			served.Add(1)

			return storage.NewHTTPSource(server.URL, settings.Timeout), nil
		},
	}

	err = downloader.transferWithRetry(context.Background(), job)
	if err != nil {
		t.Fatalf("transferWithRetry: %v", err)
	}

	assertFileContent(t, dest, content)

	if served.Load() != 1 {
		t.Errorf("source built %d times, want 1", served.Load())
	}
}

func TestTransferWithRetryRetriesFailedAttempts(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 200))

	var attempts atomic.Int64

	// The first attempt hangs up mid-body; later attempts serve normally.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content[:10])

			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("response writer does not support hijacking")

				return
			}

			conn, _, hijackErr := hijacker.Hijack()
			if hijackErr != nil {
				t.Errorf("hijacking connection: %v", hijackErr)

				return
			}

			conn.Close()

			return
		}

		newRangeServerHandler(t, content)(w, r)
	}))
	defer server.Close()

	settings := testSettings(1)
	downloader, _ := newTestDownloader(t, settings)
	dest := filepath.Join(t.TempDir(), "file.bin")

	job := transferJob{
		dest:   dest,
		sha256: hashOf(content),
		label:  cacheLabelPrefix + dest,
		origin: "cache",
		newSource: func(_ context.Context) (storage.Source, error) {
			return storage.NewHTTPSource(server.URL, settings.Timeout), nil
		},
	}

	err := downloader.transferWithRetry(context.Background(), job)
	if err != nil {
		t.Fatalf("transferWithRetry: %v", err)
	}

	assertFileContent(t, dest, content)

	if attempts.Load() < 2 {
		t.Errorf("server saw %d requests, want a retry", attempts.Load())
	}
}

func TestTransferWithRetryChecksumMismatch(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 200))

	server := newRangeServer(t, content)
	defer server.Close()

	settings := testSettings(4)
	downloader, _ := newTestDownloader(t, settings)
	dest := filepath.Join(t.TempDir(), "file.bin")

	job := transferJob{
		dest:   dest,
		sha256: hashOf([]byte("something else")),
		label:  cacheLabelPrefix + dest,
		origin: "cache",
		newSource: func(_ context.Context) (storage.Source, error) {
			return storage.NewHTTPSource(server.URL, settings.Timeout), nil
		},
	}

	err := downloader.transferWithRetry(context.Background(), job)
	if err == nil {
		t.Fatal("transferWithRetry: expected checksum mismatch, got nil")
	}

	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want checksum mismatch", err)
	}

	assertNotExists(t, dest)
	assertNotExists(t, job.partialPath())
	assertNotExists(t, segment.StatePath(job.partialPath()))
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("content of %s: got %d bytes, want %d bytes", path, len(got), len(want))
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	if !os.IsNotExist(err) {
		t.Errorf("%s should not exist", path)
	}
}
