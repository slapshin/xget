package segment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"xget/src/output"
	"xget/src/storage"
)

const (
	defaultSegmentAttempts   = 3
	defaultSegmentRetryDelay = time.Second
	stateSaveInterval        = time.Second
)

// Downloader performs segmented parallel downloads of a single file.
type Downloader struct {
	source       storage.RangeSource
	totalSize    int64
	partialPath  string
	segmentCount int
	reporter     output.Reporter
	fileName     string
	stateMu      sync.Mutex
	attempts     int
	retryDelay   time.Duration
	// lastStateSave throttles mid-transfer state saves; guarded by stateMu.
	lastStateSave time.Time
}

// NewDownloader creates a new segmented Downloader.
func NewDownloader(
	source storage.RangeSource,
	totalSize int64,
	partialPath string,
	segmentCount int,
	reporter output.Reporter,
	fileName string,
) *Downloader {
	return &Downloader{
		source:       source,
		totalSize:    totalSize,
		partialPath:  partialPath,
		segmentCount: segmentCount,
		reporter:     reporter,
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

	// Create the shared progress tracker.
	tracker := downloader.reporter.NewTracker(downloader.totalSize, downloader.fileName)
	defer tracker.Abort()

	completedBytes := state.CompletedBytes()
	if completedBytes > 0 {
		tracker.SetCurrent(completedBytes)
	}

	downloader.reporter.Debugf("segmented download %s: %d segments, %d/%d bytes already on disk",
		downloader.fileName, len(state.Segments), completedBytes, downloader.totalSize)

	// Download incomplete segments in parallel.
	downloadErr := downloader.downloadSegments(ctx, state, file, tracker, statePath)

	// Persist final per-segment progress so an interrupted or failed download
	// resumes mid-segment on the next run.
	flushErr := downloader.flushState(state, statePath)

	if downloadErr != nil {
		if flushErr != nil {
			downloader.reporter.Errorf("could not save segment state for %s: %v", downloader.fileName, flushErr)
		}

		return downloadErr
	}

	if flushErr != nil {
		return flushErr
	}

	tracker.Finish()

	return nil
}

func (downloader *Downloader) loadOrCreateState(statePath string) (*State, error) {
	state, err := LoadState(statePath)
	if err == nil && state.TotalSize == downloader.totalSize && state.SegmentCount == downloader.segmentCount {
		sanitizeSegments(state)

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
	tracker output.Tracker,
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

			err := downloader.downloadSegment(ctx, seg, file, tracker, state, statePath)
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
	tracker output.Tracker,
	state *State,
	statePath string,
) error {
	expectedBytes := seg.Size()

	// Resume from progress persisted by a previous run.
	written := seg.Written
	if written >= expectedBytes {
		// All bytes were written but the process died before the segment was
		// marked done; requesting a further range would be invalid.
		return downloader.markSegmentDone(seg, state, statePath)
	}

	progress := &segmentProgress{
		downloader: downloader,
		seg:        seg,
		state:      state,
		statePath:  statePath,
	}

	downloader.reporter.Debugf("%s: segment %d range %d-%d, resuming at %d",
		downloader.fileName, seg.Index, seg.Start, seg.End, written)

	var lastErr error

	// Transient mid-stream failures (e.g. CDN connection resets) are retried
	// here, resuming from the bytes already written instead of failing the
	// whole file.
	for attempt := 1; attempt <= downloader.attempts; attempt++ {
		n, err := downloader.transferSegment(ctx, seg, file, progress, tracker, written)
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
			downloader.reporter.Errorf("%s: segment %d attempt %d/%d: %v, retrying...",
				downloader.fileName, seg.Index, attempt, downloader.attempts, err)
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
	progress *segmentProgress,
	tracker output.Tracker,
	written int64,
) (int64, error) {
	reader, err := downloader.source.DownloadRange(ctx, seg.Start+written, seg.End)
	if err != nil {
		return 0, fmt.Errorf("downloading range: %w", err)
	}

	defer reader.Close()

	offsetWriter := io.NewOffsetWriter(file, seg.Start+written)

	// The progress recorder must come after offsetWriter: MultiWriter stops at
	// the first failed writer, so it only counts bytes the file accepted and the
	// persisted progress never exceeds the data on disk.
	n, err := io.Copy(io.MultiWriter(offsetWriter, progress, tracker), reader)
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
	seg.Written = seg.Size()

	err := SaveState(statePath, state)
	if err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	downloader.reporter.Debugf("%s: segment %d done (%d bytes)", downloader.fileName, seg.Index, seg.Size())

	return nil
}

// flushState persists the current state unconditionally.
func (downloader *Downloader) flushState(state *State, statePath string) error {
	downloader.stateMu.Lock()
	defer downloader.stateMu.Unlock()

	err := SaveState(statePath, state)
	if err != nil {
		return fmt.Errorf("saving final state: %w", err)
	}

	return nil
}

// sanitizeSegments repairs progress counters loaded from disk so that a stale
// or hand-edited state file cannot produce invalid range requests.
func sanitizeSegments(state *State) {
	for i := range state.Segments {
		seg := &state.Segments[i]

		switch {
		case seg.Done:
			seg.Written = seg.Size()
		case seg.Written < 0 || seg.Written > seg.Size():
			// An out-of-range counter means the state file is corrupt; the only
			// safe recovery is to re-download the whole segment.
			seg.Written = 0
		}
	}
}

// segmentProgress records bytes written for one segment so its progress can be
// persisted and resumed across process restarts.
type segmentProgress struct {
	downloader *Downloader
	seg        *Segment
	state      *State
	statePath  string
}

// Write implements io.Writer. It runs after the file writer in the transfer
// MultiWriter, so every byte counted here is already accepted by the file.
func (progress *segmentProgress) Write(data []byte) (int, error) {
	progress.recordProgress(int64(len(data)))

	return len(data), nil
}

// recordProgress advances the segment's persisted progress and saves the state
// at most once per stateSaveInterval. Save errors are ignored here: a missed
// throttled save only costs resume granularity, and the final flush in
// Download surfaces persistent problems.
func (progress *segmentProgress) recordProgress(n int64) {
	downloader := progress.downloader

	downloader.stateMu.Lock()
	defer downloader.stateMu.Unlock()

	progress.seg.Written += n

	if time.Since(downloader.lastStateSave) < stateSaveInterval {
		return
	}

	downloader.lastStateSave = time.Now()

	_ = SaveState(progress.statePath, progress.state)
}
