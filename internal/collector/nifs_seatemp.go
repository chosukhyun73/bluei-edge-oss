package collector

// NIFS 실시간 해양환경(risa) 연안 수온 수집 — 수취구 권역 수온 (2026-07-17 계획 확정).
//
// keyless 웹 엔드포인트 (bluei.kr 백엔드와 동일 경로 — API 키 불필요, 라이브 검증
// 2026-07-17: 강릉 19.9°C 수신). 1시간 주기, 강릉(bgna3) 1순위 + 양양·고성 폴백.
// 같은 관측시각은 중복 적재하지 않는다.
//
// 오프라인 안전: 실패는 그 회차 skip. 외부 데이터는 표시·추세 비교용이며 급이·판정
// 어디에도 자동 개입하지 않는다 (수온 보정 방안의 재료 축적 단계).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

const (
	nifsListURL      = "https://www.nifs.go.kr/risa/selectTodayObsvtrList.do"
	seaTempInterval  = time.Hour
	seaTempFirstWait = 30 * time.Second
)

// 관측소 코드는 현장 config(sea_temp.stations)로 주입한다 — 취수 권역이 현장마다
// 다르므로 하드코딩하지 않는다. 미설정(빈)이면 수집기가 비활성 (엉뚱한 지역
// 수온을 "취수 권역 수온"으로 표시하는 것 방지, 2026-07 신규 현장 안전).
// 참고 코드: bgna3(강릉)·byy87(양양)·fggo3(고성).

type seaTempSink interface {
	AppendEvent(ctx context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error)
}

type seaTempStore interface {
	QueryEvents(ctx context.Context, f storage.EventFilter) ([]*storage.Event, error)
}

// SeaTempCollector — 주기 폴링 워커. main.go 에서 go Run(ctx).
type SeaTempCollector struct {
	sink     seaTempSink
	store    seaTempStore
	stations []string // 우선순위 순 (취수 권역 근접순). 첫 유효 관측만 적재.
	log      *slog.Logger
	httpc    *http.Client
}

func NewSeaTempCollector(sink seaTempSink, store seaTempStore, stations []string) *SeaTempCollector {
	return &SeaTempCollector{
		sink:     sink,
		store:    store,
		stations: stations,
		log:      slog.With("service", "seatemp_collector"),
		httpc:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *SeaTempCollector) Run(ctx context.Context) {
	if len(c.stations) == 0 {
		c.log.Info("seatemp collector disabled — sea_temp.stations 미설정")
		return
	}
	c.log.Info("seatemp collector started", "interval", seaTempInterval.String(), "stations", c.stations)
	select {
	case <-ctx.Done():
		return
	case <-time.After(seaTempFirstWait):
	}
	c.tick(ctx)
	t := time.NewTicker(seaTempInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

type nifsObs struct {
	StationCd string  // obsvtrCd
	Name      string  // obsvtrKornNm
	WaterTemp float64 // wtem
	ObsAt     string  // obsrvnDt "2026-07-17 15:00" (KST)
}

func (c *SeaTempCollector) tick(ctx context.Context) {
	obs, err := c.fetch(ctx)
	if err != nil {
		c.log.Warn("seatemp fetch failed — 이번 회차 skip", "error", err)
		return
	}
	if obs == nil {
		c.log.Warn("seatemp: 우선순위 관측소 유효 관측 없음 — skip")
		return
	}

	observedAtUTC, err := parseNIFSTimeKST(obs.ObsAt)
	if err != nil {
		c.log.Warn("seatemp: 관측시각 파싱 실패", "obs_at", obs.ObsAt, "error", err)
		return
	}

	// 같은 관측소 + 같은 관측시각이면 중복 skip (관측은 정시 단위 갱신).
	last, err := c.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventExternalEnvRecorded, DeviceID: obs.StationCd, Limit: 1,
	})
	if err == nil && len(last) > 0 {
		var p struct {
			ObservedAt string `json:"observed_at"`
		}
		if json.Unmarshal([]byte(last[0].PayloadJSON), &p) == nil &&
			p.ObservedAt == common.FormatTime(observedAtUTC) {
			return
		}
	}

	payload := map[string]any{
		"source":       "nifs_risa",
		"metric":       "sea_water_temperature",
		"station_cd":   obs.StationCd,
		"station_name": obs.Name,
		"value":        obs.WaterTemp,
		"unit":         "°C",
		"observed_at":  common.FormatTime(observedAtUTC),
	}
	if _, err := c.sink.AppendEvent(ctx, "collector", "nifs", obs.StationCd,
		events.EventExternalEnvRecorded, "", payload); err != nil {
		c.log.Warn("seatemp append failed", "error", err)
		return
	}
	c.log.Info("seatemp recorded", "station", obs.Name, "temp_c", obs.WaterTemp, "observed_at", obs.ObsAt)
}

// fetch — 관측소 목록 1회 호출 후 우선순위 첫 유효 관측 반환 (없으면 nil).
func (c *SeaTempCollector) fetch(ctx context.Context) (*nifsObs, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nifsListURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; bluei-edge/1.0)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://www.nifs.go.kr/risa/risa/risaA/actionRisaInfo.do")
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("nifs status %d", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			ObsvtrCd     string `json:"obsvtrCd"`
			ObsvtrKornNm string `json:"obsvtrKornNm"`
			ObsrvnDt     string `json:"obsrvnDt"`
			Wtem         string `json:"wtem"`
		} `json:"todayObsvtrList"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("nifs decode: %w", err)
	}
	byCode := map[string]*nifsObs{}
	for _, it := range body.Items {
		w, err := strconv.ParseFloat(strings.TrimSpace(it.Wtem), 64)
		if err != nil || it.ObsrvnDt == "" {
			continue
		}
		byCode[it.ObsvtrCd] = &nifsObs{
			StationCd: it.ObsvtrCd, Name: it.ObsvtrKornNm, WaterTemp: w, ObsAt: it.ObsrvnDt,
		}
	}
	for _, cd := range c.stations {
		if o, ok := byCode[cd]; ok {
			return o, nil
		}
	}
	return nil, nil
}

// parseNIFSTimeKST — "2006-01-02 15:04" (KST) → UTC.
func parseNIFSTimeKST(s string) (time.Time, error) {
	kst := time.FixedZone("KST", 9*3600)
	t, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(s), kst)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
