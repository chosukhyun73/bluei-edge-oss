import { memo, useEffect, useState, useCallback } from 'react';
import { dateLocale } from '../lib/format';
import type { Tank } from '../lib/types';
import { ExternalEnv, WaterQuality, type WQBucket } from '../lib/api';
import { CanvasTimeChart, type CanvasPoint } from './CanvasTimeChart';
import { useLanguage } from '../lib/language-core';

// 수온/DO/pH 를 세로 스택, 각 차트는 선택 기간(1일/1h) 버킷 추이(수조별 라인).
// x축 = 시간(우측=현재), y축 = 값 + 어종 적정범위 밴드·한계선.
const TANK_COLORS = ['#22c55e', '#3b82f6', '#a855f7', '#f59e0b', '#ef4444', '#06b6d4', '#ec4899', '#84cc16'];
const POLL_MS = 60_000;

type RangeKey = '24h' | '1h';
// label 은 i18n 키 — 렌더 시 tr().
const RANGES: { key: RangeKey; label: string; ms: number }[] = [
  { key: '24h', label: 'dailySensor.range24h', ms: 24 * 3600 * 1000 },
  { key: '1h', label: 'dailySensor.range1h', ms: 3600 * 1000 },
];

type MetricKey = 'temperature_c_avg' | 'ph_avg' | 'do_mg_l_avg';
const METRICS: {
  key: MetricKey; rangeMetric: string; label: string; unit: string; fallback: { min: number; max: number };
  minSpan: number; // y축 최소 폭 — 데이터 중심 줌의 하한 (변화 가시성, 2026-07-11 운영자)
}[] = [
  // fallback = 대서양연어 RAS 일반 가이드(target_ranges 있으면 그걸 우선). label 은 i18n 키.
  { key: 'temperature_c_avg', rangeMetric: 'water_temperature', label: 'dailySensor.metricTemp', unit: '°C', fallback: { min: 8, max: 16 }, minSpan: 1.0 },
  { key: 'do_mg_l_avg', rangeMetric: 'dissolved_oxygen', label: 'dailySensor.metricDo', unit: 'mg/L', fallback: { min: 6, max: 14 }, minSpan: 1.0 },
  { key: 'ph_avg', rangeMetric: 'ph', label: 'dailySensor.metricPh', unit: '', fallback: { min: 6.5, max: 8.5 }, minSpan: 0.3 },
];

type SeriesPoint = { ts: number; [tankId: string]: number };

// 외부(수취구 권역) 연안 수온 — NIFS 1시간 관측 (2026-07-17). 수온 차트에만 오버레이.
export type ExtEnvPoint = { ts: number; value: number; station: string };
export const SEA_SERIES_KEY = '__sea_temp';
export const SEA_COLOR = '#94a3b8'; // 회청 — 수조 라인과 구분

// 별도 미니 차트 (자기 y축) — 수조 수온과 같은 축에 두면 수조 라인이 눌린다.
function SeaTempChart({ ext, sinceMs, nowMs, fmtTime }: {
  ext: ExtEnvPoint[]; sinceMs: number; nowMs: number; fmtTime: (ts: number) => string;
}) {
  const { tr } = useLanguage();
  const data = ext
    .filter(p => p.ts >= sinceMs)
    .map(p => ({ ts: p.ts, [SEA_SERIES_KEY]: p.value })) as CanvasPoint[];
  const latest = ext[ext.length - 1];
  return (
    <div className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3">
      <div className="flex items-center justify-between mb-2">
        <div className="text-sm text-slate-300 font-medium">{tr('dailySensor.seaTempTitle', { station: latest?.station ?? '' })}</div>
        <div className="text-xs font-mono text-slate-400">
          {tr('dailySensor.latest')} {latest ? `${latest.value.toFixed(1)}°C · ${fmtTime(latest.ts)}` : '—'}
        </div>
      </div>
      <CanvasTimeChart
        data={data}
        series={[{ key: SEA_SERIES_KEY, name: tr('dailySensor.seaSeriesName'), color: SEA_COLOR }]}
        xDomain={[sinceMs, nowMs]}
        unit="°C"
        height={110}
        minSpan={1.0}
        fmtTime={fmtTime}
      />
    </div>
  );
}

function rangeFor(tanks: Tank[], metric: string, fallback: { min: number; max: number }) {
  for (const t of tanks) {
    const r = t.target_ranges?.find(x => x.metric === metric);
    if (r && (r.min != null || r.max != null)) {
      return { min: r.min ?? fallback.min, max: r.max ?? fallback.max };
    }
  }
  // 앱에서 입식 어종 기준을 받은 수조(metadata.species_code)인데 이 항목 기준이 없으면 띠를 그리지
  // 않는다 — 연어 RAS 기준을 돌돔·광어 가두리에 그리면 틀린 판단을 준다 (2026-11-17 운영자).
  if (tanks.some(t => t.metadata?.species_code)) return null;
  return fallback;
}

// memo — 부모(GroupOverview) 5s state-vector 폴링 재렌더에서 격리. tanks 는 부모가 매
// 렌더 새 배열을 만들므로 tank_id 목록으로 비교(차트는 자체 60s 폴링으로 데이터 갱신).
// WebKitGTK 에서 recharts 재렌더마다 메모리 잔류(+100MB/분대 실측, 2026-07-06 bisect)
// — 렌더러가 수 시간 내 16GB 사망하던 근본 원인.
export const DailySensorChart = memo(function DailySensorChart({ tanks }: { tanks: Tank[] }) {
  const { tr } = useLanguage();
  const [bucketsByTank, setBucketsByTank] = useState<Map<string, WQBucket[]>>(new Map());
  const [extEnv, setExtEnv] = useState<ExtEnvPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState<RangeKey>('24h');
  const [nowMs, setNowMs] = useState(() => Date.now());
  const tankIdsKey = tanks.map(t => t.tank_id).join(',');
  const windowMs = RANGES.find(r => r.key === range)!.ms;

  const load = useCallback(async () => {
    const now = Date.now();
    // 항상 24h 받아두고 표시만 윈도우로 자름(토글 시 재요청 불필요).
    const since = new Date(now - 24 * 3600 * 1000).toISOString();
    const m = new Map<string, WQBucket[]>();
    await Promise.all(
      tanks.map(async t => {
        try {
          const res = await WaterQuality.buckets(t.tank_id, since);
          m.set(t.tank_id, res.items ?? []);
        } catch {
          m.set(t.tank_id, []);
        }
      }),
    );
    // 외부 연안 수온 (있으면 수온 차트 오버레이 — 없어도 무해)
    try {
      const ext = await ExternalEnv.list(24);
      setExtEnv(
        (ext.items ?? [])
          .filter(p => p.metric === 'sea_water_temperature')
          .map(p => ({ ts: new Date(p.observed_at).getTime(), value: p.value, station: p.station_name })),
      );
    } catch { setExtEnv([]); }
    setBucketsByTank(m);
    setNowMs(now);
    setLoading(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tankIdsKey]);

  useEffect(() => {
    setLoading(true);
    void load();
    const id = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(id);
  }, [load]);

  const sinceMs = nowMs - windowMs;

  const seriesFor = (metricKey: MetricKey): SeriesPoint[] => {
    const byTs = new Map<number, SeriesPoint>();
    for (const t of tanks) {
      for (const b of bucketsByTank.get(t.tank_id) ?? []) {
        const val = b[metricKey];
        if (typeof val !== 'number') continue;
        const ts = new Date(b.bucket_start).getTime();
        if (ts < sinceMs) continue; // 선택 윈도우만
        let row = byTs.get(ts);
        if (!row) {
          row = { ts } as SeriesPoint;
          byTs.set(ts, row);
        }
        row[t.tank_id] = val;
      }
    }
    return [...byTs.values()].sort((a, b) => a.ts - b.ts);
  };

  const hasData = [...bucketsByTank.values()].some(arr => arr.length > 0);
  const fmtTime = (ts: number) =>
    new Date(ts).toLocaleTimeString(dateLocale(), { hour: '2-digit', minute: '2-digit' });

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        {RANGES.map(r => (
          <button
            key={r.key}
            onClick={() => setRange(r.key)}
            className={`px-3 py-1 text-xs rounded font-medium transition-colors ${
              range === r.key ? 'bg-green-600 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
            }`}
          >
            {tr(r.label)}
          </button>
        ))}
        <span className="text-xs text-gray-600 ml-auto">
          {tr('dailySensor.axisHint', { ago: range === '1h' ? tr('dailySensor.ago1h') : tr('dailySensor.ago24h') })}
        </span>
      </div>

      {loading ? (
        <div className="py-10 text-center text-sm text-gray-500 font-mono">{tr('dailySensor.loading')}</div>
      ) : !hasData ? (
        <div className="py-10 text-center text-sm text-gray-500 font-mono">
          {tr('dailySensor.noData')}
        </div>
      ) : (
        METRICS.map(m => (
          <div key={m.key} className="space-y-3">
            <DailyMetricChart
              label={tr(m.label)}
              unit={m.unit}
              tanks={tanks}
              data={seriesFor(m.key)}
              sinceMs={sinceMs}
              nowMs={nowMs}
              fmtTime={fmtTime}
              range={rangeFor(tanks, m.rangeMetric, m.fallback)}
              minSpan={m.minSpan}
            />
            {/* 연안 수온은 절대값 대역이 달라(예: 22°C vs 10.8°C) 같은 축에 넣으면
                수조 라인이 눌린다 (2026-07-17 운영자) — 별도 미니 차트로 분리. */}
            {m.key === 'temperature_c_avg' && extEnv.length > 0 && (
              <SeaTempChart ext={extEnv} sinceMs={sinceMs} nowMs={nowMs} fmtTime={fmtTime} />
            )}
          </div>
        ))
      )}
    </div>
  );
}, (prev, next) =>
  prev.tanks.map(t => t.tank_id).join(',') === next.tanks.map(t => t.tank_id).join(','));

function DailyMetricChart({
  label, unit, tanks, data, sinceMs, nowMs, fmtTime, range, minSpan,
}: {
  label: string; unit: string; tanks: Tank[]; data: SeriesPoint[];
  sinceMs: number; nowMs: number; fmtTime: (ts: number) => string;
  range: { min: number; max: number } | null;
  minSpan: number;
}) {
  const { tr } = useLanguage();
  const latest = data[data.length - 1];
  let avg: number | null = null;
  if (latest) {
    const vals = tanks.map(t => latest[t.tank_id]).filter((v): v is number => typeof v === 'number');
    if (vals.length) avg = vals.reduce((a, b) => a + b, 0) / vals.length;
  }
  const inRange = avg != null && range != null && avg >= range.min && avg <= range.max;

  return (
    <div className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3">
      <div className="flex items-center justify-between mb-2">
        <div className="text-sm text-gray-300 font-medium">{label}</div>
        <div className="text-xs font-mono flex items-center gap-2">
          {range && <span className="text-gray-500">{tr('dailySensor.targetRange', { min: range.min, max: range.max })}</span>}
          {avg != null && !range && <span className="text-gray-300">{avg.toFixed(2)}</span>}
          {avg != null && range && (
            <span className={inRange ? 'text-green-400' : 'text-red-400'}>
              {inRange
                ? tr('dailySensor.currentOk', { v: avg.toFixed(2) })
                : tr('dailySensor.currentOut', { v: avg.toFixed(2) })}
            </span>
          )}
        </div>
      </div>
      {/* SVG(recharts) 금지 — WebKitGTK repaint 누수 (CanvasTimeChart 헤더 참고) */}
      <CanvasTimeChart
        data={data as CanvasPoint[]}
        series={tanks.map((t, i) => ({
          key: t.tank_id,
          name: t.display_name,
          color: TANK_COLORS[i % TANK_COLORS.length],
        }))}
        xDomain={[sinceMs, nowMs]}
        unit={unit}
        height={190}
        refBand={range}
        legend
        fmtTime={fmtTime}
        minSpan={minSpan}
      />
    </div>
  );
}
