package plantapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/storage"
)

// 생산 실행 — lot 을 게시된 계획의 공정도 위에서 움직이고, 그 사이의 측정값으로 CCP 를 판정한다.
// 판정은 규칙 엔진(haccp.EvaluateLimit)이 한다. 시각은 UTC unix 밀리초.
// References: docs/54-haccp-process-flow-ccp-design.md §7, §26.

func nowMs() int64 { return time.Now().UnixMilli() }

type lotInput struct {
	Supplier    string  `json:"supplier"`
	SupplierLot string  `json:"supplier_lot"`
	Species     string  `json:"species"`
	QtyKg       float64 `json:"qty_kg"`
}

// lotState is a lot with its plan content and movement history.
type lotState struct {
	lot     *storage.ProcessLot
	plan    *storage.HACCPPlan
	version *storage.HACCPPlanVersion
	content haccp.PlanContent
	events  []storage.ProcessLotEvent
}

func (s *Server) loadLot(ctx context.Context, lotID string) (*lotState, error) {
	lot, err := s.store.GetProcessLot(ctx, lotID)
	if err != nil || lot == nil {
		return nil, err
	}
	st := &lotState{lot: lot}
	if st.plan, err = s.store.GetHACCPPlan(ctx, lot.PlanID); err != nil {
		return nil, err
	}
	if st.version, err = s.store.GetHACCPPlanVersion(ctx, lot.VersionID); err != nil {
		return nil, err
	}
	if st.plan == nil || st.version == nil {
		return nil, fmt.Errorf("lot %s: plan version missing", lotID)
	}
	if err := json.Unmarshal([]byte(st.version.ContentJSON), &st.content); err != nil {
		return nil, err
	}
	st.events, err = s.store.ListProcessLotEvents(ctx, lotID)
	return st, err
}

// position: 지금 있는 단계, 또는 단계를 떠나 이동 중인 연결.
func (st *lotState) position() (stepID, linkID string) {
	if len(st.events) == 0 {
		return "", ""
	}
	last := st.events[len(st.events)-1]
	if last.Kind == "exit" {
		return "", last.LinkID
	}
	return last.StepID, ""
}

func (st *lotState) link(id string) *haccp.Link {
	for i := range st.content.Links {
		if st.content.Links[i].LinkID == id {
			return &st.content.Links[i]
		}
	}
	return nil
}

func (st *lotState) stepName(id string) string {
	for _, sp := range st.content.Steps {
		if sp.StepID == id {
			return sp.Name
		}
	}
	return id
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// windows: lot 이 이 CCP 범위 안에 있던 구간.
//   - 단계 CCP: 대상 단계 안에 있던 시간
//   - 구간 CCP: 대상 단계에 처음 들어가서, 대상이 아닌 단계로 나갈 때까지(대상 단계 사이의 이동 포함)
//   - 연결 CCP: 대상 연결 위에 있던 시간(앞 단계를 떠나 뒤 단계에 들어갈 때까지)
func (st *lotState) windows(k haccp.CCP) []haccp.Window {
	var out []haccp.Window
	open := false
	start := func(at int64) {
		if !open {
			out, open = append(out, haccp.Window{Start: at}), true
		}
	}
	stop := func(at int64) {
		if open {
			out[len(out)-1].End, open = at, false
		}
	}
	for _, e := range st.events {
		switch k.ScopeKind {
		case haccp.ScopeLink:
			if e.Kind == "exit" && has(k.LinkIDs, e.LinkID) {
				start(e.At)
			} else if e.Kind == "enter" {
				stop(e.At)
			}
		default:
			if e.Kind == "enter" {
				if has(k.StepIDs, e.StepID) {
					start(e.At)
				} else {
					stop(e.At)
				}
			} else if e.Kind == "exit" {
				// 구간 CCP 는 대상 단계로 이어지는 이동 중에도 계속된다.
				l := st.link(e.LinkID)
				if k.ScopeKind != haccp.ScopeSpan || l == nil || !has(k.StepIDs, l.To) {
					stop(e.At)
				}
			}
		}
	}
	return out
}

type limitView struct {
	LimitID    string            `json:"limit_id"`
	Item       string            `json:"item"`
	ItemLabel  string            `json:"item_label"`
	Text       string            `json:"text"`
	Unit       string            `json:"unit"`
	RuleKind   string            `json:"rule_kind"`
	SourceKind string            `json:"source_kind"`
	SensorID   string            `json:"sensor_id"`
	Result     haccp.LimitResult `json:"result"`
}

type ccpView struct {
	CCPID            string      `json:"ccp_id"`
	Code             string      `json:"code"`
	Name             string      `json:"name"`
	ScopeKind        string      `json:"scope_kind"`
	Status           string      `json:"status"`
	Active           bool        `json:"active"`
	CorrectiveAction string      `json:"corrective_action"`
	Limits           []limitView `json:"limits"`
}

// statusRank: CCP 의 상태는 항목 중 가장 나쁜 것.
var statusRank = map[string]int{
	haccp.StatusPending: 0, haccp.StatusOK: 1, haccp.StatusInProgress: 2,
	haccp.StatusNeedsInput: 3, haccp.StatusOutage: 4, haccp.StatusDeviation: 5,
}

// exposureSensor: 누적 노출처럼 계산값인 항목이 볼 센서. 지정이 없으면 대상 단계 구역의 온도 센서.
func exposureSensor(k haccp.CCP, l haccp.Limit, content *haccp.PlanContent, sensors []*storage.PlantSensor) string {
	if l.SensorID != "" {
		return l.SensorID
	}
	for _, sp := range content.Steps {
		if !has(k.StepIDs, sp.StepID) || sp.ZoneID == "" {
			continue
		}
		for _, sn := range sensors {
			if sn.ZoneID == sp.ZoneID && sn.Metric == "temperature" {
				return sn.SensorID
			}
		}
	}
	return ""
}

// evaluateLot judges every CCP limit of the lot at time now and records new deviations.
func (s *Server) evaluateLot(ctx context.Context, st *lotState, now int64) ([]ccpView, error) {
	if st.lot.Status == "closed" {
		now = st.lot.ClosedAt
	}
	checks, err := s.store.ListProcessChecks(ctx, st.lot.LotID)
	if err != nil {
		return nil, err
	}
	sensors, err := s.store.ListPlantSensors(ctx, st.lot.SiteID)
	if err != nil {
		return nil, err
	}
	var out []ccpView
	for _, k := range st.content.CCPs {
		wins := st.windows(k)
		view := ccpView{CCPID: k.CCPID, Code: k.Code, Name: k.Name, ScopeKind: k.ScopeKind,
			Status: haccp.StatusPending, CorrectiveAction: k.CorrectiveAction}
		for _, l := range k.Limits {
			in := haccp.LimitInput{Limit: l, Windows: wins, Now: now, OutageAfterSec: k.OutageAfterSec}
			sensorID := l.SensorID
			switch {
			case l.Item == "wait_time":
				// 대기 시간: 연결 위에서 보낸 분. 구간마다 값 하나.
				for _, w := range wins {
					end := w.End
					if end == 0 {
						end = now
					}
					in.Samples = append(in.Samples, haccp.Sample{At: end, Value: float64(end-w.Start) / 60000})
				}
				sensorID = ""
			case l.SourceKind == haccp.SourceOperator || l.RuleKind == haccp.RulePeriodicCheck:
				for _, c := range checks {
					if c.LimitID == l.LimitID {
						in.Samples = append(in.Samples, haccp.Sample{At: c.At, Value: c.Value})
					}
				}
				sensorID = ""
			case l.SourceKind == haccp.SourceDerived:
				sensorID = exposureSensor(k, l, &st.content, sensors)
			}
			if sensorID != "" && len(wins) > 0 {
				// 진입 직전 값을 진입 시점의 값으로 쓴다(측정 주기보다 빨리 지나가는 단계).
				in.CarryMs = 60000
				if k.OutageAfterSec > 0 {
					in.CarryMs = int64(k.OutageAfterSec) * 1000
				}
				readings, err := s.store.ListProcessReadings(ctx, sensorID, wins[0].Start-in.CarryMs, now)
				if err != nil {
					return nil, err
				}
				for _, r := range readings {
					in.Samples = append(in.Samples, haccp.Sample{At: r.At, Value: r.Value})
				}
			}
			res := haccp.EvaluateLimit(in)
			view.Limits = append(view.Limits, limitView{LimitID: l.LimitID, Item: l.Item, ItemLabel: haccp.ItemLabel(l.Item), Text: haccp.LimitText(l),
				Unit: l.Unit, RuleKind: l.RuleKind, SourceKind: l.SourceKind, SensorID: sensorID, Result: res})
			if statusRank[res.Status] > statusRank[view.Status] {
				view.Status = res.Status
			}
			view.Active = view.Active || res.Active

			record := func(kind string, at int64, detail string) error {
				d := &storage.ProcessDeviation{DevID: common.NewID("dev"), LotID: st.lot.LotID, CCPID: k.CCPID,
					LimitID: l.LimitID, Kind: kind, DetectedAt: at, Detail: detail}
				created, err := s.store.InsertProcessDeviation(ctx, d)
				if err != nil || !created {
					return err
				}
				_, err = s.app.AppendEvent(ctx, "process", "rules", st.lot.SiteID, events.EventHACCPDeviationDetected,
					st.lot.LotID, map[string]any{"dev_id": d.DevID, "lot_id": st.lot.LotID, "lot_no": st.lot.LotNo,
						"ccp_id": k.CCPID, "ccp_code": k.Code, "limit_id": l.LimitID, "kind": kind, "detail": detail,
						"version_id": st.lot.VersionID})
				return err
			}
			if res.Deviation {
				detail := res.Detail
				if res.Status != haccp.StatusDeviation {
					detail = haccp.ItemLabel(l.Item) + " 한계기준 이탈"
				}
				if err := record("deviation", res.DeviationAt, detail); err != nil {
					return nil, err
				}
			}
			if res.Outage {
				if err := record("outage", res.OutageAt,
					haccp.ItemLabel(l.Item)+" 값이 들어오지 않았습니다"); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// ── API ──────────────────────────────────────────────────────────────────────

type newLotRequest struct {
	SiteID    string     `json:"site_id"`
	PlanID    string     `json:"plan_id"`
	LotNo     string     `json:"lot_no"`
	Inputs    []lotInput `json:"inputs"`
	StartedBy string     `json:"started_by"`
	PIN       string     `json:"pin"`
}

// GET /v1/process/lots?site_id= — lot 목록. POST — lot 시작.
func (s *Server) handleLotsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		lots, err := s.store.ListProcessLots(r.Context(), r.URL.Query().Get("site_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		out := make([]map[string]any, 0, len(lots))
		for _, l := range lots {
			devs, err := s.store.ListProcessDeviations(r.Context(), l.LotID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
				return
			}
			open := 0
			for _, d := range devs {
				if d.Status == "open" {
					open++
				}
			}
			m := lotToMap(l)
			m["deviations"], m["open_deviations"] = len(devs), open
			out = append(out, m)
		}
		writeJSON(w, http.StatusOK, map[string]any{"lots": out, "count": len(out)})
	case http.MethodPost:
		s.handleStartLot(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStartLot(w http.ResponseWriter, r *http.Request) {
	var req newLotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	ctx := r.Context()
	plan, err := s.store.GetHACCPPlan(ctx, req.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if plan == nil || plan.SiteID != req.SiteID {
		writeError(w, http.StatusUnprocessableEntity, "PLAN_NOT_FOUND", "이 공장의 계획이 아닙니다", "")
		return
	}
	if !s.checkSigner(w, r, plan.SiteID, req.StartedBy, req.PIN, "started_by", roleAdmin, roleReviewer, roleMonitor) {
		return
	}
	// 게시된 계획으로만 생산한다.
	versions, err := s.store.ListHACCPPlanVersions(ctx, plan.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var published *storage.HACCPPlanVersion
	for _, v := range versions {
		if v.Status == haccpStatusPublished {
			published = v
		}
	}
	if published == nil {
		writeError(w, http.StatusUnprocessableEntity, "NO_PUBLISHED_VERSION", "게시된 계획이 있어야 생산을 시작할 수 있습니다", "")
		return
	}
	var content haccp.PlanContent
	if err := json.Unmarshal([]byte(published.ContentJSON), &content); err != nil || len(content.Steps) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "PLAN_HAS_NO_STEPS", "계획에 단계가 없습니다", "")
		return
	}
	var totalKg float64
	for _, in := range req.Inputs {
		if strings.TrimSpace(in.SupplierLot) == "" || in.QtyKg <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_INPUT", "투입 원료마다 공급자 lot 코드와 중량(kg)이 필요합니다", "")
			return
		}
		totalKg += in.QtyKg
	}
	if len(req.Inputs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_INPUT", "투입 원료가 1건 이상 필요합니다", "")
		return
	}
	now := nowMs()
	req.LotNo = strings.TrimSpace(req.LotNo)
	if req.LotNo == "" {
		existing, err := s.store.ListProcessLots(ctx, plan.SiteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		req.LotNo = fmt.Sprintf("%s-%03d", time.Now().Format("20060102"), len(existing)+1)
	}
	inputsJSON, _ := json.Marshal(req.Inputs)
	lot := &storage.ProcessLot{LotID: common.NewID("lot"), SiteID: plan.SiteID, PlanID: plan.PlanID,
		VersionID: published.VersionID, LotNo: req.LotNo, Status: "running", InputsJSON: string(inputsJSON),
		StartedBy: req.StartedBy, StartedAt: now}
	if err := s.store.InsertProcessLot(ctx, lot); err != nil {
		writeError(w, http.StatusConflict, "LOT_NO_TAKEN", "같은 lot 번호가 이미 있습니다: "+req.LotNo, "")
		return
	}
	// 첫 단계에 들어간다.
	if err := s.store.InsertProcessLotEvent(ctx, lot.LotID,
		&storage.ProcessLotEvent{Kind: "enter", StepID: content.Steps[0].StepID, At: now}); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.app.AppendEvent(ctx, "process", "api", plan.SiteID, events.EventProcessLotStarted, lot.LotID, map[string]any{
		"lot_id": lot.LotID, "lot_no": lot.LotNo, "plan_id": plan.PlanID, "version_id": published.VersionID,
		"content_hash": published.ContentHash, "inputs": req.Inputs, "input_kg": totalKg, "started_by": req.StartedBy})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "lot": lotToMap(lot)})
}

func lotToMap(l *storage.ProcessLot) map[string]any {
	var inputs []lotInput
	_ = json.Unmarshal([]byte(l.InputsJSON), &inputs)
	var inputKg float64
	for _, in := range inputs {
		inputKg += in.QtyKg
	}
	m := map[string]any{"lot_id": l.LotID, "site_id": l.SiteID, "plan_id": l.PlanID, "version_id": l.VersionID,
		"lot_no": l.LotNo, "status": l.Status, "inputs": inputs, "input_kg": inputKg, "output_kg": l.OutputKg,
		"started_by": l.StartedBy, "started_at": l.StartedAt, "closed_by": l.ClosedBy, "closed_at": l.ClosedAt,
		"reviewed_by": l.ReviewedBy, "reviewed_at": l.ReviewedAt}
	if l.Status == "closed" && inputKg > 0 {
		m["yield_pct"] = l.OutputKg / inputKg * 100
	}
	return m
}

// /v1/process/lots/{lot_id}[/move|checks|close|review|deviations/{dev_id}/close]
func (s *Server) handleLotRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/process/lots/"), "/"), "/")
	st, err := s.loadLot(r.Context(), parts[0])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if st == nil {
		writeError(w, http.StatusNotFound, "LOT_NOT_FOUND", "lot not found", "")
		return
	}
	post := r.Method == http.MethodPost
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.writeLotDetail(w, r, st)
	case len(parts) == 2 && parts[1] == "move" && post:
		s.handleLotMove(w, r, st)
	case len(parts) == 2 && parts[1] == "checks" && post:
		s.handleLotCheck(w, r, st)
	case len(parts) == 2 && parts[1] == "close" && post:
		s.handleLotClose(w, r, st)
	case len(parts) == 2 && parts[1] == "review" && post:
		s.handleLotReview(w, r, st)
	case len(parts) == 4 && parts[1] == "deviations" && parts[3] == "close" && post:
		s.handleDeviationClose(w, r, st, parts[2])
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown lot route", "")
	}
}

// lotDetail builds everything the lot screen and the lot record document show.
func (s *Server) lotDetail(ctx context.Context, st *lotState) (map[string]any, error) {
	ccps, err := s.evaluateLot(ctx, st, nowMs())
	if err != nil {
		return nil, err
	}
	devs, err := s.store.ListProcessDeviations(ctx, st.lot.LotID)
	if err != nil {
		return nil, err
	}
	checks, err := s.store.ListProcessChecks(ctx, st.lot.LotID)
	if err != nil {
		return nil, err
	}
	stepID, linkID := st.position()

	// 다음으로 갈 수 있는 곳: 지금 단계에서 나가는 제품 흐름.
	next := []map[string]any{}
	for _, l := range st.content.Links {
		if stepID != "" && l.From == stepID && l.FlowKind == haccp.FlowProduct {
			tracked := l.MaxWaitMin != nil
			for _, k := range st.content.CCPs {
				tracked = tracked || (k.ScopeKind == haccp.ScopeLink && has(k.LinkIDs, l.LinkID))
			}
			next = append(next, map[string]any{"link_id": l.LinkID, "to": l.To, "to_name": st.stepName(l.To),
				"tracked": tracked}) // tracked: 이동 시간을 따로 잰다(나가기 → 도착)
		}
	}
	// 단계별 머문 시간.
	timeline := []map[string]any{}
	for _, e := range st.events {
		if e.Kind == "enter" {
			timeline = append(timeline, map[string]any{"step_id": e.StepID, "name": st.stepName(e.StepID), "entered_at": e.At})
		} else if n := len(timeline); n > 0 {
			timeline[n-1]["exited_at"] = e.At
		}
	}
	devOut := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		code := ""
		for _, k := range st.content.CCPs {
			if k.CCPID == d.CCPID {
				code = k.Code
			}
		}
		devOut = append(devOut, map[string]any{"dev_id": d.DevID, "ccp_id": d.CCPID, "ccp_code": code, "limit_id": d.LimitID,
			"kind": d.Kind, "detected_at": d.DetectedAt, "detail": d.Detail, "status": d.Status,
			"corrective_action": d.CorrectiveAction, "disposition": d.Disposition,
			"closed_by": d.ClosedBy, "closed_at": d.ClosedAt})
	}
	checkOut := make([]map[string]any, 0, len(checks))
	for _, c := range checks {
		checkOut = append(checkOut, map[string]any{"limit_id": c.LimitID, "value": c.Value, "by": c.ByUser, "at": c.At})
	}
	var linkTo string
	if l := st.link(linkID); l != nil {
		linkTo = st.stepName(l.To)
	}
	return map[string]any{
		"lot":      lotToMap(st.lot),
		"plan":     map[string]any{"plan_id": st.plan.PlanID, "name": st.plan.Name, "version_no": st.version.VersionNo, "content_hash": st.version.ContentHash},
		"content":  st.content,
		"position": map[string]any{"step_id": stepID, "link_id": linkID, "step_name": st.stepName(stepID), "link_to_name": linkTo},
		"next":     next, "timeline": timeline, "ccps": ccps, "deviations": devOut, "checks": checkOut,
		"now": nowMs(),
	}, nil
}

func (s *Server) writeLotDetail(w http.ResponseWriter, r *http.Request, st *lotState) {
	d, err := s.lotDetail(r.Context(), st)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// POST move — {link_id} 로 다음 단계로 간다. action: exit(단계를 떠남) | enter(도착) | advance(한 번에).
func (s *Server) handleLotMove(w http.ResponseWriter, r *http.Request, st *lotState) {
	var req struct {
		Action string `json:"action"`
		LinkID string `json:"link_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if st.lot.Status != "running" {
		writeError(w, http.StatusConflict, "LOT_CLOSED", "마감된 lot 입니다", "")
		return
	}
	stepID, linkID := st.position()
	ctx, now := r.Context(), nowMs()
	add := func(e storage.ProcessLotEvent) bool {
		if err := s.store.InsertProcessLotEvent(ctx, st.lot.LotID, &e); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return false
		}
		st.events = append(st.events, e)
		return true
	}
	if req.Action == "exit" || req.Action == "advance" {
		l := st.link(req.LinkID)
		if stepID == "" || l == nil || l.From != stepID || l.FlowKind != haccp.FlowProduct {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_MOVE", "지금 단계에서 갈 수 없는 연결입니다", "")
			return
		}
		if !add(storage.ProcessLotEvent{Kind: "exit", StepID: stepID, LinkID: l.LinkID, At: now}) {
			return
		}
		linkID = l.LinkID
	}
	if req.Action == "enter" || req.Action == "advance" {
		l := st.link(linkID)
		if l == nil {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_MOVE", "이동 중인 lot 이 아닙니다", "")
			return
		}
		if !add(storage.ProcessLotEvent{Kind: "enter", StepID: l.To, At: now}) {
			return
		}
	}
	s.writeLotDetail(w, r, st)
}

type signedRequest struct {
	By  string `json:"by"`
	PIN string `json:"pin"`
}

// POST checks — 사람이 넣는 측정·점검 값. 누가 넣었는지 PIN 으로 확인한다.
func (s *Server) handleLotCheck(w http.ResponseWriter, r *http.Request, st *lotState) {
	var req struct {
		signedRequest
		LimitID string   `json:"limit_id"`
		Value   *float64 `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	found := false
	for _, k := range st.content.CCPs {
		for _, l := range k.Limits {
			found = found || l.LimitID == req.LimitID
		}
	}
	if !found || req.Value == nil || st.lot.Status != "running" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CHECK", "진행 중인 lot 의 한계기준 항목과 값이 필요합니다", "")
		return
	}
	if !s.checkSigner(w, r, st.lot.SiteID, req.By, req.PIN, "by", roleAdmin, roleReviewer, roleMonitor) {
		return
	}
	if err := s.store.InsertProcessCheck(r.Context(), st.lot.LotID,
		&storage.ProcessCheck{LimitID: req.LimitID, Value: *req.Value, ByUser: req.By, At: nowMs()}); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.writeLotDetail(w, r, st)
}

// POST deviations/{id}/close — 개선조치와 제품 처분을 적고 서명해야 닫힌다.
func (s *Server) handleDeviationClose(w http.ResponseWriter, r *http.Request, st *lotState, devID string) {
	var req struct {
		signedRequest
		CorrectiveAction string `json:"corrective_action"`
		Disposition      string `json:"disposition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.CorrectiveAction, req.Disposition = strings.TrimSpace(req.CorrectiveAction), strings.TrimSpace(req.Disposition)
	if req.CorrectiveAction == "" || req.Disposition == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "개선조치와 제품 처분을 모두 적어야 합니다", "")
		return
	}
	devs, err := s.store.ListProcessDeviations(r.Context(), st.lot.LotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var dev *storage.ProcessDeviation
	for _, d := range devs {
		if d.DevID == devID {
			dev = d
		}
	}
	if dev == nil {
		writeError(w, http.StatusNotFound, "DEVIATION_NOT_FOUND", "이 lot 의 이탈이 아닙니다", "")
		return
	}
	if !s.checkSigner(w, r, st.lot.SiteID, req.By, req.PIN, "by", roleAdmin, roleReviewer, roleMonitor) {
		return
	}
	now := nowMs()
	ok, err := s.store.CloseProcessDeviation(r.Context(), devID, req.CorrectiveAction, req.Disposition, req.By, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "DEVIATION_CLOSED", "이미 닫힌 이탈입니다", "")
		return
	}
	s.app.AppendEvent(r.Context(), "process", "api", st.lot.SiteID, events.EventHACCPDeviationClosed, st.lot.LotID,
		map[string]any{"dev_id": devID, "lot_id": st.lot.LotID, "corrective_action": req.CorrectiveAction,
			"disposition": req.Disposition, "closed_by": req.By})
	s.writeLotDetail(w, r, st)
}

// POST close — 마지막 단계에서 산출 중량을 적고 서명해 마감한다. 열린 이탈이 있으면 마감할 수 없다.
func (s *Server) handleLotClose(w http.ResponseWriter, r *http.Request, st *lotState) {
	var req struct {
		signedRequest
		OutputKg float64 `json:"output_kg"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	ctx := r.Context()
	stepID, _ := st.position()
	if st.lot.Status != "running" || stepID == "" {
		writeError(w, http.StatusConflict, "LOT_NOT_AT_STEP", "진행 중이고 단계 안에 있는 lot 만 마감할 수 있습니다", "")
		return
	}
	for _, l := range st.content.Links {
		if l.From == stepID && l.FlowKind == haccp.FlowProduct {
			writeError(w, http.StatusUnprocessableEntity, "NOT_LAST_STEP", "마지막 단계에서만 마감할 수 있습니다", "")
			return
		}
	}
	if req.OutputKg <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_OUTPUT", "산출 중량(kg)이 필요합니다", "")
		return
	}
	// 마감 직전 상태로 한 번 더 판정해 이탈을 남긴다.
	ccps, err := s.evaluateLot(ctx, st, nowMs())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, k := range ccps {
		if k.Status == haccp.StatusNeedsInput || k.Status == haccp.StatusPending {
			writeError(w, http.StatusUnprocessableEntity, "CCP_INCOMPLETE",
				k.Code+" 기록이 비어 있습니다. 값을 입력하거나 해당 단계를 거쳐야 합니다", "")
			return
		}
	}
	devs, err := s.store.ListProcessDeviations(ctx, st.lot.LotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, d := range devs {
		if d.Status == "open" {
			writeError(w, http.StatusUnprocessableEntity, "OPEN_DEVIATIONS", "열린 이탈을 모두 처리해야 마감할 수 있습니다", "")
			return
		}
	}
	if !s.checkSigner(w, r, st.lot.SiteID, req.By, req.PIN, "by", roleAdmin, roleReviewer, roleMonitor) {
		return
	}
	now := nowMs()
	if err := s.store.InsertProcessLotEvent(ctx, st.lot.LotID, &storage.ProcessLotEvent{Kind: "exit", StepID: stepID, At: now}); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if err := s.store.CloseProcessLot(ctx, st.lot.LotID, req.OutputKg, req.By, now); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.app.AppendEvent(ctx, "process", "api", st.lot.SiteID, events.EventProcessLotClosed, st.lot.LotID,
		map[string]any{"lot_id": st.lot.LotID, "lot_no": st.lot.LotNo, "output_kg": req.OutputKg, "closed_by": req.By,
			"deviations": len(devs)})
	s.reloadAndWriteLot(w, r, st.lot.LotID)
}

// POST review — 마감된 기록을 검토자가 서명한다. 마감한 사람은 자기 기록을 검토할 수 없다.
func (s *Server) handleLotReview(w http.ResponseWriter, r *http.Request, st *lotState) {
	var req signedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	switch {
	case st.lot.Status != "closed" || st.lot.ReviewedBy != "":
		writeError(w, http.StatusConflict, "NOT_REVIEWABLE", "마감됐고 아직 검토되지 않은 lot 만 검토할 수 있습니다", "")
		return
	case req.By == st.lot.ClosedBy:
		writeError(w, http.StatusUnprocessableEntity, "SELF_REVIEW", "마감한 사람은 자기 기록을 검토할 수 없습니다", "")
		return
	}
	if !s.checkSigner(w, r, st.lot.SiteID, req.By, req.PIN, "by", roleAdmin, roleReviewer) {
		return
	}
	if err := s.store.ReviewProcessLot(r.Context(), st.lot.LotID, req.By, nowMs()); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.app.AppendEvent(r.Context(), "process", "api", st.lot.SiteID, events.EventProcessLotReviewed, st.lot.LotID,
		map[string]any{"lot_id": st.lot.LotID, "reviewed_by": req.By})
	s.reloadAndWriteLot(w, r, st.lot.LotID)
}

func (s *Server) reloadAndWriteLot(w http.ResponseWriter, r *http.Request, lotID string) {
	st, err := s.loadLot(r.Context(), lotID)
	if err != nil || st == nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", fmt.Sprint(err), "")
		return
	}
	s.writeLotDetail(w, r, st)
}
