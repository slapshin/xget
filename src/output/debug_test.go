package output

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a concurrency-safe buffer, needed because the debug reporter
// writes stats from its own goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (buffer *syncBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.buf.Write(data)
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	return buffer.buf.String()
}

func TestDebugReporterMessages(t *testing.T) {
	tests := []struct {
		name     string
		report   func(reporter Reporter)
		wantOut  string
		wantErr  string
		notWant  string
		inStderr bool
	}{
		{
			name:    "logf goes to stdout",
			report:  func(reporter Reporter) { reporter.Logf("hello %s", "world") },
			wantOut: "hello world",
		},
		{
			name:    "debugf goes to stdout",
			report:  func(reporter Reporter) { reporter.Debugf("source %s", "http://example") },
			wantOut: "source http://example",
		},
		{
			name:     "errorf goes to stderr",
			report:   func(reporter Reporter) { reporter.Errorf("boom: %v", "reset") },
			wantErr:  "error: boom: reset",
			inStderr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := &syncBuffer{}
			errOut := &syncBuffer{}

			reporter := NewDebugReporter(context.Background(), out, errOut, time.Hour)
			test.report(reporter)
			reporter.Wait()

			if test.inStderr {
				if !strings.Contains(errOut.String(), test.wantErr) {
					t.Errorf("stderr = %q, want it to contain %q", errOut.String(), test.wantErr)
				}

				return
			}

			if !strings.Contains(out.String(), test.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), test.wantOut)
			}
		})
	}
}

func TestDebugReporterReportsStats(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewDebugReporter(context.Background(), out, out, 20*time.Millisecond)
	defer reporter.Wait()

	tracker := reporter.NewTracker(1000, "file.bin")

	_, err := tracker.Write(make([]byte, 400))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	waitFor(t, func() bool {
		return strings.Contains(out.String(), "file.bin: 400 B / 1000 B (40.0%)")
	}, "stats line for a running transfer")
}

func TestDebugTrackerFinishReportsCompletion(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewDebugReporter(context.Background(), out, out, time.Hour)
	defer reporter.Wait()

	tracker := reporter.NewTracker(100, "file.bin")

	_, err := tracker.Write(make([]byte, 100))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	tracker.Finish()
	// Abort always runs from a defer; it must not report a finished transfer.
	tracker.Abort()

	if !strings.Contains(out.String(), "completed file.bin: 100 B") {
		t.Errorf("output = %q, want it to contain the completion line", out.String())
	}

	if strings.Contains(out.String(), "aborted") {
		t.Errorf("output = %q, want no abort line after Finish", out.String())
	}
}

func TestDebugTrackerAbortReportsInterruption(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewDebugReporter(context.Background(), out, out, time.Hour)
	defer reporter.Wait()

	tracker := reporter.NewTracker(100, "file.bin")

	_, err := tracker.Write(make([]byte, 40))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	tracker.Abort()

	if !strings.Contains(out.String(), "aborted file.bin: 40 B transferred") {
		t.Errorf("output = %q, want it to contain the abort line", out.String())
	}
}

// A stopped tracker must no longer appear in the periodic stats.
func TestDebugReporterDropsStoppedTrackers(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewDebugReporter(context.Background(), out, out, 20*time.Millisecond)
	defer reporter.Wait()

	tracker := reporter.NewTracker(100, "file.bin")
	tracker.Finish()

	time.Sleep(60 * time.Millisecond)

	if strings.Contains(out.String(), "stats:") {
		t.Errorf("output = %q, want no stats for a stopped tracker", out.String())
	}
}

func TestDebugTrackerSetCurrentExcludesResumedBytes(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewDebugReporter(context.Background(), out, out, time.Hour)
	defer reporter.Wait()

	tracker := reporter.NewTracker(1000, "file.bin")
	tracker.SetCurrent(600)

	_, err := tracker.Write(make([]byte, 400))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	debug, ok := tracker.(*debugTracker)
	if !ok {
		t.Fatalf("tracker type = %T, want *debugTracker", tracker)
	}

	stats := debug.snapshot(time.Now())

	if stats.current != 1000 {
		t.Errorf("current = %d, want 1000", stats.current)
	}

	// Only the 400 freshly transferred bytes count as moved by this run.
	if stats.transferred != 400 {
		t.Errorf("transferred = %d, want 400", stats.transferred)
	}
}

// A resumed transfer must report the bytes it moved separately from the file size.
func TestDebugTrackerFinishReportsResumedTransfer(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewDebugReporter(context.Background(), out, out, time.Hour)
	defer reporter.Wait()

	tracker := reporter.NewTracker(100, "file.bin")
	tracker.SetCurrent(60)

	_, err := tracker.Write(make([]byte, 40))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	tracker.Finish()

	if !strings.Contains(out.String(), "completed file.bin: 100 B (40 B this run)") {
		t.Errorf("output = %q, want the completion line to report the resumed transfer", out.String())
	}
}

// Cancelling the context must stop the stats loop, so it cannot outlive a
// cancelled download run.
func TestDebugReporterStopsOnContextCancel(t *testing.T) {
	out := &syncBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reporter := NewDebugReporter(ctx, out, out, 10*time.Millisecond)
	defer reporter.Wait()

	tracker := reporter.NewTracker(1000, "file.bin")

	_, err := tracker.Write(make([]byte, 400))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	waitFor(t, func() bool {
		return strings.Contains(out.String(), "stats:")
	}, "the first stats line")

	cancel()

	time.Sleep(50 * time.Millisecond)

	reported := strings.Count(out.String(), "stats:")

	time.Sleep(50 * time.Millisecond)

	if strings.Count(out.String(), "stats:") != reported {
		t.Errorf("stats kept being reported after the context was cancelled")
	}
}

// waitFor polls condition until it holds or the deadline expires.
func waitFor(t *testing.T, condition func() bool, description string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if condition() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", description)
}
