package main

import (
	"io"

	"github.com/schollz/progressbar/v3"
)

// ProgressWriter wraps a writer with a progress bar.
type ProgressWriter struct {
	writer io.Writer
	bar    *progressbar.ProgressBar
}

// NewProgressWriter creates a new progress writer with a progress bar.
func NewProgressWriter(w io.Writer, total int64, description string) *ProgressWriter {
	bar := progressbar.NewOptions64(
		total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			// Print newline after completion.
			_, _ = io.WriteString(w, "\n")
		}),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	return &ProgressWriter{
		writer: w,
		bar:    bar,
	}
}

// Write implements io.Writer and updates the progress bar.
func (progressWriter *ProgressWriter) Write(data []byte) (int, error) {
	n := len(data)

	err := progressWriter.bar.Add(n)
	if err != nil {
		return n, err
	}

	return n, nil
}

// SetCurrent sets the current progress value (useful for resume).
func (progressWriter *ProgressWriter) SetCurrent(current int64) {
	_ = progressWriter.bar.Set64(current)
}

// Finish completes the progress bar.
func (progressWriter *ProgressWriter) Finish() {
	_ = progressWriter.bar.Finish()
}
