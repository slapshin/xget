package main

import (
	"io"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// ProgressWriter wraps a writer with a progress bar backed by an mpb container.
type ProgressWriter struct {
	bar    *mpb.Bar
	writer io.Writer
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
		),
	)

	return &ProgressWriter{
		bar:    bar,
		writer: bar.ProxyWriter(io.Discard),
	}
}

// Write implements io.Writer and updates the progress bar.
func (progressWriter *ProgressWriter) Write(data []byte) (int, error) {
	return progressWriter.writer.Write(data)
}

// SetCurrent sets the current progress value (useful for resume).
func (progressWriter *ProgressWriter) SetCurrent(current int64) {
	progressWriter.bar.SetCurrent(current)
}

// Finish marks the bar as complete.
func (progressWriter *ProgressWriter) Finish() {
	progressWriter.bar.SetTotal(-1, true)
}
