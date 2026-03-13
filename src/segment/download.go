package segment

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"xget/src/storage"

	"github.com/vbauerster/mpb/v8"
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

	// Clean up state file on success.
	os.Remove(statePath)

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
	reader, err := downloader.source.DownloadRange(ctx, seg.Start, seg.End)
	if err != nil {
		return fmt.Errorf("downloading range: %w", err)
	}

	defer reader.Close()

	offsetWriter := io.NewOffsetWriter(file, seg.Start)

	_, err = io.Copy(io.MultiWriter(offsetWriter, progressWriter), reader)
	if err != nil {
		return fmt.Errorf("writing segment: %w", err)
	}

	seg.Done = true

	downloader.stateMu.Lock()
	defer downloader.stateMu.Unlock()

	err = SaveState(statePath, state)
	if err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	return nil
}
