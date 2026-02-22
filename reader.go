package statelog

import (
	"errors"
	"io"
	"os"
)

// Reader iterates over log records using the Iterator pattern.
// Usage follows the bufio.Scanner convention:
//
//	r, err := statelog.NewReader[Event]("app.log")
//	if err != nil { ... }
//	defer r.Close()
//
//	for r.Next() {
//	    fmt.Println(r.Record())
//	}
//	if err := r.Err(); err != nil { ... }
type Reader[T any] struct {
	file         *os.File
	snapshotSize int64
	schema       *Schema
	pos          int64
	current      T
	err          error
	meta         map[string]any
}

// NewReader opens filePath for reading and takes a size snapshot for
// consistent iteration. Records appended after this call are not visible.
func NewReader[T any](filePath string) (*Reader[T], error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "stat log file", Err: err}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "open log file for reading", Err: err}
	}

	schema, meta, headerSize, err := decodeFileHeader(f)
	if err != nil {
		f.Close()
		if ce, ok := err.(*CorruptionError); ok {
			ce.FilePath = filePath
		}
		return nil, err
	}

	return &Reader[T]{
		file:         f,
		snapshotSize: info.Size(),
		schema:       schema,
		pos:          int64(headerSize),
		meta:         meta,
	}, nil
}

// Next advances to the next valid record. Corrupted records are silently
// skipped. Returns false when the end of the snapshot is reached or an
// unrecoverable error occurs.
func (r *Reader[T]) Next() bool {
	for {
		if r.pos+int64(r.schema.RecordSize) > r.snapshotSize {
			return false
		}

		buf := make([]byte, r.schema.RecordSize)
		n, err := r.file.ReadAt(buf, r.pos)
		if err != nil && err != io.EOF {
			r.err = &IOError{Message: "read record", Err: err}
			return false
		}
		if n < int(r.schema.RecordSize) {
			return false
		}

		record, err := decodeRecord[T](r.schema, buf, r.pos)
		r.pos += int64(r.schema.RecordSize)

		if err != nil {
			var ce *CorruptionError
			if errors.As(err, &ce) {
				continue
			}
			r.err = err
			return false
		}

		r.current = record
		return true
	}
}

// Record returns the most recently read log record.
func (r *Reader[T]) Record() T { return r.current }

// Err returns the first non-corruption error encountered during iteration.
func (r *Reader[T]) Err() error { return r.err }

// Meta returns a shallow copy of the file-level metadata from the header.
func (r *Reader[T]) Meta() map[string]any {
	cp := make(map[string]any, len(r.meta))
	for k, v := range r.meta {
		cp[k] = v
	}
	return cp
}

// Close releases the underlying file handle.
func (r *Reader[T]) Close() error { return r.file.Close() }
