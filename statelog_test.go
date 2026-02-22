package statelog

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.log")
}

func newTestLog(t *testing.T, opts ...Option) (*StateLog, string) {
	t.Helper()
	path := tempPath(t)
	defaults := []Option{WithCommitInterval(10 * time.Millisecond)}
	s, err := New(path, append(defaults, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, path
}

func collectRecords(t *testing.T, path string) []any {
	t.Helper()
	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	var records []any
	for r.Next() {
		records = append(records, r.Record())
	}
	if err := r.Err(); err != nil {
		t.Fatalf("Reader.Err: %v", err)
	}
	return records
}

func TestAppendAndReplay(t *testing.T) {
	s, path := newTestLog(t)

	for i := 0; i < 10; i++ {
		if err := s.Append(map[string]any{"i": float64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords(t, path)
	if len(records) != 10 {
		t.Fatalf("expected 10 records, got %d", len(records))
	}

	for i, rec := range records {
		m, ok := rec.(map[string]any)
		if !ok {
			t.Fatalf("record %d: expected map, got %T", i, rec)
		}
		if m["i"] != float64(i) {
			t.Errorf("record %d: expected i=%d, got %v", i, i, m["i"])
		}
	}
}

func TestAppendString(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.Append("hello world"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0] != "hello world" {
		t.Errorf("expected 'hello world', got %v", records[0])
	}
}

func TestAppendJSON(t *testing.T) {
	s, path := newTestLog(t)

	data := map[string]any{
		"event":   "user_login",
		"user_id": float64(123),
		"active":  true,
	}
	if err := s.Append(data); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	m := records[0].(map[string]any)
	if m["event"] != "user_login" {
		t.Errorf("expected event=user_login, got %v", m["event"])
	}
	if m["user_id"] != float64(123) {
		t.Errorf("expected user_id=123, got %v", m["user_id"])
	}
	if m["active"] != true {
		t.Errorf("expected active=true, got %v", m["active"])
	}
}

func TestCorruptedRecordSkipped(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.Append("first"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append("second"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append("third"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Corrupt the checksum of the second record.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// File header: 512 bytes.
	// First record: header(9) + "first"(5) = 14 bytes.
	// Second record checksum is at offset 512+14+5 = 531 (bytes 5..9 of entry header).
	offset := int64(defaultFileHeaderSize) + 14 + 5
	var bad [4]byte
	binary.LittleEndian.PutUint32(bad[:], 0xDEADBEEF)
	if _, err := f.WriteAt(bad[:], offset); err != nil {
		t.Fatal(err)
	}
	f.Close()

	records := collectRecords(t, path)
	if len(records) != 2 {
		t.Fatalf("expected 2 records (corrupt one skipped), got %d", len(records))
	}
	if records[0] != "first" {
		t.Errorf("expected 'first', got %v", records[0])
	}
	if records[1] != "third" {
		t.Errorf("expected 'third', got %v", records[1])
	}
}

func TestCloseFlushes(t *testing.T) {
	s, path := newTestLog(t, WithCommitInterval(1*time.Hour))

	for i := 0; i < 5; i++ {
		if err := s.Append(map[string]any{"n": float64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// With 1h interval, records are only on disk after Close.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords(t, path)
	if len(records) != 5 {
		t.Fatalf("expected 5 records after close, got %d", len(records))
	}
}

func TestQueueFull(t *testing.T) {
	s, _ := newTestLog(t, WithMaxQueueSize(2), WithCommitInterval(1*time.Hour))
	defer s.Close()

	if err := s.Append("a"); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := s.Append("b"); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	err := s.Append("c")
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestAppendAfterClose(t *testing.T) {
	s, _ := newTestLog(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := s.Append("late")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestEmptyReplay(t *testing.T) {
	s, path := newTestLog(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if r.Next() {
		t.Fatal("expected no records from empty file")
	}
	if r.Err() != nil {
		t.Fatalf("unexpected error: %v", r.Err())
	}
}

func TestFileRotation(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.Append("before"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Wait for commit.
	time.Sleep(50 * time.Millisecond)

	// Simulate rotation: rename old file, the next commit should create a new one.
	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if err := s.Append("after"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The new file should contain only the "after" record.
	records := collectRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("expected 1 record in new file, got %d", len(records))
	}
	if records[0] != "after" {
		t.Errorf("expected 'after', got %v", records[0])
	}

	// The rotated file should contain "before".
	rotatedRecords := collectRecords(t, rotated)
	if len(rotatedRecords) != 1 {
		t.Fatalf("expected 1 record in rotated file, got %d", len(rotatedRecords))
	}
	if rotatedRecords[0] != "before" {
		t.Errorf("expected 'before', got %v", rotatedRecords[0])
	}
}

func TestFunctionalOptions(t *testing.T) {
	path := tempPath(t)
	s, err := New(path,
		WithCommitInterval(50*time.Millisecond),
		WithMaxQueueSize(500),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if s.cfg.commitInterval != 50*time.Millisecond {
		t.Errorf("expected commitInterval=50ms, got %v", s.cfg.commitInterval)
	}
	if s.cfg.maxQueueSize != 500 {
		t.Errorf("expected maxQueueSize=500, got %d", s.cfg.maxQueueSize)
	}

	s.Close()
}

func TestConcurrentAppends(t *testing.T) {
	s, path := newTestLog(t, WithMaxQueueSize(10000))

	const goroutines = 8
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = s.Append(map[string]any{
					"goroutine": float64(id),
					"seq":       float64(i),
				})
			}
		}(g)
	}
	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords(t, path)
	expected := goroutines * perGoroutine
	if len(records) != expected {
		t.Fatalf("expected %d records, got %d", expected, len(records))
	}
}

func TestMixedTypes(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.Append("a string record"); err != nil {
		t.Fatalf("Append string: %v", err)
	}
	if err := s.Append(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("Append map: %v", err)
	}
	if err := s.Append("another string"); err != nil {
		t.Fatalf("Append string: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords(t, path)
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0] != "a string record" {
		t.Errorf("record 0: got %v", records[0])
	}
	m, ok := records[1].(map[string]any)
	if !ok || m["key"] != "value" {
		t.Errorf("record 1: got %v", records[1])
	}
	if records[2] != "another string" {
		t.Errorf("record 2: got %v", records[2])
	}
}
