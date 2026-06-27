package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixedEmbed(v []float64) EmbedFunc {
	return func(_ context.Context, _ string) ([]float64, error) { return v, nil }
}

func sampleChunks() []Chunk {
	return []Chunk{
		{ChunkID: "a#0", SourceID: "a", Title: "감성돔", Path: "Species/a.md", Text: "수온 14-25", Vector: []float64{1, 0, 0}},
		{ChunkID: "b#0", SourceID: "b", Title: "참돔", Path: "Species/b.md", Text: "FCR 1.2", Vector: []float64{0, 1, 0}},
		{ChunkID: "c#0", SourceID: "c", Title: "넙치", Path: "Species/c.md", Text: "광어", Vector: []float64{0, 0, 1}},
	}
}

func TestRetrieveRanksByCosine(t *testing.T) {
	// 질의 벡터가 참돔(0,1,0) 쪽에 가깝다 → top1 = 참돔.
	r := New(sampleChunks(), fixedEmbed([]float64{0, 0.9, 0.1}), 2)
	hits, err := r.Retrieve(context.Background(), "참돔 사료")
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("topK=2 expected 2 hits, got %d", len(hits))
	}
	if hits[0].Title != "참돔" {
		t.Errorf("expected 참돔 first, got %q (score %.3f)", hits[0].Title, hits[0].Score)
	}
	if hits[0].Score < hits[1].Score {
		t.Errorf("hits not sorted desc: %.3f < %.3f", hits[0].Score, hits[1].Score)
	}
}

func TestRetrieveNilSafe(t *testing.T) {
	var r *Retriever
	if r.Count() != 0 {
		t.Errorf("nil Count should be 0")
	}
	hits, err := r.Retrieve(context.Background(), "x")
	if err != nil || hits != nil {
		t.Errorf("nil retriever should return (nil,nil), got (%v,%v)", hits, err)
	}
}

func TestRenderContextIncludesTitles(t *testing.T) {
	r := New(sampleChunks(), fixedEmbed([]float64{1, 0, 0}), 1)
	hits, _ := r.Retrieve(context.Background(), "감성돔")
	out := RenderContext(hits)
	if !strings.Contains(out, "감성돔") || !strings.Contains(out, "참고자료") {
		t.Errorf("RenderContext missing expected content: %q", out)
	}
	if RenderContext(nil) != "" {
		t.Errorf("RenderContext(nil) should be empty")
	}
}

func TestLoadJSONL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rag-index.jsonl")
	var lines []string
	for _, c := range sampleChunks() {
		b, _ := json.Marshal(c)
		lines = append(lines, string(b))
	}
	// 빈 줄/벡터 없는 줄은 무시되어야 함.
	lines = append(lines, "", `{"chunk_id":"x","title":"노벡터","vector":[]}`)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(chunks) != 3 {
		t.Errorf("expected 3 valid chunks (벡터 없는 줄 제외), got %d", len(chunks))
	}
}
