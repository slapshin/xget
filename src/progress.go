package main

import (
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// ProgressWriter wraps a writer with a progress bar backed by an mpb container.
type ProgressWriter struct {
	bar      *mpb.Bar
	lastTime time.Time
	started  bool
}

// NewProgressWriter adds a new progress bar to the given mpb container and returns
// a ProgressWriter that updates it as data is written.
func NewProgressWriter(container *mpb.Progress, total int64, description string) *ProgressWriter {
	bar := container.AddBar(total,
		mpb.PrependDecorators(
			decor.Name(description, decor.WC{C: decor.DindentRight | decor.DextraSpace}),
		),
		mpb.AppendDecorators(
			decor.CountersKibiByte("% .2f / % .2f"),
			decor.Name(" "),
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 30),
			decor.Name(" ETA:"),
			decor.EwmaETA(decor.ET_STYLE_GO, 30),
		),
	)

	return &ProgressWriter{
		bar: bar,
	}
}

// Write implements io.Writer and updates the progress bar.
// Elapsed time is measured between successive Write calls, which reflects
// the real network read rate from the upstream io.Copy.
// The first call initialises the clock to avoid counting connection setup time.
func (progressWriter *ProgressWriter) Write(data []byte) (int, error) {
	now := time.Now()

	if !progressWriter.started {
		progressWriter.started = true
		progressWriter.lastTime = now

		progressWriter.bar.EwmaIncrBy(len(data), time.Millisecond)

		return len(data), nil
	}

	elapsed := now.Sub(progressWriter.lastTime)
	progressWriter.lastTime = now

	progressWriter.bar.EwmaIncrBy(len(data), elapsed)

	return len(data), nil
}

// SetCurrent sets the current progress value (useful for resume).
func (progressWriter *ProgressWriter) SetCurrent(current int64) {
	progressWriter.bar.SetCurrent(current)
}

// Finish marks the bar as complete.
func (progressWriter *ProgressWriter) Finish() {
	progressWriter.bar.SetTotal(-1, true)
}
