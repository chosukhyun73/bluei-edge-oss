package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/llm"
)

// 운영자 가이드 채팅 — bluei-edge-assistant(gemma4:26b + fact pack)와의 자유 대화.
// fact pack(사용법/구조)은 모델 SYSTEM 에 있고, 여기서는 라이브 상태(현재 수조/시스템)를
// 마지막 운영자 메시지에 참고 컨텍스트로 주입한다.
//
// 안전 경계: 어시스턴트는 설명·안내 도우미. 실시간 안전 판단은 Arbiter, 급이는 LRCN/룰.
// (이 경계는 모델 fact pack 이 강제하며, 주입 컨텍스트에도 명시한다.)

const assistantMaxTanksInContext = 12

// assistantMaxTurns — 모델에 보내는 최근 메시지 개수 상한 (컨텍스트 자동 관리).
// 긴 대화에서도 num_ctx 초과·지연 증가를 막는다. 운영자는 전체 대화를 계속 보지만
// 모델에는 최근 N개만 전송된다. (프론트는 전체 보관/표시)
const assistantMaxTurns = 16

type assistantChatRequest struct {
	Messages []llm.ChatTurn `json:"messages"`
}

// handleAssistantChat — 운영자 가이드 채팅. SSE 로 토큰을 스트리밍한다.
// 이벤트 형식(각 줄 "data: <json>\n\n"):
//   - {"context_ko":"..."}  최초 1회, 주입된 라이브 상태
//   - {"delta":"..."}       토큰 청크
//   - {"done":true}         완료
//   - {"error":"..."}       스트림 도중 오류
func (s *Server) handleAssistantChat(w http.ResponseWriter, r *http.Request) {
	if s.llmClient == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM_DISABLED", "어시스턴트(LLM)가 비활성화되어 있습니다", "")
		return
	}

	var req assistantChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body: "+err.Error(), "")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "messages 가 비어 있습니다", "")
		return
	}
	// 마지막 메시지는 운영자(user) 여야 한다.
	if last := req.Messages[len(req.Messages)-1]; last.Role != "user" || strings.TrimSpace(last.Content) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "마지막 메시지는 비어있지 않은 user 메시지여야 합니다", "")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "NO_STREAMING", "스트리밍을 지원하지 않는 응답기입니다", "")
		return
	}

	// 컨텍스트 자동 관리 — 최근 N개만 모델에 전송.
	msgs := req.Messages
	if len(msgs) > assistantMaxTurns {
		msgs = msgs[len(msgs)-assistantMaxTurns:]
	}

	// 라이브 상태 수집 (best-effort) → 마지막 운영자 메시지에 참고 컨텍스트로 주입.
	// (검증 전이 아니라 slice 복사 후 주입해 원본 보존)
	contextKo := s.buildAssistantContext(r)
	if contextKo != "" {
		injected := append([]llm.ChatTurn(nil), msgs...)
		li := len(injected) - 1
		injected[li].Content = "[현재 시스템·수조 상태 — 참고용. 실시간 안전 판단은 Arbiter 가 담당하며 당신의 판단이 아닙니다]\n" +
			contextKo +
			"\n\n운영자 질문: " + injected[li].Content
		msgs = injected
	}

	// SSE 헤더.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	sendSSE(w, flusher, map[string]any{"context_ko": contextKo})

	timeout := time.Duration(s.cfg.LLM.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	_, err := s.llmClient.ChatStream(ctx, s.cfg.LLM.AssistantModel, s.cfg.LLM.AssistantKeepAlive, msgs,
		func(delta string) {
			sendSSE(w, flusher, map[string]any{"delta": delta})
		})
	if err != nil {
		sendSSE(w, flusher, map[string]any{"error": "어시스턴트 호출 실패: " + err.Error()})
		return
	}
	sendSSE(w, flusher, map[string]any{"done": true})
}

// sendSSE — 한 SSE 이벤트("data: <json>\n\n") 를 쓰고 flush.
func sendSSE(w http.ResponseWriter, f http.Flusher, payload map[string]any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	f.Flush()
}

// buildAssistantContext — 현재 시스템/수조 상태를 압축 한국어 텍스트로 만든다.
// best-effort: 실패한 조회는 건너뛰고 가능한 만큼만 채운다.
func (s *Server) buildAssistantContext(r *http.Request) string {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var b strings.Builder

	// 시스템 수준 요약.
	alerts, _ := s.store.ListOpenAlerts(ctx)
	alertCounts := severityCounter()
	for _, a := range alerts {
		alertCounts.add(a.Severity)
	}
	unsynced, _ := s.store.UnsyncedCount(ctx)
	servicesOverall := s.app.Health.OverallHealth()
	fmt.Fprintf(&b, "■ 시스템: 서비스 %s · 열린 알림 %d건(critical %d / warning %d) · 미동기화 이벤트 %d건\n",
		servicesOverall, len(alerts), alertCounts[events.SeverityCritical], alertCounts[events.SeverityWarning], unsynced)

	// 컨트롤러(장비) 온라인 개요.
	if devices, err := s.store.ListDeviceStatuses(ctx); err == nil {
		online := 0
		for _, d := range devices {
			if stringValue(d["status"]) == events.DeviceStatusOnline || stringValue(d["status"]) == "ok" {
				online++
			}
		}
		fmt.Fprintf(&b, "■ 장비(컨트롤러): 온라인/정상 %d / 전체 %d\n", online, len(devices))
	}

	// 수조별 핵심 상태.
	profiles, err := s.store.ListTankProfiles(ctx)
	if err != nil || len(profiles) == 0 {
		if b.Len() == 0 {
			return ""
		}
		return strings.TrimRight(b.String(), "\n")
	}

	fmt.Fprintf(&b, "■ 수조 %d개:\n", len(profiles))
	shown := 0
	for _, p := range profiles {
		if shown >= assistantMaxTanksInContext {
			fmt.Fprintf(&b, "  …외 %d개 생략\n", len(profiles)-shown)
			break
		}
		shown++
		b.WriteString(s.assistantTankLine(r, p.TankID, p.Species))
	}

	return strings.TrimRight(b.String(), "\n")
}

// assistantTankLine — 한 수조의 한 줄 요약. state vector 재사용으로 대시보드와 값 일치.
func (s *Server) assistantTankLine(r *http.Request, tankID, fallbackSpecies string) string {
	v, err := s.buildTankStateVector(r, tankID)
	if err != nil || v == nil {
		if fallbackSpecies != "" {
			return fmt.Sprintf("  - %s (%s): 상태 조회 실패\n", tankID, fallbackSpecies)
		}
		return fmt.Sprintf("  - %s: 상태 조회 실패\n", tankID)
	}

	species := v.BiologicalContext.Species
	if species == "" {
		species = fallbackSpecies
	}
	mode := v.Autonomous.Mode
	if mode == "" {
		mode = "off"
	}

	var parts []string
	if m := waterMetricText(v.Water.Metrics, events.MetricDissolvedOxygen, "DO"); m != "" {
		parts = append(parts, m)
	}
	if m := waterMetricText(v.Water.Metrics, events.MetricWaterTemperature, "수온"); m != "" {
		parts = append(parts, m)
	}
	if m := waterMetricText(v.Water.Metrics, events.MetricPH, "pH"); m != "" {
		parts = append(parts, m)
	}
	if m := waterMetricText(v.Water.Metrics, events.MetricSalinity, "염도"); m != "" {
		parts = append(parts, m)
	}
	water := "수질 데이터 없음"
	if len(parts) > 0 {
		water = strings.Join(parts, ", ")
	}

	equip := v.Equipment.HealthSummary
	if equip == "" {
		equip = "unknown"
	}

	head := tankID
	if species != "" {
		head = fmt.Sprintf("%s (%s, mode=%s)", tankID, species, mode)
	} else {
		head = fmt.Sprintf("%s (mode=%s)", tankID, mode)
	}

	line := fmt.Sprintf("  - %s: %s | 장비 %s | 오늘 급이 %.0fg", head, water, equip, v.Feeding.TodayTotalG)
	if v.Anomaly.LatestVerdict != "" && v.Anomaly.LatestVerdict != "normal" {
		line += " | 이상감지 " + v.Anomaly.LatestVerdict
	}
	return line + "\n"
}

// waterMetricText — metric 의 현재값을 "라벨 값단위" 로. 값 없으면 빈 문자열.
func waterMetricText(metrics map[string]*WaterMetric, key, label string) string {
	m, ok := metrics[key]
	if !ok || m == nil || m.Value == nil {
		return ""
	}
	unit := m.Unit
	return fmt.Sprintf("%s %.1f%s", label, *m.Value, unit)
}
