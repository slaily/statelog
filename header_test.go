package statelog

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSetMetaRoundTrip(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("job_id", "etl-42"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.SetMeta("cursor", 15782); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}

	if meta["job_id"] != "etl-42" {
		t.Errorf("expected job_id=etl-42, got %v", meta["job_id"])
	}

	cursor, ok := meta["cursor"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number for cursor, got %T", meta["cursor"])
	}
	if cursor.String() != "15782" {
		t.Errorf("expected cursor=15782, got %s", cursor.String())
	}
}

func TestSetMetaTypes(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("str", "hello"); err != nil {
		t.Fatalf("SetMeta str: %v", err)
	}
	if err := s.SetMeta("int_val", 42); err != nil {
		t.Fatalf("SetMeta int: %v", err)
	}
	if err := s.SetMeta("float_val", 3.14); err != nil {
		t.Fatalf("SetMeta float: %v", err)
	}
	if err := s.SetMeta("bool_val", true); err != nil {
		t.Fatalf("SetMeta bool: %v", err)
	}
	if err := s.SetMeta("nil_val", nil); err != nil {
		t.Fatalf("SetMeta nil: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}

	if meta["str"] != "hello" {
		t.Errorf("str: expected hello, got %v", meta["str"])
	}
	if n, ok := meta["int_val"].(json.Number); !ok || n.String() != "42" {
		t.Errorf("int_val: expected 42, got %v (%T)", meta["int_val"], meta["int_val"])
	}
	if n, ok := meta["float_val"].(json.Number); !ok || n.String() != "3.14" {
		t.Errorf("float_val: expected 3.14, got %v (%T)", meta["float_val"], meta["float_val"])
	}
	if meta["bool_val"] != true {
		t.Errorf("bool_val: expected true, got %v", meta["bool_val"])
	}
	if meta["nil_val"] != nil {
		t.Errorf("nil_val: expected nil, got %v", meta["nil_val"])
	}
}

func TestMetaInMemory(t *testing.T) {
	s, _ := newTestLog(t)
	defer s.Close()

	if err := s.SetMeta("key", "value"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	meta := s.Meta()
	if meta["key"] != "value" {
		t.Errorf("expected key=value, got %v", meta["key"])
	}

	// Verify the returned map is a copy.
	meta["key"] = "modified"
	original := s.Meta()
	if original["key"] != "value" {
		t.Error("Meta() should return a copy, not a reference")
	}
}

func TestMetadataOverflow(t *testing.T) {
	s, _ := newTestLog(t)
	defer s.Close()

	// A single large value that exceeds the available meta space.
	bigValue := strings.Repeat("x", 500)
	err := s.SetMeta("big", bigValue)
	if !errors.Is(err, ErrMetadataOverflow) {
		t.Fatalf("expected ErrMetadataOverflow, got %v", err)
	}
}

func TestReadMetaStandalone(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("standalone", "test"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.Append(testRecord{Name: "entry1"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta["standalone"] != "test" {
		t.Errorf("expected standalone=test, got %v", meta["standalone"])
	}
}

func TestReaderExposesMetadata(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("reader_key", "reader_val"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.Append(testRecord{Name: "data"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := NewReader[testRecord](path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	meta := r.Meta()
	if meta["reader_key"] != "reader_val" {
		t.Errorf("expected reader_key=reader_val, got %v", meta["reader_key"])
	}

	// Entries should still be readable.
	if !r.Next() {
		t.Fatal("expected one record")
	}
	if r.Record().Name != "data" {
		t.Errorf("expected 'data', got %q", r.Record().Name)
	}
}

func TestInvalidMagicBytes(t *testing.T) {
	s, path := newTestLog(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Corrupt the magic bytes.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("BAAD"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = NewReader[testRecord](path)
	if err == nil {
		t.Fatal("expected error for invalid magic bytes")
	}

	var ce *CorruptionError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CorruptionError, got %T: %v", err, err)
	}
}

func TestCorruptMetadataCRC(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("key", "value"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Find the CRC32 position in the new header format.
	// The CRC is after: preamble(12) + schema bytes + metaUsed(2).
	// We'll just corrupt a byte in the meta JSON area, which will cause CRC mismatch.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// Read the full header to find the meta CRC offset.
	header := make([]byte, defaultFileHeaderSize)
	if _, err := f.ReadAt(header, 0); err != nil {
		t.Fatal(err)
	}
	// Decode enough to find the CRC position.
	schema, _ := buildSchema[testRecord]()
	schemaBytes := encodeSchema(schema)
	crcOffset := int64(fileHeaderPreambleSize + len(schemaBytes) + 2) // +2 for metaUsed
	var bad [4]byte
	binary.LittleEndian.PutUint32(bad[:], 0xDEADBEEF)
	if _, err := f.WriteAt(bad[:], crcOffset); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = ReadMeta(path)
	if err == nil {
		t.Fatal("expected error for corrupt CRC")
	}

	var ce *CorruptionError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CorruptionError, got %T: %v", err, err)
	}
}

func TestMetaSurvivesRotation(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("job_id", "persist"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.Append(testRecord{Name: "before"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Rotate: rename the file.
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

	// The new file should have the in-memory metadata written to its header.
	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if meta["job_id"] != "persist" {
		t.Errorf("expected job_id=persist in new file, got %v", meta["job_id"])
	}
}

func TestEmptyMetadata(t *testing.T) {
	s, path := newTestLog(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if len(meta) != 0 {
		t.Errorf("expected empty metadata, got %v", meta)
	}
}

func TestSetMetaOverwrite(t *testing.T) {
	s, path := newTestLog(t)

	if err := s.SetMeta("cursor", 100); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.SetMeta("cursor", 200); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}

	cursor, ok := meta["cursor"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T", meta["cursor"])
	}
	if cursor.String() != "200" {
		t.Errorf("expected cursor=200, got %s", cursor.String())
	}
}
