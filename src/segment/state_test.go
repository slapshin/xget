package segment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewState(t *testing.T) {
	tests := []struct {
		name      string
		totalSize int64
		count     int
		wantSegs  []Segment
	}{
		{
			name:      "even split",
			totalSize: 100,
			count:     4,
			wantSegs: []Segment{
				{Index: 0, Start: 0, End: 24},
				{Index: 1, Start: 25, End: 49},
				{Index: 2, Start: 50, End: 74},
				{Index: 3, Start: 75, End: 99},
			},
		},
		{
			name:      "uneven split with remainder",
			totalSize: 103,
			count:     4,
			wantSegs: []Segment{
				{Index: 0, Start: 0, End: 24},
				{Index: 1, Start: 25, End: 49},
				{Index: 2, Start: 50, End: 74},
				{Index: 3, Start: 75, End: 102},
			},
		},
		{
			name:      "single segment",
			totalSize: 500,
			count:     1,
			wantSegs: []Segment{
				{Index: 0, Start: 0, End: 499},
			},
		},
		{
			name:      "two segments",
			totalSize: 1000,
			count:     2,
			wantSegs: []Segment{
				{Index: 0, Start: 0, End: 499},
				{Index: 1, Start: 500, End: 999},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewState(tt.totalSize, tt.count)

			if state.TotalSize != tt.totalSize {
				t.Errorf("TotalSize = %d, want %d", state.TotalSize, tt.totalSize)
			}

			if state.SegmentCount != tt.count {
				t.Errorf("SegmentCount = %d, want %d", state.SegmentCount, tt.count)
			}

			assertSegments(t, state.Segments, tt.wantSegs)
			assertContiguousCoverage(t, state.Segments, tt.totalSize)
		})
	}
}

func assertSegments(t *testing.T, got, want []Segment) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d segments, want %d", len(got), len(want))
	}

	for i, seg := range got {
		exp := want[i]

		if seg.Index != exp.Index || seg.Start != exp.Start || seg.End != exp.End {
			t.Errorf("segment[%d] = {Start:%d End:%d}, want {Start:%d End:%d}",
				i, seg.Start, seg.End, exp.Start, exp.End)
		}

		if seg.Done {
			t.Errorf("segment[%d] should not be done", i)
		}
	}
}

func assertContiguousCoverage(t *testing.T, segments []Segment, totalSize int64) {
	t.Helper()

	for i := 1; i < len(segments); i++ {
		if segments[i].Start != segments[i-1].End+1 {
			t.Errorf("gap or overlap between segment %d (end=%d) and %d (start=%d)",
				i-1, segments[i-1].End, i, segments[i].Start)
		}
	}

	lastSeg := segments[len(segments)-1]
	if lastSeg.End != totalSize-1 {
		t.Errorf("last segment end = %d, want %d", lastSeg.End, totalSize-1)
	}
}

func TestCompletedBytes(t *testing.T) {
	state := NewState(100, 4)

	if got := state.CompletedBytes(); got != 0 {
		t.Errorf("CompletedBytes() = %d, want 0", got)
	}

	state.Segments[0].Done = true
	state.Segments[2].Done = true

	// Segments 0 and 2: [0-24] and [50-74] = 25 + 25 = 50.
	if got := state.CompletedBytes(); got != 50 {
		t.Errorf("CompletedBytes() = %d, want 50", got)
	}
}

func TestSaveLoadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.segments")

	original := NewState(1000, 4)
	original.Segments[0].Done = true
	original.Segments[2].Done = true

	err := SaveState(path, original)
	if err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.TotalSize != original.TotalSize {
		t.Errorf("TotalSize = %d, want %d", loaded.TotalSize, original.TotalSize)
	}

	if loaded.SegmentCount != original.SegmentCount {
		t.Errorf("SegmentCount = %d, want %d", loaded.SegmentCount, original.SegmentCount)
	}

	for i, seg := range loaded.Segments {
		orig := original.Segments[i]

		if seg.Start != orig.Start || seg.End != orig.End || seg.Done != orig.Done {
			t.Errorf("segment[%d] mismatch: got %+v, want %+v", i, seg, orig)
		}
	}
}

func TestLoadStateCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.segments")

	err := os.WriteFile(path, []byte("not json"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = LoadState(path)
	if err == nil {
		t.Error("expected error for corrupt state file")
	}
}

func TestLoadStateNotFound(t *testing.T) {
	_, err := LoadState("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing state file")
	}
}

func TestStatePath(t *testing.T) {
	got := StatePath("/tmp/file.partial")
	want := "/tmp/file.partial.segments"

	if got != want {
		t.Errorf("StatePath = %q, want %q", got, want)
	}
}
