// Package knowledge — bluei 지식팩 RAG 검색(임베딩 인덱스 기반).
//
// 어시스턴트 질의에 관련 지식 청크를 top-k 로 찾아 컨텍스트로 제공한다.
// 안전 경계: 본 패키지가 반환하는 자료는 어시스턴트 설명용 참고자료(advisory)이며,
// 실시간 안전 판단(Arbiter)·급이 결정(LRCN/룰)에는 관여하지 않는다.
//
// 인덱스 파일: build-rag-index.py 가 만든 rag-index.jsonl
// (청크 1줄 = {chunk_id, source_id, title, path, text, vector}).
package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// Chunk — 인덱스의 한 청크(텍스트 + 임베딩 벡터).
type Chunk struct {
	ChunkID  string    `json:"chunk_id"`
	SourceID string    `json:"source_id"`
	Title    string    `json:"title"`
	Path     string    `json:"path"`
	Text     string    `json:"text"`
	Vector   []float64 `json:"vector"`
}

// EmbedFunc — 질의 텍스트 → 임베딩 벡터 (보통 llm.Client.Embed 바인딩).
type EmbedFunc func(ctx context.Context, text string) ([]float64, error)

// Retriever — 메모리에 적재된 인덱스로 코사인 top-k 검색.
type Retriever struct {
	chunks []Chunk
	embed  EmbedFunc
	topK   int
}

// Hit — 검색 결과 한 건.
type Hit struct {
	Chunk
	Score float64
}

// Load — rag-index.jsonl 을 읽어 청크 목록 반환.
func Load(path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []Chunk
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // 긴 줄(벡터 768~1024) 대비
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c Chunk
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("knowledge: parse line: %w", err)
		}
		if len(c.Vector) > 0 {
			chunks = append(chunks, c)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// New — 적재된 청크 + 임베딩 함수로 Retriever 생성. topK<=0 이면 4.
func New(chunks []Chunk, embed EmbedFunc, topK int) *Retriever {
	if topK <= 0 {
		topK = 4
	}
	return &Retriever{chunks: chunks, embed: embed, topK: topK}
}

// Count — 적재된 청크 수 (nil-safe).
func (r *Retriever) Count() int {
	if r == nil {
		return 0
	}
	return len(r.chunks)
}

// Retrieve — query 임베딩 후 코사인 top-k 청크 반환 (nil-safe).
func (r *Retriever) Retrieve(ctx context.Context, query string) ([]Hit, error) {
	if r == nil || len(r.chunks) == 0 {
		return nil, nil
	}
	qv, err := r.embed(ctx, query)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(r.chunks))
	for _, c := range r.chunks {
		hits = append(hits, Hit{Chunk: c, Score: cosine(qv, c.Vector)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > r.topK {
		hits = hits[:r.topK]
	}
	return hits, nil
}

// RenderContext — top-k hits 를 어시스턴트 주입용 한국어 블록으로 만든다.
func RenderContext(hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[bluei 지식 참고자료 — 아래 근거로만 설명하고, 없는 내용은 모른다고 하세요]\n")
	for _, h := range hits {
		b.WriteString("\n— 출처: " + h.Title + "\n")
		b.WriteString(strings.TrimSpace(h.Text) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
