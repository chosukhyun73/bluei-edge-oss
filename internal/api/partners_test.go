package api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPartnerStockingSupplier — 거래처 등록 → 입식 supplier_id 연결 → source_hatchery 자동 채움 + 거래처 서류.
func TestPartnerStockingSupplier(t *testing.T) {
	s := newTestServerWithApp(t)
	s.cfg.Edge.DataDir = t.TempDir()
	ctx := context.Background()

	// 1) 거래처(부화장) 등록
	w := postJSONTo(t, s.handleCreatePartner, "/v1/partners", map[string]any{
		"partner_type": "hatchery", "name": "OO부화장", "license_no": "HATCH-2026-01",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create partner: %d %s", w.Code, w.Body.String())
	}
	partnerID := decodeMap(t, w)["partner"].(map[string]any)["partner_id"].(string)
	if partnerID == "" {
		t.Fatal("missing partner_id")
	}

	// 2) 입식 — supplier_id 만 주고 source_hatchery 는 비움 → 거래처명 자동 채움
	body := defaultStockingBody()
	body["supplier_id"] = partnerID
	if sw := postStocking(t, s, "tank_01", body); sw.Code != http.StatusOK {
		t.Fatalf("stocking: %d %s", sw.Code, sw.Body.String())
	}
	lc, _ := s.store.GetTankLifecycle(ctx, "tank_01")
	if lc == nil || lc.SourceHatchery != "OO부화장" {
		t.Fatalf("source_hatchery should be filled from partner, got %v", lc)
	}

	// 3) 거래처 서류(생산자면허) 업로드
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("doc_type", "producer_license")
	fw, _ := mw.CreateFormFile("file", "license.pdf")
	_, _ = fw.Write([]byte("%PDF license"))
	_ = mw.Close()
	dreq := httptest.NewRequest(http.MethodPost, "/v1/partners/"+partnerID+"/documents", &buf)
	dreq.Header.Set("Content-Type", mw.FormDataContentType())
	dw := httptest.NewRecorder()
	s.handlePartnerItemRoute(dw, dreq)
	if dw.Code != http.StatusOK {
		t.Fatalf("partner doc upload: %d %s", dw.Code, dw.Body.String())
	}

	// 4) 거래처 서류 목록
	lreq := httptest.NewRequest(http.MethodGet, "/v1/partners/"+partnerID+"/documents", nil)
	lw := httptest.NewRecorder()
	s.handlePartnerItemRoute(lw, lreq)
	if cnt := decodeMap(t, lw)["count"].(float64); cnt != 1 {
		t.Fatalf("partner docs count = %v, want 1", cnt)
	}

	// 5) 목록 조회 (type 필터)
	qreq := httptest.NewRequest(http.MethodGet, "/v1/partners?type=hatchery", nil)
	qw := httptest.NewRecorder()
	s.handlePartnersCollection(qw, qreq)
	if cnt := decodeMap(t, qw)["count"].(float64); cnt != 1 {
		t.Fatalf("hatchery partners count = %v, want 1", cnt)
	}
}
