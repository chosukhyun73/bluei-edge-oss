// 해상 장기 환경 분석 (최대 2년, 2026-11-17).
// 가두리 수온·DO(관측소 값 = 그 가두리 값) + 바깥 환경(기상청 부이 바람·파고·기온, 관측소 일별 수온)을
// 일별 최저·평균·최고로 본다. 작년 같은 시기를 겹쳐 비교하고, 풍속·파고 기준을 넘은 날을 음영으로 표시.
// 부이는 수십 km 밖 바깥 바다라 참고값 — 안전 판정에는 쓰지 않는다.
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { SiteEnv, type SiteEnvDaily, type EnvDaily } from '../lib/api';
import { dateLocale } from '../lib/format';
import { CanvasTimeChart, type CanvasPoint } from './CanvasTimeChart';
import { useLanguage } from '../lib/language-core';

const RANGES = [30, 90, 365, 730] as const;
const DAY = 86_400_000;
const C = { avg: '#38bdf8', min: '#64748b', max: '#f97316', lastYear: '#a3a3a3', sea: '#22c55e' };

// 'YYYY-MM-DD' → 그날 정오(KST) ms
const dayTs = (d: string) => Date.parse(`${d}T12:00:00+09:00`);

function byMetric(rows: EnvDaily[] | null, metric: string): Map<string, EnvDaily> {
  const m = new Map<string, EnvDaily>();
  for (const r of rows ?? []) if (r.metric === metric) m.set(r.date, r);
  return m;
}

export function LongTermEnvPanel() {
  const { tr } = useLanguage();
  const [days, setDays] = useState<number>(365);
  const [res, setRes] = useState<SiteEnvDaily | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    // 작년 비교를 위해 1년 더 받는다(최대 2년).
    SiteEnv.daily(Math.min(days + 365, 731))
      .then(r => { if (alive) { setRes(r); setError(null); } })
      .catch(e => { if (alive) setError(String(e?.message ?? e)); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [days]);

  const view = useMemo(() => {
    if (!res) return null;
    const nowMs = Date.now();
    const x0 = nowMs - days * DAY;
    const water = byMetric(res.tank, 'water_temperature');
    const doM = byMetric(res.tank, 'dissolved_oxygen');
    const seaAvg = byMetric(res.site, 'sea_temp_daily_avg');
    const seaMin = byMetric(res.site, 'sea_temp_daily_min');
    const seaMax = byMetric(res.site, 'sea_temp_daily_max');
    const wind = byMetric(res.site, 'wind_speed');
    const wave = byMetric(res.site, 'wave_height');
    const air = byMetric(res.site, 'air_temperature');

    // 수온: 가두리(30분 관측 일별) 우선, 없는 날은 관측소 일별 수온.
    const tempOf = (d: string) => {
      const w = water.get(d);
      if (w) return { min: w.min, avg: w.avg, max: w.max };
      const a = seaAvg.get(d);
      if (!a) return undefined;
      return { min: seaMin.get(d)?.min ?? a.avg, avg: a.avg, max: seaMax.get(d)?.max ?? a.avg };
    };
    const dates = new Set<string>();
    for (const m of [water, doM, seaAvg, wind, wave, air]) for (const d of m.keys()) dates.add(d);
    const sorted = [...dates].sort();
    const lastYearKey = (d: string) => {
      const t = new Date(dayTs(d) - 365 * DAY);
      return new Date(t.getTime() + 9 * 3600_000).toISOString().slice(0, 10);
    };

    const temp: CanvasPoint[] = [], dox: CanvasPoint[] = [], windP: CanvasPoint[] = [],
      waveP: CanvasPoint[] = [], airP: CanvasPoint[] = [];
    const windBands: { from: number; to: number; color?: string }[] = [];
    const waveBands: { from: number; to: number; color?: string }[] = [];
    let windDays = 0, waveDays = 0, tMin: number | null = null, tMax: number | null = null, covered = 0;
    for (const d of sorted) {
      const ts = dayTs(d);
      if (ts < x0) continue;
      const t = tempOf(d), ly = tempOf(lastYearKey(d));
      if (t) {
        covered++;
        tMin = tMin == null ? t.min : Math.min(tMin, t.min);
        tMax = tMax == null ? t.max : Math.max(tMax, t.max);
      }
      temp.push({ ts, avg: t?.avg, min: t?.min, max: t?.max, lastYear: ly?.avg });
      const o = doM.get(d);
      dox.push({ ts, avg: o?.avg, min: o?.min, lastYear: doM.get(lastYearKey(d))?.avg });
      const w = wind.get(d), v = wave.get(d), a = air.get(d);
      windP.push({ ts, avg: w?.avg, max: w?.max });
      waveP.push({ ts, avg: v?.avg, max: v?.max });
      airP.push({ ts, avg: a?.avg, min: a?.min, max: a?.max });
      const band = { from: ts - DAY / 2, to: ts + DAY / 2, color: 'rgba(239,68,68,0.18)' };
      if (w && w.max >= res.thresholds.wind_speed) { windDays++; windBands.push(band); }
      if (v && v.max >= res.thresholds.wave_height) { waveDays++; waveBands.push(band); }
    }
    return { x0, nowMs, temp, dox, windP, waveP, airP, windBands, waveBands, windDays, waveDays, tMin, tMax, covered };
  }, [res, days]);

  const fmtDay = (ts: number) =>
    new Date(ts).toLocaleDateString(dateLocale(), days > 120 ? { year: '2-digit', month: 'short' } : { month: 'numeric', day: 'numeric' });
  const fmtTide = (iso: string) =>
    new Date(iso).toLocaleString(dateLocale(), { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });

  const buoy = res?.sources?.buoy;
  const dw = res?.sources?.daily_water;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">{tr('longEnv.title')}</h2>
          <p className="text-xs text-muted-foreground">{tr('longEnv.subtitle')}</p>
        </div>
        <div className="flex gap-1">
          {RANGES.map(r => (
            <button key={r} onClick={() => setDays(r)}
              className={`px-3 py-1 text-xs rounded font-medium ${days === r ? 'bg-green-600 text-white' : 'bg-gray-800 text-gray-300 hover:bg-gray-700'}`}>
              {tr(`longEnv.range.${r}`)}
            </button>
          ))}
        </div>
      </div>

      {error && <div className="px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive">{error}</div>}
      {loading && !res && <div className="text-sm text-muted-foreground">{tr('longEnv.loading')}</div>}

      {view && res && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <Stat label={tr('longEnv.stat.coverage')} value={`${view.covered} / ${days}${tr('longEnv.daysUnit')}`} />
            <Stat label={tr('longEnv.stat.tempRange')}
              value={view.tMin != null && view.tMax != null ? `${view.tMin.toFixed(1)} ~ ${view.tMax.toFixed(1)}°C` : '—'} />
            <Stat label={tr('longEnv.stat.windDays', { v: res.thresholds.wind_speed })} value={`${view.windDays}${tr('longEnv.daysUnit')}`} warn={view.windDays > 0} />
            <Stat label={tr('longEnv.stat.waveDays', { v: res.thresholds.wave_height })} value={`${view.waveDays}${tr('longEnv.daysUnit')}`} warn={view.waveDays > 0} />
          </div>

          <div className="text-xs text-muted-foreground space-y-0.5">
            {dw && <div>{tr('longEnv.source.water', { name: dw.station_name, km: dw.distance_km?.toFixed(1) ?? '-' })}</div>}
            {buoy && <div>{tr('longEnv.source.buoy', { name: buoy.station_name, km: buoy.distance_km?.toFixed(1) ?? '-' })}</div>}
            {res.tide.length > 0 && (
              <div>{tr('longEnv.tide')}: {res.tide.map(t =>
                `${t.kind === 'tide_high' ? tr('longEnv.tideHigh') : tr('longEnv.tideLow')} ${fmtTide(t.at)} (${Math.round(t.level_cm)}cm)`).join(' · ')}</div>
            )}
          </div>

          <ChartCard title={tr('longEnv.chart.waterTemp')} note={tr('longEnv.note.waterTemp')}>
            <CanvasTimeChart data={view.temp} xDomain={[view.x0, view.nowMs]} unit="°C" height={200} legend fmtTime={fmtDay} minSpan={2}
              series={[{ key: 'max', name: tr('longEnv.s.max'), color: C.max }, { key: 'avg', name: tr('longEnv.s.avg'), color: C.avg },
                { key: 'min', name: tr('longEnv.s.min'), color: C.min }, { key: 'lastYear', name: tr('longEnv.s.lastYear'), color: C.lastYear }]} />
          </ChartCard>
          <ChartCard title={tr('longEnv.chart.do')}>
            <CanvasTimeChart data={view.dox} xDomain={[view.x0, view.nowMs]} unit="mg/L" height={170} legend fmtTime={fmtDay} minSpan={1}
              series={[{ key: 'avg', name: tr('longEnv.s.avg'), color: C.avg }, { key: 'min', name: tr('longEnv.s.min'), color: C.min },
                { key: 'lastYear', name: tr('longEnv.s.lastYear'), color: C.lastYear }]} />
          </ChartCard>
          <ChartCard title={tr('longEnv.chart.wind')} note={tr('longEnv.note.buoy')}>
            <CanvasTimeChart data={view.windP} xDomain={[view.x0, view.nowMs]} unit="m/s" height={170} legend fmtTime={fmtDay} minSpan={2}
              bands={view.windBands}
              series={[{ key: 'max', name: tr('longEnv.s.max'), color: C.max }, { key: 'avg', name: tr('longEnv.s.avg'), color: C.avg }]} />
          </ChartCard>
          <ChartCard title={tr('longEnv.chart.wave')} note={tr('longEnv.note.buoy')}>
            <CanvasTimeChart data={view.waveP} xDomain={[view.x0, view.nowMs]} unit="m" height={170} legend fmtTime={fmtDay} minSpan={0.5}
              bands={view.waveBands}
              series={[{ key: 'max', name: tr('longEnv.s.max'), color: C.max }, { key: 'avg', name: tr('longEnv.s.avg'), color: C.avg }]} />
          </ChartCard>
          <ChartCard title={tr('longEnv.chart.air')} note={tr('longEnv.note.buoy')}>
            <CanvasTimeChart data={view.airP} xDomain={[view.x0, view.nowMs]} unit="°C" height={170} legend fmtTime={fmtDay} minSpan={2}
              series={[{ key: 'max', name: tr('longEnv.s.max'), color: C.max }, { key: 'avg', name: tr('longEnv.s.avg'), color: C.avg },
                { key: 'min', name: tr('longEnv.s.min'), color: C.min }]} />
          </ChartCard>
        </>
      )}
    </div>
  );
}

function Stat({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3">
      <div className="text-xs text-gray-400">{label}</div>
      <div className={`text-lg font-mono ${warn ? 'text-red-400' : 'text-gray-100'}`}>{value}</div>
    </div>
  );
}

function ChartCard({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <div className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3">
      <div className="flex items-baseline justify-between mb-2">
        <div className="text-sm text-gray-300 font-medium">{title}</div>
        {note && <div className="text-[11px] text-gray-500">{note}</div>}
      </div>
      {children}
    </div>
  );
}
