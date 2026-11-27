package plantdoc

import (
	"bytes"
	"testing"
	"time"
)

func sample() *Doc {
	return &Doc{Title: "HACCP 관리계획", Subtitle: "예시 가공공장 · 버전 1", DocNo: "PLAN-1",
		At: time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC),
		Blocks: []Block{
			{Kind: "heading", Text: "제품설명서"},
			{Kind: "kv", Pairs: [][2]string{{"제품명", "냉장 방어 로인"}, {"보관", "0~4℃ 냉장"}}},
			{Kind: "flow", Steps: []FlowStep{{"a", "원료 입고", "CCP-1C"}, {"b", "냉장 보관", ""}, {"c", "손질", ""}, {"d", "포장", ""}, {"e", "출하", ""}, {"p", "포장재 입고", ""}},
				Links: []FlowLink{{"a", "b", "", false}, {"b", "c", "CCP-3C · 대기 ≤30분", false}, {"c", "d", "", false}, {"d", "e", "", false}, {"p", "d", "", false}}},
			{Kind: "table", Headers: []string{"CCP", "한계기준", "개선조치"}, Widths: []float64{1, 2, 3},
				Rows: [][]string{{"CCP-1C", "중심온도 4.4℃ 이하", "기준을 넘은 lot 은 받지 않거나 따로 두고 히스타민 검사 후 처분한다. 공급자에게 알린다."}}},
			{Kind: "sign", Signers: []Signer{{"현장 확인", "이검토", "2026-11-18 09:00"}, {"게시", "김관리", "2026-11-18 09:01"}, {"HACCP 팀장", "", ""}}},
		}}
}

func TestRenderRepeatable(t *testing.T) {
	a, err := Render(sample())
	if err == ErrNoFont {
		t.Skip("한글 글꼴이 없는 환경")
	}
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Render(sample())
	if !bytes.HasPrefix(a, []byte("%PDF")) || len(a) < 2000 {
		t.Fatalf("PDF 가 아님 (%d bytes)", len(a))
	}
	if !bytes.Equal(a, b) {
		t.Fatal("같은 내용은 같은 PDF 가 나와야 한다")
	}
	changed := sample()
	changed.Blocks[1].Pairs[0][1] = "냉장 방어 필렛"
	if changed.DataHash() == sample().DataHash() {
		t.Fatal("내용이 바뀌면 원본 데이터 해시가 바뀌어야 한다")
	}
}
