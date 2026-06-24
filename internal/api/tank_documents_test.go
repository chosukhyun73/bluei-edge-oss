package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// uploadDoc — multipart 문서 업로드 helper.
func uploadDoc(t *testing.T, s *Server, tankID, cteType, docType, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("cte_type", cteType)
	_ = mw.WriteField("doc_type", docType)
	_ = mw.WriteField("operator_id", "op1")
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/tanks/"+tankID+"/documents", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.handleTankTraceabilityRoute(w, req)
	return w
}

// TestDocumentUploadDownloadRoundTrip — 첨부 → 저장 → 다운로드(바이트 동일) → traceability 노출.
func TestDocumentUploadDownloadRoundTrip(t *testing.T) {
	s := newTestServerWithApp(t)
	s.cfg.Edge.DataDir = t.TempDir()
	ctx := context.Background()

	content := []byte("%PDF-1.4 처방전 테스트")
	w := uploadDoc(t, s, "tank_01", "treatment", "prescription", "rx.pdf", content)
	if w.Code != http.StatusOK {
		t.Fatalf("upload expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	docID, _ := resp["document_id"].(string)
	if docID == "" {
		t.Fatal("missing document_id")
	}

	// projection 확인
	doc, _ := s.store.GetTankDocument(ctx, docID)
	if doc == nil || doc.DocType != "prescription" || doc.SizeBytes != int64(len(content)) {
		t.Fatalf("document not stored correctly: %+v", doc)
	}

	// 다운로드 — 바이트 동일
	dreq := httptest.NewRequest(http.MethodGet, "/v1/documents/"+docID, nil)
	dw := httptest.NewRecorder()
	s.handleDocumentDownload(dw, dreq)
	if dw.Code != http.StatusOK {
		t.Fatalf("download expected 200, got %d", dw.Code)
	}
	if !bytes.Equal(dw.Body.Bytes(), content) {
		t.Errorf("downloaded bytes differ from uploaded")
	}

	// 허용되지 않는 확장자는 거부
	if bad := uploadDoc(t, s, "tank_01", "treatment", "prescription", "evil.exe", []byte("x")); bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for .exe, got %d", bad.Code)
	}

	// traceability 응답에 문서 노출
	gw := getTraceability(t, s, "tank_01")
	var tl map[string]any
	json.Unmarshal(gw.Body.Bytes(), &tl)
	docs, _ := tl["documents"].([]any)
	if len(docs) < 1 {
		t.Errorf("traceability should list >=1 document, got %d", len(docs))
	}
}

// TestTransferSale — 판매(sale): destination_name 필수, 출발 수조 마감, 도착 lineage 없음.
func TestTransferSale(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	body := defaultStockingBody()
	body["lot_no"] = "LOT-SALE-001"
	if w := postStocking(t, s, "tank_01", body); w.Code != http.StatusOK {
		t.Fatalf("stocking: %d %s", w.Code, w.Body.String())
	}

	// destination_name 없으면 422
	if w := postTrace(t, s, "tank_01", "/transfer", map[string]any{
		"transfer_type": "sale", "moved_count": 500, "operator_id": "op1",
	}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("sale without destination_name expected 422, got %d", w.Code)
	}

	// 정상 판매
	w := postTrace(t, s, "tank_01", "/transfer", map[string]any{
		"transfer_type":    "sale",
		"destination_name": "강릉수산",
		"vehicle_info":     "12가3456",
		"moved_count":      990,
		"operator_id":      "op1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("sale expected 200, got %d: %s", w.Code, w.Body.String())
	}

	src, _ := s.store.GetTankLifecycle(ctx, "tank_01")
	if src == nil || src.Status != "transferred" {
		t.Errorf("source should be closed after sale, got %v", src)
	}
}
