package api

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// handleVisionTrainingPool — R6.3.
// GET /v1/vision/training-pool?phase=feeding|baseline&limit=N
//
// LRCN 지도학습용 영상 풀에서 랜덤 샘플링.
//   - phase=feeding  → cycle 활성 중 캡처된 mp4 (사료 반응 평가용)
//   - phase=baseline → cycle 외 시간에 캡처된 mp4 (안정성 평가용)
//
// source: events.media.clip.stored 의 evidence.phase 필드.
// (cycle hook + 상시 캡처 모두 동일 event 로 적재, R6.2 wire.)
//
// limit 범위 [1, 50]. 기본 10.
// tank_id / camera_id 필터링 옵션.

// boxLabelsPerClipCap — 한 클립에서 받는 박스 라벨 프레임 상한. 도달 시 풀에서
// 로테이션 (다양성 확보 — 클래스별 목표는 "서로 다른 프레임" 수).
const boxLabelsPerClipCap = 3

func (s *Server) handleVisionTrainingPool(w http.ResponseWriter, r *http.Request) {
	phase := strings.ToLower(r.URL.Query().Get("phase"))
	if phase != "feeding" && phase != "baseline" {
		writeError(w, http.StatusBadRequest, "INVALID_PHASE",
			"phase must be 'feeding' or 'baseline'", "")
		return
	}
	limit := intParam(r, "limit", 10, 50)
	tankID := r.URL.Query().Get("tank_id")
	cameraID := r.URL.Query().Get("camera_id")

	// R8.2 — excluded clip_id set 구축. media.clip.excluded events 의 clip_id 는 풀에서 제거.
	excluded := make(map[string]struct{})
	excludedEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventMediaClipExcluded,
		Limit:     500,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, e := range excludedEvents {
		var p events.MediaClipExcludedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		excluded[p.ClipID] = struct{}{}
	}

	// 라벨 완료 클립도 큐에서 제외 (2026-07-09 운영자: "같은 장면이 계속 나온다").
	// 세그먼트 채점이 이미 제출된 클립은 재라벨 대상이 아니다 — 라벨은 이벤트로
	// 영구 보존되므로 풀에서 빼도 학습 데이터는 불변. include_labeled=1 로 재검토 허용.
	if r.URL.Query().Get("include_labeled") != "1" {
		labeledEvents, lerr := s.store.QueryEvents(r.Context(), storage.EventFilter{
			EventType: "vision.segment.label.recorded",
			Limit:     1000,
		})
		if lerr == nil {
			// ⚠ "라벨 완료" = **운영자가 실제로 채점한 것**만이다 (2026-08-05 운영자).
			// 슬라이더를 안 건드리면 모델 제안값이 source="prior" 로 저장되는데, 그건
			// 정답이 아니라 모델 자기 출력이다. 학습 manifest 는 그걸 빼므로
			// (vision_segment_manifest.go), 여기서도 완료로 보면 **영영 채점되지 않고
			// 학습에서도 빠진 채 사라진다.** 그래서 prior 세그먼트가 남은 클립은
			// 완료로 치지 않고 큐에 되돌려 현장 AI 학습에서 다시 뜨게 한다.
			// 채점하면 source 가 operator 로 바뀌어 자동으로 manifest 에 편입된다.
			//
			// 같은 clip_id 가 여러 번 라벨됐으면 최신(seq DESC 첫 건)이 정본이다.
			seenClip := map[string]bool{}
			for _, e := range labeledEvents {
				var p struct {
					ClipID   string `json:"clip_id"`
					Segments []struct {
						Source   string `json:"source"`
						Excluded bool   `json:"excluded"`
					} `json:"segments"`
				}
				if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.ClipID == "" {
					continue
				}
				if seenClip[p.ClipID] {
					continue
				}
				seenClip[p.ClipID] = true
				pending := false
				for _, sg := range p.Segments {
					if !sg.Excluded && sg.Source != "operator" {
						pending = true
						break
					}
				}
				if !pending {
					excluded[p.ClipID] = struct{}{}
				}
			}
		}
		// ① 탭 박스 라벨 포화 클립 로테이션 (2026-07-17 같은 이미지 반복 fix):
		// 한 클립에서 프레임 라벨을 boxLabelsPerClipCap 장 이상 얻었으면 풀에서
		// 제외해 다른 클립(다른 시간대)으로 유도한다. offset 프레임 서빙으로 한
		// 클립이 여러 장면을 공급하므로 1장 라벨에 통클립을 버리지는 않는다.
		boxLabelEvents, berr := s.store.QueryEvents(r.Context(), storage.EventFilter{
			EventType: events.EventVisionBootstrapLabelRecorded,
			Limit:     10000,
		})
		if berr == nil {
			perClip := map[string]int{}
			for _, e := range boxLabelEvents {
				var p struct {
					OriginClipID string `json:"origin_clip_id"`
				}
				if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil && p.OriginClipID != "" {
					perClip[p.OriginClipID]++
				}
			}
			for clipID, n := range perClip {
				if n >= boxLabelsPerClipCap {
					excluded[clipID] = struct{}{}
				}
			}
		}
	}

	// phase 로 DB 단에서 분리 조회 — 상시 baseline 캡처가 희소한 feeding 클립을
	// 최근 N건 윈도우 밖으로 밀어내는 문제 방지(급이 클립이 안 뜨던 원인).
	candidatePool := 200
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:     events.EventMediaClipStored,
		EvidencePhase: phase,
		Limit:         candidatePool,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 입식 게이트 — 미입식(fish_count<=0) 수조 클립은 학습/라벨링 풀에서 제외.
	// 빈 수조(예: tank_03/04) 영상이 라벨링·학습에 유입되지 않게(캡처 게이트와 동일 기준).
	stockedCache := map[string]bool{}
	isStocked := func(t string) bool {
		if v, ok := stockedCache[t]; ok {
			return v
		}
		v := s.tankHasStock(r.Context(), t)
		stockedCache[t] = v
		return v
	}

	matches := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.MediaClipStoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		// R8.2: excluded 영상 제거
		if _, isExcluded := excluded[payload.ClipID]; isExcluded {
			continue
		}
		// 미입식 수조 클립 제외
		if !isStocked(payload.TankID) {
			continue
		}
		// evidence.phase 필터링
		ev := payload.Evidence
		evPhase, _ := ev["phase"].(string)
		if !strings.EqualFold(evPhase, phase) {
			continue
		}
		if tankID != "" && payload.TankID != tankID {
			continue
		}
		if cameraID != "" && payload.CameraID != cameraID {
			continue
		}
		// 파일이 실제 존재하는 clip 만 — 삭제/평가·격리/retention 정리된 것은 풀에서 자동 제외
		// (재생 불가 항목이 풀에 남지 않게).
		if payload.URI != "" {
			// 아카이브(NAS/외장)로 옮긴 클립도 살린다 — 없으면 그때만 제외.
			if _, ok := s.resolveClipPath(payload.URI); !ok {
				continue
			}
		}
		matches = append(matches, map[string]any{
			"sequence":    e.Sequence,
			"event_id":    e.EventID,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"payload":     payload,
		})
	}

	// 랜덤 셔플 + limit. seed 는 매 요청마다 다름 (운영자가 같은 영상 반복 안 받게).
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(matches), func(i, j int) { matches[i], matches[j] = matches[j], matches[i] })
	if len(matches) > limit {
		matches = matches[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"phase":            phase,
		"count":            len(matches),
		"candidate_pool":   candidatePool,
		"items":            matches,
		"tank_id_filter":   tankID,
		"camera_id_filter": cameraID,
	})
}
