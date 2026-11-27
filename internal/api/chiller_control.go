package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
)

// handleChillerControl — POST /v1/chiller/control  body {"on": true|false}
// 운영자 수동 냉각기 ON/OFF (AI 자동제어와 별개). onoff_feeder switch control(냉각기 switch)로 전달.
// ⚠ 실제 장비 작동. 프론트에서 확인 다이얼로그 필수(특히 OFF → 수온 상승).
func (s *Server) handleChillerControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On *bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.On == nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "on (bool) is required", "")
		return
	}

	// 냉각기 switch ext_id 는 onoff_feeder collector 어댑터 설정에서 가져온다.
	extID := 0
	for _, a := range s.cfg.Collector.Adapters {
		if a.ChillerSwitchExtID > 0 {
			extID = a.ChillerSwitchExtID
			break
		}
	}
	if extID == 0 {
		writeError(w, http.StatusUnprocessableEntity, "CHILLER_NOT_CONFIGURED",
			"chiller switch ext_id not configured (collector chiller_switch_ext_id)", "")
		return
	}

	adapter := control.NewOnoffFeederControlAdapter(config.ControlAdapter{ID: "chiller-manual"})
	commandID := common.NewID("cmd")
	ack, err := adapter.Execute(r.Context(), commandID, "switch.set", map[string]any{
		"ext_id": float64(extID),
		"on":     *body.On,
	})
	if err != nil {
		slog.Error("manual chiller control failed", "on", *body.On, "ext_id", extID, "error", err)
		writeError(w, http.StatusBadGateway, "CHILLER_CONTROL_FAILED", err.Error(), "")
		return
	}
	slog.Info("manual chiller control sent", "on", *body.On, "ext_id", extID, "command_id", commandID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "on": *body.On, "command_id": commandID, "ack": ack})
}
