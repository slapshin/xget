package segment

import (
	"sync"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// SharedProgressWriter is a thread-safe progress writer for segmented downloads.
// Multiple goroutines can write to it concurrently, updating a single progress bar.
type SharedProgressWriter struct {
	bar      *mpb.Bar
	mu       sync.Mutex
	lastTime time.Time
	started  bool
}

// NewSharedProgressWriter creates a new SharedProgressWriter with a single progress bar.
func NewSharedProgressWriter(container *mpb.Progress, total int64, description string) *SharedProgressWriter {
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

	return &SharedProgressWriter{
		bar: bar,
	}
}

// Write implements io.Writer and updates the progress bar in a thread-safe manner.
func (writer *SharedProgressWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	now := time.Now()

	if !writer.started {
		writer.started = true
		writer.lastTime = now

		writer.bar.EwmaIncrBy(len(data), time.Millisecond)

		return len(data), nil
	}

	elapsed := now.Sub(writer.lastTime)
	writer.lastTime = now

	writer.bar.EwmaIncrBy(len(data), elapsed)

	return len(data), nil
}

// SetCurrent sets the current progress value for already-completed bytes.
func (writer *SharedProgressWriter) SetCurrent(current int64) {
	writer.bar.SetCurrent(current)
}

// Finish marks the progress bar as complete.
func (writer *SharedProgressWriter) Finish() {
	writer.bar.SetTotal(-1, true)
}
