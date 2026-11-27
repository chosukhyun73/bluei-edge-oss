// Package plantdoc renders processing-bluei documents (HACCP plan, lot record) as
// PDF on the device itself — no network and no external renderer.
//
// A document is a list of blocks (headings, tables, the process flow,
// signature boxes). The same blocks are hashed as the document's source data,
// and the hash is printed on every page so a paper copy can be matched to the
// record kept on the device. Rendering is repeatable: the same blocks always
// give the same PDF bytes.
// References: docs/54-haccp-process-flow-ccp-design.md §17.4, §26.
package plantdoc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-pdf/fpdf"
)

// Doc is one document.
type Doc struct {
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle"`
	DocNo    string    `json:"doc_no"`
	Blocks   []Block   `json:"blocks"`
	At       time.Time `json:"at"` // 문서 기준 시각(게시·마감 시각). PDF 작성일로 쓴다
}

// Block is one piece of a document. Exactly one field group is used, by Kind.
type Block struct {
	Kind     string      `json:"kind"` // heading | para | kv | table | flow | sign
	Text     string      `json:"text,omitempty"`
	Pairs    [][2]string `json:"pairs,omitempty"`     // kv
	Headers  []string    `json:"headers,omitempty"`   // table
	Widths   []float64   `json:"widths,omitempty"`    // table — 열 너비 비율
	Rows     [][]string  `json:"rows,omitempty"`      // table
	RowLines int         `json:"row_lines,omitempty"` // table, kv — 행의 최소 줄 수(손으로 적을 빈 칸)
	Steps    []FlowStep  `json:"steps,omitempty"`     // flow
	Links    []FlowLink  `json:"links,omitempty"`     // flow
	Signers  []Signer    `json:"signers,omitempty"`   // sign
}

type FlowStep struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mark string `json:"mark"` // CCP 번호 등
}

type FlowLink struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Mark   string `json:"mark"`
	Rework bool   `json:"rework"`
}

// Signer is one signature box: the role, and who signed in the system (if anyone).
type Signer struct {
	Role string `json:"role"`
	Name string `json:"name"`
	At   string `json:"at"`
}

// DataHash is the hash of the document's source data.
func (d *Doc) DataHash() string {
	b, _ := json.Marshal(d)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ErrNoFont — 한글 글꼴을 찾지 못했다.
var ErrNoFont = errors.New("한글 글꼴(TTF)을 찾지 못했습니다. PROCESSING_BLUEI_FONT 에 글꼴 파일 경로를 지정하십시오")

// fontCandidates: 기기에 설치된 한글 TTF. 글꼴 파일은 저장소에 싣지 않는다.
var fontCandidates = []string{
	"/usr/share/fonts/truetype/nanum/NanumGothic.ttf", // Ubuntu: apt install fonts-nanum
	"/usr/share/fonts/nanum/NanumGothic.ttf",
	"/usr/share/fonts/truetype/noto/NotoSansKR-Regular.ttf",
	"/Library/Fonts/NanumGothic.ttf",
	"/System/Library/Fonts/Supplemental/AppleGothic.ttf", // macOS
}

func loadFont() ([]byte, error) {
	paths := fontCandidates
	if p := os.Getenv("PROCESSING_BLUEI_FONT"); p != "" {
		paths = []string{p}
	}
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, ErrNoFont
}

const (
	pageW, marginX = 210.0, 15.0
	contentW       = pageW - 2*marginX
	lineH          = 4.6
)

// Render returns the PDF of d.
func Render(d *Doc) ([]byte, error) {
	font, err := loadFont()
	if err != nil {
		return nil, err
	}
	hash := d.DataHash()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCatalogSort(true)
	pdf.SetCreationDate(d.At)
	pdf.SetModificationDate(d.At)
	pdf.SetTitle(d.Title, true)
	pdf.SetCreator("processing-bluei", true)
	pdf.AddUTF8FontFromBytes("kr", "", font)
	pdf.SetMargins(marginX, 15, marginX)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("kr", "", 6.5)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(contentW-20, 3.5, d.DocNo, "", 0, "L", false, 0, "")
		pdf.CellFormat(20, 3.5, fmt.Sprintf("%d / {nb}", pdf.PageNo()), "", 1, "R", false, 0, "")
		pdf.CellFormat(contentW, 3.5, "원본 데이터 해시 "+hash, "", 0, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})
	pdf.AliasNbPages("")
	pdf.AddPage()

	pdf.SetFont("kr", "", 17)
	pdf.MultiCell(contentW, 9, d.Title, "", "L", false)
	if d.Subtitle != "" {
		pdf.SetFont("kr", "", 10)
		pdf.SetTextColor(80, 80, 80)
		pdf.MultiCell(contentW, 5.5, d.Subtitle, "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(3)

	for _, b := range d.Blocks {
		switch b.Kind {
		case "heading":
			if pdf.GetY() > 250 {
				pdf.AddPage()
			}
			pdf.Ln(3)
			pdf.SetFont("kr", "", 12)
			pdf.CellFormat(contentW, 7, b.Text, "B", 1, "L", false, 0, "")
			pdf.Ln(2)
		case "para":
			pdf.SetFont("kr", "", 9)
			pdf.MultiCell(contentW, lineH, b.Text, "", "L", false)
			pdf.Ln(1.5)
		case "kv":
			rows := make([][]string, len(b.Pairs))
			for i, p := range b.Pairs {
				rows[i] = []string{p[0], p[1]}
			}
			table(pdf, nil, []float64{1, 3}, rows, true, b.RowLines)
		case "table":
			table(pdf, b.Headers, b.Widths, b.Rows, false, b.RowLines)
		case "flow":
			flow(pdf, b.Steps, b.Links)
		case "sign":
			signatures(pdf, b.Signers)
		}
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// table draws rows with wrapped cells. Rows never split across pages.
func table(pdf *fpdf.Fpdf, headers []string, widths []float64, rows [][]string, labelFirst bool, minLines int) {
	var sum float64
	for _, w := range widths {
		sum += w
	}
	col := make([]float64, len(widths))
	for i, w := range widths {
		col[i] = contentW * w / sum
	}
	pdf.SetFont("kr", "", 8)
	drawRow := func(cells []string, shadeAll bool) {
		lines := 1
		if !shadeAll && minLines > lines {
			lines = minLines
		}
		split := make([][]string, len(col))
		for i := range col {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			split[i] = pdf.SplitText(text, col[i]-2)
			if len(split[i]) > lines {
				lines = len(split[i])
			}
		}
		h := float64(lines)*lineH + 1.6
		if pdf.GetY()+h > 297-18 {
			pdf.AddPage()
		}
		x, y := marginX, pdf.GetY()
		for i := range col {
			shade := shadeAll || (labelFirst && i == 0)
			if shade {
				pdf.SetFillColor(238, 240, 243)
			}
			style := "D"
			if shade {
				style = "FD"
			}
			pdf.Rect(x, y, col[i], h, style)
			for j, ln := range split[i] {
				pdf.Text(x+1, y+0.8+lineH*float64(j+1)-1.2, ln)
			}
			x += col[i]
		}
		pdf.SetY(y + h)
	}
	if len(headers) > 0 {
		drawRow(headers, true)
	}
	for _, r := range rows {
		drawRow(r, false)
	}
	if len(rows) == 0 && len(headers) > 0 {
		drawRow([]string{"(없음)"}, false)
	}
	pdf.Ln(2)
}

// flow draws the process flow: steps in flow order, four to a row.
func flow(pdf *fpdf.Fpdf, steps []FlowStep, links []FlowLink) {
	const cols, boxW, boxH, gapX, gapY, rowGap = 4, 37.0, 13.0, 10.6, 3.0, 9.0
	index := map[string]int{}
	for i, s := range steps {
		index[s.ID] = i
	}
	layer := make([]int, len(steps))
	var forward []FlowLink
	for _, l := range links {
		_, a := index[l.From]
		_, b := index[l.To]
		if a && b && !l.Rework {
			forward = append(forward, l)
		}
	}
	for pass := 0; pass < len(steps); pass++ {
		changed := false
		for _, l := range forward {
			a, b := index[l.From], index[l.To]
			if layer[b] < layer[a]+1 && layer[a]+1 < len(steps) {
				layer[b], changed = layer[a]+1, true
			}
		}
		if !changed {
			break
		}
	}
	// 중간에 합류하는 흐름은 합류하는 단계 바로 앞에 놓는다.
	incoming := map[string]bool{}
	for _, l := range forward {
		incoming[l.To] = true
	}
	for i, s := range steps {
		if i == 0 || incoming[s.ID] {
			continue
		}
		min := -1
		for _, l := range forward {
			if l.From == s.ID && (min < 0 || layer[index[l.To]] < min) {
				min = layer[index[l.To]]
			}
		}
		if min > 0 {
			layer[i] = min - 1
		}
	}
	type pos struct{ row, col, stack int }
	place := make([]pos, len(steps))
	stackOf := map[int]int{}
	rowStacks := map[int]int{}
	rows := 0
	for i := range steps {
		p := pos{layer[i] / cols, layer[i] % cols, stackOf[layer[i]]}
		stackOf[layer[i]]++
		place[i] = p
		if p.stack+1 > rowStacks[p.row] {
			rowStacks[p.row] = p.stack + 1
		}
		if p.row+1 > rows {
			rows = p.row + 1
		}
	}
	rowTop := make([]float64, rows+1)
	for r := 0; r < rows; r++ {
		rowTop[r+1] = rowTop[r] + float64(rowStacks[r])*(boxH+gapY) - gapY + rowGap
	}
	if pdf.GetY()+rowTop[rows] > 297-18 {
		pdf.AddPage()
	}
	top := pdf.GetY()
	xy := func(i int) (float64, float64) {
		p := place[i]
		return marginX + float64(p.col)*(boxW+gapX), top + rowTop[p.row] + float64(p.stack)*(boxH+gapY)
	}
	arrow := func(x, y, dx, dy float64) { // 화살촉: (x,y) 가 끝점, (dx,dy) 가 진행 방향
		pdf.Polygon([]fpdf.PointType{{X: x, Y: y}, {X: x - 2*dx - 1*dy, Y: y - 2*dy - 1*dx}, {X: x - 2*dx + 1*dy, Y: y - 2*dy + 1*dx}}, "F")
	}
	pdf.SetDrawColor(90, 90, 90)
	pdf.SetFillColor(90, 90, 90)
	pdf.SetFont("kr", "", 6.5)
	for _, l := range links {
		a, okA := index[l.From]
		b, okB := index[l.To]
		if !okA || !okB {
			continue
		}
		if l.Rework {
			pdf.SetDashPattern([]float64{1.2, 1}, 0)
		}
		ax, ay := xy(a)
		bx, by := xy(b)
		var mx, my float64
		if place[a].row == place[b].row && place[b].col == place[a].col+1 {
			x1, y1, x2, y2 := ax+boxW, ay+boxH/2, bx, by+boxH/2
			pdf.Line(x1, y1, x2, y2)
			arrow(x2, y2, 1, 0)
			mx, my = (x1+x2)/2, (y1+y2)/2
		} else {
			// 줄이 바뀌는 연결: 아래 통로로 내려가 옆으로 간 뒤 대상에 붙는다.
			lane := top + rowTop[place[a].row+1] - rowGap/2
			x1, x2 := ax+boxW/2, bx+boxW/2
			y2, dy := by, 1.0
			if place[b].row <= place[a].row {
				y2, dy = by+boxH, -1.0
			}
			pdf.Line(x1, ay+boxH, x1, lane)
			pdf.Line(x1, lane, x2, lane)
			pdf.Line(x2, lane, x2, y2)
			arrow(x2, y2, 0, dy)
			mx, my = (x1+x2)/2, lane
		}
		pdf.SetDashPattern([]float64{}, 0)
		if l.Mark != "" {
			w := pdf.GetStringWidth(l.Mark) + 2
			pdf.SetFillColor(255, 255, 255)
			pdf.Rect(mx-w/2, my-2, w, 4, "FD")
			pdf.SetFillColor(90, 90, 90)
			pdf.Text(mx-w/2+1, my+1, l.Mark)
		}
	}
	pdf.SetDrawColor(0, 0, 0)
	for i, s := range steps {
		x, y := xy(i)
		if s.Mark != "" {
			pdf.SetLineWidth(0.7)
			pdf.SetFillColor(245, 232, 232)
		} else {
			pdf.SetLineWidth(0.2)
			pdf.SetFillColor(255, 255, 255)
		}
		pdf.Rect(x, y, boxW, boxH, "FD")
		pdf.SetLineWidth(0.2)
		pdf.SetFont("kr", "", 8)
		lines := pdf.SplitText(fmt.Sprintf("%d. %s", i+1, s.Name), boxW-2)
		for j, ln := range lines {
			if j < 2 {
				pdf.Text(x+1, y+4+float64(j)*3.6, ln)
			}
		}
		if s.Mark != "" {
			pdf.SetFont("kr", "", 6.5)
			pdf.Text(x+1, y+boxH-1.2, s.Mark)
		}
	}
	pdf.SetY(top + rowTop[rows])
	pdf.SetFont("kr", "", 7)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(contentW, 4, "굵은 테두리 = CCP 가 있는 단계. 화살표 위 표시 = 연결(이송·대기) CCP 와 대기 한도. 점선 = 재작업.", "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(2)
}

// signatures draws signature boxes: who signed in the system, and a blank
// space for a handwritten signature on the printed copy.
func signatures(pdf *fpdf.Fpdf, signers []Signer) {
	if len(signers) == 0 {
		return
	}
	const boxH = 26.0
	if pdf.GetY()+boxH > 297-18 {
		pdf.AddPage()
	}
	w := contentW / float64(len(signers))
	x, y := marginX, pdf.GetY()
	for _, s := range signers {
		pdf.Rect(x, y, w, boxH, "D")
		pdf.SetFillColor(238, 240, 243)
		pdf.Rect(x, y, w, 6, "FD")
		pdf.SetFont("kr", "", 8)
		pdf.Text(x+1.5, y+4.2, s.Role)
		pdf.SetFont("kr", "", 7.5)
		name := s.Name
		if name == "" {
			name = "(전자 서명 없음)"
		}
		pdf.Text(x+1.5, y+10.5, "전자 서명: "+name)
		if s.At != "" {
			pdf.Text(x+1.5, y+14.2, s.At)
		}
		pdf.SetTextColor(120, 120, 120)
		pdf.Text(x+1.5, y+boxH-2, "자필 서명 · 날짜")
		pdf.SetTextColor(0, 0, 0)
		x += w
	}
	pdf.SetY(y + boxH + 3)
}
