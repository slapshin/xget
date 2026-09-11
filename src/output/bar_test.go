package output

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A container without bars discards what is written to it, so messages must go
// straight to the output writer while no transfer is running.
func TestBarReporterLogsWithoutActiveBars(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewBarReporter(context.Background(), out)
	reporter.Logf("skipping %s", "file.bin")
	reporter.Wait()

	if !strings.Contains(out.String(), "skipping file.bin") {
		t.Errorf("output = %q, want it to contain the message", out.String())
	}
}

func TestBarReporterDebugfIsSilent(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewBarReporter(context.Background(), out)
	reporter.Debugf("source for %s", "file.bin")
	reporter.Wait()

	if out.String() != "" {
		t.Errorf("output = %q, want no debug output in bar mode", out.String())
	}
}

// A finished tracker must release its slot so later messages are written
// directly again instead of being swallowed by the shut down container.
func TestBarReporterLogsAfterTrackerFinished(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewBarReporter(context.Background(), out)

	tracker := reporter.NewTracker(10, "file.bin")

	_, err := tracker.Write(make([]byte, 10))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	tracker.Finish()
	tracker.Abort()

	reporter.Logf("skipping %s", "other.bin")
	reporter.Wait()

	if !strings.Contains(out.String(), "skipping other.bin") {
		t.Errorf("output = %q, want it to contain the message", out.String())
	}
}

// An aborted tracker must not block the container's Wait.
func TestBarReporterWaitReturnsAfterAbort(t *testing.T) {
	out := &syncBuffer{}

	reporter := NewBarReporter(context.Background(), out)

	tracker := reporter.NewTracker(100, "file.bin")
	tracker.Abort()

	done := make(chan struct{})

	go func() {
		reporter.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() did not return after the only tracker was aborted")
	}
}
