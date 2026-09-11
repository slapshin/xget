package output

import (
	"fmt"
	"sync"
	"time"
)

// debugTracker counts transferred bytes so the reporter can print stats and a
// completion line for one transfer.
type debugTracker struct {
	reporter *debugReporter
	name     string
	total    int64

	mu sync.Mutex
	// current is the number of bytes already on disk, including bytes carried
	// over from a resumed transfer.
	current int64
	// resumed is the byte count the transfer started from, excluded from the
	// average speed so resumed bytes are not counted as transferred now.
	resumed   int64
	startedAt time.Time
	// lastAt and lastBytes hold the previous stats report, used to compute the
	// current speed.
	lastAt    time.Time
	lastBytes int64
	stopped   bool
}

// Write implements io.Writer and accumulates the transferred bytes.
func (tracker *debugTracker) Write(data []byte) (int, error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	tracker.current += int64(len(data))

	return len(data), nil
}

// SetCurrent sets the already transferred byte count on resume.
func (tracker *debugTracker) SetCurrent(current int64) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	tracker.current = current
	tracker.resumed = current
	tracker.lastBytes = current
}

// Finish reports the completed transfer and stops its stats reporting.
func (tracker *debugTracker) Finish() {
	stats, ok := tracker.stop(time.Now())
	if !ok {
		return
	}

	if stats.transferred == stats.current {
		tracker.reporter.Logf("completed %s: %s in %s (avg %s/s)",
			tracker.name,
			humanBytes(stats.current),
			stats.elapsed.Round(time.Millisecond),
			humanBytes(int64(stats.averageSpeed)),
		)

		return
	}

	// A resumed transfer only moved part of the file in this run.
	tracker.reporter.Logf("completed %s: %s (%s this run) in %s (avg %s/s)",
		tracker.name,
		humanBytes(stats.current),
		humanBytes(stats.transferred),
		stats.elapsed.Round(time.Millisecond),
		humanBytes(int64(stats.averageSpeed)),
	)
}

// Abort reports an interrupted transfer and stops its stats reporting.
// It is a no-op once the transfer has finished.
func (tracker *debugTracker) Abort() {
	stats, ok := tracker.stop(time.Now())
	if !ok {
		return
	}

	tracker.reporter.Logf("aborted %s: %s transferred", tracker.name, humanBytes(stats.transferred))
}

// stop unregisters the tracker and returns its final stats. The second result
// is false when the tracker has already been stopped.
func (tracker *debugTracker) stop(now time.Time) (trackerStats, bool) {
	tracker.mu.Lock()

	if tracker.stopped {
		tracker.mu.Unlock()

		return trackerStats{}, false
	}

	tracker.stopped = true
	stats := tracker.statsLocked(now)

	tracker.mu.Unlock()

	tracker.reporter.remove(tracker)

	return stats, true
}

// snapshot returns the stats since the previous report and resets the window.
func (tracker *debugTracker) snapshot(now time.Time) trackerStats {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	stats := tracker.statsLocked(now)

	tracker.lastAt = now
	tracker.lastBytes = tracker.current

	return stats
}

// statsLocked computes the stats for now. The caller must hold the mutex.
func (tracker *debugTracker) statsLocked(now time.Time) trackerStats {
	stats := trackerStats{
		name:        tracker.name,
		current:     tracker.current,
		transferred: tracker.current - tracker.resumed,
		total:       tracker.total,
		elapsed:     now.Sub(tracker.startedAt),
	}

	window := now.Sub(tracker.lastAt)
	if window > 0 {
		stats.speed = float64(tracker.current-tracker.lastBytes) / window.Seconds()
	}

	if stats.elapsed > 0 {
		stats.averageSpeed = float64(stats.transferred) / stats.elapsed.Seconds()
	}

	return stats
}

// trackerStats is a point-in-time view of one transfer.
type trackerStats struct {
	name string
	// current is everything on disk, transferred only what this run moved.
	current      int64
	transferred  int64
	total        int64
	elapsed      time.Duration
	speed        float64
	averageSpeed float64
}

// line renders the stats as a single report line.
func (stats trackerStats) line() string {
	if stats.total <= 0 {
		return fmt.Sprintf("%s: %s (size unknown) at %s/s",
			stats.name, humanBytes(stats.current), humanBytes(int64(stats.speed)))
	}

	percent := float64(stats.current) / float64(stats.total) * 100

	return fmt.Sprintf("%s: %s / %s (%.1f%%) at %s/s eta %s",
		stats.name,
		humanBytes(stats.current),
		humanBytes(stats.total),
		percent,
		humanBytes(int64(stats.speed)),
		stats.eta(),
	)
}

// eta estimates the remaining time from the current speed.
func (stats trackerStats) eta() string {
	remaining := stats.total - stats.current
	if remaining <= 0 {
		return "0s"
	}

	if stats.speed <= 0 {
		return "unknown"
	}

	seconds := float64(remaining) / stats.speed

	return (time.Duration(seconds * float64(time.Second))).Round(time.Second).String()
}
