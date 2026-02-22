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
	fileVersion            = byte(0x01)
	defaultFileHeaderSize  = uint16(512)
	fileHeaderPreambleSize = 13 // 4 (magic) + 1 (version) + 2 (header size) + 2 (meta used) + 4 (CRC32)
)

// encodeFileHeader builds a complete file header block of exactly headerSize bytes.
// It JSON-encodes meta, verifies it fits in the reserved space, computes CRC32
// over the JSON bytes, packs the preamble, and zero-pads the remainder.
func encodeFileHeader(meta map[string]any, headerSize uint16) ([]byte, error) {
	var metaJSON []byte
	if len(meta) > 0 {
		var err error
		metaJSON, err = json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("statelog: encode metadata: %w", err)
		}
	}

	maxMeta := int(headerSize) - int(fileHeaderPreambleSize)
	if len(metaJSON) > maxMeta {
		return nil, ErrMetadataOverflow
	}

	checksum := crc32.ChecksumIEEE(metaJSON)

	buf := make([]byte, headerSize)
	copy(buf[0:4], fileMagic[:])
	buf[4] = fileVersion
	binary.LittleEndian.PutUint16(buf[5:7], headerSize)
	binary.LittleEndian.PutUint16(buf[7:9], uint16(len(metaJSON)))
	binary.LittleEndian.PutUint32(buf[9:13], checksum)
	copy(buf[fileHeaderPreambleSize:], metaJSON)

	return buf, nil
}

// decodeFileHeader reads the file header from r, validates magic bytes and CRC32,
// and returns the decoded metadata map and header size.
func decodeFileHeader(r io.ReaderAt) (map[string]any, uint16, error) {
	var preamble [fileHeaderPreambleSize]byte
	if _, err := r.ReadAt(preamble[:], 0); err != nil {
		return nil, 0, &CorruptionError{Reason: "read file header preamble", Err: err}
	}

	var magic [4]byte
	copy(magic[:], preamble[0:4])
	if magic != fileMagic {
		return nil, 0, &CorruptionError{Reason: "invalid magic bytes"}
	}

	headerSize := binary.LittleEndian.Uint16(preamble[5:7])
	metaUsed := binary.LittleEndian.Uint16(preamble[7:9])
	expectedChecksum := binary.LittleEndian.Uint32(preamble[9:13])

	if metaUsed == 0 {
		return make(map[string]any), headerSize, nil
	}

	metaJSON := make([]byte, metaUsed)
	if _, err := r.ReadAt(metaJSON, int64(fileHeaderPreambleSize)); err != nil {
		return nil, 0, &CorruptionError{Reason: "read metadata", Err: err}
	}

	actualChecksum := crc32.ChecksumIEEE(metaJSON)
	if actualChecksum != expectedChecksum {
		return nil, 0, &CorruptionError{Reason: "metadata CRC32 checksum mismatch"}
	}

	dec := json.NewDecoder(bytes.NewReader(metaJSON))
	dec.UseNumber()
	var meta map[string]any
	if err := dec.Decode(&meta); err != nil {
		return nil, 0, &CorruptionError{Reason: fmt.Sprintf("decode metadata: %v", err)}
	}

	return meta, headerSize, nil
}

// ReadMeta reads metadata from a statelog file without scanning entries.
// This is an O(1) operation that only reads the file header.
func ReadMeta(filePath string) (map[string]any, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "open for metadata read", Err: err}
	}
	defer f.Close()

	meta, _, err := decodeFileHeader(f)
	if err != nil {
		if ce, ok := err.(*CorruptionError); ok {
			ce.FilePath = filePath
		}
		return nil, err
	}

	return meta, nil
}
