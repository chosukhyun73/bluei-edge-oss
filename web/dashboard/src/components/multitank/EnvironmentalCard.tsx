// EnvironmentalCard — C-3w (Environmental Safety) dashboard 표시.
// 해상(marine) Site 에만 표시. 통영 cage 의 풍속/파고/조수/수온 운영자 가시화.
//
// 임계값 (backend internal/environmental_safety/gate.go 와 동일):
//   wind_speed_ms > 12.0  → breach (차단)
//   wave_height_m > 2.0   → breach
//   caution 영역은 frontend 추정 (backend 에는 caution 단계 없음):
//     wind 8~12 m/s, wave 1~2 m
//
// 10초 폴링. site_id 변경 시 reset.

import { useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../lib/language-context';
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts';
import type { EnvironmentalSnapshot, EnvironmentalGateStatus } from '../../lib/types';
import { Environmental } from '../../lib/api';
import { ApiError } from '../../lib/api';
import { Skeleton } from '../ui/skeleton';

// ── 임계값 (backend 와 일치) ─────────────────────────────────────────────────
const WIND_BREACH = 12.0;  // m/s — 초과 시 차단 (>)
const WIND_CAUTION = 8.0;  // m/s — 이상 ~ BREACH 미만 시 주의
const WAVE_BREACH = 2.0;   // m   — 초과 시 차단 (>)
const WAVE_CAUTION = 1.0;  // m

// gate status 계산 — backend gate.go 의 Check() 로직 재현.
// 단 tide_minutes_to_low 임계 (30분) 는 별도 표시하지 않음 (운영자 시연 단순화).
function computeGateStatus(snap: EnvironmentalSnapshot | null): EnvironmentalGateStatus {
  if (!snap) return 'ok';
  const wind = snap.wind_speed_ms ?? 0;
  const wave = snap.wave_height_m ?? 0;
  if (wind > WIND_BREACH || wave > WAVE_BREACH) return 'breach';
  if (wind >= WIND_CAUTION || wave >= WAVE_CAUTION) return 'caution';
  return 'ok';
}

// returns a tr key, or null when the phase is unknown (caller falls back to raw phase / —).
function tidePhaseLabelKey(phase?: string): string | null {
  switch (phase) {
    case 'flood': return 'environmentalCard.tidePhaseFlood';
    case 'ebb':   return 'environmentalCard.tidePhaseEbb';
    case 'slack': return 'environmentalCard.tidePhaseSlack';
    case 'high':  return 'environmentalCard.tidePhaseHigh';
    case 'low':   return 'environmentalCard.tidePhaseLow';
    default:      return null;
  }
}

function statusBadgeClass(s: EnvironmentalGateStatus): string {
  if (s === 'ok') return 'bg-green-500/20 text-green-300 border-green-500/40';
  if (s === 'caution') return 'bg-amber-500/20 text-amber-300 border-amber-500/40';
  return 'bg-red-500/20 text-red-300 border-red-500/40';
}

function statusLabelKey(s: EnvironmentalGateStatus): string {
  if (s === 'ok') return 'environmentalCard.statusOk';
  if (s === 'caution') return 'environmentalCard.statusCaution';
  return 'environmentalCard.statusBreach';
}

// 개별 metric 컬러 (값 기준).
function windColorClass(v?: number | null): string {
  if (v == null) return 'text-gray-400';
  if (v > WIND_BREACH) return 'text-red-400';
  if (v >= WIND_CAUTION) return 'text-amber-300';
  return 'text-green-300';
}
function waveColorClass(v?: number | null): string {
  if (v == null) return 'text-gray-400';
  if (v > WAVE_BREACH) return 'text-red-400';
  if (v >= WAVE_CAUTION) return 'text-amber-300';
  return 'text-green-300';
}

function fmtNum(v?: number | null, digits = 1): string {
  if (v == null || Number.isNaN(v)) return '—';
  return v.toFixed(digits);
}

function fmtTime(iso?: string): string {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleString('ko-KR', {
      month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
  } catch {
    return iso;
  }
}

// ── 수동 snapshot 입력 폼 (본선 시연용) ───────────────────────────────────────

interface InjectFormProps {
  siteId: string;
  onInjected: () => void;
}

function InjectForm({ siteId, onInjected }: InjectFormProps) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(false);
  const [wind, setWind] = useState<string>('5');
  const [wave, setWave] = useState<string>('0.5');
  const [tide, setTide] = useState<string>('flood');
  const [temp, setTemp] = useState<string>('18.5');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    const body = {
      site_id: siteId,
      wind_speed_ms: wind.trim() === '' ? undefined : Number(wind),
      wave_height_m: wave.trim() === '' ? undefined : Number(wave),
      tide_phase: tide.trim() === '' ? undefined : tide,
      temperature_c: temp.trim() === '' ? undefined : Number(temp),
    };
    Environmental.snapshot(body)
      .then(() => { onInjected(); })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : tr('environmentalCard.unknownError'));
      })
      .finally(() => setSubmitting(false));
  };

  // 빠른 시연 프리셋
  const preset = (w: number, h: number, t: string) => {
    setWind(String(w)); setWave(String(h)); setTide(t);
  };

  return (
    <div className="mt-3 border-t border-gray-800/60 pt-3">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        className="text-xs text-gray-500 hover:text-gray-300 font-mono"
      >
        {open ? '▾' : '▸'}{tr('environmentalCard.injectToggle')}
      </button>
      {open && (
        <form onSubmit={submit} className="mt-2 space-y-2">
          <div className="flex gap-2 flex-wrap">
            <button type="button" onClick={() => preset(5, 0.5, 'flood')}
              className="px-2 py-0.5 text-[10px] rounded border border-green-500/30 bg-green-500/5 text-green-300 hover:bg-green-500/10">
              {tr('environmentalCard.presetNormal')}
            </button>
            <button type="button" onClick={() => preset(10, 1.5, 'flood')}
              className="px-2 py-0.5 text-[10px] rounded border border-amber-500/30 bg-amber-500/5 text-amber-300 hover:bg-amber-500/10">
              {tr('environmentalCard.presetCaution')}
            </button>
            <button type="button" onClick={() => preset(16, 2.5, 'ebb')}
              className="px-2 py-0.5 text-[10px] rounded border border-red-500/30 bg-red-500/5 text-red-300 hover:bg-red-500/10">
              {tr('environmentalCard.presetStorm')}
            </button>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <label className="flex flex-col gap-0.5">
              <span className="text-[10px] text-gray-500">{tr('environmentalCard.labelWind')}</span>
              <input type="number" step="0.1" value={wind} onChange={e => setWind(e.target.value)}
                className="h-7 px-2 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200 font-mono" />
            </label>
            <label className="flex flex-col gap-0.5">
              <span className="text-[10px] text-gray-500">{tr('environmentalCard.labelWave')}</span>
              <input type="number" step="0.1" value={wave} onChange={e => setWave(e.target.value)}
                className="h-7 px-2 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200 font-mono" />
            </label>
            <label className="flex flex-col gap-0.5">
              <span className="text-[10px] text-gray-500">{tr('environmentalCard.labelTide')}</span>
              <select value={tide} onChange={e => setTide(e.target.value)}
                className="h-7 px-2 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200">
                <option value="flood">{tr('environmentalCard.tideFlood')}</option>
                <option value="ebb">{tr('environmentalCard.tideEbb')}</option>
                <option value="slack">{tr('environmentalCard.tideSlack')}</option>
                <option value="high">{tr('environmentalCard.tideHigh')}</option>
                <option value="low">{tr('environmentalCard.tideLow')}</option>
              </select>
            </label>
            <label className="flex flex-col gap-0.5">
              <span className="text-[10px] text-gray-500">{tr('environmentalCard.labelTemp')}</span>
              <input type="number" step="0.1" value={temp} onChange={e => setTemp(e.target.value)}
                className="h-7 px-2 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200 font-mono" />
            </label>
          </div>
          {error && (
            <p className="text-[10px] text-red-400 font-mono">{tr('environmentalCard.injectError')} {error}</p>
          )}
          <button type="submit" disabled={submitting}
            className="w-full h-7 text-xs rounded bg-blue-500/20 border border-blue-500/40 text-blue-200 hover:bg-blue-500/30 disabled:opacity-50">
            {submitting ? tr('environmentalCard.submitting') : tr('environmentalCard.submitBtn')}
          </button>
        </form>
      )}
    </div>
  );
}

// ── Mini history chart ────────────────────────────────────────────────────────

interface ChartPoint {
  ts: number;
  time: string;
  wind: number | null;
  wave: number | null;
}

function buildChartData(items: EnvironmentalSnapshot[]): ChartPoint[] {
  // history 응답은 DESC 정렬 — 차트는 ASC (왼쪽이 과거).
  return [...items].reverse().map(snap => {
    const d = new Date(snap.recorded_at);
    return {
      ts: d.getTime(),
      time: d.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit' }),
      wind: snap.wind_speed_ms ?? null,
      wave: snap.wave_height_m ?? null,
    };
  });
}

// ── 메인 카드 ─────────────────────────────────────────────────────────────────

interface Props {
  siteId: string;
}

export function EnvironmentalCard({ siteId }: Props) {
  const { tr } = useLanguage();
  const [current, setCurrent] = useState<EnvironmentalSnapshot | null>(null);
  const [history, setHistory] = useState<EnvironmentalSnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // 404 (NOT_FOUND) — site 자체는 유효하나 환경 snapshot 누적 없음. 빈 상태 표시.
  const [empty, setEmpty] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchAll = () => {
    Environmental.current(siteId)
      .then(d => { setCurrent(d); setEmpty(false); setError(null); })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 404) {
          setCurrent(null);
          setEmpty(true);
          setError(null);
        } else {
          setError(err instanceof Error ? err.message : tr('environmentalCard.unknownError'));
        }
      })
      .finally(() => setLoading(false));

    Environmental.history(siteId, 200)
      .then(r => setHistory(r.items))
      .catch(() => { /* non-fatal — current 표시로 충분 */ });
  };

  useEffect(() => {
    setLoading(true);
    setCurrent(null);
    setHistory([]);
    setError(null);
    setEmpty(false);

    fetchAll();
    intervalRef.current = setInterval(fetchAll, 10_000);
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
    // siteId 변경 시 재마운트.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [siteId]);

  const status = computeGateStatus(current);
  const chartData = buildChartData(history);

  return (
    <div className="p-3 bg-gray-800/60 border border-gray-700/50 rounded-lg mb-4">
      <div className="flex items-center justify-between mb-2">
        <p className="text-xs text-gray-500 uppercase tracking-wide">
          {tr('environmentalCard.sectionTitle')}
        </p>
        {current && (
          <span
            className={[
              'inline-flex items-center px-2 py-0.5 rounded border text-xs font-semibold',
              statusBadgeClass(status),
            ].join(' ')}
            aria-label={`${tr('environmentalCard.gateStatusAria')}: ${tr(statusLabelKey(status))}`}
          >
            {tr(statusLabelKey(status))}
          </span>
        )}
      </div>

      {loading && <Skeleton className="h-24 w-full" />}

      {!loading && error && (
        <div className="px-3 py-2 bg-destructive/10 border border-destructive/30 rounded text-xs text-destructive font-mono">
          {tr('environmentalCard.loadError')} {error}
        </div>
      )}

      {!loading && !error && empty && (
        <p className="text-xs text-gray-500 py-3 text-center">
          {tr('environmentalCard.emptyHint')}
        </p>
      )}

      {!loading && !error && current && (
        <>
          {/* 4-metric grid */}
          <div className="grid grid-cols-4 gap-2 mb-3">
            <div className="p-2 bg-gray-900/50 rounded border border-gray-700/40">
              <p className="text-[10px] text-gray-500 mb-0.5">{tr('environmentalCard.metricWind')}</p>
              <p className={['text-base font-mono font-semibold', windColorClass(current.wind_speed_ms)].join(' ')}>
                {fmtNum(current.wind_speed_ms, 1)} <span className="text-[10px] text-gray-500">m/s</span>
              </p>
            </div>
            <div className="p-2 bg-gray-900/50 rounded border border-gray-700/40">
              <p className="text-[10px] text-gray-500 mb-0.5">{tr('environmentalCard.metricWave')}</p>
              <p className={['text-base font-mono font-semibold', waveColorClass(current.wave_height_m)].join(' ')}>
                {fmtNum(current.wave_height_m, 2)} <span className="text-[10px] text-gray-500">m</span>
              </p>
            </div>
            <div className="p-2 bg-gray-900/50 rounded border border-gray-700/40">
              <p className="text-[10px] text-gray-500 mb-0.5">{tr('environmentalCard.metricTide')}</p>
              <p className="text-base font-semibold text-gray-200">
                {(() => {
                  const k = tidePhaseLabelKey(current.tide_phase);
                  return k ? tr(k) : (current.tide_phase ?? '—');
                })()}
              </p>
            </div>
            <div className="p-2 bg-gray-900/50 rounded border border-gray-700/40">
              <p className="text-[10px] text-gray-500 mb-0.5">{tr('environmentalCard.metricTemp')}</p>
              <p className="text-base font-mono font-semibold text-gray-200">
                {fmtNum(current.temperature_c, 1)} <span className="text-[10px] text-gray-500">°C</span>
              </p>
            </div>
          </div>

          {/* 임계값 hint + 마지막 갱신 */}
          <div className="flex justify-between text-[10px] text-gray-600 font-mono mb-3">
            <span>{tr('environmentalCard.thresholdHint', { wind: WIND_BREACH, wave: WAVE_BREACH })}</span>
            <span>{tr('environmentalCard.updatedAt', { time: fmtTime(current.recorded_at) })} · {current.source}</span>
          </div>

          {/* 차단 사유 — breach 일 때 명시 */}
          {status === 'breach' && (
            <div className="mb-3 px-3 py-1.5 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-300">
              {(current.wind_speed_ms ?? 0) > WIND_BREACH && (
                <p>{tr('environmentalCard.windBreach', { value: fmtNum(current.wind_speed_ms), threshold: WIND_BREACH })}</p>
              )}
              {(current.wave_height_m ?? 0) > WAVE_BREACH && (
                <p>{tr('environmentalCard.waveBreach', { value: fmtNum(current.wave_height_m, 2), threshold: WAVE_BREACH })}</p>
              )}
            </div>
          )}

          {/* History chart — recharts dual-line. C-10: min-w-0 + 낮은 minWidth 로 first-paint warning 회피. */}
          {chartData.length >= 2 ? (
            <div className="mt-2 min-h-[140px] w-full min-w-0">
              <p className="text-[10px] text-gray-500 mb-1">{tr('environmentalCard.recentTrend')} ({tr('environmentalCard.countRecords', { count: chartData.length })})</p>
              <ResponsiveContainer width="100%" height={140} minWidth={120} minHeight={120} debounce={50}>
                <LineChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
                  <XAxis dataKey="time" stroke="#6b7280" fontSize={9}
                    interval="preserveStartEnd" minTickGap={40} />
                  <YAxis yAxisId="wind" orientation="left" stroke="#22c55e" fontSize={9} width={28} />
                  <YAxis yAxisId="wave" orientation="right" stroke="#60a5fa" fontSize={9} width={28} />
                  <Tooltip
                    contentStyle={{ background: '#0a0a0a', border: '1px solid rgba(75,85,99,0.5)', borderRadius: 6, fontSize: 11 }}
                    labelStyle={{ color: '#9ca3af' }}
                  />
                  <Legend wrapperStyle={{ fontSize: 10 }} />
                  <Line yAxisId="wind" type="monotone" dataKey="wind" name={tr('environmentalCard.chartWindName')}
                    stroke="#22c55e" strokeWidth={1.5} dot={false} isAnimationActive={false} />
                  <Line yAxisId="wave" type="monotone" dataKey="wave" name={tr('environmentalCard.chartWaveName')}
                    stroke="#60a5fa" strokeWidth={1.5} dot={false} isAnimationActive={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          ) : (
            <p className="text-[10px] text-gray-600 font-mono mt-1">
              {tr('environmentalCard.insufficientTrend', { count: chartData.length })}
            </p>
          )}
        </>
      )}

      <InjectForm siteId={siteId} onInjected={fetchAll} />
    </div>
  );
}
