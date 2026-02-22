package statelog

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

const entryHeaderSize = 9 // 4 (payload size) + 1 (type flag) + 4 (crc32)

// formatEntry serializes data into a binary log entry.
//
// Template method pipeline: encode → checksum → pack header → combine.
func formatEntry(enc Encoder, data any) ([]byte, error) {
	payload, typeFlag, err := enc.Encode(data)
	if err != nil {
		return nil, err
	}

	checksum := crc32.ChecksumIEEE(payload)
	payloadSize := uint32(len(payload))

	entry := make([]byte, entryHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(entry[0:4], payloadSize)
	entry[4] = typeFlag
	binary.LittleEndian.PutUint32(entry[5:9], checksum)
	copy(entry[entryHeaderSize:], payload)

	return entry, nil
}

// parseEntry reads a single log entry from r and returns the decoded record
// along with the total bytes consumed. The offset parameter is used for error
// reporting only.
func parseEntry(r io.Reader, enc Encoder, offset int64) (any, int, error) {
	var header [entryHeaderSize]byte
	n, err := io.ReadFull(r, header[:])
	if err != nil {
		reason := "incomplete header"
		if err == io.EOF {
			reason = "unexpected EOF reading header"
		}
		return nil, n, &CorruptionError{
			Offset: offset,
			Reason: reason,
			Err:    err,
		}
	}

	payloadSize := binary.LittleEndian.Uint32(header[0:4])
	typeFlag := header[4]
	expectedChecksum := binary.LittleEndian.Uint32(header[5:9])

	payload := make([]byte, payloadSize)
	pn, err := io.ReadFull(r, payload)
	if err != nil {
		return nil, n + pn, &CorruptionError{
			Offset: offset,
			Reason: fmt.Sprintf("expected payload of %d bytes but got %d", payloadSize, pn),
			Err:    err,
		}
	}

	actualChecksum := crc32.ChecksumIEEE(payload)
	if actualChecksum != expectedChecksum {
		return nil, n + pn, &CorruptionError{
			Offset: offset,
			Reason: "CRC32 checksum mismatch",
		}
	}

	record, err := enc.Decode(payload, typeFlag)
	if err != nil {
		return nil, n + pn, &CorruptionError{
			Offset: offset,
			Reason: fmt.Sprintf("decode failed: %v", err),
			Err:    err,
		}
	}

	return record, entryHeaderSize + int(payloadSize), nil
}
