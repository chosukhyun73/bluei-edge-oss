package retention

import (
	"bufio"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

type fakeRow struct {
	seq       int64
	eventType string
	recAt     time.Time
	json      string
	metric    string
}

type fakeStore struct {
	rows []fakeRow
}

// matchMetric mirrors storage.metricPredicate semantics: Include 는 해당 metric 만,
// Exclude 는 해당 metric 을 제외 (metric 없는 행은 Exclude 필터를 통과).
func matchMetric(f storage.MetricFilter, metric string) bool {
	if len(f.Include) > 0 {
		found := false
		for _, m := range f.Include {
			if m == metric {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	for _, m := range f.Exclude {
		if m == metric {
			return false
		}
	}
	return true
}

func (f *fakeStore) SelectEventsOlderThan(_ context.Context, eventType string, filter storage.MetricFilter, cutoff time.Time, limit int) ([]storage.ArchivableEvent, error) {
	var out []storage.ArchivableEvent
	for _, r := range f.rows { // f.rows kept in ascending seq order
		if r.eventType == eventType && matchMetric(filter, r.metric) && r.recAt.Before(cutoff) {
			out = append(out, storage.ArchivableEvent{Sequence: r.seq, RecordedAt: r.recAt, EventJSON: r.json})
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeStore) DeleteEventsOlderThanUpToSeq(_ context.Context, eventType string, filter storage.MetricFilter, cutoff time.Time, maxSeq int64) (int64, error) {
	var kept []fakeRow
	var n int64
	for _, r := range f.rows {
		if r.eventType == eventType && matchMetric(filter, r.metric) && r.recAt.Before(cutoff) && r.seq <= maxSeq {
			n++
			continue
		}
		kept = append(kept, r)
	}
	f.rows = kept
	return n, nil
}

func readArchive(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive %s: %v", path, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	var lines []string
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan archive: %v", err)
	}
	return lines
}

func TestArchiveAndPrune_MonthlyPartitionAndDrain(t *testing.T) {
	dir := t.TempDir()
	apr := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	may := time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)

	store := &fakeStore{rows: []fakeRow{
		{seq: 1, eventType: "sensor.reading.recorded", recAt: apr, json: `{"event_id":"a1"}`},
		{seq: 2, eventType: "sensor.reading.recorded", recAt: apr, json: `{"event_id":"a2"}`},
		{seq: 3, eventType: "sensor.reading.recorded", recAt: may, json: `{"event_id":"m1"}`},
		{seq: 4, eventType: "sensor.reading.recorded", recAt: may, json: `{"event_id":"m2"}`},
		{seq: 5, eventType: "sensor.reading.recorded", recAt: may, json: `{"event_id":"m3"}`},
		{seq: 6, eventType: "sensor.reading.recorded", recAt: recent, json: `{"event_id":"keep"}`}, // after cutoff → kept
		{seq: 7, eventType: "device.health.updated", recAt: apr, json: `{"event_id":"h1"}`},        // different type → untouched
	}}

	w := NewWorker(store, Config{
		Enabled:    true,
		ArchiveDir: dir,
		BatchSize:  2, // force pagination across batches
		Rules:      []Rule{{EventType: "sensor.reading.recorded", KeepDays: 30}},
	})

	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	n, err := w.archiveAndPrune(context.Background(), "sensor.reading.recorded", storage.MetricFilter{}, cutoff)
	if err != nil {
		t.Fatalf("archiveAndPrune: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 pruned, got %d", n)
	}

	// Monthly partition files
	aprLines := readArchive(t, filepath.Join(dir, "events-2026-04.jsonl.gz"))
	if len(aprLines) != 2 || !strings.Contains(aprLines[0], "a1") || !strings.Contains(aprLines[1], "a2") {
		t.Fatalf("april archive wrong: %v", aprLines)
	}
	mayLines := readArchive(t, filepath.Join(dir, "events-2026-05.jsonl.gz"))
	if len(mayLines) != 3 {
		t.Fatalf("may archive expected 3 lines, got %d: %v", len(mayLines), mayLines)
	}

	// Live store: only the recent sensor row + the device.health row remain
	if len(store.rows) != 2 {
		t.Fatalf("expected 2 rows kept, got %d: %+v", len(store.rows), store.rows)
	}
	for _, r := range store.rows {
		if r.eventType == "sensor.reading.recorded" && r.seq != 6 {
			t.Fatalf("unexpected sensor row kept: %+v", r)
		}
	}
}

func TestArchiveAndPrune_NoExpiredIsNoop(t *testing.T) {
	dir := t.TempDir()
	store := &fakeStore{rows: []fakeRow{
		{seq: 1, eventType: "sensor.reading.recorded", recAt: time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC), json: `{"event_id":"keep"}`},
	}}
	w := NewWorker(store, Config{Enabled: true, ArchiveDir: dir, BatchSize: 100})

	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	n, err := w.archiveAndPrune(context.Background(), "sensor.reading.recorded", storage.MetricFilter{}, cutoff)
	if err != nil {
		t.Fatalf("archiveAndPrune: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 pruned, got %d", n)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected row kept, got %d", len(store.rows))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no archive files, got %d", len(entries))
	}
}

// Metric 필터: exclude 룰(수온 제외 1일)이 수온 행을 남기고, include 룰(수온 365일)이
// 수온만 대상으로 하는지 — 이중 룰 분리 동작 검증.
func TestArchiveAndPrune_MetricFilter(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{rows: []fakeRow{
		{seq: 1, eventType: "sensor.reading.recorded", metric: "water_temperature", recAt: old, json: `{"event_id":"t1"}`},
		{seq: 2, eventType: "sensor.reading.recorded", metric: "ph", recAt: old, json: `{"event_id":"p1"}`},
		{seq: 3, eventType: "sensor.reading.recorded", metric: "dissolved_oxygen", recAt: old, json: `{"event_id":"d1"}`},
	}}
	w := NewWorker(store, Config{Enabled: true, ArchiveDir: dir, BatchSize: 100})

	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	n, err := w.archiveAndPrune(context.Background(), "sensor.reading.recorded",
		storage.MetricFilter{Exclude: []string{"water_temperature"}}, cutoff)
	if err != nil {
		t.Fatalf("archiveAndPrune(exclude): %v", err)
	}
	if n != 2 {
		t.Fatalf("exclude: expected 2 pruned, got %d", n)
	}
	if len(store.rows) != 1 || store.rows[0].metric != "water_temperature" {
		t.Fatalf("exclude: expected only temp row kept, got %+v", store.rows)
	}

	n, err = w.archiveAndPrune(context.Background(), "sensor.reading.recorded",
		storage.MetricFilter{Include: []string{"water_temperature"}}, cutoff)
	if err != nil {
		t.Fatalf("archiveAndPrune(include): %v", err)
	}
	if n != 1 || len(store.rows) != 0 {
		t.Fatalf("include: expected temp row pruned, got n=%d rows=%+v", n, store.rows)
	}
}

// gzip 멤버가 연결(append)되어도 reader 가 전부 읽는지 확인 — 백업 append 안정성.
func TestArchiveAppend_ConcatenatedMembers(t *testing.T) {
	dir := t.TempDir()
	store := &fakeStore{}
	w := NewWorker(store, Config{Enabled: true, ArchiveDir: dir, BatchSize: 100})

	m := []storage.ArchivableEvent{{Sequence: 1, RecordedAt: time.Now(), EventJSON: `{"e":1}`}}
	if err := w.appendMonth("2026-04", m); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	m2 := []storage.ArchivableEvent{{Sequence: 2, RecordedAt: time.Now(), EventJSON: `{"e":2}`}}
	if err := w.appendMonth("2026-04", m2); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	lines := readArchive(t, filepath.Join(dir, "events-2026-04.jsonl.gz"))
	if len(lines) != 2 || lines[0] != `{"e":1}` || lines[1] != `{"e":2}` {
		t.Fatalf("concatenated members not read fully: %v", lines)
	}
}
