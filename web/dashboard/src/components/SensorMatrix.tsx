import { useState, useEffect, useRef, useMemo } from 'react';
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
  ReferenceArea,
} from 'recharts';
import type { StateVector, Tank, TargetRange } from '../lib/types';
import { relativeTime } from '../lib/format';
import { useLanguage } from '../lib/language-context';

// 핵심 수질 6개 — 운영자가 동시에 비교 보고 싶어하는 metric.
// (carbon_dioxide/nitrate 등은 expanded 표 fallback 으로.)
// label 은 tr 키 — 렌더 시점에 useLanguage().tr 로 해소. ('pH' 등 보편 토큰은 그대로.)
const PRIMARY_METRICS: { key: string; label: string; unit: string }[] = [
  { key: 'water_temperature',     label: 'sensorMatrix.metric.water_temperature', unit: '°C' },
  { key: 'dissolved_oxygen',      label: 'sensorMatrix.metric.dissolved_oxygen',  unit: 'mg/L' },
  { key: 'ph',                    label: 'pH',                                    unit: '' },
  { key: 'salinity',              label: 'sensorMatrix.metric.salinity',          unit: 'PSU' },
  { key: 'unionized_ammonia',     label: 'sensorMatrix.metric.unionized_ammonia', unit: 'mg/L' },
  { key: 'flow_rate',             label: 'sensorMatrix.metric.flow_rate',         unit: 'L/min' },
];

// Tank line 색상 팔레트 — recharts 권장 채도/명도 (다크 배경에서 명확).
const TANK_COLORS = [
  '#22c55e', '#3b82f6', '#a855f7', '#f59e0b',
  '#ef4444', '#06b6d4', '#ec4899', '#84cc16',
];

// 값은 tr 키 — 'pH'/'CO₂'/'TSS' 보편 토큰만 리터럴 유지.
const METRIC_LABELS: Record<string, string> = {
  water_temperature: 'sensorMatrix.metric.water_temperature',
  dissolved_oxygen: 'sensorMatrix.metric.dissolved_oxygen',
  ph: 'pH',
  salinity: 'sensorMatrix.metric.salinity',
  nitrate: 'sensorMatrix.metric.nitrate',
  nitrite: 'sensorMatrix.metric.nitrite',
  unionized_ammonia: 'sensorMatrix.metric.unionized_ammonia',
  carbon_dioxide: 'CO₂',
  total_suspended_solids: 'TSS',
  flow_rate: 'sensorMatrix.metric.flow_rate',
  pump_pressure: 'sensorMatrix.metric.pump_pressure',
  light_intensity: 'sensorMatrix.metric.light_intensity',
  feed_weight: 'sensorMatrix.metric.feed_weight',
};

// 라벨 값이 tr 키면 해소, 보편 토큰('pH' 등)이면 그대로.
function resolveMetricLabel(tr: (k: string) => string, value: string): string {
  return value.startsWith('sensorMatrix.metric.') ? tr(value) : value;
}

interface Props {
  tanks: Tank[];
  // GroupOverview 에서 호이스팅된 stateVectors 맵 — 5초 폴링 결과.
  stateVectors: Map<string, StateVector>;
  lastFetchedAt: Date | null;
  loading: boolean;
  // 버퍼 길이 — 기본 30분.
  bufferMinutes?: number;
}

// 각 tank × metric 시계열 1개 데이터 포인트.
type SeriesPoint = {
  ts: number;
  time: string;
  // [tank_id]: value (Recharts multi-line 은 평탄화된 row 가 필요)
  [tankSeriesKey: string]: number | string;
};

function hhmm(d: Date): string {
  return d.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit' });
}

// 단일 tank 의 target_ranges 에서 metric range 추출 (시각 보조용).
function getRange(ranges: TargetRange[] | undefined, metric: string): TargetRange | undefined {
  return ranges?.find(r => r.metric === metric);
}

export function SensorMatrix({
  tanks,
  stateVectors,
  lastFetchedAt,
  loading,
  bufferMinutes = 30,
}: Props) {
  const { tr } = useLanguage();
  // buffer: metric_key → SeriesPoint[].
  // tank_id 별로 컬럼이 추가/제거되더라도 같은 buffer 에 누적.
  const bufferRef = useRef<Map<string, SeriesPoint[]>>(new Map());
  const [bufferTick, setBufferTick] = useState(0);

  // tanks 변경 시 buffer 리셋 (그룹 전환 등).
  const tankIds = useMemo(() => tanks.map(t => t.tank_id), [tanks]);
  const tankIdsKey = tankIds.join('|');
  useEffect(() => {
    bufferRef.current = new Map();
    setBufferTick(t => t + 1);
  }, [tankIdsKey]);

  // stateVectors 갱신 시 buffer 에 신규 포인트 append.
  useEffect(() => {
    if (!lastFetchedAt) return;
    if (stateVectors.size === 0) return;

    const ts = lastFetchedAt.getTime();
    const cutoff = ts - bufferMinutes * 60_000;
    const label = hhmm(lastFetchedAt);

    for (const m of PRIMARY_METRICS) {
      const prev = bufferRef.current.get(m.key) ?? [];
      const row: SeriesPoint = { ts, time: label };
      let hasAny = false;
      for (const tank of tanks) {
        const sv = stateVectors.get(tank.tank_id);
        const metric = sv?.water?.metrics?.[m.key];
        if (metric && typeof metric.value === 'number') {
          row[tank.tank_id] = metric.value;
          hasAny = true;
        }
      }
      if (!hasAny) continue;
      const next = [...prev.filter(p => p.ts >= cutoff), row];
      bufferRef.current.set(m.key, next);
    }
    setBufferTick(t => t + 1);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastFetchedAt?.getTime(), stateVectors, tankIdsKey]);

  if (tanks.length === 0) {
    return (
      <div className="py-6 text-center text-sm text-gray-500 italic">{tr('sensorMatrix.noTanks')}</div>
    );
  }

  const hasAnyData = [...bufferRef.current.values()].some(arr => arr.length > 0);

  return (
    <div className="space-y-3">
      {!hasAnyData ? (
        <div className="py-10 text-center text-sm text-gray-500 font-mono">
          {loading ? tr('sensorMatrix.collecting') : tr('sensorMatrix.noRecentData')}
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {PRIMARY_METRICS.map(m => (
            <MetricChart
              key={m.key}
              metricKey={m.key}
              label={m.label}
              unit={m.unit}
              tanks={tanks}
              data={bufferRef.current.get(m.key) ?? []}
            />
          ))}
        </div>
      )}

      {/* 마지막 갱신 + 버퍼 상태 */}
      <div className="flex items-center gap-3 text-xs text-gray-600">
        {loading && (
          <span className="inline-block w-2 h-2 rounded-full bg-green-500 animate-ping" />
        )}
        <span>
          {tr('sensorMatrix.lastUpdated')} {lastFetchedAt ? relativeTime(lastFetchedAt.toISOString()) : '—'}
        </span>
        <span className="opacity-60">{tr('sensorMatrix.bufferPrefix')} {bufferMinutes}{tr('sensorMatrix.bufferMinSuffix')} {bufferTick > 0 ? `${bufferRef.current.get(PRIMARY_METRICS[0].key)?.length ?? 0}${tr('sensorMatrix.sampleSuffix')}` : tr('sensorMatrix.waiting')}</span>
      </div>
    </div>
  );
}

interface ChartProps {
  metricKey: string;
  label: string;
  unit: string;
  tanks: Tank[];
  data: SeriesPoint[];
}

function MetricChart({ metricKey, label, unit, tanks, data }: ChartProps) {
  const { tr } = useLanguage();
  // 첫 tank 의 target_range 를 시각 가이드로 사용 (보통 같은 그룹 = 같은 species/target).
  const guideRange = useMemo(() => {
    for (const t of tanks) {
      const r = getRange(t.target_ranges, metricKey);
      if (r) return r;
    }
    return undefined;
  }, [tanks, metricKey]);

  // 최신 값 (모든 tank 평균, 표시용).
  const latest = data[data.length - 1];
  const latestSummary = useMemo(() => {
    if (!latest) return null;
    const vals = tanks
      .map(t => latest[t.tank_id])
      .filter((v): v is number => typeof v === 'number');
    if (vals.length === 0) return null;
    const avg = vals.reduce((a, b) => a + b, 0) / vals.length;
    return { avg, count: vals.length };
  }, [latest, tanks]);

  return (
    <div className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3 min-w-0">
      <div className="flex items-center justify-between mb-2">
        <div className="text-xs text-gray-300 font-medium">
          {resolveMetricLabel(tr, METRIC_LABELS[metricKey] ?? label)}
          {unit && <span className="text-gray-600 ml-1">({unit})</span>}
        </div>
        {latestSummary && (
          <span className="text-xs font-mono text-green-400">
            {tr('sensorMatrix.avg')} {latestSummary.avg.toFixed(2)}
          </span>
        )}
      </div>

      {/* C-10 — width/height 둘 다 명시. 부모 grid cell 이 first-paint 시 0px 일 수 있어
          height="100%" 를 numeric 으로 고정 + min-w-0 로 grid shrink 허용해도 안전. */}
      <div className="h-[140px] min-h-[140px] w-full min-w-0">
        {data.length === 0 ? (
          <div className="h-full flex items-center justify-center text-gray-600 text-xs font-mono">
            {tr('sensorMatrix.chartCollecting')}
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={140} minWidth={120} minHeight={120} debounce={50}>
            <LineChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: -16 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
              <XAxis
                dataKey="time"
                stroke="#4b5563"
                fontSize={9}
                interval="preserveStartEnd"
                minTickGap={40}
              />
              <YAxis
                stroke="#4b5563"
                fontSize={9}
                width={32}
                domain={['auto', 'auto']}
              />
              <Tooltip
                contentStyle={{
                  background: '#0a0a0a',
                  border: '1px solid rgba(34,197,94,0.27)',
                  borderRadius: 6,
                  fontSize: 11,
                }}
                labelStyle={{ color: '#9ca3af' }}
              />
              {/* 안전 범위 표시 (있을 때만) — target_range min~max 영역 음영. */}
              {guideRange && guideRange.min !== undefined && guideRange.max !== undefined && (
                <ReferenceArea
                  y1={guideRange.min}
                  y2={guideRange.max}
                  fill="#22c55e"
                  fillOpacity={0.05}
                  ifOverflow="extendDomain"
                />
              )}
              {tanks.map((tank, idx) => (
                <Line
                  key={tank.tank_id}
                  type="monotone"
                  dataKey={tank.tank_id}
                  name={tank.display_name}
                  stroke={TANK_COLORS[idx % TANK_COLORS.length]}
                  strokeWidth={1.6}
                  dot={false}
                  isAnimationActive={false}
                  connectNulls
                />
              ))}
              {tanks.length > 1 && (
                <Legend
                  wrapperStyle={{ fontSize: 9, color: '#9ca3af', paddingTop: 0 }}
                  iconSize={8}
                  iconType="circle"
                />
              )}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}
