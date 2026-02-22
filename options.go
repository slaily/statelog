package statelog

import "time"

const (
	defaultCommitInterval = 1 * time.Second
	defaultMaxQueueSize   = 100_000
)

type config struct {
	commitInterval time.Duration
	maxQueueSize   int
	encoder        Encoder
}

func defaultConfig() config {
	return config{
		commitInterval: defaultCommitInterval,
		maxQueueSize:   defaultMaxQueueSize,
		encoder:        DefaultEncoder{},
	}
}

// Option configures a StateLog instance.
type Option func(*config)

// WithCommitInterval sets how often the background goroutine flushes
// queued records to disk.
func WithCommitInterval(d time.Duration) Option {
	return func(c *config) { c.commitInterval = d }
}

// WithMaxQueueSize sets the capacity of the in-memory write queue.
func WithMaxQueueSize(size int) Option {
	return func(c *config) { c.maxQueueSize = size }
}

// WithEncoder sets a custom Encoder for payload serialization.
func WithEncoder(e Encoder) Option {
	return func(c *config) { c.encoder = e }
}

type readerConfig struct {
	encoder Encoder
}

func defaultReaderConfig() readerConfig {
	return readerConfig{encoder: DefaultEncoder{}}
}

// ReaderOption configures a Reader instance.
type ReaderOption func(*readerConfig)

// WithReaderEncoder sets a custom Encoder for the Reader.
func WithReaderEncoder(e Encoder) ReaderOption {
	return func(c *readerConfig) { c.encoder = e }
}
