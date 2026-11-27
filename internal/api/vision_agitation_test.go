package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bluei.kr/edge/internal/vision_pipeline"
)

// 추론 모니터링이 "화면이 보는 기간만" 받게 하는 두 파라미터 (2026-08-14).
// 종전에는 기간 선택과 무관하게 항상 23일치(series 표본 25만)를 30초마다 내려보내
// 렌더러가 60~80MB/분씩 늘다 죽었다.
func TestAgitationRecent_SinceAndMetaOnly(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	appendClip := func(clipID string, capturedAt time.Time) {
		t.Helper()
		_, err := s.app.AppendEvent(ctx, "test", "", "tank_01",
			vision_pipeline.EventAgitationRecorded, clipID, map[string]any{
				"clip_id":     clipID,
				"camera_id":   "cam_top_7_1_2_L",
				"captured_at": capturedAt.UTC().Format(time.RFC3339Nano),
				"n_seconds":   3,
				"baseline":    5.0,
				"peak":        9.0,
				"series":      []float64{5, 7, 9},
			})
		if err != nil {
			t.Fatalf("append %s: %v", clipID, err)
		}
	}
	appendClip("clip_old", time.Now().Add(-72*time.Hour))
	appendClip("clip_new", time.Now().Add(-30*time.Minute))

	get := func(qs string) []map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/v1/vision/agitation/recent"+qs, nil)
		w := httptest.NewRecorder()
		s.handleVisionAgitationRecent(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d for %q", w.Code, qs)
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return body.Items
	}

	if got := len(get("?limit=500")); got != 2 {
		t.Fatalf("파라미터 없으면 종전대로 전부: got %d want 2", got)
	}

	since := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	recent := get("?limit=500&since=" + since)
	if len(recent) != 1 {
		t.Fatalf("since 로 창 밖 클립이 걸러져야 한다: got %d want 1", len(recent))
	}
	if recent[0]["clip_id"] != "clip_new" {
		t.Fatalf("남은 것이 창 안 클립이어야 한다: got %v", recent[0]["clip_id"])
	}
	if _, ok := recent[0]["series"]; !ok {
		t.Error("meta_only 없이는 series 가 있어야 한다 (차트 표시용)")
	}

	// meta_only — 대표값만. 보정 계수 계산은 peak/baseline 만 쓰므로 series 는 낭비다.
	meta := get("?limit=500&meta_only=1")
	if len(meta) != 2 {
		t.Fatalf("meta_only 는 건수를 바꾸지 않는다: got %d want 2", len(meta))
	}
	for _, it := range meta {
		if _, ok := it["series"]; ok {
			t.Fatalf("meta_only 인데 series 가 실렸다: %v", it["clip_id"])
		}
		if _, ok := it["peak"]; !ok {
			t.Error("meta_only 여도 peak 는 있어야 한다")
		}
	}

	// 파싱 불가한 since 는 무시하고 전체를 준다 (차트 조회라 500 보다 안전).
	if got := len(get("?limit=500&since=not-a-time")); got != 2 {
		t.Fatalf("잘못된 since 는 무시: got %d want 2", got)
	}
}
