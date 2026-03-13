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
	"testing"
	"time"

	"xget/src/storage"

	"github.com/vbauerster/mpb/v8"
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

	progress := mpb.New(mpb.WithOutput(io.Discard))

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		4,
		progress,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	progress.Wait()

	// Verify file content.
	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}

	// State file should be cleaned up.
	statePath := StatePath(partialPath)

	_, err = os.Stat(statePath)
	if !os.IsNotExist(err) {
		t.Error("state file should be removed after successful download")
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

	progress := mpb.New(mpb.WithOutput(io.Discard))

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		progress,
		"testfile",
	)

	err = downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	progress.Wait()

	// Verify file content.
	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

func TestSegmentedDownloadTwoSegments(t *testing.T) {
	content := []byte(strings.Repeat("X", 1001)) // Odd size to test remainder.

	server := newTestServer(t, content)
	defer server.Close()

	source := storage.NewHTTPSource(server.URL, 30*time.Second)

	dir := t.TempDir()
	partialPath := filepath.Join(dir, "testfile.partial")

	progress := mpb.New(mpb.WithOutput(io.Discard))

	downloader := NewDownloader(
		source,
		int64(len(content)),
		partialPath,
		2,
		progress,
		"testfile",
	)

	err := downloader.Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	progress.Wait()

	got, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}
