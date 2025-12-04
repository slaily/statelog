package statelog

import (
	"path"
	"runtime"
	"testing"
)

func newTestStatelogDefault() Statelog {
	_, filePath, _, _ := runtime.Caller(0)
	rootDirPath := path.Join(path.Dir(filePath))
	testDirPath := path.Join(rootDirPath, ".testdata")
	bufferCapacity := 10
	sl := NewStatelog(testDirPath, bufferCapacity)

	return sl
}
func TestStatelogNew(t *testing.T) {
	sl := newTestStatelogDefault()

	if sl.DirPath == "" {
		t.Errorf("expected sl.DirPath to be assigned, got %s", sl.DirPath)
	}

	if sl.BufferCapacity == 0 {
		t.Error("expected sl.BufferCapacity value to be not 0")
	}
}

func TestStatelogDirCreated(t *testing.T) {
	sl := newTestStatelogDefault()
	err := sl.ensureDirExists()

	if err != nil {
		t.Error(err)
	}
}

func TestStatelogWALFileCreated(t *testing.T) {
	sl := newTestStatelogDefault()
	err := sl.ensureWALFileExists()

	if err != nil {
		t.Error(err)
	}
}
