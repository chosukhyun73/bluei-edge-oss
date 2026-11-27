package api

// 운영자 가이드북 검색 — 어시스턴트가 "이 시스템을 어떻게 쓰나"에 답할 수 있게 한다.
//
// 배경 (2026-07-28): 시스템 구조·사용법 지식이 모델 fact pack(2KB 개요)에만 있어
// 구체적인 질문("사료 보충은 어디에 적나")에 답하지 못했다. 임베딩 RAG 경로
// (internal/knowledge)는 인덱스와 임베딩 모델(bge-m3)이 배포돼 있어야 동작하는데
// 현장 기기에 없어, 문서를 그때그때 읽는 단순 검색으로 먼저 채운다.
//
// 설계 의도:
//   - 문서(docs/operator-guide/*.md)를 고치면 재시작 없이 다음 질문부터 반영된다.
//   - 섹션(## 제목) 단위로 잘라 질의어와 겹치는 섹션만 돌려준다 — 컨텍스트 절약.
//   - 임베딩 인덱스가 배포되면 knowledge 패키지 검색을 여기에 얹으면 된다.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	guideDir          = "docs/operator-guide"
	guideMaxSections  = 4
	guideMaxSectionKB = 3000 // 섹션 1건 최대 바이트
)

type guideSection struct {
	file  string
	title string
	body  string
	score int
}

// searchOperatorGuide — 질의어와 겹치는 가이드북 섹션을 찾아 텍스트로 돌려준다.
func (s *Server) searchOperatorGuide(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return `{"error":"query 가 필요합니다"}`
	}
	sections := loadGuideSections()
	if len(sections) == 0 {
		return `{"note":"가이드북 문서를 찾을 수 없습니다"}`
	}

	terms := guideTerms(query)
	for i := range sections {
		hay := sections[i].title + "\n" + sections[i].body
		for _, t := range terms {
			sections[i].score += strings.Count(hay, t)
			if strings.Contains(sections[i].title, t) {
				sections[i].score += 3 // 제목 일치 가중
			}
		}
	}
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].score > sections[j].score })

	var b strings.Builder
	hits := 0
	for _, sec := range sections {
		if sec.score == 0 || hits >= guideMaxSections {
			break
		}
		body := sec.body
		if len(body) > guideMaxSectionKB {
			body = body[:guideMaxSectionKB] + "…"
		}
		b.WriteString("## " + sec.title + "  (" + sec.file + ")\n" + body + "\n\n")
		hits++
	}
	if hits == 0 {
		return `{"note":"가이드북에서 관련 내용을 찾지 못했습니다. 확인이 필요하다고 답하세요."}`
	}
	return b.String()
}

// loadGuideSections — 가이드북 md 파일을 "## 제목" 단위 섹션으로 자른다.
func loadGuideSections() []guideSection {
	entries, err := os.ReadDir(guideDir)
	if err != nil {
		return nil
	}
	var out []guideSection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(guideDir, e.Name()))
		if err != nil {
			continue
		}
		title := strings.TrimSuffix(e.Name(), ".md")
		var body strings.Builder
		flush := func() {
			if strings.TrimSpace(body.String()) != "" {
				out = append(out, guideSection{file: e.Name(), title: title, body: strings.TrimSpace(body.String())})
			}
			body.Reset()
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "## ") {
				flush()
				title = strings.TrimSpace(strings.TrimPrefix(line, "## "))
				continue
			}
			body.WriteString(line + "\n")
		}
		flush()
	}
	return out
}

// guideTerms — 질의어를 검색 토큰으로 쪼갠다. 한국어는 조사가 붙어도 어간이 겹치도록
// 2글자 이상 어절을 그대로 쓰고, 3글자 이상이면 앞 2글자도 후보에 넣는다.
func guideTerms(query string) []string {
	seen := map[string]bool{}
	var terms []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if len([]rune(t)) < 2 || seen[t] {
			return
		}
		seen[t] = true
		terms = append(terms, t)
	}
	for _, w := range strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '?' || r == '.' || r == '!' || r == '\n'
	}) {
		add(w)
		if rs := []rune(w); len(rs) >= 3 {
			add(string(rs[:2]))
		}
	}
	return terms
}
