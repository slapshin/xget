package segment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"xget/src/storage"

	"github.com/vbauerster/mpb/v8"
)

const (
	defaultSegmentAttempts   = 3
	defaultSegmentRetryDelay = time.Second
)

// Downloader performs segmented parallel downloads of a single file.
type Downloader struct {
	source       storage.RangeSource
	totalSize    int64
	partialPath  string
	segmentCount int
	progress     *mpb.Progress
	fileName     string
	stateMu      sync.Mutex
	attempts     int
	retryDelay   time.Duration
}

// NewDownloader creates a new segmented Downloader.
func NewDownloader(
	source storage.RangeSource,
	totalSize int64,
	partialPath string,
	segmentCount int,
	progress *mpb.Progress,
	fileName string,
) *Downloader {
	return &Downloader{
		source:       source,
		totalSize:    totalSize,
		partialPath:  partialPath,
		segmentCount: segmentCount,
		progress:     progress,
		fileName:     fileName,
		attempts:     defaultSegmentAttempts,
		retryDelay:   defaultSegmentRetryDelay,
	}
}

// Download executes the segmented download.
func (downloader *Downloader) Download(ctx context.Context) error {
	statePath := StatePath(downloader.partialPath)

	state, err := downloader.loadOrCreateState(statePath)
	if err != nil {
		return err
	}

	// Pre-allocate the file.
	err = downloader.preallocateFile()
	if err != nil {
		return err
	}

	// Open file for concurrent writing.
	file, err := os.OpenFile(downloader.partialPath, os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening partial file: %w", err)
	}

	defer file.Close()

	// Create shared progress writer.
	progressWriter := NewSharedProgressWriter(downloader.progress, downloader.totalSize, downloader.fileName)

	completedBytes := state.CompletedBytes()
	if completedBytes > 0 {
		progressWriter.SetCurrent(completedBytes)
	}

	// Download incomplete segments in parallel.
	err = downloader.downloadSegments(ctx, state, file, progressWriter, statePath)
	if err != nil {
		return err
	}

	progressWriter.Finish()

	return nil
}

func (downloader *Downloader) loadOrCreateState(statePath string) (*State, error) {
	state, err := LoadState(statePath)
	if err == nil && state.TotalSize == downloader.totalSize && state.SegmentCount == downloader.segmentCount {
		return state, nil
	}

	state = NewState(downloader.totalSize, downloader.segmentCount)

	err = SaveState(statePath, state)
	if err != nil {
		return nil, fmt.Errorf("saving initial state: %w", err)
	}

	return state, nil
}

func (downloader *Downloader) preallocateFile() error {
	file, err := os.OpenFile(downloader.partialPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("creating partial file: %w", err)
	}

	defer file.Close()

	err = file.Truncate(downloader.totalSize)
	if err != nil {
		return fmt.Errorf("pre-allocating file: %w", err)
	}

	return nil
}

func (downloader *Downloader) downloadSegments(
	ctx context.Context,
	state *State,
	file *os.File,
	progressWriter *SharedProgressWriter,
	statePath string,
) error {
	var wg sync.WaitGroup

	errCh := make(chan error, len(state.Segments))

	for i := range state.Segments {
		if state.Segments[i].Done {
			continue
		}

		wg.Add(1)

		go func(seg *Segment) {
			defer wg.Done()

			err := downloader.downloadSegment(ctx, seg, file, progressWriter, state, statePath)
			if err != nil {
				errCh <- fmt.Errorf("segment %d: %w", seg.Index, err)
			}
		}(&state.Segments[i])
	}

	wg.Wait()
	close(errCh)

	// Return first error.
	for err := range errCh {
		return err
	}

	return nil
}

func (downloader *Downloader) downloadSegment(
	ctx context.Context,
	seg *Segment,
	file *os.File,
	progressWriter *SharedProgressWriter,
	state *State,
	statePath string,
) error {
	expectedBytes := seg.End - seg.Start + 1

	var written int64

	var lastErr error

	// Transient mid-stream failures (e.g. CDN connection resets) are retried
	// here, resuming from the bytes already written instead of failing the
	// whole file.
	for attempt := 1; attempt <= downloader.attempts; attempt++ {
		n, err := downloader.transferSegment(ctx, seg, file, progressWriter, written)
		written += n

		if err == nil && written == expectedBytes {
			return downloader.markSegmentDone(seg, state, statePath)
		}

		if err == nil {
			err = fmt.Errorf("short read: expected %d bytes, got %d", expectedBytes, written)
		}

		lastErr = err

		if ctx.Err() != nil {
			return lastErr
		}

		if attempt < downloader.attempts {
			time.Sleep(downloader.retryDelay)
		}
	}

	return fmt.Errorf("all %d attempts: %w", downloader.attempts, lastErr)
}

// transferSegment downloads the remaining bytes of the segment, starting after
// the already-written prefix, and returns the number of bytes transferred.
func (downloader *Downloader) transferSegment(
	ctx context.Context,
	seg *Segment,
	file *os.File,
	progressWriter *SharedProgressWriter,
	written int64,
) (int64, error) {
	reader, err := downloader.source.DownloadRange(ctx, seg.Start+written, seg.End)
	if err != nil {
		return 0, fmt.Errorf("downloading range: %w", err)
	}

	defer reader.Close()

	offsetWriter := io.NewOffsetWriter(file, seg.Start+written)

	n, err := io.Copy(io.MultiWriter(offsetWriter, progressWriter), reader)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return n, fmt.Errorf("short read: %w", err)
		}

		return n, fmt.Errorf("writing segment: %w", err)
	}

	return n, nil
}

func (downloader *Downloader) markSegmentDone(seg *Segment, state *State, statePath string) error {
	downloader.stateMu.Lock()
	defer downloader.stateMu.Unlock()

	seg.Done = true

	err := SaveState(statePath, state)
	if err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	return nil
}
