package plantapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"bluei.kr/edge/internal/plantdoc"
	"bluei.kr/edge/internal/plantform"
	"bluei.kr/edge/internal/storage"
)

// 공장 기록 양식. 기본 양식 묶음에서 골라 받아 공장 판으로 쓰고, 빈 양식을 PDF 로 뽑는다.
// 양식을 고치는 것과 쓰던 양식을 가져오는 것은 다음 단계다 — docs/56 §18.

// GET /v1/forms/defaults — 기본 양식 묶음.
func (s *Server) handleFormDefaults(w http.ResponseWriter, r *http.Request) {
	packs, err := plantform.Packs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FORM_PACK_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packs": packs, "count": len(packs)})
}

func formToMap(pf *storage.PlantForm, withContent bool) (map[string]any, error) {
	var f plantform.Form
	if err := json.Unmarshal([]byte(pf.Content), &f); err != nil {
		return nil, fmt.Errorf("form %s: %w", pf.FormID, err)
	}
	m := map[string]any{
		"form_id": pf.FormID, "revision": pf.Revision, "source": pf.Source, "base_pack": pf.BasePack, "base_id": pf.BaseID,
		"note": pf.Note, "created_at": pf.CreatedAt,
		"name": f.Name, "kind": f.Kind, "cycle": f.Cycle, "owner": f.Owner, "covers": f.Covers,
	}
	if withContent {
		m["form"] = f
	}
	return m, nil
}

// /v1/plant/{site_id}/forms[/{form_id}[/blank.pdf]]
func (s *Server) handlePlantForms(w http.ResponseWriter, r *http.Request, siteID string, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		s.handleListPlantForms(w, r, siteID)
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.handleAdoptPlantForms(w, r, siteID)
	case len(rest) == 1 && r.Method == http.MethodGet:
		s.handleGetPlantForm(w, r, siteID, rest[0], false)
	case len(rest) == 2 && rest[1] == "blank.pdf" && r.Method == http.MethodGet:
		s.handleGetPlantForm(w, r, siteID, rest[0], true)
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/plant/{site_id}/forms[/{form_id}[/blank.pdf]]", "")
	}
}

func (s *Server) handleListPlantForms(w http.ResponseWriter, r *http.Request, siteID string) {
	forms, err := s.store.ListPlantForms(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := []map[string]any{}
	for _, pf := range forms {
		m, err := formToMap(pf, false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "FORM_CONTENT_ERROR", err.Error(), "")
			return
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"forms": out, "count": len(out)})
}

type adoptFormsRequest struct {
	Pack    string   `json:"pack"`
	FormIDs []string `json:"form_ids"` // 비우면 묶음 전부
}

// POST — 기본 양식을 공장 판 1호로 받는다. 이미 있는 양식은 건드리지 않는다.
func (s *Server) handleAdoptPlantForms(w http.ResponseWriter, r *http.Request, siteID string) {
	var req adoptFormsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	pack, err := plantform.LoadPack(req.Pack)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FORM_PACK_ERROR", err.Error(), "")
		return
	}
	if pack == nil {
		writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_PACK", "기본 양식 묶음이 없습니다: "+req.Pack, "")
		return
	}
	all, want := len(req.FormIDs) == 0, map[string]bool{}
	for _, id := range req.FormIDs {
		want[id] = true
	}
	known := map[string]bool{}
	for _, f := range pack.Forms {
		known[f.ID] = true
	}
	for _, id := range req.FormIDs {
		if !known[id] {
			writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_FORM", "묶음에 없는 양식입니다: "+id, "")
			return
		}
	}
	ctx := r.Context()
	adopted, skipped := []string{}, []string{}
	now := time.Now().UnixMilli()
	for i := range pack.Forms {
		f := &pack.Forms[i]
		if !all && !want[f.ID] {
			continue
		}
		cur, err := s.store.GetPlantForm(ctx, siteID, f.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if cur != nil {
			skipped = append(skipped, f.ID)
			continue
		}
		b, _ := json.Marshal(f)
		if err := s.store.InsertPlantForm(ctx, &storage.PlantForm{
			SiteID: siteID, FormID: f.ID, Source: "default", BasePack: pack.ID, BaseID: f.ID,
			Content: string(b), Note: "기본 양식에서 받음", CreatedAt: now,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		adopted = append(adopted, f.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "adopted": adopted, "skipped": skipped})
}

func (s *Server) handleGetPlantForm(w http.ResponseWriter, r *http.Request, siteID, formID string, pdf bool) {
	pf, err := s.store.GetPlantForm(r.Context(), siteID, formID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if pf == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "양식이 없습니다: "+formID, "")
		return
	}
	if !pdf {
		m, err := formToMap(pf, true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "FORM_CONTENT_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, m)
		return
	}
	doc, err := s.blankFormDoc(r.Context(), pf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FORM_CONTENT_ERROR", err.Error(), "")
		return
	}
	b, err := plantdoc.Render(doc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "RENDER_ERROR", err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s-r%d.pdf"`, pf.FormID, pf.Revision))
	_, _ = w.Write(b)
}

// blankFormDoc builds the printable blank copy of a form revision.
func (s *Server) blankFormDoc(ctx context.Context, pf *storage.PlantForm) (*plantdoc.Doc, error) {
	var f plantform.Form
	if err := json.Unmarshal([]byte(pf.Content), &f); err != nil {
		return nil, err
	}
	_, plantName, err := s.plantHeader(ctx, pf.SiteID)
	if err != nil {
		return nil, err
	}
	d := &plantdoc.Doc{
		Title: f.Name, Subtitle: plantName,
		DocNo: fmt.Sprintf("양식 %s · 개정 %d", pf.FormID, pf.Revision),
		At:    time.UnixMilli(pf.CreatedAt).UTC(),
	}
	head := [][2]string{{"작성일", ""}, {"주기", f.Cycle}, {"담당", f.Owner}}
	for _, n := range f.Notes {
		head = append(head, [2]string{n.Label, n.Text})
	}
	d.Blocks = append(d.Blocks, plantdoc.Block{Kind: "kv", Pairs: head})

	switch f.Kind {
	case plantform.KindChecklist:
		var rows [][]string
		for _, sec := range f.Sections {
			for i, q := range sec.Questions {
				title := ""
				if i == 0 {
					title = sec.Title
				}
				rows = append(rows, []string{title, q.Text, "□", "□"})
			}
		}
		d.Blocks = append(d.Blocks, plantdoc.Block{Kind: "table",
			Headers: []string{"구분", "점검 내용", "예", "아니오"}, Widths: []float64{2, 12, 1, 1.4}, Rows: rows})
	case plantform.KindLog:
		headers, widths := make([]string, len(f.Columns)), make([]float64, len(f.Columns))
		for i, c := range f.Columns {
			headers[i], widths[i] = c.Name, c.Width
			if widths[i] <= 0 {
				widths[i] = 1
			}
		}
		var rows [][]string
		for _, name := range f.Rows {
			rows = append(rows, []string{name})
		}
		for i := len(rows); i < f.BlankRows; i++ {
			rows = append(rows, []string{""})
		}
		d.Blocks = append(d.Blocks, plantdoc.Block{Kind: "table", Headers: headers, Widths: widths, Rows: rows, RowLines: 2})
	case plantform.KindSheet:
		for _, fd := range f.Fields {
			lines := fd.Lines
			if lines < 1 {
				lines = 1
			}
			d.Blocks = append(d.Blocks, plantdoc.Block{Kind: "kv", Pairs: [][2]string{{fd.Label, ""}}, RowLines: lines + 1})
		}
	}
	if f.Deviation {
		d.Blocks = append(d.Blocks, plantdoc.Block{Kind: "table",
			Headers: []string{"기준 이탈 내용", "개선조치와 결과", "조치자", "확인"}, Widths: []float64{4, 5, 1.5, 1.5},
			Rows: [][]string{{""}}, RowLines: 4})
	}
	d.Blocks = append(d.Blocks, plantdoc.Block{Kind: "table", Headers: f.Signers, Widths: ones(len(f.Signers)),
		Rows: [][]string{{""}}, RowLines: 3})
	return d, nil
}

func ones(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1
	}
	return out
}
