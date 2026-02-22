package statelog

import (
	"bytes"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"sort"
)

// indexEntry maps a key hash to a record index in the log file.
// 12 bytes per entry. Sorted by keyHash for binary search.
type indexEntry struct {
	keyHash uint64
	index   uint32
}

// indexDef is a named index referencing a schema field.
type indexDef struct {
	name       string
	fieldIndex int
}

// Querier opens a log file, scans it once building multiple named indexes,
// then supports O(log n) equality lookups on any indexed field.
//
// Snapshot semantics: entries appended after NewQuerier returns are not visible.
//
//	q, err := statelog.NewQuerier[Event]("events.log",
//	    statelog.Index("SiteID"),
//	    statelog.Index("Status"),
//	)
//	if err != nil { ... }
//	defer q.Close()
//
//	results := q.Lookup("SiteID", "acme_x")
type Querier[T any] struct {
	file         *os.File
	schema       *Schema
	snapshotSize int64
	headerSize   uint16
	defs         []indexDef
	indexes      map[string][]indexEntry
	meta         map[string]any
}

// NewQuerier opens filePath, scans it once, and builds all registered indexes.
// At least one Index option must be provided.
func NewQuerier[T any](filePath string, opts ...QuerierOption) (*Querier[T], error) {
	cfg := defaultQuerierConfig()
	for _, o := range opts {
		o(&cfg)
	}

	if len(cfg.indexNames) == 0 {
		return nil, errors.New("statelog: NewQuerier requires at least one Index")
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "stat log file", Err: err}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, &IOError{FilePath: filePath, Message: "open log file for querying", Err: err}
	}

	schema, meta, headerSize, err := decodeFileHeader(f)
	if err != nil {
		f.Close()
		if ce, ok := err.(*CorruptionError); ok {
			ce.FilePath = filePath
		}
		return nil, err
	}

	// Resolve index names to field indexes in the schema.
	defs := make([]indexDef, len(cfg.indexNames))
	for i, name := range cfg.indexNames {
		fi := schema.FieldIndex(name)
		if fi < 0 {
			f.Close()
			return nil, errors.New("statelog: Index field " + name + " not found in schema")
		}
		defs[i] = indexDef{name: name, fieldIndex: fi}
	}

	q := &Querier[T]{
		file:         f,
		schema:       schema,
		snapshotSize: info.Size(),
		headerSize:   headerSize,
		defs:         defs,
		meta:         meta,
	}

	if err := q.buildIndexes(); err != nil {
		f.Close()
		return nil, err
	}

	return q, nil
}

// buildIndexes performs a single sequential scan, populating all indexes.
// Reads raw record bytes — no full decode, just extractField + hash.
func (q *Querier[T]) buildIndexes() error {
	dataSize := q.snapshotSize - int64(q.headerSize)
	recordSize := int64(q.schema.RecordSize)

	if dataSize <= 0 || recordSize == 0 {
		q.indexes = make(map[string][]indexEntry, len(q.defs))
		for _, def := range q.defs {
			q.indexes[def.name] = nil
		}
		return nil
	}

	estimatedEntries := int(dataSize / recordSize)
	q.indexes = make(map[string][]indexEntry, len(q.defs))
	for _, def := range q.defs {
		q.indexes[def.name] = make([]indexEntry, 0, estimatedEntries)
	}

	if _, err := q.file.Seek(int64(q.headerSize), io.SeekStart); err != nil {
		return &IOError{Message: "seek past header for index build", Err: err}
	}

	buf := make([]byte, recordSize)
	var recordIndex uint32

	for pos := int64(q.headerSize); pos+recordSize <= q.snapshotSize; pos += recordSize {
		n, err := q.file.Read(buf)
		if err != nil || int64(n) < recordSize {
			break
		}

		for i := range q.defs {
			raw := extractField(q.schema, q.defs[i].fieldIndex, buf)
			h := hashKey(raw)
			q.indexes[q.defs[i].name] = append(q.indexes[q.defs[i].name], indexEntry{
				keyHash: h,
				index:   recordIndex,
			})
		}

		recordIndex++
	}

	for name := range q.indexes {
		idx := q.indexes[name]
		sort.Slice(idx, func(i, j int) bool {
			return idx[i].keyHash < idx[j].keyHash
		})
	}

	return nil
}

// Lookup returns all records matching key on the named index.
// The key should be a Go value matching the field type (string for string
// fields, uint64 for uint64 fields, etc.).
// O(log n) binary search + O(m) disk reads for m matching entries.
func (q *Querier[T]) Lookup(index string, key any) []T {
	idx, ok := q.indexes[index]
	if !ok || len(idx) == 0 {
		return nil
	}

	// Find the field index for key encoding and verification.
	def := q.defByName(index)
	if def == nil {
		return nil
	}

	encodedKey, err := encodeKey(q.schema, def.fieldIndex, key)
	if err != nil {
		return nil
	}

	h := hashKey(encodedKey)

	// Binary search for the first entry with this hash.
	i := sort.Search(len(idx), func(i int) bool {
		return idx[i].keyHash >= h
	})

	var results []T
	for ; i < len(idx) && idx[i].keyHash == h; i++ {
		record, err := q.readAt(idx[i].index)
		if err != nil {
			continue
		}

		// Verify actual key equality to handle hash collisions.
		actualRaw := q.readFieldAt(idx[i].index, def.fieldIndex)
		if !bytes.Equal(actualRaw, encodedKey) {
			continue
		}

		results = append(results, record)
	}

	return results
}

// Count returns the number of indexed entries matching key on the named index.
// O(log n) binary search + O(m) disk reads to verify hash collisions.
func (q *Querier[T]) Count(index string, key any) int {
	return len(q.Lookup(index, key))
}

// Meta returns a shallow copy of the file-level metadata from the header.
func (q *Querier[T]) Meta() map[string]any {
	cp := make(map[string]any, len(q.meta))
	for k, v := range q.meta {
		cp[k] = v
	}
	return cp
}

// Close releases the underlying file handle.
func (q *Querier[T]) Close() error {
	return q.file.Close()
}

// readAt reads and decodes a single record at the given record index.
func (q *Querier[T]) readAt(recordIndex uint32) (T, error) {
	offset := int64(q.headerSize) + int64(recordIndex)*int64(q.schema.RecordSize)
	buf := make([]byte, q.schema.RecordSize)
	n, err := q.file.ReadAt(buf, offset)
	if err != nil || n < int(q.schema.RecordSize) {
		var zero T
		return zero, &IOError{Message: "read record at index", Err: err}
	}
	return decodeRecord[T](q.schema, buf, offset)
}

// readFieldAt reads the raw bytes of a single field from a record at the given index.
func (q *Querier[T]) readFieldAt(recordIndex uint32, fieldIndex int) []byte {
	f := &q.schema.Fields[fieldIndex]
	offset := int64(q.headerSize) + int64(recordIndex)*int64(q.schema.RecordSize) + int64(f.Offset)
	buf := make([]byte, f.Size)
	n, err := q.file.ReadAt(buf, offset)
	if err != nil || n < int(f.Size) {
		return nil
	}
	return buf
}

// defByName returns the index definition for the given name.
func (q *Querier[T]) defByName(name string) *indexDef {
	for i := range q.defs {
		if q.defs[i].name == name {
			return &q.defs[i]
		}
	}
	return nil
}

// hashKey computes FNV-1a hash of the key bytes.
func hashKey(key []byte) uint64 {
	h := fnv.New64a()
	h.Write(key)
	return h.Sum64()
}
