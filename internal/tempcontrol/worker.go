// Package tempcontrol — 자동 수온 제어 워커.
// 냉각기(히트펌프 onoff_feeder switch, ON=냉각/온도↓)를 목표수온 유지하도록 자동 ON/OFF.
// 결정 = optimize.RecommendTemp (히스테리시스 + DO 안전제약 + temp_max 내장, shadow→active).
// 안전: cool 은 temp>목표+히스테리시스일 때만(추운 어체를 더 냉각하지 않음), 최소 전환간격(압축기 보호),
//
//	수온 미수신 시 무동작. 공유 수처리라 대표 수온 1개로 냉각기 1대 제어.
package tempcontrol

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/optimize"
	"bluei.kr/edge/internal/storage"
)

const tempParamsKey = "optimize.temp.params"

type Worker struct {
	store        storage.Store
	cfg          config.TempControlConfig
	chillerExtID int
	// staleSec — 수온 관측이 이보다 오래되면 "데이터 이상" 알림 (0 = 기본 300초).
	// arbiter 안전 게이트가 급이를 막을 때 쓰는 sensor_max_stale_sec 과 같은 값을
	// 받아 두 판단이 어긋나지 않게 한다. stale_alert.go 참고.
	staleSec int
	log      *slog.Logger
	done     chan struct{}
	cancel   context.CancelFunc // loop 컨텍스트 취소(Stop 시)

	mu           sync.Mutex
	lastSwitchAt time.Time
	lastDesired  *bool        // 마지막으로 명령한 냉각기 상태(nil=미명령)
	tempHist     []tempSample // 변화율 계산용 최근 수온 이력
}

type tempSample struct {
	t    time.Time
	temp float64
}

const tempHistWindowMin = 20 // 변화율 계산 창(분)

// recordAndRate — 수온 이력에 추가 + 최근 변화율(℃/h) 계산.
// 창(20분) 내 가장 오래된↔최신 표본으로 계산(최소 5분 간격 확보 시).
func (w *Worker) recordAndRate(temp float64) (ratePerHour float64, ok bool) {
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tempHist = append(w.tempHist, tempSample{t: now, temp: temp})
	cutoff := now.Add(-tempHistWindowMin * time.Minute)
	i := 0
	for i < len(w.tempHist) && w.tempHist[i].t.Before(cutoff) {
		i++
	}
	w.tempHist = w.tempHist[i:]
	if len(w.tempHist) < 2 {
		return 0, false
	}
	first, last := w.tempHist[0], w.tempHist[len(w.tempHist)-1]
	hours := last.t.Sub(first.t).Hours()
	if hours < 5.0/60.0 { // 최소 5분 표본 간격
		return 0, false
	}
	return (last.temp - first.temp) / hours, true
}

// anticipatedCoastOff — 열관성 보정. 냉각 중(rate<0)일 때, 지금 정지하면 lead분 뒤 관성
// 하강으로 도달할 예상 온도가 목표에 닿으면 true(=지금 정지). 정지 후 코스팅이 목표에 착지한다.
func anticipatedCoastOff(temp, ratePerHour, leadMin, setpoint float64) bool {
	if ratePerHour >= 0 || leadMin <= 0 {
		return false
	}
	projected := temp + ratePerHour*(leadMin/60.0)
	return projected <= setpoint
}

func NewWorker(store storage.Store, cfg config.TempControlConfig, chillerExtID int) *Worker {
	return &Worker{
		store:        store,
		cfg:          cfg,
		chillerExtID: chillerExtID,
		log:          slog.With("service", "temp_control"),
		done:         make(chan struct{}),
	}
}

// SetSensorStaleSec wires the staleness threshold (arbiter 안전 게이트와 같은 값).
func (w *Worker) SetSensorStaleSec(sec int) { w.staleSec = sec }

func (w *Worker) intervalSec() int {
	if w.cfg.IntervalSec > 0 {
		return w.cfg.IntervalSec
	}
	return 60
}

func (w *Worker) minSwitchSec() int {
	if w.cfg.MinSwitchSec > 0 {
		return w.cfg.MinSwitchSec
	}
	return 600
}

func (w *Worker) Name() string { return "temp_control" }

func (w *Worker) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.log.Info("temp control disabled")
		return nil
	}
	if w.chillerExtID <= 0 {
		w.log.Warn("temp control: chiller switch ext_id not configured — disabled")
		return nil
	}
	// startup ctx 에 묶으면 startup_timeout 후 루프가 죽는다 → Background 에서 분리(피어 워커 패턴).
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(runCtx)
	w.log.Info("temp control worker started",
		"interval_sec", w.intervalSec(), "min_switch_sec", w.minSwitchSec(),
		"dry_run", w.cfg.DryRun, "chiller_ext", w.chillerExtID)
	return nil
}

func (w *Worker) Stop(context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	close(w.done)
	return nil
}

func (w *Worker) loop(ctx context.Context) {
	tick := time.NewTicker(time.Duration(w.intervalSec()) * time.Second)
	defer tick.Stop()
	w.tick(ctx) // 즉시 1회
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case <-tick.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	temp, do, hasDO, ok, tempAt := w.readTempDO(ctx)
	// 수온 데이터 이상이면 알림만 올린다 — 냉각기 상태는 건드리지 않는다
	// (2026-08-28 운영자 결정). 판단 불가 상태 자체를 운영자가 알게 하는 것이 목적이다.
	w.checkTempStaleness(ctx, tempAt, ok)
	if !ok {
		return // 수온 미수신 → 판단 불가 → 무동작(안전)
	}
	p := w.loadParams(ctx)
	rate, haveRate := w.recordAndRate(temp)
	rec := optimize.RecommendTemp(optimize.TempInputs{CurrentTempC: temp, CurrentDO: do, HasDO: hasDO}, p)

	// 전력 피드백으로 실제 냉각기 가동 여부 판정 (마스터 ON 이면 최근 5분 내 유의미 전력).
	actualOn, havePower := w.chillerRunning(ctx)
	// 현재 유효 가동상태: 전력 우선, 없으면 마지막 명령.
	w.mu.Lock()
	curOn := false
	if havePower {
		curOn = actualOn
	} else if w.lastDesired != nil {
		curOn = *w.lastDesired
	}
	w.mu.Unlock()

	// 냉각 역부족 감시 (2026-10-01) — 돌고 있는데 효과가 없으면 알린다.
	w.checkCoolingShortfall(ctx, temp, curOn)

	// action → 냉각기 desired 가동상태. hold=현재 상태 유지(코스팅 예측으로 조기 정지 가능).
	var desiredOn bool
	switch rec.Action {
	case optimize.ActionCool:
		desiredOn = true
	case optimize.ActionAllowRise:
		desiredOn = false
	default:
		desiredOn = curOn
	}

	// 변화율 가드(열충격 방지): 승온 과속 → 냉각(ON)으로 완화, 냉각 과속 → 중단(OFF)으로 완화.
	if maxRamp := p.MaxRampCPerHour; maxRamp > 0 && haveRate {
		switch {
		case rate > maxRamp:
			if !desiredOn {
				w.log.Warn("temp ramp guard: 승온 과속 → 냉각 완화", "rate_c_per_h", rate, "max_c_per_h", maxRamp, "temp_c", temp)
			}
			desiredOn = true
		case rate < -maxRamp:
			if desiredOn {
				w.log.Warn("temp ramp guard: 냉각 과속 → 냉각 중단", "rate_c_per_h", rate, "max_c_per_h", maxRamp, "temp_c", temp)
			}
			desiredOn = false
		}
	}

	// 코스팅 예측 조기 정지(열관성 보정): 냉각 중 하강이면 lead분 뒤 예상 온도가 목표에 도달할 때
	// 미리 정지 → 정지 후 관성 하강이 목표 근처에 착지(언더슈트 방지).
	coasted := false
	if desiredOn && haveRate && p.CoolingLeadMin > 0 &&
		anticipatedCoastOff(temp, rate, p.CoolingLeadMin, rec.RecommendedSetpointC) {
		projected := temp + rate*(p.CoolingLeadMin/60.0)
		w.log.Info("temp control: 코스팅 예측 조기 정지(열관성 보정)",
			"temp_c", temp, "rate_c_per_h", rate, "projected_c", projected,
			"target_c", rec.RecommendedSetpointC, "lead_min", p.CoolingLeadMin)
		desiredOn = false
		coasted = true
	}

	// 판단 스냅샷 저장(대시보드 노출용) — 실동작 여부와 무관, 매 tick 최신 판단 기록.
	active := w.isActive(ctx)
	reason := rec.Rationale
	if coasted {
		reason = "코스팅 예측 정지 — 관성으로 목표 착지 예상"
	}
	w.persistDecision(ctx, decisionSnapshot{
		TempC: temp, HaveRate: haveRate, RateCPerH: rate,
		TargetC: rec.RecommendedSetpointC, Action: rec.Action,
		DesiredOn: desiredOn, ActualOn: actualOn, HavePower: havePower,
		Coasting: coasted, Active: active, Reason: reason,
	})

	// 이미 원하는 상태면 아무것도 안 함 (Hold 포함, lastDesired 동기화).
	if desiredOn == curOn {
		w.mu.Lock()
		d := desiredOn
		w.lastDesired = &d
		w.mu.Unlock()
		return
	}

	// 충돌 감지: 우리가 이미 desiredOn 으로 명령했는데 실제가 다르면 = onoff_feeder/현장이 바꾼 것.
	w.mu.Lock()
	conflict := havePower && actualOn != desiredOn && w.lastDesired != nil && *w.lastDesired == desiredOn
	// 압축기 보호: 최소 전환 간격 (우리 마지막 명령 기준). 저쪽 변경은 우리 lastSwitchAt 과 무관하므로,
	// 최근에 우리가 안 건드렸으면 즉시 교정, 지속 충돌 시엔 600s 로 throttle.
	if !w.lastSwitchAt.IsZero() && time.Since(w.lastSwitchAt) < time.Duration(w.minSwitchSec())*time.Second {
		w.mu.Unlock()
		w.log.Info("temp control: switch suppressed (min interval)",
			"temp_c", temp, "action", rec.Action, "desired_on", desiredOn,
			"since_last_sec", int(time.Since(w.lastSwitchAt).Seconds()))
		return
	}
	w.mu.Unlock()

	if conflict {
		w.log.Warn("냉각기 상태 충돌 — onoff_feeder/현장이 우리 결정과 다르게 변경됨, 재교정(bypass)",
			"desired_on", desiredOn, "actual_on", actualOn, "temp_c", temp)
	}
	w.log.Info("temp control decision",
		"temp_c", temp, "target_c", rec.RecommendedSetpointC, "do", do, "action", rec.Action,
		"chiller_on", desiredOn, "actual_on", actualOn, "have_power", havePower,
		"active", active, "rationale", rec.Rationale)

	if !active {
		return // 관찰(shadow) 모드 — 로그만, 실작동 X
	}

	if err := w.actuate(ctx, desiredOn); err != nil {
		w.log.Error("temp control actuation failed — 냉각기 상태 미변경", "error", err, "desired_on", desiredOn)
		if !desiredOn {
			// 냉각기 OFF 가 transport 재전송(3회)까지 소진하고도 실패 = 과냉각 위험 → critical.
			w.raiseChillerOffFailureAlert(ctx, err)
		}
		return
	}
	// 전환 성공 — 이전에 열린 OFF 실패 알림이 있으면 해소(스위치 채널 응답 복구).
	w.clearChillerOffFailureAlert(ctx)
	w.mu.Lock()
	w.lastSwitchAt = time.Now()
	d := desiredOn
	w.lastDesired = &d
	w.mu.Unlock()
}

const (
	chillerDeviceID           = "chiller_1"
	chillerRunningThresholdKW = 0.5 // 이 이상 전력이면 냉각기 가동(마스터 ON)으로 판정
	chillerPowerWindowMin     = 5   // 최근 N분 전력으로 판정 (내부 압축기 사이클 평활)
)

// chillerRunning — 최근 전력으로 냉각기 마스터 가동 여부 판정.
// 마스터 OFF 면 전력 0 지속, ON 이면 내부 압축기 사이클로 간헐 전력 → 창 내 최댓값으로 판정.
func (w *Worker) chillerRunning(ctx context.Context) (running, ok bool) {
	since := time.Now().UTC().Add(-chillerPowerWindowMin * time.Minute)
	evs, err := w.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		DeviceID:  chillerDeviceID,
		Since:     &since,
		Limit:     30,
	})
	if err != nil || len(evs) == 0 {
		return false, false
	}
	found := false
	maxKW := 0.0
	for _, e := range evs {
		var p events.SensorReadingPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.Value == nil {
			continue
		}
		if p.Metric == events.MetricChillerPower {
			found = true
			if *p.Value > maxKW {
				maxKW = *p.Value
			}
		}
	}
	if !found {
		return false, false
	}
	return maxKW > chillerRunningThresholdKW, true
}

// readTempDO — 대표 수온/DO (공유 수처리라 첫 유효 수조값 사용).
// readTempDO — 수온/DO 최신값. tempObservedAt 은 **수온 관측 시각**으로, 수집이 멎어도
// 마지막 값은 테이블에 남으므로 단절 판별에는 값이 아니라 이 시각을 봐야 한다
// (2026-08-28 과냉각 사고). stale_alert.go 가 쓴다.
func (w *Worker) readTempDO(ctx context.Context) (temp, do float64, hasDO, ok bool, tempObservedAt string) {
	profiles, err := w.store.ListTankProfiles(ctx)
	if err != nil {
		return 0, 0, false, false, ""
	}
	for _, tp := range profiles {
		envs, err := w.store.ListTankEnvironment(ctx, tp.TankID)
		if err != nil {
			continue
		}
		var t, d *float64
		var tAt string
		for _, e := range envs {
			if e.Value == nil {
				continue
			}
			switch e.Metric {
			case events.MetricWaterTemperature:
				t, tAt = e.Value, e.ObservedAt
			case events.MetricDissolvedOxygen:
				d = e.Value
			}
		}
		if t != nil {
			if d != nil {
				return *t, *d, true, true, tAt
			}
			return *t, 0, false, true, tAt
		}
	}
	return 0, 0, false, false, ""
}

const tempControlActiveKey = "tempcontrol.active" // 런타임 자동제어 on/off (대시보드 토글, KV)

// isActive — 런타임 자동제어 여부. KV(대시보드 토글) 우선, 없으면 config default(!DryRun).
func (w *Worker) isActive(ctx context.Context) bool {
	if v, ok, err := w.store.KVGet(ctx, tempControlActiveKey); err == nil && ok && v != "" {
		return v == "true"
	}
	return !w.cfg.DryRun
}

const tempLastDecisionKey = "tempcontrol.last_decision" // 워커 마지막 판단(대시보드 노출)

// decisionSnapshot — 워커가 매 tick 내린 판단. 대시보드가 "지금 왜 켜졌나/꺼졌나"를 표시.
type decisionSnapshot struct {
	At        string  `json:"at"`
	TempC     float64 `json:"temp_c"`
	HaveRate  bool    `json:"have_rate"`
	RateCPerH float64 `json:"rate_c_per_h"`
	TargetC   float64 `json:"target_c"`
	Action    string  `json:"action"` // cool | hold | allow_rise
	DesiredOn bool    `json:"desired_on"`
	ActualOn  bool    `json:"actual_on"`
	HavePower bool    `json:"have_power"`
	Coasting  bool    `json:"coasting"` // 코스팅 예측으로 조기 정지한 tick
	Active    bool    `json:"active"`   // 자동제어 활성(false=관찰 모드)
	Reason    string  `json:"reason"`
}

func (w *Worker) persistDecision(ctx context.Context, d decisionSnapshot) {
	d.At = time.Now().UTC().Format(time.RFC3339)
	b, err := json.Marshal(d)
	if err != nil {
		return
	}
	if err := w.store.KVSet(ctx, tempLastDecisionKey, string(b)); err != nil {
		w.log.Warn("temp control: 판단 스냅샷 저장 실패", "error", err)
	}
}

func (w *Worker) loadParams(ctx context.Context) optimize.TempParams {
	p := optimize.DefaultTempParams()
	if v, ok, err := w.store.KVGet(ctx, tempParamsKey); err == nil && ok && v != "" {
		_ = json.Unmarshal([]byte(v), &p)
	}
	return p
}

// actuate — onoff_feeder switch control 로 냉각기 ON/OFF. fresh ctx(shutdown 무관).
func (w *Worker) actuate(_ context.Context, on bool) error {
	adapter := control.NewOnoffFeederControlAdapter(config.ControlAdapter{ID: "temp-control"})
	fctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := adapter.Execute(fctx, common.NewID("cmd"), "switch.set", map[string]any{
		"ext_id": float64(w.chillerExtID),
		"on":     on,
	})
	return err
}

const chillerOffFailureKey = "chiller.off_failure." + chillerDeviceID

// raiseChillerOffFailureAlert — 냉각기 OFF 가 재전송까지 소진하고도 실패하면 critical.
// 냉각기가 꺼지지 않아 과냉각으로 방치되는 유일한 시나리오. dedupeKey 로 1건만 유지(idempotent).
func (w *Worker) raiseChillerOffFailureAlert(ctx context.Context, cause error) {
	existing, err := w.store.GetOpenAlert(ctx, chillerOffFailureKey)
	if err != nil {
		w.log.Warn("get chiller off-failure alert failed", "error", err)
		return
	}
	if existing != nil {
		return // 이미 열려있음 — idempotent
	}
	at := common.NowUTC()
	alertID := common.NewAlertID()
	msg := "냉각기 정지(OFF) 명령이 재전송 후에도 실패 — 냉각기가 계속 가동해 과냉각될 수 있음, 즉시 수동 확인 필요"
	payload := map[string]any{
		"alert_id":   alertID,
		"alert_type": "chiller.off_failure",
		"severity":   "critical",
		"status":     "open",
		"subject":    map[string]any{"kind": "device", "id": chillerDeviceID},
		"message":    msg,
		"evidence": map[string]any{
			"ext_id":      w.chillerExtID,
			"error":       cause.Error(),
			"detected_at": common.FormatTime(at),
		},
		"raised_at":  common.FormatTime(at),
		"updated_at": common.FormatTime(at),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        alertID,
		AlertDedupeKey: chillerOffFailureKey,
		AlertType:      "chiller.off_failure",
		Severity:       "critical",
		SubjectKind:    "device",
		SubjectID:      chillerDeviceID,
		Status:         "open",
		RaisedAt:       at,
		UpdatedAt:      at,
		PayloadJSON:    string(body),
	}
	if _, err := w.store.UpsertAlert(ctx, row); err != nil {
		w.log.Warn("upsert chiller off-failure alert failed", "error", err)
		return
	}
	w.log.Error("냉각기 OFF 지속 실패 — critical 알림 발행", "ext_id", w.chillerExtID)
}

// clearChillerOffFailureAlert — 전환 성공 시 열린 OFF 실패 알림 해소(없으면 무해).
func (w *Worker) clearChillerOffFailureAlert(ctx context.Context) {
	if err := w.store.ClearAlert(ctx, chillerOffFailureKey); err != nil {
		w.log.Warn("clear chiller off-failure alert failed", "error", err)
	}
}
