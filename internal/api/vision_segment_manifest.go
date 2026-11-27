package api

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 테스트/검증용 operator — 학습 export 에서 제외(실 사이클에 주입된 검증 라벨 차단).
var manifestTestOperators = map[string]bool{"verify_test": true, "test": true}

// tankHasStock — 입식(fish_count>0) 여부. 미입식 수조는 학습 수집/라벨링/export 에서 제외
// (빈 수조 영상 방지). 캡처 게이트(main.go SetActiveTankFilter)와 동일 기준.
func (s *Server) tankHasStock(ctx context.Context, tankID string) bool {
	p, err := s.store.GetTankProfile(ctx, tankID)
	return err == nil && p != nil && p.FishCount > 0
}

// manifestSplit — cycle_id 결정적 해시로 train/val 배정.
//
// 같은 사이클의 인접 세그먼트는 거의 동일하므로 세그먼트 단위로 나누면 val 점수가
// 허위로 오른다(누수). cycle_id 로 배정해 한 사이클은 통째로 train 또는 val 로 간다.
// 해시 기반이라 재export 시에도 배정이 안정적(랜덤 시드 없음).
func manifestSplit(cycleID string, valRatio float64) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(cycleID))
	if float64(h.Sum32()%1000)/1000.0 < valRatio {
		return "val"
	}
	return "train"
}

type manifestRow struct {
	CycleID       string  `json:"cycle_id"`
	ClipID        string  `json:"clip_id"`
	CameraID      string  `json:"camera_id"`
	TankID        string  `json:"tank_id,omitempty"`
	PulseNo       int     `json:"pulse_no"`
	ClipURI       string  `json:"clip_uri"`
	OffsetStartMs int64   `json:"offset_start_ms"`
	OffsetEndMs   int64   `json:"offset_end_ms"`
	Score         float64 `json:"score"`
	Source        string  `json:"source"`
	Split         string  `json:"split"`
}

// handleVisionSegmentLabelManifest — GET /v1/vision/segment-labels/manifest?val_ratio=0.2
//
// 세그먼트 라벨 → LRCN A1 먹이반응 학습 manifest (docs/50 4단계).
//   - 격리(excluded) 세그먼트 제외(학습 라벨 아님) — 사유별로 요약 집계(급이 전달 실패 추적).
//   - 테스트 operator 제외.
//   - train/val 사이클 단위 분할(누수 방지).
//   - 같은 clip_id 재라벨 시 최신(sequence 내림차순 첫 건)만 사용.
func (s *Server) handleVisionSegmentLabelManifest(w http.ResponseWriter, r *http.Request) {
	valRatio := 0.2
	if v := r.URL.Query().Get("val_ratio"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f < 1 {
			valRatio = f
		}
	}
	// camera_line: top(기본 — LRCN 학습 라인) | underwater(수중 별도 라인) | all.
	// 2026-07-17 운영자 확정: LRCN 은 탑뷰 전용, 수중 라벨은 보존하되 별도 소비.
	line := r.URL.Query().Get("camera_line")
	if line == "" {
		line = "top"
	}
	rows, summary, err := s.buildSegmentManifest(r.Context(), valRatio, true, line)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "summary": summary})
}

// cameraLineMatch — 카메라가 요청된 라인에 속하는지.
func cameraLineMatch(line, cameraID string) bool {
	uw := strings.Contains(cameraID, "underwater")
	switch line {
	case "underwater":
		return uw
	case "all":
		return true
	default: // "top"
		return !uw
	}
}

// buildSegmentManifest — manifest 본체. includeURIs=false 면 clip_uri 조회를
// 생략한다 (요약만 필요한 호출자 — 학습 준비도 카드).
func (s *Server) buildSegmentManifest(ctx context.Context, valRatio float64, includeURIs bool, cameraLine string) ([]manifestRow, map[string]any, error) {
	labelEvents, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventVisionSegmentLabelRecorded, Limit: 10000,
	})
	if err != nil {
		return nil, nil, err
	}

	// clip_id → uri (급이 클립 — 학습 스크립트가 mp4 를 찾도록).
	clipURI := map[string]string{}
	if includeURIs {
		clipEvents, err := s.store.QueryEvents(ctx, storage.EventFilter{
			EventType: events.EventMediaClipStored, EvidencePhase: "feeding", Limit: 5000,
		})
		if err != nil {
			return nil, nil, err
		}
		for _, e := range clipEvents {
			var p events.MediaClipStoredPayload
			if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil && p.ClipID != "" {
				if _, ok := clipURI[p.ClipID]; !ok {
					// 아카이브로 옮겼으면 그 경로를 준다(학습 스크립트가 직접 연다).
					resolved, _ := s.resolveClipPath(p.URI)
					clipURI[p.ClipID] = resolved
				}
			}
		}
	}

	rows := []manifestRow{}
	seenClip := map[string]bool{} // 같은 clip_id 재라벨 시 최신만
	stockedCache := map[string]bool{}
	cyclesTrain, cyclesVal := map[string]bool{}, map[string]bool{}
	var trainRows, valRows, excludedCount, testFiltered, unstockedFiltered, lineFiltered, prefillFiltered int
	reasonBreakdown := map[string]int{}

	for _, e := range labelEvents {
		var p events.VisionSegmentLabelPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if seenClip[p.ClipID] {
			continue // 최신(seq DESC 첫 건)만
		}
		seenClip[p.ClipID] = true
		if manifestTestOperators[p.OperatorID] {
			testFiltered++
			continue
		}
		// 카메라 라인 분리 (탑뷰=LRCN / 수중=별도) — 라벨은 보존, manifest 만 필터.
		if !cameraLineMatch(cameraLine, p.CameraID) {
			lineFiltered++
			continue
		}
		// 미입식 수조 라벨 제외(빈 수조 방어 — 캡처 게이트와 동일 기준).
		if v, ok := stockedCache[p.TankID]; !ok {
			v = s.tankHasStock(ctx, p.TankID)
			stockedCache[p.TankID] = v
		}
		if !stockedCache[p.TankID] {
			unstockedFiltered++
			continue
		}
		split := manifestSplit(p.CycleID, valRatio)
		for _, seg := range p.Segments {
			if seg.Excluded {
				excludedCount++
				reasonBreakdown[seg.ExcludeReason]++
				continue
			}
			// 프리필 수락(source=prior) 제외 — 2026-08-05 운영자.
			// 운영자가 슬라이더를 안 건드리면 모델이 낸 값(r3 또는 요동 prior)이 그대로
			// 저장된다. 그걸 학습에 넣으면 **모델이 자기 출력을 정답으로 다시 배운다**.
			// 양은 작지만(1,315행 중 41건 = 3.1%) 되먹임은 나중에 원인 추적이 어렵다.
			//
			// ⚠ 버리는 것이 아니라 **되돌린다** — training-pool 이 이런 클립을 "라벨 완료"로
			// 보지 않고 큐에 다시 올리므로(vision_training_pool.go), 현장 AI 학습에서
			// 채점하면 source 가 operator 로 바뀌어 자동으로 여기 편입된다.
			if seg.Source != "operator" {
				prefillFiltered++
				continue
			}
			rows = append(rows, manifestRow{
				CycleID: p.CycleID, ClipID: p.ClipID, CameraID: p.CameraID, TankID: p.TankID,
				PulseNo: seg.PulseNo, ClipURI: clipURI[p.ClipID],
				OffsetStartMs: seg.OffsetStartMs, OffsetEndMs: seg.OffsetEndMs,
				Score: seg.Score, Source: seg.Source, Split: split,
			})
			if split == "val" {
				valRows++
				cyclesVal[p.CycleID] = true
			} else {
				trainRows++
				cyclesTrain[p.CycleID] = true
			}
		}
	}

	summary := map[string]any{
		"total_rows":                len(rows),
		"train_rows":                trainRows,
		"val_rows":                  valRows,
		"train_cycles":              len(cyclesTrain),
		"val_cycles":                len(cyclesVal),
		"excluded_segments":         excludedCount,
		"excluded_reasons":          reasonBreakdown,
		"test_filtered_labels":      testFiltered,
		"unstocked_filtered_labels": unstockedFiltered,
		"line_filtered_labels":      lineFiltered,
		// 프리필 수락으로 학습에서 뺀 세그먼트. training-pool 이 되돌려 재채점 대상이 된다.
		"prefill_filtered_segments": prefillFiltered,
		"camera_line":               cameraLine,
		"val_ratio":                 valRatio,
	}
	return rows, summary, nil
}

// segmentGateSummary — LRCN 세그먼트 라벨 게이트 (학습 준비도 카드). 3차 학습
// 트리거 = 유효 세그먼트 1,000+ (2026-07-16 운영 기준).
func (s *Server) segmentGateSummary(ctx context.Context) map[string]any {
	rows, summary, err := s.buildSegmentManifest(ctx, 0.2, false, "top")
	if err != nil {
		return nil
	}
	return map[string]any{
		"valid_rows": len(rows),
		"excluded":   summary["excluded_segments"],
		"gate":       1000,
	}
}
