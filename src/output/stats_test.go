package output

import (
	"testing"
	"time"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 B"},
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "kibibytes", bytes: 2048, want: "2.00 KiB"},
		{name: "mebibytes", bytes: 5 * 1024 * 1024, want: "5.00 MiB"},
		{name: "gibibytes", bytes: 3 * 1024 * 1024 * 1024, want: "3.00 GiB"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := humanBytes(test.bytes)
			if got != test.want {
				t.Errorf("humanBytes(%d) = %q, want %q", test.bytes, got, test.want)
			}
		})
	}
}

func TestTrackerStatsLine(t *testing.T) {
	tests := []struct {
		name  string
		stats trackerStats
		want  string
	}{
		{
			name: "known size",
			stats: trackerStats{
				name:    "file.bin",
				current: 512,
				total:   1024,
				speed:   256,
			},
			want: "file.bin: 512 B / 1.00 KiB (50.0%) at 256 B/s eta 2s",
		},
		{
			name: "unknown size",
			stats: trackerStats{
				name:    "file.bin",
				current: 512,
				speed:   128,
			},
			want: "file.bin: 512 B (size unknown) at 128 B/s",
		},
		{
			name: "stalled transfer",
			stats: trackerStats{
				name:    "file.bin",
				current: 512,
				total:   1024,
			},
			want: "file.bin: 512 B / 1.00 KiB (50.0%) at 0 B/s eta unknown",
		},
		{
			name: "complete transfer",
			stats: trackerStats{
				name:    "file.bin",
				current: 1024,
				total:   1024,
				speed:   256,
			},
			want: "file.bin: 1.00 KiB / 1.00 KiB (100.0%) at 256 B/s eta 0s",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.stats.line()
			if got != test.want {
				t.Errorf("line() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTrackerStatsSpeedWindow(t *testing.T) {
	start := time.Now()

	tracker := &debugTracker{
		reporter:  &debugReporter{},
		name:      "file.bin",
		total:     1000,
		startedAt: start,
		lastAt:    start,
	}

	_, err := tracker.Write(make([]byte, 500))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	stats := tracker.snapshot(start.Add(time.Second))
	if stats.speed != 500 {
		t.Errorf("speed = %f, want 500", stats.speed)
	}

	// The next window only counts the bytes written since the last snapshot.
	_, err = tracker.Write(make([]byte, 100))
	if err != nil {
		t.Fatalf("writing to tracker: %v", err)
	}

	stats = tracker.snapshot(start.Add(2 * time.Second))
	if stats.speed != 100 {
		t.Errorf("speed = %f, want 100", stats.speed)
	}
}
