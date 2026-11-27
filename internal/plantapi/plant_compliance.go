package plantapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/compliance"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/storage"
)

// 인증서 원장, 적용 제도 선택, 제도별 충족 현황.
// 기록은 한 벌이고 제도는 대응표로 읽는다 — docs/54 §18.4, §23.

const dateLayout = "2006-01-02"

var certSchemes = []string{"haccp_kr", "haccp_kr_export", "msc_coc", "asc_coc", "bap",
	"brcgs", "ifs", "sqf", "fssc22000", "iso22000", "other"}

// GET /v1/compliance/profiles — 제도 대응표 목록.
func (s *Server) handleComplianceProfiles(w http.ResponseWriter, r *http.Request) {
	ps, err := compliance.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PROFILE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": ps, "count": len(ps)})
}

// ── 인증서 원장 ──────────────────────────────────────────────────────────────

type plantCertificateRequest struct {
	CertID     string `json:"cert_id"`
	Scheme     string `json:"scheme"`
	CertNo     string `json:"cert_no"`
	Scope      string `json:"scope"`
	HolderKind string `json:"holder_kind"`
	HolderName string `json:"holder_name"`
	ValidFrom  string `json:"valid_from"`
	ValidUntil string `json:"valid_until"`
	VerifiedAt string `json:"verified_at"`
}

func validDate(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse(dateLayout, v)
	return err == nil
}

func (s *Server) handleListPlantCertificates(w http.ResponseWriter, r *http.Request, siteID string) {
	certs, err := s.store.ListPlantCertificates(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	today := time.Now().Format(dateLayout)
	out := make([]map[string]any, 0, len(certs))
	for _, c := range certs {
		out = append(out, plantCertificateToMap(c, today))
	}
	writeJSON(w, http.StatusOK, map[string]any{"certificates": out, "count": len(out)})
}

func (s *Server) handlePostPlantCertificate(w http.ResponseWriter, r *http.Request, siteID string) {
	var req plantCertificateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.HolderKind == "" {
		req.HolderKind = "own"
	}
	id := plantItemID(req.CertID, "cert")
	req.CertNo, req.HolderName = strings.TrimSpace(req.CertNo), strings.TrimSpace(req.HolderName)
	switch {
	case id == "" || req.CertNo == "" || !oneOf(req.Scheme, certSchemes...) || !oneOf(req.HolderKind, "own", "supplier"):
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CERTIFICATE",
			"cert_no 필수, scheme 은 "+strings.Join(certSchemes, "|")+", holder_kind 는 own|supplier", "")
		return
	case req.HolderKind == "supplier" && req.HolderName == "":
		writeError(w, http.StatusUnprocessableEntity, "MISSING_HOLDER_NAME", "공급자 인증서는 holder_name 필수", "")
		return
	case !validDate(req.ValidFrom) || !validDate(req.ValidUntil) || !validDate(req.VerifiedAt):
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DATE", "날짜는 YYYY-MM-DD", "")
		return
	case req.ValidFrom != "" && req.ValidUntil != "" && req.ValidUntil < req.ValidFrom:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DATE", "valid_until 이 valid_from 보다 앞섭니다", "")
		return
	}
	c := &storage.PlantCertificate{CertID: id, SiteID: siteID, Scheme: req.Scheme, CertNo: req.CertNo,
		Scope: strings.TrimSpace(req.Scope), HolderKind: req.HolderKind, HolderName: req.HolderName,
		ValidFrom: req.ValidFrom, ValidUntil: req.ValidUntil, VerifiedAt: req.VerifiedAt}
	if err := s.store.UpsertPlantCertificate(r.Context(), c); err != nil {
		writePlantUpsertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true,
		"certificate": plantCertificateToMap(c, time.Now().Format(dateLayout))})
}

// certExpired: YYYY-MM-DD 문자열은 그대로 비교할 수 있다. 만료일 당일까지 유효.
func certExpired(c *storage.PlantCertificate, today string) bool {
	return c.ValidUntil != "" && c.ValidUntil < today
}

func plantCertificateToMap(c *storage.PlantCertificate, today string) map[string]any {
	return map[string]any{"cert_id": c.CertID, "site_id": c.SiteID, "scheme": c.Scheme, "cert_no": c.CertNo,
		"scope": c.Scope, "holder_kind": c.HolderKind, "holder_name": c.HolderName,
		"valid_from": c.ValidFrom, "valid_until": c.ValidUntil, "verified_at": c.VerifiedAt,
		"expired": certExpired(c, today)}
}

// ── 적용 제도와 충족 현황 ────────────────────────────────────────────────────

func (s *Server) handlePutPlantCompliance(w http.ResponseWriter, r *http.Request, siteID string) {
	var req struct {
		ProfileIDs []string `json:"profile_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	ps, err := compliance.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PROFILE_ERROR", err.Error(), "")
		return
	}
	known := map[string]bool{}
	for _, p := range ps {
		known[p.ID] = true
	}
	for _, id := range req.ProfileIDs {
		if !known[id] {
			writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_PROFILE", "없는 제도: "+id, "")
			return
		}
	}
	if err := s.store.SetPlantComplianceProfiles(r.Context(), siteID, req.ProfileIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.handleGetPlantCompliance(w, r, siteID)
}

func (s *Server) handleGetPlantCompliance(w http.ResponseWriter, r *http.Request, siteID string) {
	selected, err := s.store.ListPlantComplianceProfiles(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	ps, err := compliance.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PROFILE_ERROR", err.Error(), "")
		return
	}
	st, err := s.complianceState(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	chosen := map[string]bool{}
	for _, id := range selected {
		chosen[id] = true
	}
	// 보관기간은 고른 제도 중 가장 긴 쪽에 맞춰야 한다.
	requiredYears := 0
	out := []map[string]any{}
	for _, p := range ps {
		if !chosen[p.ID] {
			continue
		}
		if p.MinRetentionYears > requiredYears {
			requiredYears = p.MinRetentionYears
		}
		results := compliance.Evaluate(p, st)
		counts := map[string]int{}
		blocking := 0
		for _, res := range results {
			counts[res.Status]++
			if res.Required && res.Status == compliance.StatusUnmet {
				blocking++
			}
		}
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "version": p.Version,
			"checked_at": p.CheckedAt, "retention_note": p.RetentionNote,
			"items": results, "counts": counts, "required_unmet": blocking})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"site_id": siteID, "profile_ids": selected, "profiles": out,
		"retention": map[string]any{"set_years": st.RetentionYears, "required_years": requiredYears,
			"ok": st.RetentionYears >= requiredYears},
	})
}

// complianceState reduces what the plant has registered to what the checks need.
func (s *Server) complianceState(ctx context.Context, siteID string) (compliance.State, error) {
	st := compliance.State{RetentionYears: 3, ValidOwnCerts: map[string]bool{}, AllCCPsAutoRecorded: true}
	prof, err := s.store.GetPlantProfile(ctx, siteID)
	if err != nil {
		return st, err
	}
	if prof != nil {
		st.LicenseComplete = prof.LicenseName != "" && prof.LicenseNo != "" && prof.LicenseAuthority != ""
		st.HumanWelfareSet = prof.HumanWelfarePolicy != ""
		st.HACCPCertNo = prof.HACCPCertNo
		st.RetentionYears = prof.RetentionYears
	}
	users, err := s.store.ListLocalUsers(ctx, siteID)
	if err != nil {
		return st, err
	}
	for _, u := range users {
		if u.Active && oneOf(u.Role, roleAdmin, roleReviewer) {
			st.HasReviewer = true
		}
	}
	certs, err := s.store.ListPlantCertificates(ctx, siteID)
	if err != nil {
		return st, err
	}
	today := time.Now().Format(dateLayout)
	for _, c := range certs {
		if c.HolderKind == "own" && !certExpired(c, today) {
			st.ValidOwnCerts[c.Scheme] = true
		}
	}
	plans, err := s.store.ListHACCPPlans(ctx, siteID)
	if err != nil {
		return st, err
	}
	for _, p := range plans {
		versions, err := s.store.ListHACCPPlanVersions(ctx, p.PlanID)
		if err != nil {
			return st, err
		}
		for _, v := range versions {
			if v.Status != haccpStatusPublished {
				continue
			}
			st.PublishedPlans++
			var content haccp.PlanContent
			if err := json.Unmarshal([]byte(v.ContentJSON), &content); err != nil {
				return st, err
			}
			for _, k := range content.CCPs {
				auto := false
				for _, l := range k.Limits {
					if l.AutomationLevel != "" && l.AutomationLevel != haccp.AutoManual {
						auto = true
					}
				}
				if !auto {
					st.AllCCPsAutoRecorded = false
				}
			}
		}
	}
	return st, nil
}
