# statelog

[![Go Reference](https://pkg.go.dev/badge/github.com/slaily/statelog.svg)](https://pkg.go.dev/github.com/slaily/statelog)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Durable state log.

### Installation

```bash
go get github.com/slaily/statelog
```

### Quickstart

```go
package main

import (
	"fmt"
	"log"

	"github.com/slaily/statelog"
)

func main() {
	sl, err := statelog.New("app.log")
	if err != nil {
		log.Fatal(err)
	}
	defer sl.Close()

	// Append records (string or any JSON-serializable value).
	sl.Append("service started")
	sl.Append(map[string]any{"event": "user_login", "user_id": 123})

	// Replay the log to rebuild state.
	r, err := statelog.NewReader("app.log")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	for r.Next() {
		fmt.Println(r.Record())
	}
	if err := r.Err(); err != nil {
		log.Fatal(err)
	}
}
```

### API Reference

---

**`statelog.New(filePath string, opts ...Option) (*StateLog, error)`**

Creates a new log. A background goroutine flushes queued records to disk at a configurable interval.

```go
sl, err := statelog.New("app.log",
	statelog.WithCommitInterval(500 * time.Millisecond),
	statelog.WithMaxQueueSize(50_000),
)
```

| Option | Default | Description |
|---|---|---|
| `WithCommitInterval(d)` | `1s` | How often the background goroutine flushes to disk |
| `WithMaxQueueSize(n)` | `100,000` | Capacity of the in-memory write queue |
| `WithEncoder(e)` | `DefaultEncoder` | Custom `Encoder` implementation for serialization |

---

**`(*StateLog).Append(data any) error`**

Queues a record for durable persistence. Non-blocking.

```go
sl.Append(map[string]any{"event": "checkout", "total": 49.99})
```

Returns `ErrQueueFull` if the queue is at capacity, or `ErrClosed` if the log has been closed.

---

**`(*StateLog).Close() error`**

Flushes all pending records, stops the background goroutine, and closes the file. Safe to call multiple times.

```go
sl.Close()
```

---

**`statelog.NewReader(filePath string, opts ...ReaderOption) (*Reader, error)`**

Opens the log for reading with snapshot semantics — records appended after this call are not visible.

```go
r, err := statelog.NewReader("app.log")
defer r.Close()

for r.Next() {
	fmt.Println(r.Record())
}
if err := r.Err(); err != nil { ... }
```

Corrupted records are silently skipped.

### Binary Format

Each record on disk is a 9-byte header followed by the payload:

```
[4B payload size, uint32 LE] [1B type flag] [4B CRC32, uint32 LE] [payload]
```

Type flags: `0x01` = JSON, `0x02` = raw UTF-8 string.

Implement the `Encoder` interface to support custom serialization formats.

### License

statelog is available under the MIT License.
