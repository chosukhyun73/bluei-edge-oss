package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/training"
	"bluei.kr/edge/internal/vision"
)

// handleTrainingStatus aggregates: label progress vs validation gate, current
// job state, and the configured candidate algorithm summary so the UI can
// drive the wizard with one call.
func (s *Server) handleTrainingStatus(w http.ResponseWriter, r *http.Request) {
	tankID := r.URL.Query().Get("tank_id")
	algoID := r.URL.Query().Get("algorithm_id")

	vc, err := config.LoadVisionAlgorithms(defaultVisionAlgorithmsPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ALGO_LOAD_FAILED", err.Error(), "")
		return
	}
	if algoID == "" {
		if tankID != "" {
			for _, app := range vc.TankApplications {
				if app.TankID == tankID {
					algoID = app.AppliedVisionAlgorithmID
					break
				}
			}
		}
		if algoID == "" && len(vc.Algorithms) > 0 {
			algoID = vc.Algorithms[0].VisionAlgorithmID
		}
	}
	var algo *config.VisionAlgorithmEntry
	for i := range vc.Algorithms {
		if vc.Algorithms[i].VisionAlgorithmID == algoID {
			algo = &vc.Algorithms[i]
			break
		}
	}

	// 두 가지 게이트를 분리:
	// (a) dispute 게이트 — 운영자 정정 라벨 N건 (행동 회귀 검증용)
	// (b) bootstrap 게이트 — bbox 박스 N개 + 고유 프레임 M개 (검출기 학습용)
	disputeMin := 30
	bootstrapMinBoxes := 200
	bootstrapMinFrames := 40
	if algo != nil {
		if v, ok := numFromMap(algo.Validation, "required_operator_label_count"); ok && v > 0 {
			disputeMin = int(v)
		}
		if v, ok := numFromMap(algo.Validation, "bootstrap_min_boxes"); ok && v > 0 {
			bootstrapMinBoxes = int(v)
		}
		if v, ok := numFromMap(algo.Validation, "bootstrap_min_frames"); ok && v > 0 {
			bootstrapMinFrames = int(v)
		}
	}

	labelCount, totalObs, err := s.countLabels(r, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	bootBoxes, bootFrames, err := s.countBootstrapLabels(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	var current map[string]any
	if job, ok := s.train.Current(); ok {
		current = jobToMap(job)
	}

	bootstrapReady := bootBoxes >= bootstrapMinBoxes && bootFrames >= bootstrapMinFrames
	disputeReady := labelCount >= disputeMin

	active, _ := vision.ActiveState(algoID)

	resp := map[string]any{
		"tank_id":           tankID,
		"algorithm_id":      algoID,
		"algorithm_status":  algoStatus(algo),
		"algorithm_display": algoDisplay(algo),
		"active_weights":    active,
		"labels": map[string]any{
			"count":              labelCount,
			"required":           disputeMin,
			"observations":       totalObs,
			"can_start_training": disputeReady, // 하위 호환 (UI 구버전)
		},
		"bootstrap": map[string]any{
			"box_count":          bootBoxes,
			"frame_count":        bootFrames,
			"required_boxes":     bootstrapMinBoxes,
			"required_frames":    bootstrapMinFrames,
			"can_start_training": bootstrapReady,
			"hint":               "박스 좌표 + 사진 묶음으로 검출기(YOLO)를 fine-tune 합니다. 박스 200개·프레임 40장은 절대 최소이고, 박스 1,000+ 프레임 200+ 권장합니다.",
		},
		"dispute": map[string]any{
			"label_count":  labelCount,
			"required":     disputeMin,
			"can_validate": disputeReady,
			"hint":         "맞아요/틀려요 라벨은 행동 점수 정확도 검증에 사용되며, 검출기 학습에는 직접 쓰이지 않습니다.",
		},
		"can_start_training": bootstrapReady, // 검출기 학습 게이트가 우선
		"current_job":        current,
		"is_running":         s.train.IsRunning(),
		"validation_gate":    gateValues(algo),
		"safety_note":        "AI 학습이 진행 중에도 현재 AI 는 평소대로 일하고 있습니다. 새 AI 는 운영자가 [현장에 적용]을 누르기 전에는 사용되지 않습니다.",
	}
	writeJSON(w, http.StatusOK, resp)
}

// countBootstrapLabels returns total box count and unique frame (label_id) count.
func (s *Server) countBootstrapLabels(r *http.Request) (boxes, frames int, err error) {
	es, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionBootstrapLabelRecorded, Limit: 10000,
	})
	if err != nil {
		return 0, 0, err
	}
	for _, e := range es {
		var p events.VisionBootstrapLabelRecordedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		// 학습에 실제로 쓸 수 있으려면 스냅샷 jpg 가 있어야 함.
		// snapshot_ref 가 .jpg 로 끝나는 경우만 카운트.
		if !strings.HasSuffix(p.SnapshotRef, ".jpg") {
			continue
		}
		frames++
		boxes += len(p.Boxes)
	}
	return boxes, frames, nil
}

func (s *Server) handleTrainingJobsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleTrainingJobStart(w, r)
	case http.MethodGet:
		s.handleTrainingJobList(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type trainingJobStartRequest struct {
	Kind        string `json:"kind"`
	AlgorithmID string `json:"algorithm_id"`
	TankID      string `json:"tank_id"`
	DatasetPath string `json:"dataset_path"`
}

func (s *Server) handleTrainingJobStart(w http.ResponseWriter, r *http.Request) {
	var req trainingJobStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.Kind == "" {
		req.Kind = "vision-detector"
	}
	switch req.Kind {
	case "vision-detector":
		if req.AlgorithmID == "" {
			writeError(w, http.StatusUnprocessableEntity, "MISSING_ALGORITHM_ID", "algorithm_id is required", "")
			return
		}
		if req.DatasetPath == "" {
			req.DatasetPath = "local-ai/training/data/vision_labels.jsonl"
		}
	case "tank-baseline", "water-forecast":
		if req.TankID == "" {
			writeError(w, http.StatusUnprocessableEntity, "MISSING_TANK_ID", "tank_id is required for "+req.Kind, "")
			return
		}
	case "lrcn-finetune":
		// R5: LRCN fc2 fine-tune. algorithm_id 필수 (어느 LRCN 카드를 학습할지).
		if req.AlgorithmID == "" {
			writeError(w, http.StatusUnprocessableEntity, "MISSING_ALGORITHM_ID",
				"algorithm_id is required for lrcn-finetune", "")
			return
		}
		// dataset 은 run_training.py 가 events 에서 자동 export (R5.5).
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_KIND",
			"kind must be vision-detector, tank-baseline, water-forecast, or lrcn-finetune", "")
		return
	}
	job, err := s.train.Start(r.Context(), training.StartOptions{
		Kind:        req.Kind,
		AlgorithmID: req.AlgorithmID,
		TankID:      req.TankID,
		DatasetPath: req.DatasetPath,
	})
	if err != nil {
		if errors.Is(err, training.ErrBusy) {
			writeError(w, http.StatusConflict, "TRAINING_BUSY",
				"다른 학습 작업이 진행 중입니다. 끝나거나 취소된 뒤 다시 시도해주세요.", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "TRAINING_START_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job": jobToMap(job)})
}

func (s *Server) handleTrainingJobList(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 30, 200)
	es, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionTrainingJobUpdate, Limit: limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(es))
	for _, e := range es {
		var p events.VisionTrainingJobUpdatePayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"sequence":    e.Sequence,
			"event_id":    e.EventID,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"payload":     p,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleTrainingJobRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/vision/training/jobs/"), "/")
	if rel == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "job id required", "")
		return
	}
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 1 && parts[0] == "current":
		s.handleTrainingJobCurrent(w, r)
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		s.handleTrainingJobCancel(w, r, parts[0])
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/vision/training/jobs/current or /v1/vision/training/jobs/{id}/cancel", "")
	}
}

func (s *Server) handleTrainingJobCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	job, ok := s.train.Current()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": jobToMap(job)})
}

func (s *Server) handleTrainingJobCancel(w http.ResponseWriter, r *http.Request, jobID string) {
	_ = jobID // 현재 구현은 동시 1 job 만 있으므로 id 일치 확인은 생략
	if err := s.train.Cancel(r.Context()); err != nil {
		if errors.Is(err, training.ErrNotRunning) {
			writeError(w, http.StatusConflict, "NOT_RUNNING", "취소할 학습 작업이 없습니다.", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "CANCEL_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleVisionAlgorithmActionRoute serves /v1/vision/algorithms/{id}/{state|promote|rollback}.
// state    (GET):  active weights + history (manifest read-through)
// promote  (POST): copies candidate weights into the active manifest and records
//
//	an audit event so subsequent inference uses the new model.
//
// rollback (POST): restores the most recent prior weights from manifest history.
// In both cases the rules engine remains the safety owner; this endpoint only
// changes which weights file the vision service is told to load.
func (s *Server) handleVisionAlgorithmActionRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/vision/algorithms/"), "/")
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/vision/algorithms/{id}/{state|promote|rollback}", "")
		return
	}
	algoID := parts[0]
	action := parts[1]

	if action == "state" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st, err := vision.ActiveState(algoID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANIFEST_READ_FAILED", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, st)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if action != "promote" && action != "rollback" {
		writeError(w, http.StatusBadRequest, "INVALID_ACTION",
			"action must be state, promote or rollback", "")
		return
	}

	var body struct {
		PreviousAlgorithmID string `json:"previous_algorithm_id"`
		JobID               string `json:"job_id"`
		CandidatePath       string `json:"candidate_path"`
		OperatorID          string `json:"operator_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.OperatorID == "" {
		body.OperatorID = "operator"
	}

	// 후보 weights 결정 우선순위:
	// 1) 클라이언트가 명시한 candidate_path
	// 2) 같은 algorithm 의 가장 최근 completed 학습 job 의 candidate_path
	candidate := body.CandidatePath
	jobID := body.JobID
	if action == "promote" && candidate == "" {
		if job, ok := s.train.Current(); ok && job.AlgorithmID == algoID && job.Status == "completed" {
			candidate = job.CandidatePath + "/best.pt"
			if jobID == "" {
				jobID = job.JobID
			}
		}
	}

	var manifestState *vision.AlgorithmActiveState
	if action == "promote" {
		st, err := vision.Promote(algoID, candidate, jobID, body.OperatorID)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "PROMOTE_FAILED",
				"새 AI 적용 실패: "+err.Error(), "")
			return
		}
		manifestState = st
	} else {
		st, err := vision.Rollback(algoID, body.OperatorID)
		if err != nil {
			if errors.Is(err, vision.ErrNoHistory) {
				writeError(w, http.StatusConflict, "NO_HISTORY",
					"이전에 적용된 AI 가 없어 되돌릴 수 없습니다.", "")
				return
			}
			writeError(w, http.StatusInternalServerError, "ROLLBACK_FAILED", err.Error(), "")
			return
		}
		manifestState = st
	}

	payload := events.VisionAlgorithmAppliedPayload{
		Action:              action,
		AlgorithmID:         algoID,
		PreviousAlgorithmID: body.PreviousAlgorithmID,
		JobID:               jobID,
		OperatorID:          body.OperatorID,
		AppliedAt:           common.FormatTime(common.NowUTC()),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "vision", algoID,
		events.EventVisionAlgorithmApplied, algoID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"sequence": seq,
		"applied":  payload,
		"active":   manifestState,
		"note":     "Vision 서비스가 새 weights 를 사용하려면 재시작 또는 hot-reload 가 필요합니다 (다음 작업 후보).",
	})
}

// helpers ------------------------------------------------------------------

func jobToMap(j training.Job) map[string]any {
	m := map[string]any{
		"job_id":         j.JobID,
		"kind":           j.Kind,
		"algorithm_id":   j.AlgorithmID,
		"tank_id":        j.TankID,
		"status":         j.Status,
		"stage_label":    j.StageLabel,
		"progress_pct":   j.ProgressPct,
		"candidate_path": j.CandidatePath,
		"started_at":     common.FormatTime(j.StartedAt),
		"updated_at":     common.FormatTime(j.UpdatedAt),
	}
	if j.Error != "" {
		m["error"] = j.Error
	}
	if j.Metrics != nil {
		m["metrics"] = j.Metrics
	}
	return m
}

func algoStatus(a *config.VisionAlgorithmEntry) string {
	if a == nil {
		return "unknown"
	}
	return a.Status
}

func algoDisplay(a *config.VisionAlgorithmEntry) string {
	if a == nil {
		return ""
	}
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.VisionAlgorithmID
}

func gateValues(a *config.VisionAlgorithmEntry) map[string]any {
	if a == nil {
		return nil
	}
	return a.Validation
}

// numFromMap reads a numeric field from a yaml map[string]any. yaml decoder
// may return int, float64 or json.Number; handle the common cases.
func numFromMap(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func (s *Server) countLabels(r *http.Request, tankID string) (labels, observations int, err error) {
	obs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionObservationRecorded, Limit: 10000,
	})
	if err != nil {
		return 0, 0, err
	}
	disputes, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionObservationDisputed, Limit: 10000,
	})
	if err != nil {
		return 0, 0, err
	}

	disputed := map[string]bool{}
	for _, e := range disputes {
		var p events.VisionObservationDisputedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		disputed[p.ObservationID] = true
	}
	for _, e := range obs {
		var p events.VisionObservationPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if tankID != "" && p.TankID != tankID {
			continue
		}
		observations++
		if disputed[p.ObservationID] {
			labels++
		}
	}
	return labels, observations, nil
}
