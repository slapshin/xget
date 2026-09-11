package output

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// DefaultStatsInterval is how often debug mode reports transfer stats.
const DefaultStatsInterval = 5 * time.Second

const timestampLayout = "15:04:05.000"

// debugReporter renders plain, timestamped lines instead of progress bars and
// periodically reports the stats of all running transfers.
type debugReporter struct {
	out      io.Writer
	errOut   io.Writer
	interval time.Duration

	// writeMu serialises output so lines from concurrent downloads stay intact.
	writeMu sync.Mutex

	// trackersMu guards trackers. It is kept separate from writeMu so a slow or
	// blocked output writer cannot stall goroutines that start or stop a transfer.
	trackersMu sync.Mutex
	trackers   []*debugTracker

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewDebugReporter creates a Reporter that writes raw lines to out, errors to
// errOut and reports transfer stats every interval. Stats reporting stops when
// ctx is cancelled or Wait is called.
func NewDebugReporter(ctx context.Context, out, errOut io.Writer, interval time.Duration) Reporter {
	if interval <= 0 {
		interval = DefaultStatsInterval
	}

	reporter := &debugReporter{
		out:      out,
		errOut:   errOut,
		interval: interval,
		stop:     make(chan struct{}),
	}

	reporter.wg.Add(1)

	go reporter.reportLoop(ctx)

	return reporter
}

// NewTracker registers a transfer whose stats are reported periodically.
func (reporter *debugReporter) NewTracker(total int64, name string) Tracker {
	tracker := &debugTracker{
		reporter:  reporter,
		name:      name,
		total:     total,
		startedAt: time.Now(),
		lastAt:    time.Now(),
	}

	reporter.trackersMu.Lock()
	defer reporter.trackersMu.Unlock()

	reporter.trackers = append(reporter.trackers, tracker)

	return tracker
}

// Logf writes a timestamped message.
func (reporter *debugReporter) Logf(format string, args ...any) {
	reporter.write(reporter.out, fmt.Sprintf(format, args...))
}

// Debugf writes a timestamped debug message. Debug mode shows every message,
// so it is the same as Logf here.
func (reporter *debugReporter) Debugf(format string, args ...any) {
	reporter.Logf(format, args...)
}

// Errorf writes a timestamped error message to the error stream.
func (reporter *debugReporter) Errorf(format string, args ...any) {
	reporter.write(reporter.errOut, "error: "+fmt.Sprintf(format, args...))
}

// Wait stops the stats reporting loop.
func (reporter *debugReporter) Wait() {
	reporter.stopOnce.Do(func() {
		close(reporter.stop)
	})

	reporter.wg.Wait()
}

// write emits a single timestamped line.
func (reporter *debugReporter) write(dest io.Writer, message string) {
	reporter.writeMu.Lock()
	defer reporter.writeMu.Unlock()

	fmt.Fprintf(dest, "[%s] %s\n", time.Now().Format(timestampLayout), message)
}

// reportLoop periodically reports the stats of all running transfers.
func (reporter *debugReporter) reportLoop(ctx context.Context) {
	defer reporter.wg.Done()

	ticker := time.NewTicker(reporter.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-reporter.stop:
			return
		case <-ticker.C:
			reporter.reportStats()
		}
	}
}

// reportStats writes one stats line per running transfer, plus a totals line
// when more than one transfer is running.
func (reporter *debugReporter) reportStats() {
	now := time.Now()

	trackers := reporter.activeTrackers()
	lines := make([]string, 0, len(trackers)+1)

	var (
		totalSpeed float64
		doneBytes  int64
		wantBytes  int64
	)

	for _, tracker := range trackers {
		stats := tracker.snapshot(now)

		lines = append(lines, "  "+stats.line())

		totalSpeed += stats.speed
		doneBytes += stats.current
		wantBytes += stats.total
	}

	if len(lines) == 0 {
		return
	}

	if len(lines) > 1 {
		lines = append(lines, fmt.Sprintf("  total: %s / %s at %s/s",
			humanBytes(doneBytes), humanBytes(wantBytes), humanBytes(int64(totalSpeed))))
	}

	reporter.Logf("stats:\n%s", strings.Join(lines, "\n"))
}

// activeTrackers returns a snapshot of the currently registered trackers.
// Stats are best-effort: a transfer that stops right after the snapshot is
// taken still contributes one last line, which is intentional.
func (reporter *debugReporter) activeTrackers() []*debugTracker {
	reporter.trackersMu.Lock()
	defer reporter.trackersMu.Unlock()

	trackers := make([]*debugTracker, len(reporter.trackers))
	copy(trackers, reporter.trackers)

	return trackers
}

// remove unregisters a finished or aborted tracker.
func (reporter *debugReporter) remove(tracker *debugTracker) {
	reporter.trackersMu.Lock()
	defer reporter.trackersMu.Unlock()

	for i, registered := range reporter.trackers {
		if registered == tracker {
			reporter.trackers = append(reporter.trackers[:i], reporter.trackers[i+1:]...)

			return
		}
	}
}
