package output

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

const ewmaAge = 30

// barReporter renders progress with mpb progress bars.
type barReporter struct {
	container *mpb.Progress
	out       io.Writer
	// activeBars counts the bars currently rendered. Messages are routed
	// through the container only while it renders, because a container with no
	// bars discards what is written to it.
	activeBars atomic.Int64
}

// NewBarReporter creates a Reporter that renders progress bars to out.
func NewBarReporter(ctx context.Context, out io.Writer) Reporter {
	return &barReporter{
		container: mpb.NewWithContext(ctx, mpb.WithOutput(out)),
		out:       out,
	}
}

// NewTracker adds a progress bar to the container.
func (reporter *barReporter) NewTracker(total int64, name string) Tracker {
	// Count the bar before it exists: a message must never be written directly
	// to the output while the container renders a bar to the same writer.
	reporter.activeBars.Add(1)

	bar := reporter.container.AddBar(total,
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{C: decor.DindentRight | decor.DextraSpace}),
		),
		mpb.AppendDecorators(
			decor.CountersKibiByte("% .2f / % .2f"),
			decor.Name(" "),
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", ewmaAge),
			decor.Name(" ETA:"),
			decor.EwmaETA(decor.ET_STYLE_GO, ewmaAge),
		),
	)

	return &barTracker{bar: bar, reporter: reporter}
}

// Logf prints a message above the running bars.
func (reporter *barReporter) Logf(format string, args ...any) {
	message := fmt.Sprintf(format, args...) + "\n"

	if reporter.activeBars.Load() == 0 {
		fmt.Fprint(reporter.out, message)

		return
	}

	_, err := io.WriteString(reporter.container, message)
	if err != nil {
		// The container is already shut down, so write directly instead.
		fmt.Fprint(reporter.out, message)
	}
}

// Debugf is a no-op: debug details are only rendered in debug mode.
func (reporter *barReporter) Debugf(_ string, _ ...any) {}

// Errorf prints an error message above the running bars.
func (reporter *barReporter) Errorf(format string, args ...any) {
	reporter.Logf("error: "+format, args...)
}

// Wait blocks until all bars have completed.
func (reporter *barReporter) Wait() {
	reporter.container.Wait()
}

// barTracker updates a single mpb bar. It is safe for concurrent use so that
// segmented downloads can share one bar across goroutines.
type barTracker struct {
	bar      *mpb.Bar
	reporter *barReporter
	stopOnce sync.Once
	mu       sync.Mutex
	lastTime time.Time
	started  bool
}

// Write implements io.Writer and updates the progress bar.
// Elapsed time is measured between successive Write calls, which reflects
// the real network read rate from the upstream io.Copy.
// The first call initialises the clock to avoid counting connection setup time.
func (tracker *barTracker) Write(data []byte) (int, error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	now := time.Now()

	if !tracker.started {
		tracker.started = true
		tracker.lastTime = now

		tracker.bar.EwmaIncrBy(len(data), time.Millisecond)

		return len(data), nil
	}

	elapsed := now.Sub(tracker.lastTime)
	tracker.lastTime = now

	tracker.bar.EwmaIncrBy(len(data), elapsed)

	return len(data), nil
}

// SetCurrent sets the current progress value (useful for resume).
func (tracker *barTracker) SetCurrent(current int64) {
	tracker.bar.SetCurrent(current)
}

// Finish marks the bar as complete.
func (tracker *barTracker) Finish() {
	tracker.bar.SetTotal(-1, true)
	tracker.release()
}

// Abort terminates the bar so the container's Wait does not block on an
// incomplete bar after a download error.
func (tracker *barTracker) Abort() {
	tracker.bar.Abort(true)
	tracker.release()
}

// release drops the tracker from the reporter's active bar count exactly once.
func (tracker *barTracker) release() {
	tracker.stopOnce.Do(func() {
		tracker.reporter.activeBars.Add(-1)
	})
}
