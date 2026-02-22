package statelog

import (
	"errors"
	"fmt"
)

var (
	// ErrClosed is returned when Append is called on a closed StateLog.
	ErrClosed = errors.New("statelog: log is closed")

	// ErrQueueFull is returned when the write queue has reached capacity.
	ErrQueueFull = errors.New("statelog: write queue is full")

	// ErrMetadataOverflow is returned when metadata exceeds the reserved header space.
	ErrMetadataOverflow = errors.New("statelog: metadata exceeds reserved header space")
)

// CorruptionError indicates a corrupt record was detected during reading.
type CorruptionError struct {
	FilePath string
	Offset   int64
	Reason   string
	Err      error
}

func (e *CorruptionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("statelog: corruption at offset %d in %s: %s: %v", e.Offset, e.FilePath, e.Reason, e.Err)
	}
	return fmt.Sprintf("statelog: corruption at offset %d in %s: %s", e.Offset, e.FilePath, e.Reason)
}

func (e *CorruptionError) Unwrap() error { return e.Err }

// IOError wraps a low-level OS error during a file operation.
type IOError struct {
	FilePath string
	Message  string
	Err      error
}

func (e *IOError) Error() string {
	return fmt.Sprintf("statelog: %s (%s): %v", e.Message, e.FilePath, e.Err)
}

func (e *IOError) Unwrap() error { return e.Err }
