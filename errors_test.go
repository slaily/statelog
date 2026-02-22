package statelog

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestErrClosed(t *testing.T) {
	if ErrClosed.Error() != "statelog: log is closed" {
		t.Errorf("unexpected message: %s", ErrClosed.Error())
	}

	err := fmt.Errorf("wrapped: %w", ErrClosed)
	if !errors.Is(err, ErrClosed) {
		t.Error("expected errors.Is to find ErrClosed in chain")
	}
}

func TestErrQueueFull(t *testing.T) {
	if ErrQueueFull.Error() != "statelog: write queue is full" {
		t.Errorf("unexpected message: %s", ErrQueueFull.Error())
	}

	err := fmt.Errorf("wrapped: %w", ErrQueueFull)
	if !errors.Is(err, ErrQueueFull) {
		t.Error("expected errors.Is to find ErrQueueFull in chain")
	}
}

func TestCorruptionError_Error(t *testing.T) {
	underlying := errors.New("bad checksum")

	tests := []struct {
		name     string
		err      *CorruptionError
		contains []string
	}{
		{
			name: "with underlying error",
			err: &CorruptionError{
				FilePath: "/var/data/log.bin",
				Offset:   1024,
				Reason:   "header mismatch",
				Err:      underlying,
			},
			contains: []string{
				"corruption",
				"offset 1024",
				"/var/data/log.bin",
				"header mismatch",
				"bad checksum",
			},
		},
		{
			name: "without underlying error",
			err: &CorruptionError{
				FilePath: "test.log",
				Offset:   0,
				Reason:   "truncated record",
			},
			contains: []string{
				"corruption",
				"offset 0",
				"test.log",
				"truncated record",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.contains {
				if !strings.Contains(msg, want) {
					t.Errorf("expected %q in error message, got: %s", want, msg)
				}
			}
		})
	}
}

func TestCorruptionError_ErrorExcludesWrappedWhenNil(t *testing.T) {
	err := &CorruptionError{
		FilePath: "test.log",
		Offset:   42,
		Reason:   "short read",
	}

	msg := err.Error()

	// The nil-Err branch should not contain a trailing ": <nil>" artifact.
	if strings.Contains(msg, "<nil>") {
		t.Errorf("nil Err should not appear in message, got: %s", msg)
	}
}

func TestCorruptionError_Unwrap(t *testing.T) {
	underlying := io.ErrUnexpectedEOF

	err := &CorruptionError{
		FilePath: "data.bin",
		Offset:   256,
		Reason:   "incomplete frame",
		Err:      underlying,
	}

	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Error("errors.Is should find io.ErrUnexpectedEOF through Unwrap")
	}

	var target *CorruptionError
	if !errors.As(err, &target) {
		t.Fatal("errors.As should match *CorruptionError")
	}
	if target.Offset != 256 {
		t.Errorf("expected Offset 256, got %d", target.Offset)
	}
}

func TestCorruptionError_UnwrapNil(t *testing.T) {
	err := &CorruptionError{Reason: "no cause"}

	if err.Unwrap() != nil {
		t.Errorf("expected nil from Unwrap, got %v", err.Unwrap())
	}
}

func TestIOError_Error(t *testing.T) {
	underlying := errors.New("permission denied")

	err := &IOError{
		FilePath: "/var/log/state.db",
		Message:  "failed to sync",
		Err:      underlying,
	}

	msg := err.Error()
	for _, want := range []string{"failed to sync", "/var/log/state.db", "permission denied"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected %q in error message, got: %s", want, msg)
		}
	}
}

func TestIOError_Unwrap(t *testing.T) {
	underlying := io.ErrClosedPipe

	err := &IOError{
		FilePath: "state.db",
		Message:  "write failed",
		Err:      underlying,
	}

	if !errors.Is(err, io.ErrClosedPipe) {
		t.Error("errors.Is should find io.ErrClosedPipe through Unwrap")
	}

	var target *IOError
	if !errors.As(err, &target) {
		t.Fatal("errors.As should match *IOError")
	}
	if target.FilePath != "state.db" {
		t.Errorf("expected FilePath 'state.db', got %q", target.FilePath)
	}
}

func TestIOError_UnwrapNil(t *testing.T) {
	err := &IOError{Message: "no cause"}

	if err.Unwrap() != nil {
		t.Errorf("expected nil from Unwrap, got %v", err.Unwrap())
	}
}
