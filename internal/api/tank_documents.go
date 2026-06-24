package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// GDST 증빙 서류 업로드/다운로드. subject = tank | inventory_purchase.
// blob 은 {data_dir}/documents/{...}, 메타는 tank.document.attached 이벤트 + tank_documents projection.
// References: docs/49-gdst-traceability-contract.md.

const maxDocBytes = 25 << 20 // 25 MiB

// 허용 확장자 → mime. GDST 서류는 보통 pdf/이미지/엑셀.
var allowedDocExt = map[string]string{
	".pdf":  "application/pdf",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".heic": "image/heic",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
}

func (s *Server) docsRoot() string {
	return filepath.Join(s.cfg.Edge.DataDir, "documents")
}

// savedBlob — saveDocBlob 결과.
type savedBlob struct {
	DocID    string
	Filename string
	RelPath  string
	FullPath string
	Mime     string
	Size     int64
	SHA      string
}

// saveDocBlob — multipart "file" 을 {data_dir}/{subdir}/{docID}{ext} 에 저장.
// 에러 시 HTTP 응답을 직접 쓰고 (nil,false) 반환.
func (s *Server) saveDocBlob(w http.ResponseWriter, r *http.Request, subdir string) (*savedBlob, bool) {
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "MISSING_FILE", "file field is required", "")
		return nil, false
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	mime, ok := allowedDocExt[ext]
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "UNSUPPORTED_FILE_TYPE",
			"허용 형식: pdf, jpg, png, heic, xlsx (받은 확장자: "+ext+")", "")
		return nil, false
	}

	docID := common.NewID("doc")
	relPath := filepath.Join(subdir, docID+ext)
	fullPath := filepath.Join(s.cfg.Edge.DataDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "MKDIR_FAILED", err.Error(), "")
		return nil, false
	}

	dst, err := os.Create(fullPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FILE_CREATE_FAILED", err.Error(), "")
		return nil, false
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dst, hasher), file)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(fullPath)
		writeError(w, http.StatusBadRequest, "UPLOAD_FAILED", copyErr.Error(), "")
		return nil, false
	}
	if closeErr != nil {
		_ = os.Remove(fullPath)
		writeError(w, http.StatusInternalServerError, "FILE_CLOSE_FAILED", closeErr.Error(), "")
		return nil, false
	}
	if size > maxDocBytes {
		_ = os.Remove(fullPath)
		writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "최대 25MB", "")
		return nil, false
	}

	return &savedBlob{
		DocID:    docID,
		Filename: header.Filename,
		RelPath:  relPath,
		FullPath: fullPath,
		Mime:     mime,
		Size:     size,
		SHA:      hex.EncodeToString(hasher.Sum(nil)),
	}, true
}

// recordDocument — blob 저장 후 이벤트 + projection 적재 (공통).
func (s *Server) recordDocument(w http.ResponseWriter, r *http.Request, blob *savedBlob, d storage.TankDocument) (int64, bool) {
	payload := events.TankDocumentAttachedPayload{
		DocumentID:  blob.DocID,
		TankID:      d.TankID,
		LotNo:       d.LotNo,
		CTEType:     d.CTEType,
		DocType:     d.DocType,
		EventRef:    d.EventRef,
		SubjectType: d.SubjectType,
		SubjectID:   d.SubjectID,
		Filename:    blob.Filename,
		MimeType:    blob.Mime,
		SizeBytes:   blob.Size,
		SHA256:      blob.SHA,
		StoredPath:  blob.RelPath,
		Notes:       d.Notes,
		UploadedBy:  d.UploadedBy,
		UploadedAt:  d.UploadedAt.Format(time.RFC3339Nano),
	}
	if err := payload.Validate(); err != nil {
		_ = os.Remove(blob.FullPath)
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return 0, false
	}
	seq, err := s.app.AppendEvent(r.Context(),
		"api", "documents", d.TankID,
		events.EventTankDocumentAttached, blob.DocID, payload)
	if err != nil {
		_ = os.Remove(blob.FullPath)
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return 0, false
	}
	if err := s.store.InsertTankDocument(r.Context(), &d); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return 0, false
	}
	return seq, true
}

// POST /v1/tanks/{id}/documents — CTE 증빙 서류 (subject=tank).
func (s *Server) handleDocumentUpload(w http.ResponseWriter, r *http.Request, tankID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error(), "")
		return
	}
	cteType := strings.TrimSpace(r.FormValue("cte_type"))
	docType := strings.TrimSpace(r.FormValue("doc_type"))
	if !events.ValidCTEType(cteType) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CTE_TYPE", "cte_type invalid: "+cteType, "")
		return
	}
	if !events.ValidDocType(docType) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DOC_TYPE", "doc_type invalid: "+docType, "")
		return
	}

	blob, ok := s.saveDocBlob(w, r, filepath.Join("documents", tankID))
	if !ok {
		return
	}

	operator := strings.TrimSpace(r.FormValue("operator_id"))
	if operator == "" {
		operator = "operator"
	}
	lotNo := strings.TrimSpace(r.FormValue("lot_no"))
	if lotNo == "" {
		_, lotNo = s.activeLineage(r, tankID)
	}

	doc := storage.TankDocument{
		DocumentID:  blob.DocID,
		TankID:      tankID,
		LotNo:       lotNo,
		CTEType:     cteType,
		DocType:     docType,
		EventRef:    strings.TrimSpace(r.FormValue("event_ref")),
		Filename:    blob.Filename,
		MimeType:    blob.Mime,
		SizeBytes:   blob.Size,
		SHA256:      blob.SHA,
		StoredPath:  blob.RelPath,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
		UploadedBy:  operator,
		UploadedAt:  common.NowUTC(),
		SubjectType: "tank",
		SubjectID:   tankID,
	}
	seq, ok := s.recordDocument(w, r, blob, doc)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"sequence":     seq,
		"document_id":  blob.DocID,
		"tank_id":      tankID,
		"cte_type":     cteType,
		"doc_type":     docType,
		"filename":     blob.Filename,
		"size_bytes":   blob.Size,
		"sha256":       blob.SHA,
		"download_url": "/v1/documents/" + blob.DocID,
	})
}

// POST /v1/inventory/purchases/{purchase_id}/documents — 구매 증빙 서류 (subject=inventory_purchase).
func (s *Server) handleInventoryDocUpload(w http.ResponseWriter, r *http.Request, purchaseID string) {
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

	blob, ok := s.saveDocBlob(w, r, filepath.Join("documents", "_inventory", purchaseID))
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
		EventRef:    purchaseID,
		Filename:    blob.Filename,
		MimeType:    blob.Mime,
		SizeBytes:   blob.Size,
		SHA256:      blob.SHA,
		StoredPath:  blob.RelPath,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
		UploadedBy:  operator,
		UploadedAt:  common.NowUTC(),
		SubjectType: "inventory_purchase",
		SubjectID:   purchaseID,
	}
	seq, ok := s.recordDocument(w, r, blob, doc)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"sequence":     seq,
		"document_id":  blob.DocID,
		"purchase_id":  purchaseID,
		"doc_type":     docType,
		"filename":     blob.Filename,
		"size_bytes":   blob.Size,
		"sha256":       blob.SHA,
		"download_url": "/v1/documents/" + blob.DocID,
	})
}

// GET /v1/tanks/{id}/documents — tank 의 첨부 서류 목록.
func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request, tankID string) {
	docs, err := s.store.ListTankDocuments(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":   tankID,
		"documents": documentsToMaps(docs),
		"count":     len(docs),
	})
}

// GET /v1/documents/{doc_id} — 다운로드 (경로 검증 + ServeFile).
func (s *Server) handleDocumentDownload(w http.ResponseWriter, r *http.Request) {
	docID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/documents/"), "/")
	if docID == "" || strings.Contains(docID, "/") {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/documents/{doc_id}", "")
		return
	}
	doc, err := s.store.GetTankDocument(r.Context(), docID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "DOC_NOT_FOUND", "document not found", "")
		return
	}

	root := filepath.Clean(s.docsRoot())
	full := filepath.Clean(filepath.Join(s.cfg.Edge.DataDir, doc.StoredPath))
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "OUTSIDE_DOCS", "stored_path가 documents 밖을 가리킵니다", "")
		return
	}
	fi, statErr := os.Stat(full)
	if statErr != nil || fi.IsDir() {
		writeError(w, http.StatusNotFound, "DOC_FILE_NOT_FOUND", "파일이 존재하지 않습니다", "")
		return
	}

	w.Header().Set("Content-Type", doc.MimeType)
	w.Header().Set("Content-Disposition", "inline; filename=\""+sanitizeFilename(doc.Filename)+"\"")
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, full)
}

func documentsToMaps(docs []*storage.TankDocument) []map[string]any {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, map[string]any{
			"document_id":  d.DocumentID,
			"tank_id":      d.TankID,
			"lot_no":       d.LotNo,
			"cte_type":     d.CTEType,
			"doc_type":     d.DocType,
			"event_ref":    d.EventRef,
			"filename":     d.Filename,
			"mime_type":    d.MimeType,
			"size_bytes":   d.SizeBytes,
			"uploaded_by":  d.UploadedBy,
			"uploaded_at":  d.UploadedAt.Format(time.RFC3339Nano),
			"download_url": "/v1/documents/" + d.DocumentID,
		})
	}
	return out
}

// sanitizeFilename — 헤더 인젝션 방지용 따옴표/개행 제거.
func sanitizeFilename(name string) string {
	rep := strings.NewReplacer("\"", "", "\r", "", "\n", "")
	return rep.Replace(name)
}
