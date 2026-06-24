package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type hatcheryTreatmentRequest struct {
	GroupID         string  `json:"group_id"`
	SubjectKind     string  `json:"subject_kind"` // spawn | larval
	BatchID         string  `json:"batch_id"`
	TankID          string  `json:"tank_id"`
	Species         string  `json:"species"`
	TreatmentType   string  `json:"treatment_type"`
	Substance       string  `json:"substance"`
	Dose            float64 `json:"dose"`
	DoseUnit        string  `json:"dose_unit"`
	Route           string  `json:"route"`
	Reason          string  `json:"reason"`
	WithdrawalUntil string  `json:"withdrawal_until"`
	AdministeredAt  string  `json:"administered_at"`
	OperatorID      string  `json:"operator_id"`
	ItemID          string  `json:"item_id"`
	ConsumedQty     float64 `json:"consumed_qty"`
	Notes           string  `json:"notes"`
}

// handleHatcheryTreatmentRoute — /v1/hatchery-treatments (collection).
func (s *Server) handleHatcheryTreatmentRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListHatcheryTreatments(w, r)
	case http.MethodPost:
		s.handleCreateHatcheryTreatment(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleHatcheryTreatmentItemRoute — /v1/hatchery-treatments/{treatment_id}.
func (s *Server) handleHatcheryTreatmentItemRoute(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/hatchery-treatments/"), "/")
	if id == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/hatchery-treatments/{treatment_id}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteHatcheryTreatment(w, r, id)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListHatcheryTreatments(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "group_id query param is required", "")
		return
	}
	items, err := s.store.ListHatcheryTreatmentsByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = make([]*storage.HatcheryTreatment, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "items": items, "count": len(items)})
}

func (s *Server) handleCreateHatcheryTreatment(w http.ResponseWriter, r *http.Request) {
	var req hatcheryTreatmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateHatcheryTreatment(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TREATMENT_BODY", err.Error(), "")
		return
	}
	t := treatmentFromRequest(req)
	t.TreatmentID = common.NewID("htreat")
	s.resolveTreatmentLot(r, t)
	if err := s.store.UpsertHatcheryTreatment(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// KDE 부분집합만 클라우드로 push(raw 미전송: item_id/consumed_qty/notes 제외). lot_code
	// 가 있어야 seed lot 계보에 귀속되므로, 없으면 push 생략(로컬 기록은 유지).
	if t.LotCode != "" {
		payload := map[string]any{
			"treatment_id":     t.TreatmentID,
			"group_id":         t.GroupID,
			"lot_code":         t.LotCode,
			"subject_kind":     t.SubjectKind,
			"species":          t.Species,
			"treatment_type":   t.TreatmentType,
			"substance":        t.Substance,
			"dose":             t.Dose,
			"dose_unit":        t.DoseUnit,
			"route":            t.Route,
			"reason":           t.Reason,
			"withdrawal_until": t.WithdrawalUntil,
			"administered_at":  t.AdministeredAt,
		}
		if _, err := s.sync.PushTreatment(r.Context(), payload); err != nil {
			slog.Warn("push hatchery treatment failed", "treatment_id", t.TreatmentID, "error", err)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": t, "lot_code": t.LotCode})
}

func (s *Server) handleDeleteHatcheryTreatment(w http.ResponseWriter, r *http.Request, id string) {
	existing, err := s.store.GetHatcheryTreatment(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "TREATMENT_NOT_FOUND", "treatment not found", "")
		return
	}
	if err := s.store.DeleteHatcheryTreatment(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": id})
}

// resolveTreatmentLot fills lot_code (and species) from the treated batch so the
// treatment attaches to the seed lot lineage. spawn→lot_code, larval→source_lot_code.
func (s *Server) resolveTreatmentLot(r *http.Request, t *storage.HatcheryTreatment) {
	if t.LotCode != "" || t.BatchID == "" {
		return
	}
	switch t.SubjectKind {
	case "spawn":
		if b, err := s.store.GetSpawnBatch(r.Context(), t.BatchID); err == nil && b != nil {
			t.LotCode = b.LotCode
			if t.Species == "" {
				t.Species = b.Species
			}
		}
	case "larval":
		if b, err := s.store.GetLarvalBatch(r.Context(), t.BatchID); err == nil && b != nil {
			t.LotCode = b.SourceLotCode
			if t.Species == "" {
				t.Species = b.Species
			}
		}
	}
}

func validateHatcheryTreatment(req hatcheryTreatmentRequest) error {
	if strings.TrimSpace(req.GroupID) == "" {
		return errRequired("group_id")
	}
	if req.SubjectKind != "spawn" && req.SubjectKind != "larval" {
		return apiInputError("subject_kind must be spawn or larval")
	}
	if !events.ValidTreatmentType(req.TreatmentType) {
		return apiInputError("invalid treatment_type (allowed: sex_reversal|disinfection|antibiotic|vaccine|chemical|probiotic|anesthetic|other)")
	}
	if strings.TrimSpace(req.Substance) == "" {
		return errRequired("substance")
	}
	if strings.TrimSpace(req.AdministeredAt) == "" {
		return errRequired("administered_at")
	}
	if req.Dose < 0 || req.ConsumedQty < 0 {
		return apiInputError("dose/consumed_qty must be >= 0")
	}
	return nil
}

func treatmentFromRequest(req hatcheryTreatmentRequest) *storage.HatcheryTreatment {
	return &storage.HatcheryTreatment{
		GroupID:         strings.TrimSpace(req.GroupID),
		SubjectKind:     strings.TrimSpace(req.SubjectKind),
		BatchID:         strings.TrimSpace(req.BatchID),
		TankID:          strings.TrimSpace(req.TankID),
		Species:         strings.TrimSpace(req.Species),
		TreatmentType:   strings.TrimSpace(req.TreatmentType),
		Substance:       strings.TrimSpace(req.Substance),
		Dose:            req.Dose,
		DoseUnit:        strings.TrimSpace(req.DoseUnit),
		Route:           strings.TrimSpace(req.Route),
		Reason:          strings.TrimSpace(req.Reason),
		WithdrawalUntil: strings.TrimSpace(req.WithdrawalUntil),
		AdministeredAt:  strings.TrimSpace(req.AdministeredAt),
		OperatorID:      strings.TrimSpace(req.OperatorID),
		ItemID:          strings.TrimSpace(req.ItemID),
		ConsumedQty:     req.ConsumedQty,
		Notes:           strings.TrimSpace(req.Notes),
	}
}
