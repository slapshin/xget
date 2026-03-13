package segment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State tracks the progress of a segmented download.
type State struct {
	TotalSize    int64     `json:"total_size"`
	SegmentCount int       `json:"segment_count"`
	Segments     []Segment `json:"segments"`
}

// Segment represents a single byte range within a segmented download.
type Segment struct {
	Index int   `json:"index"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  bool  `json:"done"`
}

// NewState creates a new State by dividing totalSize into count equal segments.
func NewState(totalSize int64, count int) *State {
	segments := make([]Segment, count)
	segSize := totalSize / int64(count)

	for i := range count {
		segments[i] = Segment{
			Index: i,
			Start: int64(i) * segSize,
			End:   int64(i+1)*segSize - 1,
		}
	}

	// Last segment absorbs remainder.
	segments[count-1].End = totalSize - 1

	return &State{
		TotalSize:    totalSize,
		SegmentCount: count,
		Segments:     segments,
	}
}

// CompletedBytes returns the total number of bytes in completed segments.
func (state *State) CompletedBytes() int64 {
	var total int64

	for i := range state.Segments {
		if state.Segments[i].Done {
			total += state.Segments[i].End - state.Segments[i].Start + 1
		}
	}

	return total
}

// LoadState reads segment state from a JSON file.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state State

	err = json.Unmarshal(data, &state)
	if err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &state, nil
}

// SaveState writes segment state to a JSON file atomically.
func SaveState(path string, state *State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	tmpPath := path + ".tmp"

	err = os.WriteFile(tmpPath, data, 0o600)
	if err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	err = os.Rename(tmpPath, path)
	if err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// StatePath returns the segment state file path for a given partial file path.
func StatePath(partialPath string) string {
	return filepath.Clean(partialPath) + ".segments"
}
