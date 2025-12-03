package statelog

import (
	"testing"
)

func TestStatelogNew(t *testing.T) {
	bufferCapacity := 1
	statelog := NewStatelog("test", bufferCapacity)

	if statelog.DirPath == "" {
		t.Errorf("expected statelog.DirPath to be assigned, got %s", statelog.DirPath)
	}

	if statelog.BufferCapacity != bufferCapacity {
		t.Errorf("expected statelog.BufferCapacity value %d to be assigned, got %d", bufferCapacity, statelog.BufferCapacity)
	}
}
