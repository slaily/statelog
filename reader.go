package statelog

import (
	"errors"
	"io"
	"os"
)

// Reader iterates over log entries using the Iterator pattern.
// Usage follows the bufio.Scanner convention:
//
//	r, err := statelog.NewReader("app.log")
//	if err != nil { ... }
//	defer r.Close()
//
//	for r.Next() {
//	    fmt.Println(r.Record())
//	}
//	if err := r.Err(); err != nil { ... }
type Reader struct {
	file         *os.File
	snapshotSize int64
	encoder      Encoder
	pos          int64
	current      any
	err          error
	meta         map[string]any
}

// NewReader opens filePath for reading and takes a size snapshot for
// consistent iteration. Records appended after this call are not visible.
func NewReader(filePath string, opts ...ReaderOption) (*Reader, error) {
	cfg := defaultReaderConfig()
	for _, o := range opts {
		o(&cfg)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "stat log file", Err: err}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "open log file for reading", Err: err}
	}

	meta, headerSize, err := decodeFileHeader(f)
	if err != nil {
		f.Close()
		if ce, ok := err.(*CorruptionError); ok {
			ce.FilePath = filePath
		}
		return nil, err
	}

	if _, err := f.Seek(int64(headerSize), io.SeekStart); err != nil {
		f.Close()
		return nil, &IOError{FilePath: filePath, Message: "seek past file header", Err: err}
	}

	return &Reader{
		file:         f,
		snapshotSize: info.Size(),
		encoder:      cfg.encoder,
		pos:          int64(headerSize),
		meta:         meta,
	}, nil
}

// Next advances to the next valid record. Corrupted entries are silently
// skipped. Returns false when the end of the snapshot is reached or an
// unrecoverable error occurs.
func (r *Reader) Next() bool {
	for {
		if r.pos+entryHeaderSize > r.snapshotSize {
			return false
		}

		record, consumed, err := parseEntry(r.file, r.encoder, r.pos)
		r.pos += int64(consumed)

		if err != nil {
			var ce *CorruptionError
			if errors.As(err, &ce) {
				if errors.Is(ce.Err, io.EOF) || errors.Is(ce.Err, io.ErrUnexpectedEOF) {
					return false
				}
				continue
			}
			r.err = err
			return false
		}

		r.current = record
		return true
	}
}

// Record returns the most recently read log entry.
func (r *Reader) Record() any { return r.current }

// Err returns the first non-corruption error encountered during iteration.
func (r *Reader) Err() error { return r.err }

// Meta returns a shallow copy of the file-level metadata from the header.
func (r *Reader) Meta() map[string]any {
	cp := make(map[string]any, len(r.meta))
	for k, v := range r.meta {
		cp[k] = v
	}
	return cp
}

// Close releases the underlying file handle.
func (r *Reader) Close() error { return r.file.Close() }
