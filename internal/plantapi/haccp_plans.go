package plantapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

// 가공공장 HACCP 계획(공정도·CCP) 등록·버전·게시.
// 계획 내용은 버전마다 haccp.PlanContent 한 덩어리로 저장한다. 초안만 고칠 수 있고,
// 게시 규칙(haccp.ValidateForPublish)을 통과해야 게시된다. 게시본은 바뀌지 않는다.
// References: docs/54-haccp-process-flow-ccp-design.md.

const (
	haccpStatusDraft     = "draft"
	haccpStatusPublished = "published"
)

// GET /v1/haccp/templates
func (s *Server) handleHACCPTemplates(w http.ResponseWriter, r *http.Request) {
	all, err := haccp.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TEMPLATE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(all))
	for _, t := range all {
		out = append(out, map[string]any{
			"id": t.ID, "name": t.Name, "product_group": t.ProductGroup,
			"source": t.Source, "notice": t.Notice,
			"step_count": len(t.Content.Steps), "ccp_count": len(t.Content.CCPs),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out, "count": len(out)})
}

// GET /v1/haccp/plans?site_id=  |  POST /v1/haccp/plans
func (s *Server) handleHACCPPlansCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		plans, err := s.store.ListHACCPPlans(r.Context(), strings.TrimSpace(r.URL.Query().Get("site_id")))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		out := make([]map[string]any, 0, len(plans))
		for _, p := range plans {
			out = append(out, haccpPlanToMap(p))
		}
		writeJSON(w, http.StatusOK, map[string]any{"plans": out, "count": len(out)})
	case http.MethodPost:
		s.handleCreateHACCPPlan(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type newHACCPPlanRequest struct {
	SiteID       string `json:"site_id"`
	Name         string `json:"name"`
	ProductGroup string `json:"product_group"`
	TemplateID   string `json:"template_id"` // 비우면 빈 초안
}

func (s *Server) handleCreateHACCPPlan(w http.ResponseWriter, r *http.Request) {
	var req newHACCPPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.SiteID == "" || req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "site_id, name 필수", "")
		return
	}
	siteType, err := s.store.GetSiteType(r.Context(), req.SiteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if siteType != string(site.TypeProcessing) {
		writeError(w, http.StatusUnprocessableEntity, "NOT_PROCESSING_SITE",
			"가공 사업장(site_type=processing)에만 계획을 만들 수 있습니다", "")
		return
	}

	content := haccp.PlanContent{}
	if req.TemplateID != "" {
		tpl, err := haccp.LoadTemplate(req.TemplateID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "TEMPLATE_ERROR", err.Error(), "")
			return
		}
		if tpl == nil {
			writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_TEMPLATE", "template_id 가 없습니다: "+req.TemplateID, "")
			return
		}
		content = tpl.Content
		if req.ProductGroup == "" {
			req.ProductGroup = tpl.ProductGroup
		}
	}
	contentJSON, _ := json.Marshal(content)

	now := common.NowUTC()
	plan := &storage.HACCPPlan{
		PlanID: common.NewID("hplan"), SiteID: req.SiteID, ProductGroup: strings.TrimSpace(req.ProductGroup),
		Name: req.Name, CreatedAt: now, UpdatedAt: now,
	}
	first := &storage.HACCPPlanVersion{
		VersionID: common.NewID("hver"), PlanID: plan.PlanID, VersionNo: 1, Status: haccpStatusDraft,
		TemplateRef: req.TemplateID, ContentJSON: string(contentJSON), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateHACCPPlan(r.Context(), plan, first); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "plan": haccpPlanToMap(plan), "version": haccpVersionToMap(first, true),
	})
}

// /v1/haccp/plans/{plan_id}
// /v1/haccp/plans/{plan_id}/versions
// /v1/haccp/plans/{plan_id}/versions/{version_id}
// /v1/haccp/plans/{plan_id}/versions/{version_id}/publish
func (s *Server) handleHACCPPlanItemRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/haccp/plans/"), "/")
	parts := strings.Split(rest, "/")
	if parts[0] == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/haccp/plans/{plan_id}", "")
		return
	}
	plan, err := s.store.GetHACCPPlan(r.Context(), parts[0])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if plan == nil {
		writeError(w, http.StatusNotFound, "PLAN_NOT_FOUND", "plan not found", "")
		return
	}

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleGetHACCPPlan(w, r, plan)
	case len(parts) == 2 && parts[1] == "versions" && r.Method == http.MethodPost:
		s.handleNewHACCPDraft(w, r, plan)
	case len(parts) == 3 && parts[1] == "versions" && r.Method == http.MethodGet:
		s.handleGetHACCPVersion(w, r, plan, parts[2])
	case len(parts) == 3 && parts[1] == "versions" && r.Method == http.MethodPut:
		s.handleSaveHACCPDraft(w, r, plan, parts[2])
	case len(parts) == 4 && parts[1] == "versions" && parts[3] == "publish" && r.Method == http.MethodPost:
		s.handlePublishHACCPVersion(w, r, plan, parts[2])
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/haccp/plans/{plan_id}[/versions[/{version_id}[/publish]]]", "")
	}
}

func (s *Server) handleGetHACCPPlan(w http.ResponseWriter, r *http.Request, plan *storage.HACCPPlan) {
	versions, err := s.store.ListHACCPPlanVersions(r.Context(), plan.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		out = append(out, haccpVersionToMap(v, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": haccpPlanToMap(plan), "versions": out})
}

// loadHACCPVersion fetches a version and checks it belongs to the plan in the path.
func (s *Server) loadHACCPVersion(w http.ResponseWriter, r *http.Request, plan *storage.HACCPPlan, versionID string) *storage.HACCPPlanVersion {
	v, err := s.store.GetHACCPPlanVersion(r.Context(), versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return nil
	}
	if v == nil || v.PlanID != plan.PlanID {
		writeError(w, http.StatusNotFound, "VERSION_NOT_FOUND", "version not found", "")
		return nil
	}
	return v
}

func (s *Server) handleGetHACCPVersion(w http.ResponseWriter, r *http.Request, plan *storage.HACCPPlan, versionID string) {
	v := s.loadHACCPVersion(w, r, plan, versionID)
	if v == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": haccpVersionToMap(v, true)})
}

type saveHACCPDraftRequest struct {
	Content haccp.PlanContent `json:"content"`
}

// PUT — 초안 저장. 구조가 맞지 않으면 저장하지 않는다. 게시본은 고칠 수 없다.
func (s *Server) handleSaveHACCPDraft(w http.ResponseWriter, r *http.Request, plan *storage.HACCPPlan, versionID string) {
	v := s.loadHACCPVersion(w, r, plan, versionID)
	if v == nil {
		return
	}
	if v.Status != haccpStatusDraft {
		writeError(w, http.StatusConflict, "VERSION_NOT_DRAFT", "게시된 버전은 고칠 수 없습니다. 새 초안을 만드십시오", "")
		return
	}
	var req saveHACCPDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if issues := haccp.ValidateStructure(&req.Content); len(issues) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  map[string]any{"code": "INVALID_STRUCTURE", "message": "계획 구조가 올바르지 않습니다"},
			"issues": issues,
		})
		return
	}
	contentJSON, _ := json.Marshal(req.Content)
	ok, err := s.store.UpdateHACCPDraftContent(r.Context(), v.VersionID, string(contentJSON))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "VERSION_NOT_DRAFT", "게시된 버전은 고칠 수 없습니다. 새 초안을 만드십시오", "")
		return
	}
	v.ContentJSON = string(contentJSON)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": haccpVersionToMap(v, true)})
}

type newHACCPDraftRequest struct {
	ChangeReason string `json:"change_reason"`
}

// POST /versions — 최신 게시본을 복사해 새 초안을 만든다. 초안은 계획당 하나만 둔다.
func (s *Server) handleNewHACCPDraft(w http.ResponseWriter, r *http.Request, plan *storage.HACCPPlan) {
	var req newHACCPDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.ChangeReason = strings.TrimSpace(req.ChangeReason)
	if req.ChangeReason == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_CHANGE_REASON", "개정 사유(change_reason) 필수", "")
		return
	}
	versions, err := s.store.ListHACCPPlanVersions(r.Context(), plan.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var published *storage.HACCPPlanVersion
	maxNo := 0
	for _, v := range versions {
		if v.Status == haccpStatusDraft {
			writeError(w, http.StatusConflict, "DRAFT_EXISTS", "작성 중인 초안이 이미 있습니다", "")
			return
		}
		if v.Status == haccpStatusPublished {
			published = v
		}
		if v.VersionNo > maxNo {
			maxNo = v.VersionNo
		}
	}
	if published == nil {
		writeError(w, http.StatusConflict, "NO_PUBLISHED_VERSION", "게시된 버전이 없습니다", "")
		return
	}
	now := common.NowUTC()
	draft := &storage.HACCPPlanVersion{
		VersionID: common.NewID("hver"), PlanID: plan.PlanID, VersionNo: maxNo + 1, Status: haccpStatusDraft,
		TemplateRef: published.TemplateRef, ContentJSON: published.ContentJSON,
		ChangeReason: req.ChangeReason, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.InsertHACCPPlanVersion(r.Context(), draft); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "version": haccpVersionToMap(draft, true)})
}

type publishHACCPRequest struct {
	OnsiteConfirmedBy  string `json:"onsite_confirmed_by"` // 공정흐름도 현장 확인자 (admin 또는 reviewer 계정)
	OnsiteConfirmedPIN string `json:"onsite_confirmed_pin"`
	PublishedBy        string `json:"published_by"` // 게시자 (admin 계정)
	PublishedPIN       string `json:"published_pin"`
}

// POST /publish — 게시 규칙을 통과한 초안만 게시한다.
func (s *Server) handlePublishHACCPVersion(w http.ResponseWriter, r *http.Request, plan *storage.HACCPPlan, versionID string) {
	v := s.loadHACCPVersion(w, r, plan, versionID)
	if v == nil {
		return
	}
	if v.Status != haccpStatusDraft {
		writeError(w, http.StatusConflict, "VERSION_NOT_DRAFT", "초안만 게시할 수 있습니다", "")
		return
	}
	var req publishHACCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.OnsiteConfirmedBy = strings.TrimSpace(req.OnsiteConfirmedBy)
	req.PublishedBy = strings.TrimSpace(req.PublishedBy)
	if req.OnsiteConfirmedBy == "" || req.PublishedBy == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_SIGNATURE",
			"현장 확인자(onsite_confirmed_by)와 게시자(published_by) 필수", "")
		return
	}

	// 서명: 현장 확인은 관리자 또는 검토자, 게시는 관리자. 각자 PIN 으로 본인 확인한다.
	if !s.checkSigner(w, r, plan.SiteID, req.OnsiteConfirmedBy, req.OnsiteConfirmedPIN, "onsite_confirmed_by", roleAdmin, roleReviewer) ||
		!s.checkSigner(w, r, plan.SiteID, req.PublishedBy, req.PublishedPIN, "published_by", roleAdmin) {
		return
	}

	var content haccp.PlanContent
	if err := json.Unmarshal([]byte(v.ContentJSON), &content); err != nil {
		writeError(w, http.StatusInternalServerError, "CONTENT_CORRUPT", err.Error(), "")
		return
	}
	issues := haccp.ValidateForPublish(&content)
	refs, err := s.plantRefs(r.Context(), plan.SiteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	issues = append(issues, haccp.ValidateRefs(&content, refs)...)
	if len(issues) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  map[string]any{"code": "PUBLISH_BLOCKED", "message": "게시 조건을 채우지 못했습니다"},
			"issues": issues,
		})
		return
	}
	hash, err := haccp.ContentHash(&content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HASH_ERROR", err.Error(), "")
		return
	}

	// 직전 게시본과 CCP 를 비교 — 바뀌었으면 인증 변경 신청 대상.
	versions, err := s.store.ListHACCPPlanVersions(r.Context(), plan.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, pv := range versions {
		if pv.Status == haccpStatusPublished {
			var prev haccp.PlanContent
			if err := json.Unmarshal([]byte(pv.ContentJSON), &prev); err == nil {
				v.CCPChanged = haccp.CCPsChanged(&prev, &content)
			}
		}
	}

	now := common.NowUTC()
	v.ContentHash = hash
	v.OnsiteConfirmedBy = req.OnsiteConfirmedBy
	v.OnsiteConfirmedAt = &now
	v.PublishedBy = req.PublishedBy
	v.PublishedAt = &now
	ok, err := s.store.PublishHACCPPlanVersion(r.Context(), v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "VERSION_NOT_DRAFT", "초안만 게시할 수 있습니다", "")
		return
	}
	v.Status = haccpStatusPublished

	if _, err := s.app.AppendEvent(r.Context(), "api", "haccp_plan", plan.SiteID,
		events.EventHACCPPlanPublished, v.VersionID, map[string]any{
			"plan_id": plan.PlanID, "version_id": v.VersionID, "version_no": v.VersionNo,
			"content_hash": hash, "change_reason": v.ChangeReason, "ccp_changed": v.CCPChanged,
			"onsite_confirmed_by": v.OnsiteConfirmedBy, "published_by": v.PublishedBy,
		}); err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": haccpVersionToMap(v, true)})
}

// checkSigner verifies one signature and writes the error response itself.
// field names the request field, so the caller can tell which signature failed.
func (s *Server) checkSigner(w http.ResponseWriter, r *http.Request, siteID, userID, pin, field string, roles ...string) bool {
	_, err := s.verifySigner(r.Context(), siteID, userID, pin, roles...)
	switch {
	case err == nil:
		return true
	case errors.Is(err, errSignerNotFound), errors.Is(err, errSignerRole), errors.Is(err, errSignerPIN):
		writeError(w, http.StatusForbidden, "SIGNATURE_REJECTED", field+": "+err.Error(), "")
	default:
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
	}
	return false
}

func haccpPlanToMap(p *storage.HACCPPlan) map[string]any {
	return map[string]any{
		"plan_id": p.PlanID, "site_id": p.SiteID, "product_group": p.ProductGroup, "name": p.Name,
		"created_at": common.FormatTime(p.CreatedAt), "updated_at": common.FormatTime(p.UpdatedAt),
	}
}

// haccpVersionToMap renders a version. With full=true it includes the content
// and, for a draft, the list of things still blocking publication.
func haccpVersionToMap(v *storage.HACCPPlanVersion, full bool) map[string]any {
	m := map[string]any{
		"version_id": v.VersionID, "plan_id": v.PlanID, "version_no": v.VersionNo, "status": v.Status,
		"template_ref": v.TemplateRef, "content_hash": v.ContentHash, "change_reason": v.ChangeReason,
		"ccp_changed": v.CCPChanged, "onsite_confirmed_by": v.OnsiteConfirmedBy, "published_by": v.PublishedBy,
		"created_at": common.FormatTime(v.CreatedAt), "updated_at": common.FormatTime(v.UpdatedAt),
	}
	if v.PublishedAt != nil {
		m["published_at"] = common.FormatTime(*v.PublishedAt)
	}
	if !full {
		return m
	}
	var content haccp.PlanContent
	if err := json.Unmarshal([]byte(v.ContentJSON), &content); err == nil {
		m["content"] = content
		if v.Status == haccpStatusDraft {
			issues := haccp.ValidateForPublish(&content)
			if issues == nil {
				issues = []haccp.Issue{}
			}
			m["publish_issues"] = issues
		}
	}
	return m
}
