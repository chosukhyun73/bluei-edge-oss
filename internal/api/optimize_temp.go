package api

import (
	"context"
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/optimize"
)

const tempParamsKey = "optimize.temp.params"
const tempLastDecisionKey = "tempcontrol.last_decision" // 워커 마지막 판단(tempcontrol 패키지와 동일 키)

// loadTempParams — 운영자 설정(runtime_kv) 우선, 없으면 기본값.
func (s *Server) loadTempParams(ctx context.Context) optimize.TempParams {
	p := optimize.DefaultTempParams()
	if v, ok, err := s.store.KVGet(ctx, tempParamsKey); err == nil && ok && v != "" {
		_ = json.Unmarshal([]byte(v), &p)
	}
	return p
}

// handleOptimizeTempParams — GET/PUT /v1/optimize/temp/params
// 운영자가 한계 수온(최적성장·상하한·DO하한 등)을 조회/설정.
func (s *Server) handleOptimizeTempParams(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.loadTempParams(r.Context()))
	case http.MethodPut:
		var p optimize.TempParams
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON", "")
			return
		}
		if p.TempMinC >= p.TempMaxC || p.OptimalGrowthC <= 0 || p.DOFloor <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_PARAMS",
				"temp_min < temp_max, optimal_growth_c>0, do_floor>0 필요", "")
			return
		}
		if p.HysteresisC <= 0 {
			p.HysteresisC = optimize.DefaultTempParams().HysteresisC
		}
		if p.DOMargin < 0 {
			p.DOMargin = optimize.DefaultTempParams().DOMargin
		}
		b, _ := json.Marshal(p)
		if err := s.store.KVSet(r.Context(), tempParamsKey, string(b)); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "params": p})
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleOptimizeTemp — GET /v1/optimize/temp
// 수조별 현재 수질(수온/DO) → 최적환경조성(수온) 권고 산출. SHADOW: 권고만, 제어 안 함.
func (s *Server) handleOptimizeTemp(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.ListTankProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	p := s.loadTempParams(r.Context())
	items := make([]optimize.TempRecommendation, 0, len(profiles))
	for _, tp := range profiles {
		envs, err := s.store.ListTankEnvironment(r.Context(), tp.TankID)
		if err != nil {
			continue
		}
		var temp, do *float64
		for _, e := range envs {
			if e.Value == nil {
				continue
			}
			switch e.Metric {
			case events.MetricWaterTemperature:
				temp = e.Value
			case events.MetricDissolvedOxygen:
				do = e.Value
			}
		}
		if temp == nil {
			continue // 수온 없으면 권고 불가
		}
		in := optimize.TempInputs{TankID: tp.TankID, CurrentTempC: *temp}
		if do != nil {
			in.CurrentDO = *do
			in.HasDO = true
		}
		items = append(items, optimize.RecommendTemp(in, p))
	}
	// 실제 모드: 런타임 자동제어 on/off (KV 우선, 없으면 config default).
	mode := "shadow"
	if s.tempControlActive(r.Context()) {
		mode = "active"
	}
	// 워커의 실제 마지막 판단(코스팅 예측 정지 등) — 대시보드 표시용.
	var control json.RawMessage
	if v, ok, err := s.store.KVGet(r.Context(), tempLastDecisionKey); err == nil && ok && v != "" {
		control = json.RawMessage(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":    mode,
		"params":  p,
		"items":   items,
		"count":   len(items),
		"control": control,
	})
}

const tempControlActiveKey = "tempcontrol.active" // 런타임 자동제어 on/off (KV)

// tempControlActive — 런타임 자동제어 여부. 워커 미기동(config)면 항상 false. KV 우선, 없으면 config default(!DryRun).
func (s *Server) tempControlActive(ctx context.Context) bool {
	if !s.cfg.TempControl.Enabled {
		return false
	}
	if v, ok, err := s.store.KVGet(ctx, tempControlActiveKey); err == nil && ok && v != "" {
		return v == "true"
	}
	return !s.cfg.TempControl.DryRun
}

// handleOptimizeTempMode — POST /v1/optimize/temp/mode {active: bool}
// 재시작 없이 자동 수온제어 on(active)/off(shadow·관찰) 전환. 워커가 매 tick KV 를 읽어 반영.
func (s *Server) handleOptimizeTempMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Active *bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Active == nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "active (bool) required", "")
		return
	}
	if !s.cfg.TempControl.Enabled {
		writeError(w, http.StatusUnprocessableEntity, "TEMP_CONTROL_DISABLED",
			"temp control worker not enabled (config)", "")
		return
	}
	val := "false"
	mode := "shadow"
	if *body.Active {
		val, mode = "true", "active"
	}
	if err := s.store.KVSet(r.Context(), tempControlActiveKey, val); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode})
}
