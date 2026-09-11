package segment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xget/src/output"
	"xget/src/storage"
)

func newTestServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
		if err != nil {
			// Try open-ended range.
			_, err = fmt.Sscanf(rangeHeader, "bytes=%d-", &start)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)

				return
			}

			end = int64(len(content)) - 1
		}

		if start >= int64(len(content)) || end >= int64(len(content)) || start > end {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)

			return
		}

		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[start : end+1])
	}))
}

func TestSegmentedDownload(t *testing.T) {
	// Create test content.
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes.

	server := newTestServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	// Verify file content.
	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}

	// State file should still exist — cleanup is the caller's responsibility
	// (after checksum verification and file rename).
	statePath := StatePath(partialPath)

	_, err = os.Stat(statePath)
	if os.IsNotExist(err) {
		t.Error("state file should be preserved after Download() for caller to clean up")
	}
}

func TestSegmentedDownloadResume(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes.

	server := newTestServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	// Pre-create partial file and state with segment 0 done.
	state := NewState(int64(len(content)), 2)
	state.Segments[0].Done = true

	// Write first half to the partial file.
	f, err := os.Create(partialPath)
	if err != nil {
		t.Fatal(err)
	}

	err = f.Truncate(int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.WriteAt(content[:500], 0)
	if err != nil {
		t.Fatal(err)
	}

	f.Close()

	// Save state.
	statePath := StatePath(partialPath)

	err = SaveState(statePath, state)
	if err != nil {
		t.Fatal(err)
	}

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		reporter,
		"testfile",
	)

	err = downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	// Verify file content.
	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

// newShortReadServer returns a test server that truncates range responses by
// dropping the last truncateBytes bytes of each requested range.
func newShortReadServer(t *testing.T, content []byte, truncateBytes int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		slice := content[start : end+1]

		// Truncate the slice to simulate a premature EOF.
		if truncateBytes > 0 && len(slice) > truncateBytes {
			slice = slice[:len(slice)-truncateBytes]
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(slice)
	}))
}

func TestSegmentedDownloadShortReadRecovers(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes.

	// Server drops the last 10 bytes of every range response; the per-segment
	// retry resumes from the bytes already written and completes the download.
	server := newShortReadServer(t, content, 10)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)
	downloader.retryDelay = 10 * time.Millisecond

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

// newEmptyRangeServer returns a test server that declares the full range
// length but never writes a body, so every range request fails mid-stream.
func newEmptyRangeServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		// No body written: the connection closes short of Content-Length.
	}))
}

func TestSegmentedDownloadExhaustsRetries(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes.

	server := newEmptyRangeServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)
	downloader.retryDelay = 10 * time.Millisecond

	err := downloader.Download(context.Background())
	if err == nil {
		t.Fatal("expected error when every range request fails, got nil")
	}

	if !strings.Contains(err.Error(), "all 3 attempts") {
		t.Errorf("expected 'all 3 attempts' in error, got: %v", err)
	}

	// State file must still exist so the segments can be retried.
	statePath := StatePath(partialPath)

	_, statErr := os.Stat(statePath)
	if os.IsNotExist(statErr) {
		t.Error("state file should be preserved after a failed download")
	}
}

// A failed download must abort its progress bar; otherwise the mpb container's
// Wait blocks forever on the incomplete bar and the program freezes after all
// downloads finish.
func TestSegmentedDownloadFailureUnblocksProgressWait(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes.

	server := newEmptyRangeServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)
	downloader.retryDelay = 10 * time.Millisecond

	err := downloader.Download(context.Background())
	if err == nil {
		t.Fatal("expected error when every range request fails, got nil")
	}

	waitDone := make(chan struct{})

	go func() {
		reporter.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reporter.Wait() did not return after a failed download (leaked progress bar)")
	}
}

// newAbortingServer returns a test server that aborts the connection partway
// through the body of the first range request, simulating a CDN stream reset.
func newAbortingServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	var callCount atomic.Int32

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		slice := content[start : end+1]

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)

		if callCount.Add(1) == 1 {
			// Send half the body, flush it, then abort the connection.
			_, _ = w.Write(slice[:len(slice)/2])

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			panic(http.ErrAbortHandler)
		}

		_, _ = w.Write(slice)
	}))
}

func TestSegmentedDownloadConnectionAbortRecovers(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes.

	server := newAbortingServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)
	downloader.retryDelay = 10 * time.Millisecond

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

// newOneSegmentShortReadServer returns a test server that truncates only the
// first range request to simulate a short read on one segment.
func newOneSegmentShortReadServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	var callCount atomic.Int32

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		slice := content[start : end+1]
		count := callCount.Add(1)

		// Truncate only the very first range request.
		if count == 1 && len(slice) > 10 {
			slice = slice[:len(slice)-10]
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(slice)
	}))
}

func TestSegmentedDownloadShortReadOneSegment(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes, 4 segments of 250 bytes.

	server := newOneSegmentShortReadServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)
	downloader.retryDelay = 10 * time.Millisecond

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}

	// All segments must be marked Done in the preserved state file.
	statePath := StatePath(partialPath)

	state, loadErr := LoadState(statePath)
	if loadErr != nil {
		t.Fatalf("loading state after download: %v", loadErr)
	}

	for _, seg := range state.Segments {
		if !seg.Done {
			t.Errorf("segment %d not marked Done after successful download", seg.Index)
		}
	}
}

// newRangeRecordingServer returns a test server that serves ranges correctly
// and records the start offset of every range request it receives.
func newRangeRecordingServer(t *testing.T, content []byte) (*httptest.Server, func() []int64) {
	t.Helper()

	var mu sync.Mutex

	var starts []int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		mu.Lock()

		starts = append(starts, start)

		mu.Unlock()

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[start : end+1])
	}))

	rangeStarts := func() []int64 {
		mu.Lock()
		defer mu.Unlock()

		return append([]int64(nil), starts...)
	}

	return server, rangeStarts
}

// assertPartialContent fails the test when the partial file does not match the
// expected content.
func assertPartialContent(t *testing.T, partialPath string, content []byte) {
	t.Helper()

	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

// assertAllSegmentsDone fails the test when any segment in the state file is
// not marked done with its full size written.
func assertAllSegmentsDone(t *testing.T, statePath string) {
	t.Helper()

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("loading final state: %v", err)
	}

	for _, seg := range state.Segments {
		if !seg.Done || seg.Written != seg.Size() {
			t.Errorf("segment %d = {Done:%v Written:%d}, want done with full written", seg.Index, seg.Done, seg.Written)
		}
	}
}

// assertNoSegmentRestart fails the test when any recorded range request starts
// exactly at a segment boundary, i.e. a segment restarted instead of resuming.
func assertNoSegmentRestart(t *testing.T, starts []int64, segments []Segment) {
	t.Helper()

	for _, start := range starts {
		for _, seg := range segments {
			if start == seg.Start {
				t.Errorf("segment %d restarted from its beginning instead of resuming", seg.Index)
			}
		}
	}
}

// prepareResumeFixture writes a partial file containing the given prefix of
// content (zero-padded to full size) and saves the given state next to it.
func prepareResumeFixture(t *testing.T, partialPath string, content []byte, prefixLen int, state *State) {
	t.Helper()

	file, err := os.Create(partialPath)
	if err != nil {
		t.Fatal(err)
	}

	err = file.Truncate(int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}

	_, err = file.WriteAt(content[:prefixLen], 0)
	if err != nil {
		t.Fatal(err)
	}

	err = file.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = SaveState(StatePath(partialPath), state)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSegmentedDownloadResumePartialSegment(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes, 2 segments of 500.

	server, rangeStarts := newRangeRecordingServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	// Segment 0 fully done, segment 1 interrupted after 100 bytes.
	state := NewState(int64(len(content)), 2)
	state.Segments[0].Done = true
	state.Segments[1].Written = 100

	prepareResumeFixture(t, partialPath, content, 600, state)

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		reporter,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	assertPartialContent(t, partialPath, content)

	// The interrupted segment must resume mid-range, not from its start.
	starts := rangeStarts()
	if len(starts) != 1 || starts[0] != 600 {
		t.Errorf("range starts = %v, want a single request at 600", starts)
	}

	assertAllSegmentsDone(t, StatePath(partialPath))
}

func TestSegmentedDownloadFullyWrittenSegmentSkipsRequest(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes, 2 segments of 500.

	server, rangeStarts := newRangeRecordingServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	// Segment 0 fully written but the process died before it was marked done.
	state := NewState(int64(len(content)), 2)
	state.Segments[0].Written = 500

	prepareResumeFixture(t, partialPath, content, 500, state)

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		reporter,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	assertPartialContent(t, partialPath, content)

	for _, start := range rangeStarts() {
		if start < 500 {
			t.Errorf("fully written segment 0 was re-requested (range start %d)", start)
		}
	}

	assertAllSegmentsDone(t, StatePath(partialPath))
}

// newHalfThenAbortServer returns a test server that sends the first half of
// every requested range, flushes it, then aborts the connection.
func newHalfThenAbortServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.WriteHeader(http.StatusOK)

			return
		}

		var start, end int64

		_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		slice := content[start : end+1]

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(slice[:len(slice)/2])

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		panic(http.ErrAbortHandler)
	}))
}

func TestSegmentedDownloadFailurePersistsWritten(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes, 4 segments of 250.

	abortingServer := newHalfThenAbortServer(t, content)
	defer abortingServer.Close()

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		storage.NewHTTPSource(abortingServer.URL, 30*time.Second),
		int64(len(content)),
		partialPath,
		4,
		reporter,
		"testfile",
	)
	downloader.retryDelay = 10 * time.Millisecond

	err := downloader.Download(context.Background())
	if err == nil {
		t.Fatal("expected error when every range request aborts, got nil")
	}

	reporter.Wait()

	// Partial per-segment progress must survive the failed run.
	state, err := LoadState(StatePath(partialPath))
	if err != nil {
		t.Fatalf("loading state after failed download: %v", err)
	}

	for _, seg := range state.Segments {
		if seg.Written <= 0 || seg.Written >= seg.Size() {
			t.Errorf("segment %d Written = %d, want partial progress in (0, %d)", seg.Index, seg.Written, seg.Size())
		}
	}

	// A second run against a healthy server must resume mid-segment.
	goodServer, rangeStarts := newRangeRecordingServer(t, content)
	defer goodServer.Close()

	resumeReporter := output.NewBarReporter(context.Background(), io.Discard)

	resumeDownloader := NewDownloader(
		storage.NewHTTPSource(goodServer.URL, 30*time.Second),
		int64(len(content)),
		partialPath,
		4,
		resumeReporter,
		"testfile",
	)

	err = resumeDownloader.Download(context.Background())
	if err != nil {
		t.Fatalf("resumed Download: %v", err)
	}

	resumeReporter.Wait()

	assertPartialContent(t, partialPath, content)
	assertNoSegmentRestart(t, rangeStarts(), state.Segments)
}

func TestSegmentedDownloadSanitizesCorruptWritten(t *testing.T) {
	content := []byte(strings.Repeat("abcdefghij", 100)) // 1000 bytes, 2 segments of 500.

	server := newTestServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	// Corrupt counters: negative on one segment, beyond size on the other.
	state := NewState(int64(len(content)), 2)
	state.Segments[0].Written = -5
	state.Segments[1].Written = 600

	prepareResumeFixture(t, partialPath, content, 0, state)

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		reporter,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	assertPartialContent(t, partialPath, content)
}

func TestSegmentedDownloadTwoSegments(t *testing.T) {
	content := []byte(strings.Repeat("X", 1001)) // Odd size to test remainder.

	server := newTestServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	reporter := output.NewBarReporter(context.Background(), io.Discard)

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		reporter,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	reporter.Wait()

	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}
