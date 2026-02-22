// Package statelog provides a high-performance, goroutine-safe, durable
// Write-Ahead Log (WAL) with fixed-size records. The record schema is derived
// from a Go struct — the struct IS the schema. Records are queued in memory
// and flushed to disk by a background goroutine, combining low-latency
// appends with strong durability guarantees via fsync.
package statelog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// StateLog is a durable, append-only log backed by a binary file on disk.
// The type parameter T defines the fixed-size record schema.
type StateLog[T any] struct {
	filePath       string
	file           *os.File
	inode          uint64
	schema         *Schema
	queue          chan []byte
	done           chan struct{}
	stopped        chan struct{}
	closed         atomic.Bool
	cfg            config
	meta           map[string]any
	metaMu         sync.Mutex
	fileHeaderSize uint16
}

// New creates a StateLog that writes to filePath. The record schema is
// derived from the struct type T. The parent directory is created if it
// does not exist. A background goroutine is started to periodically flush
// queued records to disk.
func New[T any](filePath string, opts ...Option) (*StateLog[T], error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	schema, err := buildSchema[T]()
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("statelog: resolve path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, &IOError{FilePath: absPath, Message: "create parent directory", Err: err}
	}

	s := &StateLog[T]{
		filePath:       absPath,
		schema:         schema,
		queue:          make(chan []byte, cfg.maxQueueSize),
		done:           make(chan struct{}),
		stopped:        make(chan struct{}),
		cfg:            cfg,
		meta:           make(map[string]any),
		fileHeaderSize: defaultFileHeaderSize,
	}

	if err := s.reopenFile(); err != nil {
		return nil, err
	}

	go s.commitLoop()
	return s, nil
}

// Append queues a record for durable persistence. It returns immediately
// without blocking on disk I/O. Returns ErrQueueFull if the in-memory queue
// is at capacity, or ErrClosed if the log has been closed.
func (s *StateLog[T]) Append(data T) error {
	if s.closed.Load() {
		return ErrClosed
	}

	buf, err := encodeRecord(s.schema, data)
	if err != nil {
		return err
	}

	select {
	case s.queue <- buf:
		return nil
	default:
		return ErrQueueFull
	}
}

// Close flushes all pending records to disk, stops the background goroutine,
// and closes the underlying file. It is safe to call multiple times.
func (s *StateLog[T]) Close() error {
	if s.closed.Swap(true) {
		return nil
	}

	close(s.done)
	<-s.stopped

	if err := s.commit(); err != nil {
		s.file.Close()
		return err
	}

	return s.file.Close()
}

func (s *StateLog[T]) commitLoop() {
	defer close(s.stopped)
	ticker := time.NewTicker(s.cfg.commitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			_ = s.commit()
		}
	}
}

func (s *StateLog[T]) commit() error {
	var entries [][]byte
	for {
		select {
		case buf := <-s.queue:
			entries = append(entries, buf)
		default:
			goto write
		}
	}

write:
	if len(entries) == 0 {
		return nil
	}

	if err := s.lockFile(); err != nil {
		return err
	}
	defer s.unlockFile()

	return s.syncToDisk(entries)
}

func (s *StateLog[T]) syncToDisk(entries [][]byte) error {
	if err := s.detectRotation(); err != nil {
		return err
	}

	if _, err := s.file.Seek(0, io.SeekEnd); err != nil {
		return &IOError{FilePath: s.filePath, Message: "seek to end", Err: err}
	}

	for _, entry := range entries {
		if _, err := s.file.Write(entry); err != nil {
			return &IOError{FilePath: s.filePath, Message: "write entry", Err: err}
		}
	}

	if err := s.file.Sync(); err != nil {
		return &IOError{FilePath: s.filePath, Message: "fsync", Err: err}
	}

	return nil
}

func (s *StateLog[T]) detectRotation() error {
	info, err := os.Stat(s.filePath)
	if os.IsNotExist(err) {
		return s.reopenFile()
	}
	if err != nil {
		return &IOError{FilePath: s.filePath, Message: "stat for rotation check", Err: err}
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && stat.Ino != s.inode {
		return s.reopenFile()
	}

	return nil
}

func (s *StateLog[T]) reopenFile() error {
	if s.file != nil {
		s.file.Close()
	}

	f, err := os.OpenFile(s.filePath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return &IOError{FilePath: s.filePath, Message: "open log file", Err: err}
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return &IOError{FilePath: s.filePath, Message: "stat log file", Err: err}
	}

	s.file = f
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		s.inode = stat.Ino
	}

	if info.Size() == 0 {
		return s.writeFileHeader()
	}
	return s.loadFileHeader()
}

func (s *StateLog[T]) writeFileHeader() error {
	header, err := encodeFileHeader(s.schema, s.meta, s.fileHeaderSize)
	if err != nil {
		return err
	}
	if _, err := s.file.WriteAt(header, 0); err != nil {
		return &IOError{FilePath: s.filePath, Message: "write file header", Err: err}
	}
	return s.file.Sync()
}

func (s *StateLog[T]) loadFileHeader() error {
	f, err := os.Open(s.filePath)
	if err != nil {
		return &IOError{FilePath: s.filePath, Message: "open for header read", Err: err}
	}
	defer f.Close()

	_, meta, headerSize, err := decodeFileHeader(f)
	if err != nil {
		return err
	}
	s.meta = meta
	s.fileHeaderSize = headerSize
	return nil
}

// SetMeta sets a metadata key-value pair and persists it to the file header.
// Supported value types: string, int, float64, bool, nil, and other
// JSON-serializable types.
func (s *StateLog[T]) SetMeta(key string, value any) error {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()

	s.meta[key] = value
	return s.writeFileHeader()
}

// Meta returns a shallow copy of the current metadata.
func (s *StateLog[T]) Meta() map[string]any {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()

	cp := make(map[string]any, len(s.meta))
	for k, v := range s.meta {
		cp[k] = v
	}
	return cp
}

func (s *StateLog[T]) lockFile() error {
	if err := syscall.Flock(int(s.file.Fd()), syscall.LOCK_EX); err != nil {
		return &IOError{FilePath: s.filePath, Message: "acquire file lock", Err: err}
	}
	return nil
}

func (s *StateLog[T]) unlockFile() {
	_ = syscall.Flock(int(s.file.Fd()), syscall.LOCK_UN)
}
