package statelog

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

var fileMagic = [4]byte{'S', 'L', 'O', 'G'}

const (
	fileVersion           = byte(0x02)
	defaultFileHeaderSize = uint16(512)

	// Preamble: 4 (magic) + 1 (version) + 2 (header size) + 4 (record size) + 1 (field count) = 12 bytes.
	fileHeaderPreambleSize = 12
)

// encodeFileHeader builds a complete file header block of exactly headerSize bytes.
// Layout: [preamble][schema bytes][2B meta used][4B CRC32][meta JSON][zero pad].
func encodeFileHeader(schema *Schema, meta map[string]any, headerSize uint16) ([]byte, error) {
	schemaBytes := encodeSchema(schema)

	var metaJSON []byte
	if len(meta) > 0 {
		var err error
		metaJSON, err = json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("statelog: encode metadata: %w", err)
		}
	}

	// Available space: headerSize - preamble - schema - 2 (meta used) - 4 (CRC32).
	overhead := fileHeaderPreambleSize + len(schemaBytes) + 2 + 4
	if overhead > int(headerSize) {
		return nil, fmt.Errorf("statelog: schema too large for header (%d bytes needed, %d available)", overhead, headerSize)
	}

	maxMeta := int(headerSize) - overhead
	if len(metaJSON) > maxMeta {
		return nil, ErrMetadataOverflow
	}

	checksum := crc32.ChecksumIEEE(metaJSON)

	buf := make([]byte, headerSize)
	pos := 0

	// Preamble.
	copy(buf[pos:], fileMagic[:])
	pos += 4
	buf[pos] = fileVersion
	pos++
	binary.LittleEndian.PutUint16(buf[pos:], headerSize)
	pos += 2
	binary.LittleEndian.PutUint32(buf[pos:], schema.RecordSize)
	pos += 4
	buf[pos] = byte(len(schema.Fields))
	pos++

	// Schema.
	copy(buf[pos:], schemaBytes)
	pos += len(schemaBytes)

	// Meta used + CRC32 + meta JSON.
	binary.LittleEndian.PutUint16(buf[pos:], uint16(len(metaJSON)))
	pos += 2
	binary.LittleEndian.PutUint32(buf[pos:], checksum)
	pos += 4
	copy(buf[pos:], metaJSON)

	return buf, nil
}

// decodeFileHeader reads the file header from r, validates magic bytes and CRC32,
// and returns the decoded schema, metadata, and header size.
func decodeFileHeader(r io.ReaderAt) (*Schema, map[string]any, uint16, error) {
	var preamble [fileHeaderPreambleSize]byte
	if _, err := r.ReadAt(preamble[:], 0); err != nil {
		return nil, nil, 0, &CorruptionError{Reason: "read file header preamble", Err: err}
	}

	var magic [4]byte
	copy(magic[:], preamble[0:4])
	if magic != fileMagic {
		return nil, nil, 0, &CorruptionError{Reason: "invalid magic bytes"}
	}

	headerSize := binary.LittleEndian.Uint16(preamble[5:7])
	recordSize := binary.LittleEndian.Uint32(preamble[7:11])
	fieldCount := int(preamble[11])

	// Read the rest of the header.
	remaining := make([]byte, int(headerSize)-fileHeaderPreambleSize)
	if _, err := r.ReadAt(remaining, int64(fileHeaderPreambleSize)); err != nil {
		return nil, nil, 0, &CorruptionError{Reason: "read header body", Err: err}
	}

	// Decode schema.
	schema, consumed, err := decodeSchema(remaining, fieldCount)
	if err != nil {
		return nil, nil, 0, &CorruptionError{Reason: fmt.Sprintf("decode schema: %v", err)}
	}

	// Validate record size matches schema.
	if schema.RecordSize != recordSize {
		return nil, nil, 0, &CorruptionError{
			Reason: fmt.Sprintf("record size mismatch: header says %d, schema computes %d", recordSize, schema.RecordSize),
		}
	}

	pos := consumed

	// Meta used + CRC32 + meta JSON.
	if pos+6 > len(remaining) {
		return nil, nil, 0, &CorruptionError{Reason: "header truncated before meta fields"}
	}

	metaUsed := binary.LittleEndian.Uint16(remaining[pos:])
	pos += 2
	expectedChecksum := binary.LittleEndian.Uint32(remaining[pos:])
	pos += 4

	if metaUsed == 0 {
		return schema, make(map[string]any), headerSize, nil
	}

	if pos+int(metaUsed) > len(remaining) {
		return nil, nil, 0, &CorruptionError{Reason: "metadata extends beyond header"}
	}

	metaJSON := remaining[pos : pos+int(metaUsed)]

	actualChecksum := crc32.ChecksumIEEE(metaJSON)
	if actualChecksum != expectedChecksum {
		return nil, nil, 0, &CorruptionError{Reason: "metadata CRC32 checksum mismatch"}
	}

	dec := json.NewDecoder(bytes.NewReader(metaJSON))
	dec.UseNumber()
	var meta map[string]any
	if err := dec.Decode(&meta); err != nil {
		return nil, nil, 0, &CorruptionError{Reason: fmt.Sprintf("decode metadata: %v", err)}
	}

	return schema, meta, headerSize, nil
}

// ReadMeta reads metadata from a statelog file without scanning entries.
// This is an O(1) operation that only reads the file header.
func ReadMeta(filePath string) (map[string]any, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "open for metadata read", Err: err}
	}
	defer f.Close()

	_, meta, _, err := decodeFileHeader(f)
	if err != nil {
		if ce, ok := err.(*CorruptionError); ok {
			ce.FilePath = filePath
		}
		return nil, err
	}

	return meta, nil
}
