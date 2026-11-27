import { useEffect, useReducer, useState } from 'react';
import { useLanguage } from '../../lib/language-core';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { computeFeedingPolicy, feedingWeightG } from '../../lib/species-policy';
import { Schedules } from '../../lib/api';
import type { FeedingSchedule } from '../../lib/types';
import {
  getEffectiveTankPolicy, getGroupPolicy, setGroupPolicy, ensureLoaded,
} from '../../lib/feeding-policy-store';
import { generateCrossTankSchedule } from '../../lib/feeding-schedule';
import { Optimize } from '../../lib/api';
import type { Tank } from '../../lib/types';

function minToHHMM(m: number): string {
  const h = Math.floor(m / 60), mm = m % 60;
  return `${String(h).padStart(2, '0')}:${String(mm).padStart(2, '0')}`;
}
function hhmmToMin(s: string): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec(s);
  if (!m) return null;
  const h = Number(m[1]), mm = Number(m[2]);
  if (h > 23 || mm > 59) return null;
  return h * 60 + mm;
}

// 급이 스케줄 자동생성 카드 — 1회최대량 분할 + 수조간 교차. 점등시간대(입력) 안에서만 배치.
export function FeedingScheduleCard({ tanks }: { tanks: Tank[] }) {
  const { tr } = useLanguage();

  // 실측 수온(수조별) — GET 계산에 주입. 미수신 시 null → fallback(경고).
  const [temps, setTemps] = useState<Record<string, number>>({});
  useEffect(() => {
    let alive = true;
    Optimize.temp()
      .then(res => {
        if (!alive) return;
        const map: Record<string, number> = {};
        for (const r of res.items) map[r.tank_id] = r.current_temp_c;
        setTemps(map);
      })
      .catch(() => { /* 미수신 시 fallback 유지 */ });
    return () => { alive = false; };
  }, []);

  // 백엔드가 실제로 등록한 AI 스케줄 — 화면의 진실원 (2026-10-03, 운영자 결정).
  //
  // 종전에는 이 카드가 회당량을 프론트에서 자체 계산했는데, backend 는 2026-07-18 부터
  // 수온-급이율 곡선(salmonGrowoutFeedRatePct)을 쓰고 프론트는 정적 BSF 표를 그대로
  // 둬서 11.6℃ 에서 264g vs 189g 으로 갈렸다. 수온 급변 감산(×0.5)도 프론트는 모른다.
  // 같은 계산을 두 군데서 하면 반드시 어긋나므로, 스케줄이 있으면 그 값을 그대로 쓴다.
  // 스케줄이 없는 경우(manual 모드)에만 종전 로컬 계산을 예정값으로 보여준다.
  const [backendSched, setBackendSched] = useState<Record<string, FeedingSchedule>>({});
  useEffect(() => {
    let alive = true;
    const load = () => {
      Schedules.list()
        .then(res => {
          if (!alive) return;
          const map: Record<string, FeedingSchedule> = {};
          for (const s of res.items ?? []) {
            for (const tid of s.tank_ids ?? []) map[tid] = s;
          }
          setBackendSched(map);
        })
        .catch(() => { /* 조회 실패 시 직전 값 유지 */ });
    };
    load();
    const t = window.setInterval(load, 5 * 60_000); // 스케줄러 재계산 주기와 동일
    return () => { alive = false; window.clearInterval(t); };
  }, []);

  // 그룹 정책(점등시간대) 로드/변경 시 re-render.
  const [, bump] = useReducer((x: number) => x + 1, 0);
  useEffect(() => {
    ensureLoaded().catch(() => {});
    const h = () => bump();
    window.addEventListener('bluei:feeding-policy-changed', h);
    return () => window.removeEventListener('bluei:feeding-policy-changed', h);
  }, []);

  // 점등시간대는 그룹(양식장) 단위. 입식된 수조의 그룹 기준.
  const groupId =
    tanks.find(t => (t.fish_count ?? 0) > 0 && (t.avg_weight_g ?? 0) > 0)?.group_id
    ?? tanks[0]?.group_id ?? '';
  const gp = getGroupPolicy(groupId);
  const startMin = gp.photoperiod_start_min;
  const endMin = gp.photoperiod_end_min;
  const windowInvalid = endMin <= startMin;

  const setPhoto = (field: 'photoperiod_start_min' | 'photoperiod_end_min', hhmm: string) => {
    const min = hhmmToMin(hhmm);
    if (min == null || !groupId) return;
    setGroupPolicy(groupId, { [field]: min });
  };
  // 급이기 마이크로펄스 run/stop/리드(초) — 저장 즉시 반영, 다음 시작 사이클부터 이 값으로 구동.
  const setPulseSec = (field: 'feeder_run_sec' | 'feeder_stop_sec' | 'feeder_spinup_sec', v: string) => {
    const n = Number(v);
    if (!Number.isFinite(n) || n <= 0 || !groupId) return;
    setGroupPolicy(groupId, { [field]: n });
  };

  // backend AIScheduler 와 동일 입력 — 자동(auto) 수조만 교차배치 계산. 수동(정지) 수조를
  // 포함하면 배치 시각이 실제 발화와 어긋난다 (2026-07-05: tank_01 정지 중 두 화면 불일치).
  const stocked = tanks.filter(t => (t.avg_weight_g ?? 0) > 0 && (t.fish_count ?? 0) > 0);
  const isAutoTank = (t: Tank) =>
    getEffectiveTankPolicy(t.tank_id, t.group_id ?? '').operating_mode === 'auto';
  const pausedTanks = stocked.filter(t => !isAutoTank(t));

  const withPlan = stocked
    .filter(isAutoTank)
    .map(t => {
      const eff = getEffectiveTankPolicy(t.tank_id, t.group_id ?? '');
      return {
        tank_id: t.tank_id,
        display_name: t.display_name ?? t.tank_id,
        computation: computeFeedingPolicy({
          species: t.species,
          fish_count: t.fish_count ?? null,
          avg_weight_g: feedingWeightG(t),
          volume_m3: t.volume_m3 ?? null,
          bsf_mode: eff.bsf_mode,
          temperature_c: temps[t.tank_id] ?? null,
          max_daily_cycles: eff.max_daily_cycles,
          daily_cycles: eff.daily_cycles,
        }),
      };
    });

  const localSchedule = generateCrossTankSchedule(withPlan, {
    startHour: startMin / 60,
    endHour: endMin / 60,
  });
  // 백엔드 스케줄이 있으면 시각·횟수·회당량을 그걸로 대체한다. 로컬 계산은 폴백.
  const schedule = localSchedule.map(row => {
    const bs = backendSched[row.tank_id];
    const times = bs?.times ?? [];
    const target = bs?.pattern?.target_amount_g;
    if (!bs || times.length === 0 || target == null) return { ...row, fromBackend: false };
    // 간격은 백엔드 시각으로 다시 잰다 — GET80 경고가 옛 로컬 배치를 보면 안 된다.
    const mins = times.map(t => Number(t.slice(0, 2)) * 60 + Number(t.slice(3, 5))).sort((a, b) => a - b);
    const interval = mins.length >= 2 ? (mins[1] - mins[0]) / 60 : null;
    return {
      ...row,
      times,
      daily_cycles: times.length,
      cycle_target_amount_g: Math.round(target),
      scheduled_interval_h: interval,
      refeed_below_get80: interval != null && row.get80_h != null && interval < row.get80_h,
      fromBackend: true,
    };
  });
  if (schedule.length === 0 && pausedTanks.length === 0) return null;
  const nameOf = new Map(withPlan.map(p => [p.tank_id, p.display_name]));

  // 펄스 간격 수온 보정 노출 (2026-10-05, 운영자 요청).
  //
  // 아래 "정지" 입력란은 운영자 설정값이고, 백엔드는 저수온에서 coldGapMs 로 간격을
  // 늘려 구동한다(기준 13.5℃, Q10 3.0, 상한 3분). 9.4℃ 에서 77초 → 120.7초.
  // 화면엔 설정값만 있어서 실제 구동 간격을 알 길이 없었다 — 회당량 괴리(10-03)와 같은 형태.
  //
  // 여기서도 프론트가 보정을 재계산하지 않는다. 백엔드가 스케줄에 실어 보낸 gap_ms 만 읽는다.
  // 차이가 없으면(평수온) 아무것도 보여주지 않는다.
  const gapNotice = (() => {
    const setMs = (gp.feeder_stop_sec ?? 0) * 1000;
    if (setMs <= 0) return null;
    let worst: { ms: number; tankId: string } | null = null;
    for (const [tid, sc] of Object.entries(backendSched)) {
      const ms = sc.pattern?.gap_ms ?? 0;
      if (ms <= 0) continue;
      if (!worst || ms > worst.ms) worst = { ms, tankId: tid };
    }
    if (!worst || Math.abs(worst.ms - setMs) < 1000) return null;
    const t = temps[worst.tankId];
    return {
      setSec: String(Math.round(setMs / 1000)),
      actualSec: (Math.round(worst.ms / 100) / 10).toFixed(1),
      mult: (worst.ms / setMs).toFixed(2),
      tempC: t != null ? t.toFixed(1) : null,
    };
  })();

  // 일 사이클 축소 노출 (2026-10-05, 운영자 요청) — 펄스 간격과 같은 "설정 → 실제" 모양.
  //
  // 9℃ 미만에서 백엔드가 coldDailyCycles 로 하루 횟수를 줄인다(소화 30시간 초과 시
  // 잘게 쪼개면 회당 양이 물고기가 인지 못할 크기가 됨). 그러면 사이클 간격이 함께 늘어난다.
  //
  // 설정이 0(자동)이면 비교 기준이 없다 — 프론트 자동 산출은 저수온 보정을 모르므로
  // 그 값과 비교하면 보정과 무관한 괴리까지 섞여 거짓 경고가 된다. 그 경우 표시하지 않는다.
  const cycleNotice = (() => {
    const setCycles = gp.daily_cycles ?? 0;
    if (setCycles <= 0) return null;
    let fewest: { n: number; tankId: string } | null = null;
    for (const [tid, sc] of Object.entries(backendSched)) {
      const n = sc.times?.length ?? 0;
      if (n <= 0) continue;
      if (!fewest || n < fewest.n) fewest = { n, tankId: tid };
    }
    if (!fewest || fewest.n >= setCycles) return null;
    // 간격도 백엔드가 등록한 시각에서 잰 값(schedule 행)을 그대로 쓴다.
    const row = schedule.find(r => r.tank_id === fewest!.tankId);
    const ih = row?.scheduled_interval_h;
    return {
      setCycles: String(setCycles),
      actualCycles: String(fewest.n),
      intervalH: ih != null ? ih.toFixed(1) : null,
    };
  })();

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{tr('feedingSchedule.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        {gapNotice && (
          <p
            className="mb-2 text-xs text-amber-300/90"
            title={tr('feedingSchedule.gapAdjustedHint')}
          >
            {gapNotice.tempC != null
              ? tr('feedingSchedule.gapAdjusted', {
                  set: gapNotice.setSec, actual: gapNotice.actualSec,
                  mult: gapNotice.mult, temp: gapNotice.tempC,
                })
              : tr('feedingSchedule.gapAdjustedNoTemp', {
                  set: gapNotice.setSec, actual: gapNotice.actualSec, mult: gapNotice.mult,
                })}
          </p>
        )}
        {cycleNotice && (
          <p
            className="mb-2 text-xs text-amber-300/90"
            title={tr('feedingSchedule.cyclesAdjustedHint')}
          >
            {cycleNotice.intervalH != null
              ? tr('feedingSchedule.cyclesAdjusted', {
                  set: cycleNotice.setCycles, actual: cycleNotice.actualCycles,
                  interval: cycleNotice.intervalH,
                })
              : tr('feedingSchedule.cyclesAdjustedNoInterval', {
                  set: cycleNotice.setCycles, actual: cycleNotice.actualCycles,
                })}
          </p>
        )}
        <p className="text-xs text-gray-500 mb-2">{tr('feedingSchedule.subtitle')}</p>

        {/* 점등시간대 입력 — 소등 후엔 영상이 없어 급이 불가. 이 창 안에서만 스케줄 배치. */}
        <div className="flex flex-wrap items-center gap-2 mb-3 text-xs">
          <span className="text-gray-400">{tr('feedingSchedule.photoperiodLabel')}</span>
          <label className="flex items-center gap-1 text-gray-500">
            {tr('feedingSchedule.photoStart')}
            <input
              type="time"
              value={minToHHMM(startMin)}
              onChange={e => setPhoto('photoperiod_start_min', e.target.value)}
              className="bg-gray-900/60 border border-gray-700/60 rounded px-1.5 py-0.5 text-gray-200 font-mono focus:border-green-500/60 outline-none"
            />
          </label>
          <span className="text-gray-600">~</span>
          <label className="flex items-center gap-1 text-gray-500">
            {tr('feedingSchedule.photoEnd')}
            <input
              type="time"
              value={minToHHMM(endMin)}
              onChange={e => setPhoto('photoperiod_end_min', e.target.value)}
              className="bg-gray-900/60 border border-gray-700/60 rounded px-1.5 py-0.5 text-gray-200 font-mono focus:border-green-500/60 outline-none"
            />
          </label>
          {windowInvalid && (
            <span className="text-amber-400">{tr('feedingSchedule.photoInvalid')}</span>
          )}
          <span className="text-gray-700">|</span>
          <span className="text-gray-400">{tr('feedingSchedule.feederPulseLabel')}</span>
          <label className="flex items-center gap-1 text-gray-500">
            {tr('feedingSchedule.feederRun')}
            <input
              type="number" min={1} step={1}
              value={gp.feeder_run_sec}
              onChange={e => setPulseSec('feeder_run_sec', e.target.value)}
              className="w-12 bg-gray-900/60 border border-gray-700/60 rounded px-1.5 py-0.5 text-gray-200 font-mono focus:border-green-500/60 outline-none"
            />s
          </label>
          <label className="flex items-center gap-1 text-gray-500">
            {tr('feedingSchedule.feederStop')}
            <input
              type="number" min={1} step={1}
              value={gp.feeder_stop_sec}
              onChange={e => setPulseSec('feeder_stop_sec', e.target.value)}
              className="w-12 bg-gray-900/60 border border-gray-700/60 rounded px-1.5 py-0.5 text-gray-200 font-mono focus:border-green-500/60 outline-none"
            />s
          </label>
          {/* 공급모터 리드 — 살포모터 기동 후 공급 체결까지 대기(실측 +11~13s). ON = 리드 + 가동. */}
          <label className="flex items-center gap-1 text-gray-500" title={tr('feedingSchedule.feederSpinupHint')}>
            {tr('feedingSchedule.feederSpinup')}
            <input
              type="number" min={1} step={1}
              value={gp.feeder_spinup_sec}
              onChange={e => setPulseSec('feeder_spinup_sec', e.target.value)}
              className="w-12 bg-gray-900/60 border border-gray-700/60 rounded px-1.5 py-0.5 text-gray-200 font-mono focus:border-green-500/60 outline-none"
            />s
          </label>
          <span className="text-gray-700">|</span>
          {/* 일 사이클 수 직접 지정 — 0 = 자동(1일량÷1회최대량). >0 이면 자동 산출 대체 (2026-07-07). */}
          <label className="flex items-center gap-1 text-gray-500" title={tr('feedingSchedule.dailyCyclesHint')}>
            {tr('feedingSchedule.dailyCycles')}
            <input
              type="number" min={0} max={12} step={1}
              value={gp.daily_cycles}
              onChange={e => {
                const n = Number(e.target.value);
                if (Number.isFinite(n) && n >= 0 && groupId) setGroupPolicy(groupId, { daily_cycles: Math.min(12, Math.round(n)) });
              }}
              className="w-12 bg-gray-900/60 border border-gray-700/60 rounded px-1.5 py-0.5 text-gray-200 font-mono focus:border-green-500/60 outline-none"
            />
            <span className="text-gray-600">{tr('feedingSchedule.dailyCyclesAuto')}</span>
          </label>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-gray-500 border-b border-gray-700/50">
                <th className="text-left font-medium py-1 pr-3">{tr('feedingSchedule.colTank')}</th>
                <th className="text-left font-medium py-1 pr-3">{tr('feedingSchedule.colTimes')}</th>
                <th className="text-right font-medium py-1 pr-3">{tr('feedingSchedule.colCycles')}</th>
                <th className="text-right font-medium py-1 pr-3">{tr('feedingSchedule.colAmount')}</th>
                <th className="text-right font-medium py-1">{tr('feedingSchedule.colRefeed')}</th>
              </tr>
            </thead>
            <tbody>
              {schedule.map(s => (
                <tr key={s.tank_id} className="border-b border-gray-800/40">
                  <td className="py-1.5 pr-3 text-gray-300">{nameOf.get(s.tank_id) ?? s.tank_id}</td>
                  <td className="py-1.5 pr-3 font-mono text-green-300">{s.times.join(' · ')}</td>
                  <td className="py-1.5 pr-3 text-right text-gray-400">{tr('feedingSchedule.cyclesUnit', { n: s.daily_cycles })}</td>
                  <td className="py-1.5 pr-3 text-right font-mono text-gray-300">
                    {s.cycle_target_amount_g ?? '—'}g
                    {!s.fromBackend && (
                      <span className="ml-1 font-sans text-[10px] text-gray-500">
                        {tr('feedingSchedule.estimateTag')}
                      </span>
                    )}
                  </td>
                  <td
                    className={`py-1.5 text-right font-mono ${s.refeed_below_get80 ? 'text-amber-400' : 'text-gray-500'}`}
                    title={
                      s.refeed_below_get80 && s.scheduled_interval_h != null && s.get80_h != null
                        ? tr('feedingSchedule.refeedWarn', {
                            interval: s.scheduled_interval_h.toFixed(1),
                            get80: s.get80_h.toFixed(1),
                          })
                        : undefined
                    }
                  >
                    {s.scheduled_interval_h != null ? `${s.scheduled_interval_h.toFixed(1)}h${s.refeed_below_get80 ? ' ⚠' : ''}` : '—'}
                  </td>
                </tr>
              ))}
              {/* 정지(수동) 수조 — 교차배치 제외됨을 명시 (숨기면 "스케줄 누락"으로 오해). */}
              {pausedTanks.map(t => (
                <tr key={t.tank_id} className="border-b border-gray-800/40 opacity-70">
                  <td className="py-1.5 pr-3 text-gray-400">{t.display_name ?? t.tank_id}</td>
                  <td className="py-1.5 pr-3 text-amber-400/90">⏸ {tr('feedingSchedule.paused')}</td>
                  <td className="py-1.5 pr-3 text-right text-gray-600">—</td>
                  <td className="py-1.5 pr-3 text-right text-gray-600">—</td>
                  <td className="py-1.5 text-right text-gray-600">—</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="text-[11px] text-gray-600 italic mt-2">{tr('feedingSchedule.note')}</p>
      </CardContent>
    </Card>
  );
}
