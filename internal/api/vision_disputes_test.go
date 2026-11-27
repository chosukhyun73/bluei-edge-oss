package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// postDispute — POST /v1/vision/observations/{id}/disputes 호출 helper.
func postDispute(t *testing.T, s *Server, obsID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/v1/vision/observations/"+obsID+"/disputes", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	s.handleVisionObservationRoute(w, r)
	return w
}

// disputePayload — 적재된 vision.observation.disputed 중 최신 1건을 복원.
func disputePayload(t *testing.T, s *Server) events.VisionObservationDisputedPayload {
	t.Helper()
	evts, err := s.store.QueryEvents(context.Background(), storage.EventFilter{
		EventType: events.EventVisionObservationDisputed,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if len(evts) == 0 {
		t.Fatal("dispute event not appended")
	}
	var p events.VisionObservationDisputedPayload
	if err := json.Unmarshal([]byte(evts[0].PayloadJSON), &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

// prefill_modified 는 "AI 프리필을 실제로 고쳤는가" 표식이다. 학습·편향보정에서
// 프리필 수락 라벨을 걸러내는 근거이므로, 요청 → 이벤트까지 값이 보존돼야 한다.
// 값 비교(operator_score==ai_score)로는 우연 일치와 구분이 안 되는 게 도입 이유.
func TestDisputePrefillModifiedRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		send any // prefill_modified 로 보낼 값 (nil = 필드 생략)
		want *bool
	}{
		{"운영자가 고침", true, boolPtr(true)},
		{"프리필 수락", false, boolPtr(false)},
		{"미기록(도입 전 호환)", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServerWithApp(t)
			body := map[string]any{
				"camera_id":      "cam_top_7_1_2_L",
				"tank_id":        "tank_01",
				"operator_id":    "operator_test",
				"verdict":        "correct",
				"operator_score": 0.7,
				"ai_score":       0.7,
			}
			if tc.send != nil {
				body["prefill_modified"] = tc.send
			}
			w := postDispute(t, s, "vis_obs_test_1", body)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
			}
			got := disputePayload(t, s).PrefillModified
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("prefill_modified = %v, want nil (미기록)", *got)
			case tc.want != nil && got == nil:
				t.Errorf("prefill_modified = nil, want %v", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("prefill_modified = %v, want %v", *got, *tc.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
