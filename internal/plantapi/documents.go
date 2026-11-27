package plantapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/plantdoc"
	"bluei.kr/edge/internal/storage"
)

// 문서 발행 — 게시된 계획과 마감된 lot 기록을 PDF 로 만들고 발행 대장에 남긴다.
// PDF 는 이 기기에서 직접 만든다. 원본 데이터(JSON)를 같이 보관하고, 둘의 해시를 대장에 적는다.
// 같은 내용을 다시 발행하면 새 문서를 만들지 않고 기존 발행본을 돌려준다.
// References: docs/54-haccp-process-flow-ccp-design.md §17.4, §26.

func fmtMs(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04:05")
}

func yesNo(b bool) string {
	if b {
		return "예"
	}
	return "아니오"
}

func (s *Server) userNames(ctx context.Context, siteID string) (map[string]string, error) {
	users, err := s.store.ListLocalUsers(ctx, siteID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, u := range users {
		out[u.UserID] = u.Name
	}
	return out, nil
}

func (s *Server) plantHeader(ctx context.Context, siteID string) ([][2]string, string, error) {
	name, address := siteID, ""
	sites, err := s.store.ListSites(ctx, "")
	if err != nil {
		return nil, "", err
	}
	for _, m := range sites {
		if m["site_id"] == siteID {
			name, _ = m["name"].(string)
			address, _ = m["address"].(string)
		}
	}
	p, err := s.store.GetPlantProfile(ctx, siteID)
	if err != nil {
		return nil, "", err
	}
	if p == nil {
		p = &storage.PlantProfile{}
	}
	return [][2]string{{"공장", name}, {"주소", address}, {"사업자번호", p.BusinessNo},
		{"영업 허가·등록", strings.TrimSpace(p.LicenseName + " " + p.LicenseNo + " (" + p.LicenseAuthority + ")")},
		{"HACCP 인증번호", p.HACCPCertNo}}, name, nil
}

var (
	sourceText = map[string]string{haccp.SourceSensor: "센서", haccp.SourceOperator: "사람 입력", haccp.SourceDerived: "계산값"}
	autoText   = map[string]string{haccp.AutoMeasure: "자동측정", haccp.AutoObserve: "자동관찰", haccp.AutoRecord: "자동기록", haccp.AutoManual: "수기"}
	statusText = map[string]string{haccp.StatusPending: "해당 없음", haccp.StatusOK: "적합", haccp.StatusInProgress: "진행 중",
		haccp.StatusNeedsInput: "입력 없음", haccp.StatusDeviation: "이탈", haccp.StatusOutage: "모니터링 불능"}
)

// planDoc builds the plan document of a published version.
func (s *Server) planDoc(ctx context.Context, plan *storage.HACCPPlan, v *storage.HACCPPlanVersion) (*plantdoc.Doc, error) {
	var c haccp.PlanContent
	if err := json.Unmarshal([]byte(v.ContentJSON), &c); err != nil {
		return nil, err
	}
	header, plantName, err := s.plantHeader(ctx, plan.SiteID)
	if err != nil {
		return nil, err
	}
	names, err := s.userNames(ctx, plan.SiteID)
	if err != nil {
		return nil, err
	}
	zones, err := s.store.ListPlantZones(ctx, plan.SiteID)
	if err != nil {
		return nil, err
	}
	zoneName := map[string]string{}
	for _, z := range zones {
		zoneName[z.ZoneID] = z.Name
	}
	stepName := map[string]string{}
	for _, sp := range c.Steps {
		stepName[sp.StepID] = sp.Name
	}
	linkName := func(id string) string {
		for _, l := range c.Links {
			if l.LinkID == id {
				return stepName[l.From] + " → " + stepName[l.To]
			}
		}
		return id
	}
	// 연결·구간 CCP 는 한국 서류에서 단계 이름으로 적는다.
	scopeText := func(k haccp.CCP) string {
		if k.ExportStepName != "" {
			return k.ExportStepName
		}
		var parts []string
		for _, id := range k.StepIDs {
			parts = append(parts, stepName[id])
		}
		for _, id := range k.LinkIDs {
			parts = append(parts, linkName(id))
		}
		return strings.Join(parts, ", ")
	}

	d := &plantdoc.Doc{Title: "HACCP 관리계획 — " + plan.Name,
		Subtitle: fmt.Sprintf("%s · 버전 %d", plantName, v.VersionNo), DocNo: fmt.Sprintf("PLAN-%s-v%d", plan.PlanID[len(plan.PlanID)-8:], v.VersionNo)}
	if v.PublishedAt != nil {
		d.At = v.PublishedAt.UTC()
	}
	add := func(b plantdoc.Block) { d.Blocks = append(d.Blocks, b) }
	add(plantdoc.Block{Kind: "kv", Pairs: append(header, [2]string{"계획 내용 해시", v.ContentHash})})

	add(plantdoc.Block{Kind: "heading", Text: "1. 제·개정 이력"})
	versions, err := s.store.ListHACCPPlanVersions(ctx, plan.PlanID)
	if err != nil {
		return nil, err
	}
	var hist [][]string
	for _, pv := range versions {
		if pv.VersionNo > v.VersionNo || pv.PublishedAt == nil {
			continue
		}
		reason := pv.ChangeReason
		if reason == "" {
			reason = "최초 제정"
		}
		hist = append(hist, []string{fmt.Sprint(pv.VersionNo), pv.PublishedAt.Local().Format("2006-01-02"), reason,
			yesNo(pv.CCPChanged), names[pv.PublishedBy]})
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"버전", "게시일", "제·개정 사유", "CCP 변경", "게시자"},
		Widths: []float64{1, 2, 6, 1.5, 2}, Rows: hist})

	add(plantdoc.Block{Kind: "heading", Text: "2. 제품설명서"})
	p := c.Product
	add(plantdoc.Block{Kind: "kv", Pairs: [][2]string{{"제품명", p.Name}, {"식품유형", p.FoodType}, {"성상", p.Form},
		{"배합비율", p.Composition}, {"제조단위", p.BatchUnit}, {"완제품 규격", p.Spec}, {"보관·유통 주의사항", p.Storage},
		{"소비기한", p.ShelfLife}, {"포장방법·재질", p.Packaging}, {"가열·섭취 방법", p.IntendedUse}, {"소비 대상", p.Consumers}}})

	add(plantdoc.Block{Kind: "heading", Text: "3. 공정흐름도"})
	flow := plantdoc.Block{Kind: "flow"}
	for _, sp := range c.Steps {
		var marks []string
		for _, k := range c.CCPs {
			if k.ScopeKind != haccp.ScopeLink && has(k.StepIDs, sp.StepID) {
				marks = append(marks, k.Code)
			}
		}
		flow.Steps = append(flow.Steps, plantdoc.FlowStep{ID: sp.StepID, Name: sp.Name, Mark: strings.Join(marks, " ")})
	}
	for _, l := range c.Links {
		var marks []string
		for _, k := range c.CCPs {
			if k.ScopeKind == haccp.ScopeLink && has(k.LinkIDs, l.LinkID) {
				marks = append(marks, k.Code)
			}
		}
		if l.MaxWaitMin != nil {
			marks = append(marks, fmt.Sprintf("대기 ≤%.4g분", *l.MaxWaitMin))
		}
		flow.Links = append(flow.Links, plantdoc.FlowLink{From: l.From, To: l.To, Mark: strings.Join(marks, " · "), Rework: l.FlowKind == haccp.FlowRework})
	}
	add(flow)
	var stepRows [][]string
	for i, sp := range c.Steps {
		stepRows = append(stepRows, []string{fmt.Sprint(i + 1), sp.Name, zoneName[sp.ZoneID], sp.Method})
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"번호", "단계", "구역", "가공 방법·조건"}, Widths: []float64{1, 3, 2.5, 6}, Rows: stepRows})

	add(plantdoc.Block{Kind: "heading", Text: "4. 위해요소 분석"})
	var hzRows [][]string
	for _, h := range c.Hazards {
		target := stepName[h.StepID]
		if h.LinkID != "" {
			target = linkName(h.LinkID)
		}
		hzRows = append(hzRows, []string{target, h.Category, h.Name, h.Cause, fmt.Sprint(h.Severity), fmt.Sprint(h.Likelihood),
			yesNo(h.Significant), h.ControlMeasure})
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"단계·연결", "구분", "위해요소", "발생 원인", "심각성", "가능성", "중요", "예방 조치"},
		Widths: []float64{2.4, 0.8, 2.2, 3, 0.9, 0.9, 0.9, 3.4}, Rows: hzRows})

	add(plantdoc.Block{Kind: "heading", Text: "5. 중요관리점(CCP) 결정"})
	var ccpRows [][]string
	hazardName := map[string]string{}
	for _, h := range c.Hazards {
		hazardName[h.HazardID] = h.Category + " · " + h.Name
	}
	scopeKindText := map[string]string{haccp.ScopeStep: "단계", haccp.ScopeLink: "연결(이송·대기)", haccp.ScopeSpan: "구간(여러 단계)"}
	for _, k := range c.CCPs {
		var hz []string
		for _, id := range k.HazardIDs {
			hz = append(hz, hazardName[id])
		}
		ccpRows = append(ccpRows, []string{k.Code, k.Name, scopeText(k), scopeKindText[k.ScopeKind], strings.Join(hz, ", ")})
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"CCP", "이름", "대상 단계", "관리 범위", "관리하는 위해요소"},
		Widths: []float64{1.2, 2.5, 3, 2, 3.5}, Rows: ccpRows})

	add(plantdoc.Block{Kind: "heading", Text: "6. HACCP 관리계획"})
	var mgmt [][]string
	for _, k := range c.CCPs {
		for _, l := range k.Limits {
			monitor := sourceText[l.SourceKind] + " · " + autoText[l.AutomationLevel]
			if l.SensorID != "" {
				monitor += "\n센서 " + l.SensorID
			}
			if l.SampleIntervalSec > 0 {
				monitor += fmt.Sprintf("\n%d초마다", l.SampleIntervalSec)
			}
			monitor += "\n담당 " + names[k.MonitorPrimary]
			if k.OutageAfterSec > 0 && l.SensorID != "" {
				monitor += fmt.Sprintf("\n%d초 무응답 시 모니터링 불능", k.OutageAfterSec)
			}
			mgmt = append(mgmt, []string{k.Code + "\n" + scopeText(k), haccp.ItemLabel(l.Item) + "\n" + haccp.LimitText(l) + "\n근거: " + l.BasisRef,
				monitor, k.CorrectiveAction, k.Verification})
		}
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"CCP", "한계기준", "모니터링", "개선조치", "검증"},
		Widths: []float64{1.6, 3.2, 2.6, 3.2, 2.6}, Rows: mgmt})

	add(plantdoc.Block{Kind: "heading", Text: "7. 확인과 서명"})
	at := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Local().Format("2006-01-02 15:04:05")
	}
	add(plantdoc.Block{Kind: "sign", Signers: []plantdoc.Signer{
		{Role: "현장 확인", Name: names[v.OnsiteConfirmedBy], At: at(v.OnsiteConfirmedAt)},
		{Role: "게시(승인)", Name: names[v.PublishedBy], At: at(v.PublishedAt)},
		{Role: "HACCP 팀장"}}})
	return d, nil
}

// lotDoc builds the lot record: movement, CCP monitoring, deviations, signatures.
func (s *Server) lotDoc(ctx context.Context, st *lotState) (*plantdoc.Doc, error) {
	ccps, err := s.evaluateLot(ctx, st, nowMs())
	if err != nil {
		return nil, err
	}
	header, plantName, err := s.plantHeader(ctx, st.lot.SiteID)
	if err != nil {
		return nil, err
	}
	names, err := s.userNames(ctx, st.lot.SiteID)
	if err != nil {
		return nil, err
	}
	lot := lotToMap(st.lot)
	d := &plantdoc.Doc{Title: "생산 기록 — lot " + st.lot.LotNo, Subtitle: plantName + " · " + st.plan.Name,
		DocNo: "LOT-" + st.lot.LotNo, At: time.UnixMilli(st.lot.ClosedAt).UTC()}
	add := func(b plantdoc.Block) { d.Blocks = append(d.Blocks, b) }
	yield := ""
	if y, ok := lot["yield_pct"].(float64); ok {
		yield = fmt.Sprintf("%.1f%%", y)
	}
	add(plantdoc.Block{Kind: "kv", Pairs: append(header[:2], [][2]string{
		{"lot 번호", st.lot.LotNo}, {"제품", st.content.Product.Name},
		{"적용 계획", fmt.Sprintf("%s 버전 %d (해시 %s)", st.plan.Name, st.version.VersionNo, st.version.ContentHash)},
		{"시작", fmtMs(st.lot.StartedAt) + " · " + names[st.lot.StartedBy]},
		{"마감", fmtMs(st.lot.ClosedAt) + " · " + names[st.lot.ClosedBy]},
		{"투입·산출·수율", fmt.Sprintf("%.4g kg → %.4g kg · %s", lot["input_kg"], st.lot.OutputKg, yield)}}...)})

	add(plantdoc.Block{Kind: "heading", Text: "1. 투입 원료"})
	var inRows [][]string
	for _, in := range lot["inputs"].([]lotInput) {
		inRows = append(inRows, []string{in.Supplier, in.SupplierLot, in.Species, fmt.Sprintf("%.4g", in.QtyKg)})
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"공급자", "공급자 lot 코드", "어종", "중량(kg)"}, Widths: []float64{3, 3, 3, 1.5}, Rows: inRows})

	add(plantdoc.Block{Kind: "heading", Text: "2. 단계별 이동"})
	var moveRows [][]string
	for i := 0; i < len(st.events); i++ {
		e := st.events[i]
		if e.Kind != "enter" {
			continue
		}
		exit := int64(0)
		if i+1 < len(st.events) {
			exit = st.events[i+1].At
		}
		stay := ""
		if exit > 0 {
			stay = fmt.Sprintf("%.1f분", float64(exit-e.At)/60000)
		}
		moveRows = append(moveRows, []string{st.stepName(e.StepID), fmtMs(e.At), fmtMs(exit), stay})
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"단계", "진입", "이탈", "머문 시간"}, Widths: []float64{3, 3, 3, 1.5}, Rows: moveRows})

	add(plantdoc.Block{Kind: "heading", Text: "3. CCP 모니터링 기록"})
	num := func(v *float64) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%.3g", *v)
	}
	var monRows [][]string
	for _, k := range ccps {
		for _, l := range k.Limits {
			measured := fmt.Sprintf("%d회 · 최소 %s · 최대 %s", l.Result.Count, num(l.Result.Min), num(l.Result.Max))
			if l.RuleKind == haccp.RuleCumulativeExposure {
				measured += fmt.Sprintf("\n누적 노출 %.1f분", l.Result.ExposureMin)
			}
			source := sourceText[l.SourceKind]
			if l.SensorID != "" {
				source += " " + l.SensorID
			}
			monRows = append(monRows, []string{k.Code + " " + k.Name, l.ItemLabel + "\n" + l.Text, source, measured, statusText[l.Result.Status]})
		}
	}
	add(plantdoc.Block{Kind: "table", Headers: []string{"CCP", "항목·한계기준", "값의 출처", "측정", "판정"},
		Widths: []float64{2.6, 3.4, 2.6, 3, 1.6}, Rows: monRows})

	checks, err := s.store.ListProcessChecks(ctx, st.lot.LotID)
	if err != nil {
		return nil, err
	}
	if len(checks) > 0 {
		add(plantdoc.Block{Kind: "heading", Text: "4. 사람이 입력한 값"})
		itemOf := map[string]string{}
		for _, k := range st.content.CCPs {
			for _, l := range k.Limits {
				itemOf[l.LimitID] = k.Code + " " + haccp.ItemLabel(l.Item)
			}
		}
		var rows [][]string
		for _, c := range checks {
			rows = append(rows, []string{fmtMs(c.At), itemOf[c.LimitID], fmt.Sprintf("%.4g", c.Value), names[c.ByUser]})
		}
		add(plantdoc.Block{Kind: "table", Headers: []string{"시각", "항목", "값", "입력자"}, Widths: []float64{3, 4, 1.5, 2.5}, Rows: rows})
	}

	add(plantdoc.Block{Kind: "heading", Text: "5. 이탈과 개선조치"})
	devs, err := s.store.ListProcessDeviations(ctx, st.lot.LotID)
	if err != nil {
		return nil, err
	}
	kindText := map[string]string{"deviation": "한계기준 이탈", "outage": "모니터링 불능"}
	var devRows [][]string
	for _, dv := range devs {
		code := ""
		for _, k := range st.content.CCPs {
			if k.CCPID == dv.CCPID {
				code = k.Code
			}
		}
		devRows = append(devRows, []string{fmtMs(dv.DetectedAt), code + "\n" + kindText[dv.Kind], dv.Detail, dv.CorrectiveAction,
			dv.Disposition, names[dv.ClosedBy] + "\n" + fmtMs(dv.ClosedAt)})
	}
	if len(devRows) == 0 {
		add(plantdoc.Block{Kind: "para", Text: "이 lot 에서 기록된 이탈과 모니터링 불능이 없습니다."})
	} else {
		add(plantdoc.Block{Kind: "table", Headers: []string{"발견 시각", "CCP·종류", "내용", "개선조치", "제품 처분", "처리자"},
			Widths: []float64{2, 1.8, 3, 3, 2.5, 2}, Rows: devRows})
	}

	add(plantdoc.Block{Kind: "heading", Text: "6. 확인과 서명"})
	add(plantdoc.Block{Kind: "sign", Signers: []plantdoc.Signer{
		{Role: "작성(마감)", Name: names[st.lot.ClosedBy], At: fmtMs(st.lot.ClosedAt)},
		{Role: "검토", Name: names[st.lot.ReviewedBy], At: fmtMs(st.lot.ReviewedAt)},
		{Role: "HACCP 팀장"}}})
	return d, nil
}

// ── API ──────────────────────────────────────────────────────────────────────

func (s *Server) docDir() string { return filepath.Join(s.cfg.Edge.DataDir, "documents") }

func fileHash(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Server) docToMap(d *storage.IssuedDocument, names map[string]string) map[string]any {
	return map[string]any{"doc_id": d.DocID, "kind": d.Kind, "ref_id": d.RefID, "title": d.Title, "sha256": d.SHA256,
		"data_hash": d.DataHash, "issued_by": d.IssuedBy, "issued_by_name": names[d.IssuedBy], "issued_at": d.IssuedAt,
		// 보관 중인 파일이 발행 때와 같은지: 지금 파일의 해시를 대장의 해시와 대조한다.
		"intact": fileHash(d.Path) == d.SHA256}
}

// GET /v1/documents?site_id= — 발행 대장. POST — 발행.
func (s *Server) handleDocumentsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		siteID := r.URL.Query().Get("site_id")
		docs, err := s.store.ListIssuedDocuments(r.Context(), siteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		names, err := s.userNames(r.Context(), siteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		out := make([]map[string]any, 0, len(docs))
		for _, d := range docs {
			out = append(out, s.docToMap(d, names))
		}
		writeJSON(w, http.StatusOK, map[string]any{"documents": out, "count": len(out)})
	case http.MethodPost:
		s.handleIssueDocument(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleIssueDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		signedRequest
		Kind  string `json:"kind"`
		RefID string `json:"ref_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	ctx := r.Context()
	var doc *plantdoc.Doc
	var siteID string
	var err error
	switch req.Kind {
	case "plan":
		v, verr := s.store.GetHACCPPlanVersion(ctx, req.RefID)
		if verr != nil || v == nil || v.Status == haccpStatusDraft {
			writeError(w, http.StatusUnprocessableEntity, "NOT_PUBLISHED", "게시된 계획 버전만 발행할 수 있습니다", "")
			return
		}
		plan, perr := s.store.GetHACCPPlan(ctx, v.PlanID)
		if perr != nil || plan == nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", fmt.Sprint(perr), "")
			return
		}
		siteID = plan.SiteID
		doc, err = s.planDoc(ctx, plan, v)
	case "lot":
		st, lerr := s.loadLot(ctx, req.RefID)
		if lerr != nil || st == nil || st.lot.Status != "closed" {
			writeError(w, http.StatusUnprocessableEntity, "LOT_NOT_CLOSED", "마감된 lot 만 발행할 수 있습니다", "")
			return
		}
		siteID = st.lot.SiteID
		doc, err = s.lotDoc(ctx, st)
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_KIND", "kind 는 plan|lot", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DOCUMENT_BUILD_FAILED", err.Error(), "")
		return
	}
	if !s.checkSigner(w, r, siteID, req.By, req.PIN, "by", roleAdmin, roleReviewer, roleMonitor) {
		return
	}
	names, err := s.userNames(ctx, siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	// 같은 내용이 이미 발행돼 있으면 그 발행본을 돌려준다.
	dataHash := doc.DataHash()
	existing, err := s.store.ListIssuedDocuments(ctx, siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, e := range existing {
		if e.Kind == req.Kind && e.RefID == req.RefID && e.DataHash == dataHash {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reissued": true, "document": s.docToMap(e, names)})
			return
		}
	}
	pdf, err := plantdoc.Render(doc)
	if err != nil {
		code := "DOCUMENT_RENDER_FAILED"
		if errors.Is(err, plantdoc.ErrNoFont) {
			code = "NO_KOREAN_FONT"
		}
		writeError(w, http.StatusInternalServerError, code, err.Error(), "")
		return
	}
	docID := common.NewID("doc")
	if err := os.MkdirAll(s.docDir(), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	path := filepath.Join(s.docDir(), docID+".pdf")
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, pdf, 0o644); err == nil {
		err = os.WriteFile(filepath.Join(s.docDir(), docID+".json"), data, 0o644)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	sum := sha256.Sum256(pdf)
	rec := &storage.IssuedDocument{DocID: docID, SiteID: siteID, Kind: req.Kind, RefID: req.RefID, Title: doc.Title,
		SHA256: hex.EncodeToString(sum[:]), DataHash: dataHash, IssuedBy: req.By, IssuedAt: nowMs(), Path: path}
	if err := s.store.InsertIssuedDocument(ctx, rec); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.app.AppendEvent(ctx, "process", "api", siteID, events.EventHACCPDocumentIssued, rec.RefID, map[string]any{
		"doc_id": docID, "kind": req.Kind, "ref_id": req.RefID, "sha256": rec.SHA256, "data_hash": dataHash, "issued_by": req.By})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "document": s.docToMap(rec, names)})
}

// GET /v1/documents/{doc_id}/file — PDF. /data — 원본 데이터(JSON).
func (s *Server) handleDocumentFile(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/documents/"), "/"), "/")
	if len(parts) != 2 || (parts[1] != "file" && parts[1] != "data") {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/documents/{doc_id}/file|data", "")
		return
	}
	d, err := s.store.GetIssuedDocument(r.Context(), parts[0])
	if err != nil || d == nil {
		writeError(w, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "document not found", "")
		return
	}
	path, ctype, name := d.Path, "application/pdf", d.DocID+".pdf"
	if parts[1] == "data" {
		path, ctype, name = strings.TrimSuffix(d.Path, ".pdf")+".json", "application/json", d.DocID+".json"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}
