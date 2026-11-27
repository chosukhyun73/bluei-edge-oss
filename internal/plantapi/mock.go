package plantapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"time"

	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

// 모의 센서와 생산 판정 작업.
// 모의 센서(plant_sensors.is_mock)는 일정 간격으로 값을 낸다. 평소에는 한계기준 안의 값을 내고,
// 화면에서 값을 지정하거나 끊김을 걸어 이탈·모니터링 불능을 시험할 수 있다.
// 실제 센서가 붙으면 같은 process_readings 로 들어오고, 판정 쪽은 바뀌지 않는다.
// References: docs/54-haccp-process-flow-ccp-design.md §9, §26.

const mockInterval = 5 * time.Second

// ProcessWorker emits mock readings and judges running lots. It implements runtime.Service.
type ProcessWorker struct {
	srv  *Server
	stop chan struct{}
}

func NewProcessWorker(s *Server) *ProcessWorker {
	return &ProcessWorker{srv: s, stop: make(chan struct{})}
}

func (p *ProcessWorker) Name() string { return "process" }

func (p *ProcessWorker) Start(ctx context.Context) error {
	go func() {
		t := time.NewTicker(mockInterval)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				bg := context.Background()
				if err := p.srv.emitMockReadings(bg); err != nil {
					slog.Warn("mock readings failed", "error", err)
				}
				if err := p.srv.evaluateRunningLots(bg); err != nil {
					slog.Warn("lot evaluation failed", "error", err)
				}
			}
		}
	}()
	return nil
}

func (p *ProcessWorker) Stop(ctx context.Context) error {
	close(p.stop)
	return nil
}

// evaluateRunningLots: 화면을 열어 두지 않아도 이탈이 기록되게 한다.
func (s *Server) evaluateRunningLots(ctx context.Context) error {
	lots, err := s.store.ListRunningProcessLots(ctx)
	if err != nil {
		return err
	}
	for _, l := range lots {
		st, err := s.loadLot(ctx, l.LotID)
		if err != nil || st == nil {
			return err
		}
		if _, err := s.evaluateLot(ctx, st, nowMs()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) processingSiteIDs(ctx context.Context) ([]string, error) {
	sites, err := s.store.ListSites(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range sites {
		if m["site_type"] == string(site.TypeProcessing) {
			if id, ok := m["site_id"].(string); ok {
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// publishedLimits: 공장의 게시본에 있는 한계기준 항목. 센서별 정상 값을 정하는 데 쓴다.
func (s *Server) publishedLimits(ctx context.Context, siteID string) ([]haccp.Limit, error) {
	plans, err := s.store.ListHACCPPlans(ctx, siteID)
	if err != nil {
		return nil, err
	}
	var out []haccp.Limit
	for _, p := range plans {
		versions, err := s.store.ListHACCPPlanVersions(ctx, p.PlanID)
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if v.Status != haccpStatusPublished {
				continue
			}
			var c haccp.PlanContent
			if err := json.Unmarshal([]byte(v.ContentJSON), &c); err != nil {
				return nil, err
			}
			for _, k := range c.CCPs {
				out = append(out, k.Limits...)
			}
		}
	}
	return out, nil
}

// mockBaseline: 모의 센서가 평소에 내는 값. 이 센서를 보는 한계기준이 있으면 그 안쪽 값,
// 없으면 구역의 온도 구분에 맞는 값. emit=false 면 값을 내지 않는다(사람이 하는 주기 점검).
func mockBaseline(sn *storage.PlantSensor, zones map[string]*storage.PlantZone, limits []haccp.Limit) (value float64, emit bool) {
	for _, l := range limits {
		if l.SensorID != sn.SensorID {
			continue
		}
		switch l.RuleKind {
		case haccp.RulePeriodicCheck:
			return 0, false
		case haccp.RuleCount:
			return 0, true
		case haccp.RuleCumulativeExposure:
			return 12, true // 작업장 온도
		}
		switch {
		case l.Max != nil:
			return *l.Max - math.Max(1, 0.3*math.Abs(*l.Max)), true
		case l.Min != nil:
			return *l.Min * 1.2, true
		}
	}
	if sn.Metric == "temperature" || sn.Metric == "core_temperature" {
		if z := zones[sn.ZoneID]; z != nil {
			switch z.TempClass {
			case "chilled":
				return 2.5, true
			case "frozen":
				return -20, true
			}
		}
		return 12, true
	}
	return 0, true
}

func (s *Server) emitMockReadings(ctx context.Context) error {
	siteIDs, err := s.processingSiteIDs(ctx)
	if err != nil {
		return err
	}
	states, err := s.store.ListMockSensorStates(ctx)
	if err != nil {
		return err
	}
	now := nowMs()
	override := map[string]storage.MockSensorState{}
	for _, st := range states {
		if st.UntilAt > now {
			override[st.SensorID] = st
		}
	}
	for _, siteID := range siteIDs {
		sensors, err := s.store.ListPlantSensors(ctx, siteID)
		if err != nil {
			return err
		}
		zoneList, err := s.store.ListPlantZones(ctx, siteID)
		if err != nil {
			return err
		}
		zones := map[string]*storage.PlantZone{}
		for _, z := range zoneList {
			zones[z.ZoneID] = z
		}
		limits, err := s.publishedLimits(ctx, siteID)
		if err != nil {
			return err
		}
		for _, sn := range sensors {
			if !sn.IsMock {
				continue
			}
			value, emit := mockBaseline(sn, zones, limits)
			if st, ok := override[sn.SensorID]; ok {
				if st.Mode == "outage" {
					continue
				}
				value, emit = st.Value, true
			} else if value != 0 {
				value += (rand.Float64() - 0.5) * 0.04 * math.Abs(value) // ±2% 흔들림
			}
			if !emit {
				continue
			}
			value = math.Round(value*100) / 100
			if err := s.store.InsertProcessReading(ctx, siteID,
				&storage.ProcessReading{SensorID: sn.SensorID, Value: value, Source: "mock", At: now}); err != nil {
				return err
			}
		}
	}
	return nil
}

// GET /v1/plant/{site_id}/readings — 센서별 최근 값과 모의 설정.
func (s *Server) handleListReadings(w http.ResponseWriter, r *http.Request, siteID string) {
	ctx := r.Context()
	sensors, err := s.store.ListPlantSensors(ctx, siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	states, err := s.store.ListMockSensorStates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	now := nowMs()
	out := make([]map[string]any, 0, len(sensors))
	for _, sn := range sensors {
		m := plantSensorToMap(sn)
		latest, err := s.store.LatestProcessReading(ctx, sn.SensorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if latest != nil {
			m["latest"] = map[string]any{"value": latest.Value, "at": latest.At, "source": latest.Source}
		}
		for _, st := range states {
			if st.SensorID == sn.SensorID && st.UntilAt > now {
				m["mock"] = map[string]any{"mode": st.Mode, "value": st.Value, "until_at": st.UntilAt}
			}
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sensors": out, "now": now})
}

// POST /v1/plant/{site_id}/readings — 모의 센서 설정. mode: fixed(값 지정) | outage(끊김) | normal(되돌림).
func (s *Server) handlePostMock(w http.ResponseWriter, r *http.Request, siteID string) {
	var req struct {
		SensorID string  `json:"sensor_id"`
		Mode     string  `json:"mode"`
		Value    float64 `json:"value"`
		Minutes  float64 `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	sensors, err := s.store.ListPlantSensors(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var sensor *storage.PlantSensor
	for _, sn := range sensors {
		if sn.SensorID == req.SensorID {
			sensor = sn
		}
	}
	if sensor == nil || !sensor.IsMock || !oneOf(req.Mode, "fixed", "outage", "normal") {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_MOCK",
			"이 공장의 모의 센서와 mode(fixed|outage|normal)가 필요합니다. 실제 센서의 값은 바꿀 수 없습니다", "")
		return
	}
	if req.Minutes <= 0 {
		req.Minutes = 10
	}
	st := &storage.MockSensorState{SensorID: req.SensorID, Mode: req.Mode, Value: req.Value,
		UntilAt: nowMs() + int64(req.Minutes*60000)}
	if req.Mode == "normal" {
		st.Mode, st.UntilAt = "fixed", 0
	}
	if err := s.store.SetMockSensorState(r.Context(), st); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	// 주기 점검처럼 평소에 값을 내지 않는 센서도 지정한 값은 바로 한 번 낸다.
	if req.Mode == "fixed" {
		if err := s.store.InsertProcessReading(r.Context(), siteID,
			&storage.ProcessReading{SensorID: req.SensorID, Value: req.Value, Source: "mock", At: nowMs()}); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
	}
	s.handleListReadings(w, r, siteID)
}
