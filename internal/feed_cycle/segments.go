package feed_cycle

import (
	"sort"
	"time"
)

// PulseTiming — feed.pulse.completed 에서 파싱한 최소 필드(세그먼트 유도 전용).
type PulseTiming struct {
	PulseNo          int       // 1-based 펄스 순번
	CompletedAt      time.Time // 펄스 OFF 시각
	ActualDurationMs int       // 펄스 ON 지속(리드+공급). ON 시작 = CompletedAt - 이 값.
}

// Segment — 펄스 사이클 단위 학습 세그먼트(오프셋 참조).
//
// 급이 통클립을 물리 분할하지 않고 원본 대비 [StartAbs, EndAbs) 구간만 기록한다
// (docs/50: 오프셋 참조 확정). 한 세그먼트 = 한 펄스의 자극→반응 창.
//
// 세그먼트 길이는 고정이 아니다 — 운영자가 바꾸는 펄스 파라미터(리드+공급+정지)의
// 합에서 나오며, 실측 펄스 간격으로 동적으로 산출된다(사용자 지시 2026-07-06:
// "이 시간 총합을 기준으로 영상을 잘라 저장"). 예: 리드11+공급3+정지15 = 29s.
type Segment struct {
	Index      int       // 0-based 세그먼트 인덱스
	PulseNo    int       // 대응 펄스 순번
	StartAbs   time.Time // 펄스 ON 시작(절대 시각)
	EndAbs     time.Time // 다음 펄스 ON 시작(마지막 펄스는 측정 간격 중앙값만큼 연장)
	DurationMs int
}

// DeriveSegments — 펄스 타이밍 목록 → 펄스 사이클 세그먼트.
//
// 경계: 세그먼트 i = [pulse_i.ON시작, pulse_{i+1}.ON시작).
//
//	ON시작 = CompletedAt - ActualDurationMs.
//
// 각 세그먼트 길이 = 실측 펄스 간격 = 그 사이클에서 실제 실현된 (리드+공급+정지) 합.
// 파라미터가 바뀌면 이 값이 데이터에서 자동으로 따라간다(하드코딩 없음).
//
// 마지막 펄스는 다음 펄스가 없으므로 tail 로 연장한다:
//   - 측정 간격이 있으면(펄스 2개 이상) 그 중앙값을 사용(그 사이클의 실제 리듬).
//   - 단일 펄스면 fallbackTailMs 사용(호출부가 정책 리드+공급+정지 합을 넘김).
//
// PulseNo 로 정렬 후 산출. 빈 입력이면 nil.
func DeriveSegments(pulses []PulseTiming, fallbackTailMs int) []Segment {
	if len(pulses) == 0 {
		return nil
	}
	sorted := append([]PulseTiming(nil), pulses...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PulseNo < sorted[j].PulseNo })

	starts := make([]time.Time, len(sorted))
	for i, p := range sorted {
		starts[i] = p.CompletedAt.Add(-time.Duration(p.ActualDurationMs) * time.Millisecond)
	}

	// 측정 펄스 간격(ms) — 마지막 펄스 tail 의 중앙값 재료.
	var gaps []int64
	for i := 0; i < len(starts)-1; i++ {
		if g := starts[i+1].Sub(starts[i]).Milliseconds(); g > 0 {
			gaps = append(gaps, g)
		}
	}
	tailMs := int64(fallbackTailMs)
	if len(gaps) > 0 {
		tailMs = medianMs(gaps)
	}
	if tailMs <= 0 {
		// 정책 미설정(합=0) + 단일 펄스 방어 — 최소 ON 길이만이라도 확보.
		tailMs = int64(sorted[len(sorted)-1].ActualDurationMs)
	}

	segs := make([]Segment, len(sorted))
	for i := range sorted {
		start := starts[i]
		var end time.Time
		if i < len(sorted)-1 {
			end = starts[i+1]
		} else {
			end = start.Add(time.Duration(tailMs) * time.Millisecond)
		}
		if !end.After(start) { // 시계 역전/순서 이상 방어.
			end = start.Add(time.Duration(tailMs) * time.Millisecond)
		}
		segs[i] = Segment{
			Index:      i,
			PulseNo:    sorted[i].PulseNo,
			StartAbs:   start,
			EndAbs:     end,
			DurationMs: int(end.Sub(start).Milliseconds()),
		}
	}
	return segs
}

// medianMs — 정렬 후 중앙값(짝수는 두 중앙의 평균). 입력 비어있지 않다고 가정.
func medianMs(v []int64) int64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
