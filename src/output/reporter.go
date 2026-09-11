// Package output renders download progress, stats and messages. It provides
// an interactive implementation backed by progress bars and a debug
// implementation that emits plain lines with periodic transfer stats.
package output

import (
	"fmt"
	"io"
)

// Tracker records the progress of a single transfer.
type Tracker interface {
	io.Writer

	// SetCurrent sets the already transferred byte count, used on resume.
	SetCurrent(current int64)

	// Finish marks the transfer as successfully completed.
	Finish()

	// Abort terminates the tracker after a failed or cancelled transfer.
	// It is a no-op once the transfer has finished, so it is safe to defer
	// right after creation.
	Abort()
}

// Reporter creates trackers and renders messages for the user.
type Reporter interface {
	// NewTracker creates a tracker for a transfer of total bytes.
	// A total of zero or less means the size is unknown.
	NewTracker(total int64, name string) Tracker

	// Logf renders a message that is always shown.
	Logf(format string, args ...any)

	// Debugf renders a message that is only shown in debug mode.
	Debugf(format string, args ...any)

	// Errorf renders an error message.
	Errorf(format string, args ...any)

	// Wait blocks until all rendering has completed.
	Wait()
}

const (
	bytesInUnit = 1024
	unitSuffix  = "KMGTPE"
)

// humanBytes formats a byte count using binary units.
func humanBytes(bytes int64) string {
	if bytes < bytesInUnit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(bytesInUnit), 0

	for n := bytes / bytesInUnit; n >= bytesInUnit && exp < len(unitSuffix)-1; n /= bytesInUnit {
		div *= bytesInUnit
		exp++
	}

	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(div), unitSuffix[exp])
}
