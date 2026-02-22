package statelog

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testRecord struct {
	Name  string `sl:"32"`
	Value string `sl:"32"`
	Seq   uint64
}

type simpleRecord struct {
	Msg string `sl:"64"`
}

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.log")
}

func newTestLog(t *testing.T, opts ...Option) (*StateLog[testRecord], string) {
	t.Helper()
	path := tempPath(t)
	defaults := []Option{WithCommitInterval(10 * time.Millisecond)}
	s, err := New[testRecord](path, append(defaults, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, path
}

func collectRecords[T any](t *testing.T, path string) []T {
	t.Helper()
	r, err := NewReader[T](path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	var records []T
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
		if err := s.Append(testRecord{Name: "i", Value: "v", Seq: uint64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords[testRecord](t, path)
	if len(records) != 10 {
		t.Fatalf("expected 10 records, got %d", len(records))
	}

	for i, rec := range records {
		if rec.Seq != uint64(i) {
			t.Errorf("record %d: expected Seq=%d, got %d", i, i, rec.Seq)
		}
	}
}

func TestAppendSimpleRecord(t *testing.T) {
	path := tempPath(t)
	s, err := New[simpleRecord](path, WithCommitInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Append(simpleRecord{Msg: "hello world"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords[simpleRecord](t, path)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Msg != "hello world" {
		t.Errorf("expected 'hello world', got %q", records[0].Msg)
	}
}

func TestAppendMultiField(t *testing.T) {
	s, path := newTestLog(t)

	data := testRecord{
		Name:  "user_login",
		Value: "user-123",
		Seq:   42,
	}
	if err := s.Append(data); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords[testRecord](t, path)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	rec := records[0]
	if rec.Name != "user_login" {
		t.Errorf("expected Name=user_login, got %q", rec.Name)
	}
	if rec.Value != "user-123" {
		t.Errorf("expected Value=user-123, got %q", rec.Value)
	}
	if rec.Seq != 42 {
		t.Errorf("expected Seq=42, got %d", rec.Seq)
	}
}

func TestCorruptedRecordSkipped(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.Append(testRecord{Name: "first", Seq: 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append(testRecord{Name: "second", Seq: 2}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append(testRecord{Name: "third", Seq: 3}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Corrupt the CRC32 of the second record.
	schema, _ := buildSchema[testRecord]()
	secondRecordOffset := int64(defaultFileHeaderSize) + int64(schema.RecordSize)
	// CRC32 is at DataSize offset within the record.
	crcOffset := secondRecordOffset + int64(schema.DataSize)

	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, crcOffset); err != nil {
		t.Fatal(err)
	}
	f.Close()

	records := collectRecords[testRecord](t, path)
	if len(records) != 2 {
		t.Fatalf("expected 2 records (corrupt one skipped), got %d", len(records))
	}
	if records[0].Name != "first" {
		t.Errorf("expected 'first', got %q", records[0].Name)
	}
	if records[1].Name != "third" {
		t.Errorf("expected 'third', got %q", records[1].Name)
	}
}

func TestCloseFlushes(t *testing.T) {
	s, path := newTestLog(t, WithCommitInterval(1*time.Hour))

	for i := 0; i < 5; i++ {
		if err := s.Append(testRecord{Name: "n", Seq: uint64(i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// With 1h interval, records are only on disk after Close.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords[testRecord](t, path)
	if len(records) != 5 {
		t.Fatalf("expected 5 records after close, got %d", len(records))
	}
}

func TestQueueFull(t *testing.T) {
	s, _ := newTestLog(t, WithMaxQueueSize(2), WithCommitInterval(1*time.Hour))
	defer s.Close()

	if err := s.Append(testRecord{Name: "a"}); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := s.Append(testRecord{Name: "b"}); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	err := s.Append(testRecord{Name: "c"})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestAppendAfterClose(t *testing.T) {
	s, _ := newTestLog(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := s.Append(testRecord{Name: "late"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestEmptyReplay(t *testing.T) {
	s, path := newTestLog(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := NewReader[testRecord](path)
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

	if err := s.Append(testRecord{Name: "before"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Wait for commit.
	time.Sleep(50 * time.Millisecond)

	// Simulate rotation: rename old file, the next commit should create a new one.
	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if err := s.Append(testRecord{Name: "after"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The new file should contain only the "after" record.
	records := collectRecords[testRecord](t, path)
	if len(records) != 1 {
		t.Fatalf("expected 1 record in new file, got %d", len(records))
	}
	if records[0].Name != "after" {
		t.Errorf("expected 'after', got %q", records[0].Name)
	}

	// The rotated file should contain "before".
	rotatedRecords := collectRecords[testRecord](t, rotated)
	if len(rotatedRecords) != 1 {
		t.Fatalf("expected 1 record in rotated file, got %d", len(rotatedRecords))
	}
	if rotatedRecords[0].Name != "before" {
		t.Errorf("expected 'before', got %q", rotatedRecords[0].Name)
	}
}

func TestFunctionalOptions(t *testing.T) {
	path := tempPath(t)
	s, err := New[testRecord](path,
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
				_ = s.Append(testRecord{
					Name: "goroutine",
					Seq:  uint64(id*perGoroutine + i),
				})
			}
		}(g)
	}
	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := collectRecords[testRecord](t, path)
	expected := goroutines * perGoroutine
	if len(records) != expected {
		t.Fatalf("expected %d records, got %d", expected, len(records))
	}
}

func TestStringTooLong(t *testing.T) {
	s, _ := newTestLog(t)
	defer s.Close()

	// testRecord.Name is sl:"32", so 33 bytes should fail.
	longName := "this-string-is-way-too-long-for-32"
	err := s.Append(testRecord{Name: longName})
	if err == nil {
		t.Fatal("expected error for string exceeding max size")
	}
}
