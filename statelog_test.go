package statelog

import (
	"os"
	"path"
	"runtime"
	"sync"
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

func assertNoPanic(t *testing.T, fn func()) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Test case panicked: %v", r)
		}
	}()

	fn()
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

func TestStatelogConcurrentInitialization(t *testing.T) {
	assertNoPanic(t, func() {
		const numGoroutines = 100
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for range numGoroutines {
			go func() {
				defer wg.Done()
				sl := newTestStatelogDefault()
				sl.ensureDirExists()
				sl.ensureWALFileExists()
			}()
		}

		wg.Wait()

		entries, err := os.ReadDir(".testdata")

		if err != nil {
			t.Fatal(err)
		}

		if len(entries) > 1 {
			t.Errorf("expected .testdata directory to have 1 WAL file, got %d", len(entries))
		}
	})
}
