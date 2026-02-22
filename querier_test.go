package statelog

import (
	"fmt"
	"os"
	"testing"
	"time"
)

type queryRecord struct {
	UserID string `sl:"16"`
	Event  string `sl:"32"`
}

type multiIndexRecord struct {
	SiteID string `sl:"16"`
	Status string `sl:"16"`
}

func writeTestLog[T any](t *testing.T, entries ...T) string {
	t.Helper()
	path := tempPath(t)
	s, err := New[T](path, WithCommitInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, e := range entries {
		if err := s.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

func TestQuerierLookupSingleIndex(t *testing.T) {
	path := writeTestLog(t,
		queryRecord{UserID: "42", Event: "login"},
		queryRecord{UserID: "7", Event: "login"},
		queryRecord{UserID: "42", Event: "purchase"},
	)

	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	results := q.Lookup("UserID", "42")
	if len(results) != 2 {
		t.Fatalf("expected 2 results for UserID=42, got %d", len(results))
	}

	for _, rec := range results {
		if rec.UserID != "42" {
			t.Errorf("expected UserID=42, got %q", rec.UserID)
		}
	}

	results = q.Lookup("UserID", "7")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for UserID=7, got %d", len(results))
	}
}

func TestQuerierLookupMultipleIndexes(t *testing.T) {
	path := writeTestLog(t,
		multiIndexRecord{SiteID: "acme_x", Status: "ok"},
		multiIndexRecord{SiteID: "acme_x", Status: "failed"},
		multiIndexRecord{SiteID: "beta_y", Status: "failed"},
	)

	q, err := NewQuerier[multiIndexRecord](path,
		Index("SiteID"),
		Index("Status"),
	)
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	// Lookup by SiteID.
	results := q.Lookup("SiteID", "acme_x")
	if len(results) != 2 {
		t.Fatalf("expected 2 results for SiteID=acme_x, got %d", len(results))
	}

	// Lookup by Status.
	results = q.Lookup("Status", "failed")
	if len(results) != 2 {
		t.Fatalf("expected 2 results for Status=failed, got %d", len(results))
	}

	results = q.Lookup("Status", "ok")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for Status=ok, got %d", len(results))
	}
}

func TestQuerierLookupNoMatches(t *testing.T) {
	path := writeTestLog(t,
		queryRecord{UserID: "42", Event: "login"},
	)

	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	results := q.Lookup("UserID", "999")
	if len(results) != 0 {
		t.Fatalf("expected 0 results for non-existent key, got %d", len(results))
	}
}

func TestQuerierLookupUnknownIndex(t *testing.T) {
	path := writeTestLog(t,
		queryRecord{UserID: "42"},
	)

	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	results := q.Lookup("nonexistent", "42")
	if results != nil {
		t.Fatalf("expected nil for unknown index, got %v", results)
	}
}

func TestQuerierCount(t *testing.T) {
	path := writeTestLog(t,
		multiIndexRecord{Status: "ok"},
		multiIndexRecord{Status: "failed"},
		multiIndexRecord{Status: "ok"},
		multiIndexRecord{Status: "ok"},
	)

	q, err := NewQuerier[multiIndexRecord](path, Index("Status"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	if c := q.Count("Status", "ok"); c != 3 {
		t.Errorf("expected count=3 for ok, got %d", c)
	}
	if c := q.Count("Status", "failed"); c != 1 {
		t.Errorf("expected count=1 for failed, got %d", c)
	}
	if c := q.Count("Status", "unknown"); c != 0 {
		t.Errorf("expected count=0 for unknown, got %d", c)
	}
}

func TestQuerierCorruptedEntrySkipped(t *testing.T) {
	path := writeTestLog(t,
		queryRecord{UserID: "1", Event: "first"},
		queryRecord{UserID: "2", Event: "second"},
		queryRecord{UserID: "1", Event: "third"},
	)

	// Corrupt the CRC32 of the second record.
	schema, _ := buildSchema[queryRecord]()
	secondRecordOffset := int64(defaultFileHeaderSize) + int64(schema.RecordSize)
	crcOffset := secondRecordOffset + int64(schema.DataSize)

	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, crcOffset); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// The querier reads raw bytes for indexing (no CRC check during scan).
	// But readAt + decodeRecord will catch the corrupt CRC.
	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	// UserID=1 should find 2 entries (first and third).
	results := q.Lookup("UserID", "1")
	if len(results) != 2 {
		t.Fatalf("expected 2 results (corrupt entry skipped), got %d", len(results))
	}

	// UserID=2 (the corrupted entry) should find 0 (decode fails on CRC).
	results = q.Lookup("UserID", "2")
	if len(results) != 0 {
		t.Fatalf("expected 0 results for corrupted entry, got %d", len(results))
	}
}

func TestQuerierSnapshotSemantics(t *testing.T) {
	path := tempPath(t)
	s, err := New[queryRecord](path, WithCommitInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.Append(queryRecord{UserID: "42", Event: "before"})
	s.Close()

	// Open querier — takes snapshot.
	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	// Append more data after querier was created.
	s2, err := New[queryRecord](path, WithCommitInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s2.Append(queryRecord{UserID: "42", Event: "after"})
	s2.Close()

	// Querier should only see the entry from before.
	results := q.Lookup("UserID", "42")
	if len(results) != 1 {
		t.Fatalf("expected 1 result (snapshot), got %d", len(results))
	}
	if results[0].Event != "before" {
		t.Errorf("expected Event=before, got %q", results[0].Event)
	}
}

func TestQuerierEmptyLog(t *testing.T) {
	path := writeTestLog[queryRecord](t) // no entries

	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	results := q.Lookup("UserID", "anything")
	if len(results) != 0 {
		t.Fatalf("expected 0 results from empty log, got %d", len(results))
	}
}

func TestQuerierMeta(t *testing.T) {
	path := tempPath(t)
	s, err := New[queryRecord](path, WithCommitInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.SetMeta("version", "1.0")
	s.Append(queryRecord{UserID: "x", Event: "y"})
	s.Close()

	q, err := NewQuerier[queryRecord](path, Index("UserID"))
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	meta := q.Meta()
	if meta["version"] != "1.0" {
		t.Errorf("expected version=1.0, got %v", meta["version"])
	}
}

func TestQuerierRequiresAtLeastOneIndex(t *testing.T) {
	path := writeTestLog(t, queryRecord{UserID: "x"})

	_, err := NewQuerier[queryRecord](path)
	if err == nil {
		t.Fatal("expected error when no Index provided")
	}
}

type bucketRecord struct {
	ID     string `sl:"16"`
	Bucket string `sl:"16"`
}

func TestQuerierManyEntries(t *testing.T) {
	entries := make([]bucketRecord, 1000)
	for i := range entries {
		entries[i] = bucketRecord{
			ID:     fmt.Sprintf("%d", i),
			Bucket: fmt.Sprintf("%d", i%10),
		}
	}
	path := writeTestLog(t, entries...)

	q, err := NewQuerier[bucketRecord](path,
		Index("ID"),
		Index("Bucket"),
	)
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()

	// Each ID is unique.
	for i := 0; i < 1000; i++ {
		results := q.Lookup("ID", fmt.Sprintf("%d", i))
		if len(results) != 1 {
			t.Fatalf("ID=%d: expected 1 result, got %d", i, len(results))
		}
	}

	// Each bucket has 100 entries.
	for b := 0; b < 10; b++ {
		results := q.Lookup("Bucket", fmt.Sprintf("%d", b))
		if len(results) != 100 {
			t.Fatalf("Bucket=%d: expected 100 results, got %d", b, len(results))
		}
	}
}

type hugeRecord struct {
	ID      string `sl:"16"`
	SiteID  string `sl:"12"`
	Status  string `sl:"10"`
	Region  string `sl:"12"`
	Seq     uint64
	Payload string `sl:"56"`
}

func TestQuerierHugeVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping huge volume test in short mode")
	}

	const totalEntries = 500_000
	const numSites = 1_000
	const numStatuses = 5
	const numRegions = 50

	statuses := []string{"ok", "failed", "pending", "timeout", "cancelled"}
	regions := make([]string, numRegions)
	for i := range regions {
		regions[i] = fmt.Sprintf("region-%03d", i)
	}

	// Write 500K entries.
	path := tempPath(t)
	s, err := New[hugeRecord](path, WithCommitInterval(10*time.Millisecond), WithMaxQueueSize(totalEntries+1000))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < totalEntries; i++ {
		entry := hugeRecord{
			ID:      fmt.Sprintf("evt-%d", i),
			SiteID:  fmt.Sprintf("site-%04d", i%numSites),
			Status:  statuses[i%numStatuses],
			Region:  regions[i%numRegions],
			Seq:     uint64(i),
			Payload: fmt.Sprintf("data-block-%d-padding-to-make-entries-bigger", i),
		}
		if err := s.Append(entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Check file size.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	fileSizeMB := float64(info.Size()) / (1024 * 1024)
	t.Logf("Log file size: %.2f MB (%d entries)", fileSizeMB, totalEntries)

	schema, _ := buildSchema[hugeRecord]()
	t.Logf("Record size: %d bytes (data: %d)", schema.RecordSize, schema.DataSize)

	if fileSizeMB > 65 {
		t.Errorf("file size %.2f MB exceeds 65 MB target (was 78 MB with JSON)", fileSizeMB)
	}

	// Build querier with 3 indexes.
	buildStart := time.Now()
	q, err := NewQuerier[hugeRecord](path,
		Index("SiteID"),
		Index("Status"),
		Index("Region"),
	)
	if err != nil {
		t.Fatalf("NewQuerier: %v", err)
	}
	defer q.Close()
	buildDuration := time.Since(buildStart)
	t.Logf("Index build time (3 indexes, %d entries): %v", totalEntries, buildDuration)

	// --- Correctness: verify counts across all dimensions ---

	// Each site has totalEntries/numSites = 500 entries.
	expectedPerSite := totalEntries / numSites
	for i := 0; i < numSites; i++ {
		key := fmt.Sprintf("site-%04d", i)
		c := q.Count("SiteID", key)
		if c != expectedPerSite {
			t.Fatalf("SiteID=%s: expected count=%d, got %d", key, expectedPerSite, c)
		}
	}
	t.Logf("Verified %d SiteID keys, each with %d entries", numSites, expectedPerSite)

	// Each status has totalEntries/numStatuses = 100_000 entries.
	expectedPerStatus := totalEntries / numStatuses
	for _, status := range statuses {
		c := q.Count("Status", status)
		if c != expectedPerStatus {
			t.Fatalf("Status=%s: expected count=%d, got %d", status, expectedPerStatus, c)
		}
	}
	t.Logf("Verified %d Status keys, each with %d entries", numStatuses, expectedPerStatus)

	// Each region has totalEntries/numRegions = 10_000 entries.
	expectedPerRegion := totalEntries / numRegions
	for _, region := range regions {
		c := q.Count("Region", region)
		if c != expectedPerRegion {
			t.Fatalf("Region=%s: expected count=%d, got %d", region, expectedPerRegion, c)
		}
	}
	t.Logf("Verified %d Region keys, each with %d entries", numRegions, expectedPerRegion)

	// --- Correctness: spot-check actual records ---

	results := q.Lookup("SiteID", "site-0000")
	if len(results) != expectedPerSite {
		t.Fatalf("site-0000 lookup: expected %d results, got %d", expectedPerSite, len(results))
	}
	for _, rec := range results {
		if rec.SiteID != "site-0000" {
			t.Fatalf("wrong SiteID in result: %q", rec.SiteID)
		}
	}

	// --- Performance: measure lookup latency ---

	lookupStart := time.Now()
	const lookupRounds = 100
	for i := 0; i < lookupRounds; i++ {
		key := fmt.Sprintf("site-%04d", i)
		results := q.Lookup("SiteID", key)
		if len(results) != expectedPerSite {
			t.Fatalf("lookup round %d: expected %d, got %d", i, expectedPerSite, len(results))
		}
	}
	lookupDuration := time.Since(lookupStart)
	t.Logf("Lookup latency (SiteID, %d results each, avg over %d lookups): %v",
		expectedPerSite, lookupRounds, lookupDuration/lookupRounds)

	// Lookup a low-cardinality key (status, 100K results).
	lookupStart = time.Now()
	results = q.Lookup("Status", "failed")
	lookupDuration = time.Since(lookupStart)
	if len(results) != expectedPerStatus {
		t.Fatalf("Status=failed: expected %d, got %d", expectedPerStatus, len(results))
	}
	t.Logf("Lookup latency (Status=failed, %d results): %v", expectedPerStatus, lookupDuration)

	// Lookup a non-existent key — should be fast.
	lookupStart = time.Now()
	for i := 0; i < 10_000; i++ {
		results = q.Lookup("SiteID", "nonexistent-site")
		if len(results) != 0 {
			t.Fatal("expected 0 results for nonexistent key")
		}
	}
	lookupDuration = time.Since(lookupStart)
	t.Logf("Miss latency (10,000 lookups for nonexistent key): %v total, %v avg",
		lookupDuration, lookupDuration/10_000)
}
