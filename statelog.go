// Package statelog provides a high-performance, goroutine-safe, durable
// Write-Ahead Log (WAL). Records are queued in memory and flushed to disk
// by a background goroutine, combining low-latency appends with strong
// durability guarantees via fsync.
package statelog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

// StateLog is a durable, append-only log backed by a binary file on disk.
type StateLog struct {
	filePath string
	file     *os.File
	inode    uint64
	encoder  Encoder
	queue    chan any
	done     chan struct{}
	stopped  chan struct{}
	closed   atomic.Bool
	cfg      config
}

// New creates a StateLog that writes to filePath. The parent directory is
// created if it does not exist. A background goroutine is started to
// periodically flush queued records to disk.
func New(filePath string, opts ...Option) (*StateLog, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("statelog: resolve path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, &IOError{FilePath: absPath, Message: "create parent directory", Err: err}
	}

	s := &StateLog{
		filePath: absPath,
		encoder:  cfg.encoder,
		queue:    make(chan any, cfg.maxQueueSize),
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
		cfg:      cfg,
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
func (s *StateLog) Append(data any) error {
	if s.closed.Load() {
		return ErrClosed
	}
	select {
	case s.queue <- data:
		return nil
	default:
		return ErrQueueFull
	}
}

// Close flushes all pending records to disk, stops the background goroutine,
// and closes the underlying file. It is safe to call multiple times.
func (s *StateLog) Close() error {
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

func (s *StateLog) commitLoop() {
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

func (s *StateLog) commit() error {
	var entries [][]byte
	for {
		select {
		case data := <-s.queue:
			entry, err := formatEntry(s.encoder, data)
			if err != nil {
				continue
			}
			entries = append(entries, entry)
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

func (s *StateLog) syncToDisk(entries [][]byte) error {
	if err := s.detectRotation(); err != nil {
		return err
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

func (s *StateLog) detectRotation() error {
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

func (s *StateLog) reopenFile() error {
	if s.file != nil {
		s.file.Close()
	}

	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	return nil
}

func (s *StateLog) lockFile() error {
	if err := syscall.Flock(int(s.file.Fd()), syscall.LOCK_EX); err != nil {
		return &IOError{FilePath: s.filePath, Message: "acquire file lock", Err: err}
	}
	return nil
}

func (s *StateLog) unlockFile() {
	_ = syscall.Flock(int(s.file.Fd()), syscall.LOCK_UN)
}
