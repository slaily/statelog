package statelog

import "time"

const (
	defaultCommitInterval = 1 * time.Second
	defaultMaxQueueSize   = 100_000
)

type config struct {
	commitInterval time.Duration
	maxQueueSize   int
}

func defaultConfig() config {
	return config{
		commitInterval: defaultCommitInterval,
		maxQueueSize:   defaultMaxQueueSize,
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
