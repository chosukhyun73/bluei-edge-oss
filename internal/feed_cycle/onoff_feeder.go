package feed_cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/controller"
	"bluei.kr/edge/internal/onoff_feeder"
	"bluei.kr/edge/internal/storage"
)

// onoff 급이기 구동 — feeder control run/stop (경연 경로).
// ESP32 폴링(dispatchPulse)과 완전 분리. 펄스 지속시간은 edge 가 제어(run → 대기 → stop).

const defaultFeederRateGPS = 9.4 // 배출률 g/s (config 0 → 이 값)

// resolveOnoffFeeder — 사이클을 onoff 급이기에 바인딩한다.
// controller_id 가 있으면 그 컨트롤러, 없으면(대시보드 Start) tank_id 로 급이기 컨트롤러 해석.
// metadata control_backend=onoff_feeder + feeder_ext_id>0 이어야 한다.
// 실작동(live)은 config allowlist(onoff_feeder_live_tanks) 에 명시된 tank 만;
// allowlist 미설정 시 전 수조 dry-run(추정만 누적) — fail-safe.
func (w *Worker) resolveOnoffFeeder(ctx context.Context, c *Cycle) {
	var ctrl *controller.Controller
	if c.ControllerID != "" {
		ctrl, _ = w.store.GetController(ctx, c.ControllerID)
	} else {
		ctrl = w.feederControllerForTank(ctx, c.TankID)
	}
	if ctrl == nil {
		return
	}
	if backend, _ := ctrl.Metadata["control_backend"].(string); backend != "onoff_feeder" {
		return
	}
	ext := metaInt(ctrl.Metadata, "feeder_ext_id")
	if ext <= 0 {
		return
	}
	c.ControllerID = ctrl.ControllerID
	live := w.onoffFeederLive(c.TankID)
	c.SetOnoffFeeder(ext, live)
	// 대시보드 리드타임(그룹정책 feeder_spinup_sec) — 설정 시 config 보다 우선.
	if pol, err := w.store.GetEffectiveFeedingPolicy(ctx, c.TankID); err == nil && pol != nil && pol.FeederSpinupSec > 0 {
		c.SetFeederSpinupSec(pol.FeederSpinupSec)
	}
	w.log.Info("onoff feeder bound to cycle",
		"cycle_id", c.CycleID, "tank_id", c.TankID, "controller_id", ctrl.ControllerID,
		"ext_id", ext, "live", live, "spinup_sec", c.FeederSpinupSec())
}

// feederControllerForTank — active 컨트롤러 중 해당 tank 의 onoff 급이기를 찾는다.
func (w *Worker) feederControllerForTank(ctx context.Context, tankID string) *controller.Controller {
	list, err := w.store.ListControllers(ctx, "active")
	if err != nil {
		return nil
	}
	for _, ct := range list {
		if ct.TankID == tankID && metaInt(ct.Metadata, "feeder_ext_id") > 0 {
			return ct
		}
	}
	return nil
}

// onoffFeederLive — 이 tank 가 실작동 allowlist 에 있는지. allowlist 미설정
// (빈 목록) 이면 전 수조 dry-run — fail-safe. 종전엔 미설정 시 tank_02 를 실작동
// 시켰으나, 신규 현장에서 설정 누락이 엉뚱한 수조의 실 급이기를 구동하는 위험이라
// 빈=실작동 없음 으로 변경 (2026-07, 신규 현장 안전). 실작동 수조는 config 의
// onoff_feeder_live_tanks 로 명시해야 한다.
func (w *Worker) onoffFeederLive(tankID string) bool {
	for _, t := range w.cfg.OnoffFeederLiveTanks {
		if t == tankID {
			return true
		}
	}
	return false
}

func (w *Worker) feederRateGPS() float64 {
	if w.cfg.FeederRateGramsPerSec > 0 {
		return w.cfg.FeederRateGramsPerSec
	}
	return defaultFeederRateGPS
}

// WP-F — onoff_feeder 마이크로펄스 기본값(현장에서 조정). run초 가동 → stop초 정지 반복.
const (
	defaultFeederRunSec  = 2 // 2초 가동 ≈ 18.8g
	defaultFeederStopSec = 6 // 6초 정지(섭취) — 현장 실측: 정지 길수록 onoff_feeder rate limit 충돌↓·정지 안정↑
)

func (w *Worker) feederRunSec() int {
	if w.cfg.FeederRunSec > 0 {
		return w.cfg.FeederRunSec
	}
	return defaultFeederRunSec
}

func (w *Worker) feederStopSec() int {
	if w.cfg.FeederStopSec > 0 {
		return w.cfg.FeederStopSec
	}
	return defaultFeederStopSec
}

// defaultFeederSpinupSec — 공급모터 리드타임(초). 살포모터 기동 후 공급모터가 걸려
// 실배출이 시작되기까지의 대기. 실측 2026-07-04(탑뷰 영상): run 후 +11~13s 낙하 시작.
const defaultFeederSpinupSec = 10

// feederSpinupSec — 펄스 ON 시간에 더해지는 리드(초). -1 → 리드 없음(테스트용).
func (w *Worker) feederSpinupSec() int {
	if w.cfg.FeederSpinupSec < 0 {
		return 0
	}
	if w.cfg.FeederSpinupSec == 0 {
		return defaultFeederSpinupSec
	}
	return w.cfg.FeederSpinupSec
}

// defaultFeederStopReinforceSec — gap 동안 stop 재전송 간격(초). onoff_feeder 1req/s 준수(≥1s).
const defaultFeederStopReinforceSec = 2

func (w *Worker) feederStopReinforceSec() int {
	if w.cfg.FeederStopReinforceSec > 0 {
		return w.cfg.FeederStopReinforceSec
	}
	return defaultFeederStopReinforceSec
}

// feederRun — onoff feeder control run (live) 또는 로그(dry-run).
func (w *Worker) feederRun(c *Cycle) error {
	if !c.FeederLive() {
		w.log.Info("feeder run (dry-run, 실제 미구동)", "cycle_id", c.CycleID, "tank_id", c.TankID, "ext_id", c.FeederExtID())
		return nil
	}
	return w.feederActuate(c, "feeder.run")
}

// feederStop — onoff feeder control stop. ⚠ 항상 fresh context(shutdown/취소 무관)로 발행.
// 실패 시 error 반환(호출부가 연속 실패 카운트) + 로깅. 급이기가 켜진 채 남을 수 있어 error 로 강조.
// stop 은 멱등 — 이미 멈춘 상태에 재전송해도 무해.
func (w *Worker) feederStop(c *Cycle) error {
	if !c.FeederLive() {
		w.log.Info("feeder stop (dry-run)", "cycle_id", c.CycleID, "tank_id", c.TankID, "ext_id", c.FeederExtID())
		return nil
	}
	err := w.feederActuate(c, "feeder.stop")
	if err != nil {
		w.log.Error("feeder stop FAILED — 급이기가 계속 돌 수 있음, 즉시 수동 확인 필요",
			"cycle_id", c.CycleID, "tank_id", c.TankID, "ext_id", c.FeederExtID(), "error", err)
	}
	return err
}

// feederStopFailureThreshold — stop 이 연속 이 횟수만큼 모두 실패하면 critical 알림 +
// 사이클 안전정지. 단일 드롭은 재전송으로 흡수하되, 지속 장애는 방치 X.
//
// 2026-08-18 에 2 → 1. 종전에는 sendFeederStop 1회 = 시도 1회라 2회를 요구했으나,
// 이제 1회 호출 안에 즉시 재전송 6회가 들어간다 — 그게 다 실패했다면 이미 12초간
// 6번 실패한 것이고, 종전 임계(2회 = 시도 2번)보다 강한 근거다. 그대로 2 로 두면
// 알림이 29초까지 밀려(종전 21초) 오히려 나빠진다.
const feederStopFailureThreshold = 1

// feederStopQuickRetries / feederStopQuickRetryGap — stop 실패 시 즉시 재전송 횟수와 간격.
// 다음 정기 stop(gap +5초 확인 stop)까지 기다리지 않는다. 1초 간격은 onoff_feeder 1req/s 준수.
//
//	종전   t=0 시도 → 8초 타임아웃 → gap+5초 → t=13 에 2번째 시도   (13초 안에 2회)
//	현재   t=0·2·4 시도, t=5 에 판정                               (5초 안에 3회)
//
// 3회인 이유 (2026-08-18 운영자): 저쪽이 무응답이면 재전송을 아무리 늘려도 소용이
// 없고, 늘릴수록 실패 확정이 밀려 critical 알림만 늦어진다. 응답 간격(P99 0.54초)에
// 맞춰 촘촘히 3발을 보내고, 안 되면 빨리 사람을 부르는 편이 낫다.
// 이후에도 기회는 남는다 — gap +5초 확인 stop 이 같은 3발을 한 번 더 보내고,
// 사이클 종료 시 feederStopFinal 이 또 보낸다.
//
// ⚠ 예산은 종전(13초) 안에 묶는다. 재전송을 늘리는 것과 포기를 미루는 것은 다른
// 문제다 — 예산을 넘기면 stop 이 끝내 안 되는 상황에서 critical 알림이 그만큼 밀린다.
const (
	feederStopQuickRetries  = 2
	feederStopQuickRetryGap = 1 * time.Second
)

// feederOverrunWarnMs — 초과 회전 경고 임계(ms). 최근 4일 246발 stop ACK 지연이
// P99 0.54초라, 1초를 넘는 초과는 정상 변동이 아니다. 경고 1건 = 사료가 설계보다
// 9.4g/s 만큼 더 나갔다는 뜻이라 장부·평가 양쪽에서 확인이 필요하다.
const feederOverrunWarnMs = 1000

// feederActuateTimeout — 명령별 API 타임아웃. stop 은 run 보다 짧게 잡는다.
//
// run 이 늦으면 급이가 늦어질 뿐이지만, stop 이 늦으면 **그동안 모터가 계속 돈다**.
// 2026-08-18 14:49 tank_02 p7 실사고: 공통 8초 타임아웃 탓에 stop 이 8초를 다 쓰고
// 실패했고, 확인 stop(gap +5초)이 성공하기까지 모터가 최대 29.5초 돌았다 —
// 설계 13.2초의 2.2배. 최근 4일 240발 중 P99 가 13.54초라 명백한 이상치였는데,
// 장부에는 다른 펄스와 똑같이 28.2g 으로 적혔다.
//
// 1초 근거 — 최근 4일 246발 stop ACK 지연 실측:
//
//	P50 0.214s · P90 0.339 · P95 0.397 · P99 0.540 · 최대 16.47(사고)
//	타임아웃 0.50s → 헛 타임아웃 3/246 · 1.00s → 2/246 · 3.00s → 1/246
//
// P99 의 약 2배인 1초면 정상 응답은 거의 다 받고, 헛 타임아웃은 246발 중 2건이다.
// 그 대가는 재전송 1회뿐이다 — stop 은 멱등이라 중복 실행 위험이 없다(run 과 다름).
// 기다려서 얻는 것보다 새 요청을 보내는 편이 모터를 빨리 세운다.
//
// ⚠ 2026-10-02 수정 — 위 1초는 **네트워크 왕복만** 잰 값이라 게이트 대기가 빠져 있었다.
// 모든 onoff_feeder 호출은 전역 게이트가 1.2초 간격으로 직렬화한다(onoff_feeder.DefaultMinInterval).
// 직전 1.2초 안에 다른 호출이 나갔으면 stop 은 큐에서 기다리다 자기 1초에 죽는다 —
// 네트워크에 나가지도 못한다. PriorityHigh 는 줄 순서만 바꾸고 간격은 못 건너뛴다.
// 실측: 수질 폴링이 매분 :13 에 버스트로 나가고, 그날 stop 실패 4건이 전부 :14~:15 였다
// (같은 날 펄스 종료 초는 00~59 고르게 분포). 16:30 건은 4연속 실패로 tank_02 사이클이
// 5발에서 safety_block 됐는데 급이기는 실제로 멈춰 있었다 — 거짓 경보다.
//
// 그래서 **게이트 간격에서 파생**시킨다. 두 상수를 각각 손으로 정하면 또 어긋난다.
// PriorityHigh 는 대기 중인 호출을 앞질러 가므로 stop 이 실제로 기다리는 것은
// "진행 중인 호출 1건 + 간격 1.2초" 뿐이다. 폴링이 정상 속도(P99 0.54초)면 이 예산 안에 든다.
const feederStopNetworkBudget = 1 * time.Second

func feederActuateTimeout(cmd string) time.Duration {
	if cmd == "feeder.stop" {
		return onoff_feeder.DefaultMinInterval + feederStopNetworkBudget
	}
	return 8 * time.Second
}

// raiseFeederStopFailureAlert — stop 연속 실패 시 운영자 critical 알림으로 승격.
// dedupeKey 로 Cage/Tank 당 1건만 유지 (idempotent). 급이기가 켜진 채 방치되는
// 유일한 시나리오이므로 severity=critical. 운영자가 수동 확인 후 close.
func (w *Worker) raiseFeederStopFailureAlert(ctx context.Context, tankID, cycleID string, failures int, at time.Time) {
	key := "tank.feeder.stop_failure." + tankID
	existing, err := w.store.GetOpenAlert(ctx, key)
	if err != nil {
		w.log.Warn("get feeder stop-failure alert failed", "tank_id", tankID, "error", err)
		return
	}
	if existing != nil {
		return // 이미 열려있음 — idempotent
	}
	alertID := common.NewAlertID()
	msg := fmt.Sprintf("Cage/Tank %s 급이기 정지 명령 %d회 연속 실패 — 급이기가 계속 돌 수 있음, 즉시 수동 정지 확인 필요", tankID, failures)
	payload := map[string]any{
		"alert_id":   alertID,
		"alert_type": "tank.feeder.stop_failure",
		"severity":   "critical",
		"status":     "open",
		"subject":    map[string]any{"kind": "tank", "id": tankID},
		"message":    msg,
		"evidence": map[string]any{
			"tank_id":              tankID,
			"cycle_id":             cycleID,
			"consecutive_failures": failures,
			"detected_at":          common.FormatTime(at),
		},
		"raised_at":  common.FormatTime(at),
		"updated_at": common.FormatTime(at),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        alertID,
		AlertDedupeKey: key,
		AlertType:      "tank.feeder.stop_failure",
		Severity:       "critical",
		SubjectKind:    "tank",
		SubjectID:      tankID,
		Status:         "open",
		RaisedAt:       at,
		UpdatedAt:      at,
		PayloadJSON:    string(body),
	}
	if _, err := w.store.UpsertAlert(ctx, row); err != nil {
		w.log.Warn("upsert feeder stop-failure alert failed", "tank_id", tankID, "error", err)
		return
	}
	if w.app != nil {
		_, _ = w.app.AppendEvent(ctx, "feed_cycle", "", tankID, "alert.raised", alertID, payload)
	}
}

// feederStopFinal — 사이클 종료 시 마무리 stop. "한 번 더 확실히 끈다".
// 실패하면 reinforce 간격으로 재시도, 그래도 실패면 critical 알림(급이기 방치 방지).
// feederActuate 는 항상 fresh context 로 발행하므로 shutdown 중에도 stop 자체는 발행된다.
func (w *Worker) feederStopFinal(ctx context.Context, c *Cycle) {
	if !c.FeederLive() {
		w.feederStop(c) // dry-run 로그
		return
	}
	const attempts = 2
	for i := 0; i < attempts; i++ {
		if w.feederStop(c) == nil {
			return
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
			case <-time.After(time.Duration(w.feederStopReinforceSec()) * time.Second):
			}
		}
	}
	// 마무리 stop 이 모두 실패 — 급이기가 켜진 채 남을 수 있음. critical 알림(idempotent).
	w.raiseFeederStopFailureAlert(ctx, c.TankID, c.CycleID, attempts, common.NowUTC())
}

func (w *Worker) feederActuate(c *Cycle, cmd string) error {
	if w.feederActuateFn != nil {
		return w.feederActuateFn(c, cmd)
	}
	adapter := control.NewOnoffFeederControlAdapter(config.ControlAdapter{ID: "feeder-cycle"})
	fctx, cancel := context.WithTimeout(context.Background(), feederActuateTimeout(cmd))
	defer cancel()
	_, err := adapter.Execute(fctx, common.NewID("cmd"), cmd, map[string]any{
		"ext_id": float64(c.FeederExtID()),
	})
	return err
}

// hardCapExceeded — params 무관 하드 안전 상한(최대 시간 / 최대 총량). onoff_feeder 실작동 폭주 방지.
func (w *Worker) hardCapExceeded(c *Cycle) bool {
	maxSec := w.cfg.HardMaxSessionSec
	if maxSec <= 0 {
		maxSec = 3600
	}
	maxG := w.cfg.HardMaxTotalGrams
	if maxG <= 0 {
		maxG = 25000
	}
	if time.Since(c.StartedAt) >= time.Duration(maxSec)*time.Second {
		return true
	}
	return c.TotalAmountG() >= maxG
}

// recordFeedingAndConsume — 사이클 종료 시 사료투입 기록(feeding.recorded) + 재고 차감.
// 실제 급이(dispense)한 경우에만 차감: ESP32 는 항상, onoff_feeder 은 live(실작동)만. dry-run 은 기록만.
// 사료 품목 = 사이트 feed 재고의 첫 품목(현재 단일: skreting no6). 다품목 시 tank 매핑은 후속.
func (w *Worker) recordFeedingAndConsume(ctx context.Context, c *Cycle) {
	grams := c.TotalAmountG()
	if grams <= 0 {
		return
	}
	feedingID := common.NewID("feeding")
	payload := map[string]any{
		"feeding_id":    feedingID,
		"tank_id":       c.TankID,
		"feeder_id":     c.ControllerID,
		"source":        "feeder_command",
		"feed_amount_g": grams,
		"fed_at":        common.FormatTime(common.NowUTC()),
		"quality":       "ok",
	}

	realDispense := !c.IsOnoffFeeder() || c.FeederLive()
	if realDispense {
		if items, err := w.store.ListInventoryItems(ctx, "feed"); err == nil && len(items) > 0 {
			item := items[0]
			qty := gramsToUnit(grams, item.Unit)
			if _, err := w.store.AdjustInventoryOnHand(ctx, item.ItemID, -qty); err != nil {
				w.log.Warn("feed inventory consume failed", "item_id", item.ItemID, "grams", grams, "error", err)
			} else {
				payload["item_id"] = item.ItemID
				payload["feed_type"] = item.Name
				payload["consumed_qty"] = qty
				w.log.Info("feed consumed from inventory", "tank_id", c.TankID, "item", item.Name, "consumed_qty", qty, "unit", item.Unit)
			}
		}
	}
	w.emitEvent(ctx, "feeding.recorded", feedingID, c.TankID, payload)
}

// gramsToUnit — 그램을 품목 단위로 변환. 사료는 보통 kg.
func gramsToUnit(grams float64, unit string) float64 {
	switch unit {
	case "g", "그램":
		return grams
	default: // kg 등 (사료 기본 kg)
		return grams / 1000.0
	}
}

// metaInt — Metadata(JSON 언마샬 시 숫자는 float64)에서 int 추출.
func metaInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
