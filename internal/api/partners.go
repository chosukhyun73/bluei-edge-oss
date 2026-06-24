package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 거래처(공급처/구매처) 마스터 + 거래처 증빙 서류(생산자면허 등).
// References: docs/49-gdst-traceability-contract.md.

// GET /v1/partners?type=  |  POST /v1/partners
func (s *Server) handlePartnersCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPartners(w, r)
	case http.MethodPost:
		s.handleCreatePartner(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListPartners(w http.ResponseWriter, r *http.Request) {
	pt := strings.TrimSpace(r.URL.Query().Get("type"))
	siteID := strings.TrimSpace(r.URL.Query().Get("site_id"))
	if pt != "" && !events.ValidPartnerType(pt) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TYPE", "type invalid: "+pt, "")
		return
	}
	partners, err := s.store.ListPartners(r.Context(), pt, siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"partners": partnersToMaps(partners),
		"count":    len(partners),
	})
}

type newPartnerRequest struct {
	PartnerType string `json:"partner_type"`
	Name        string `json:"name"`
	BusinessNo  string `json:"business_no"`
	LicenseNo   string `json:"license_no"`
	Contact     string `json:"contact"`
	Address     string `json:"address"`
	GLN         string `json:"gln"`
	Notes       string `json:"notes"`
	SiteID      string `json:"site_id"`
}

func (s *Server) handleCreatePartner(w http.ResponseWriter, r *http.Request) {
	var req newPartnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !events.ValidPartnerType(req.PartnerType) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TYPE",
			"partner_type must be hatchery|feed_supplier|drug_supplier|buyer|other", "")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_NAME", "name 필수", "")
		return
	}
	now := common.NowUTC()
	p := &storage.Partner{
		PartnerID:   common.NewID("partner"),
		PartnerType: req.PartnerType,
		Name:        req.Name,
		BusinessNo:  strings.TrimSpace(req.BusinessNo),
		LicenseNo:   strings.TrimSpace(req.LicenseNo),
		Contact:     strings.TrimSpace(req.Contact),
		Address:     strings.TrimSpace(req.Address),
		GLN:         strings.TrimSpace(req.GLN),
		Notes:       strings.TrimSpace(req.Notes),
		SiteID:      strings.TrimSpace(req.SiteID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.UpsertPartner(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "partner": partnerToMap(p)})
}

// /v1/partners/{id}  |  /v1/partners/{id}/documents
func (s *Server) handlePartnerItemRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/partners/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "documents" && parts[0] != "" {
		switch r.Method {
		case http.MethodPost:
			s.handlePartnerDocUpload(w, r, parts[0])
		case http.MethodGet:
			s.handlePartnerDocList(w, r, parts[0])
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet {
		p, err := s.store.GetPartner(r.Context(), parts[0])
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if p == nil {
			writeError(w, http.StatusNotFound, "PARTNER_NOT_FOUND", "partner not found", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"partner": partnerToMap(p)})
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/partners/{id}[/documents]", "")
}

// POST /v1/partners/{id}/documents — 거래처 서류(생산자면허 등) (subject=partner).
func (s *Server) handlePartnerDocUpload(w http.ResponseWriter, r *http.Request, partnerID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error(), "")
		return
	}
	docType := strings.TrimSpace(r.FormValue("doc_type"))
	if !events.ValidDocType(docType) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DOC_TYPE", "doc_type invalid: "+docType, "")
		return
	}
	blob, ok := s.saveDocBlob(w, r, "documents/_partner/"+partnerID)
	if !ok {
		return
	}
	operator := strings.TrimSpace(r.FormValue("operator_id"))
	if operator == "" {
		operator = "operator"
	}
	doc := storage.TankDocument{
		DocumentID:  blob.DocID,
		TankID:      "",
		CTEType:     "other",
		DocType:     docType,
		EventRef:    partnerID,
		Filename:    blob.Filename,
		MimeType:    blob.Mime,
		SizeBytes:   blob.Size,
		SHA256:      blob.SHA,
		StoredPath:  blob.RelPath,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
		UploadedBy:  operator,
		UploadedAt:  common.NowUTC(),
		SubjectType: "partner",
		SubjectID:   partnerID,
	}
	seq, ok := s.recordDocument(w, r, blob, doc)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"sequence":     seq,
		"document_id":  blob.DocID,
		"partner_id":   partnerID,
		"doc_type":     docType,
		"filename":     blob.Filename,
		"download_url": "/v1/documents/" + blob.DocID,
	})
}

func (s *Server) handlePartnerDocList(w http.ResponseWriter, r *http.Request, partnerID string) {
	docs, err := s.store.ListDocumentsBySubject(r.Context(), "partner", partnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"partner_id": partnerID,
		"documents":  documentsToMaps(docs),
		"count":      len(docs),
	})
}

func partnerToMap(p *storage.Partner) map[string]any {
	return map[string]any{
		"partner_id":   p.PartnerID,
		"partner_type": p.PartnerType,
		"name":         p.Name,
		"business_no":  p.BusinessNo,
		"license_no":   p.LicenseNo,
		"contact":      p.Contact,
		"address":      p.Address,
		"gln":          p.GLN,
		"notes":        p.Notes,
		"site_id":      p.SiteID,
	}
}

func partnersToMaps(partners []*storage.Partner) []map[string]any {
	out := make([]map[string]any, 0, len(partners))
	for _, p := range partners {
		out = append(out, partnerToMap(p))
	}
	return out
}
