import { useEffect, useMemo, useState } from 'react';
import { Activity } from 'lucide-react';
import { Vision, Cameras, FeedCycles, Tanks } from '../../lib/api';
import type {
  VisionObservation, VisionAlgorithm, FeedCycle, Camera,
} from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';
import { CandleScoreChart, RANGE_LABELS, type TimeRange } from './CandleScoreChart';
import { useLanguage } from '../../lib/language-context';

const RANGES: TimeRange[] = ['1h', '6h', '12h', '1d', '7d', '30d', 'since-stocking'];

type CameraRow = {
  camera: Camera;
  stockingAtMs: number | null;
  feedCycles: FeedCycle[];
};

export function InferenceMonitor() {
  const { tr } = useLanguage();
  const [observations, setObservations] = useState<VisionObservation[]>([]);
  const [cameraRows, setCameraRows] = useState<CameraRow[]>([]);
  const [algorithms, setAlgorithms] = useState<VisionAlgorithm[]>([]);

  const [scoreKey, setScoreKey] = useState<string>('activity_score');
  const [range, setRange] = useState<TimeRange>('1h');

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);

  // 알고리즘 — 초기 1회 (점수 종류 옵션 소스).
  useEffect(() => {
    let cancelled = false;
    Vision.algorithms()
      .then(r => { if (!cancelled) setAlgorithms(r.items); })
      .catch(err => {
        if (!cancelled) setError(err instanceof Error ? err.message : tr('inferenceMonitor.errorAlgorithmLoad'));
      });
    return () => { cancelled = true; };
  }, []);

  // cameras + observations + 카메라별 lifecycle/feed-cycles — 30초 polling.
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | null = null;

    async function fetchAll() {
      try {
        const [camRes, obsRes] = await Promise.all([
          Cameras.list(),
          Vision.observations({ limit: 5000 }),
        ]);
        if (cancelled) return;

        // 카메라를 수조 순서대로 정렬 (tank_id 우선, 없으면 camera_id).
        const cams = [...camRes.items].sort((a, b) => {
          const ta = a.tank_id ?? '';
          const tb = b.tank_id ?? '';
          if (ta !== tb) return ta.localeCompare(tb);
          return a.camera_id.localeCompare(b.camera_id);
        });

        // 카메라별 보조 데이터 (lifecycle + feed-cycles) — tank 가 같으면 한 번씩 캐시.
        const tankCache = new Map<string, { stocking: number | null; cycles: FeedCycle[] }>();
        const rows: CameraRow[] = [];
        for (const cam of cams) {
          if (cam.tank_id && !tankCache.has(cam.tank_id)) {
            let stocking: number | null = null;
            let cycles: FeedCycle[] = [];
            try {
              const lc = await Tanks.lifecycle(cam.tank_id);
              if (lc.current?.stocked_at) stocking = new Date(lc.current.stocked_at).getTime();
            } catch { /* optional */ }
            try {
              const fc = await FeedCycles.listForTank(cam.tank_id, 200);
              cycles = fc.items;
            } catch { /* optional */ }
            tankCache.set(cam.tank_id, { stocking, cycles });
          }
          const cached = cam.tank_id ? tankCache.get(cam.tank_id) : undefined;
          rows.push({
            camera: cam,
            stockingAtMs: cached?.stocking ?? null,
            feedCycles: cached?.cycles ?? [],
          });
        }

        if (cancelled) return;
        setObservations(obsRes.items);
        setCameraRows(rows);
        setError(null);
        setLastRefresh(new Date());
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : tr('inferenceMonitor.errorLoad'));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void fetchAll();
    timer = setInterval(fetchAll, 30_000);
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, []);

  const scoreOptions = useMemo<string[]>(() => {
    const all = new Set<string>();
    for (const a of algorithms) {
      for (const o of a.outputs ?? []) all.add(o);
    }
    if (all.size === 0) return ['activity_score', 'feeding_response_score'];
    return Array.from(all);
  }, [algorithms]);

  useEffect(() => {
    if (scoreOptions.length > 0 && !scoreOptions.includes(scoreKey)) {
      setScoreKey(scoreOptions[0]);
    }
  }, [scoreOptions, scoreKey]);

  // 'since-stocking' 버튼은 row 중 하나라도 입식 정보가 있으면 활성.
  const anyStocking = cameraRows.some(r => r.stockingAtMs !== null);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-green-400" />
          {tr('inferenceMonitor.title')}
        </CardTitle>
        {lastRefresh && (
          <span className="text-xs text-gray-500 font-mono">
            {tr('inferenceMonitor.lastRefresh')} {lastRefresh.toLocaleTimeString('ko-KR')} · {tr('inferenceMonitor.refreshInterval')}
          </span>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {/* 공통 컨트롤 — 점수 종류 + 시간 범위 */}
        <div className="flex flex-wrap items-end gap-x-5 gap-y-3 pb-2 border-b border-gray-700/30">
          <div className="flex flex-col gap-1">
            <label htmlFor="score-select" className="text-xs text-gray-400">{tr('inferenceMonitor.scoreType')}</label>
            <select
              id="score-select"
              value={scoreKey}
              onChange={e => setScoreKey(e.target.value)}
              className="h-8 px-2 rounded border border-gray-700 bg-gray-800 text-sm text-white min-w-[16rem]"
            >
              {scoreOptions.map(opt => (
                <option key={opt} value={opt}>{opt}</option>
              ))}
            </select>
          </div>

          <div className="flex flex-col gap-1 flex-1">
            <label className="text-xs text-gray-400">{tr('inferenceMonitor.timeRange')}</label>
            <div className="flex flex-wrap gap-1">
              {RANGES.map(r => (
                <button
                  key={r}
                  type="button"
                  onClick={() => setRange(r)}
                  disabled={r === 'since-stocking' && !anyStocking}
                  className={
                    range === r
                      ? 'px-2.5 py-1 text-xs rounded bg-green-600/20 border border-green-500/60 text-green-200'
                      : 'px-2.5 py-1 text-xs rounded border border-gray-700 text-gray-400 hover:text-white hover:border-gray-600 disabled:opacity-30 disabled:cursor-not-allowed'
                  }
                  title={r === 'since-stocking' && !anyStocking ? tr('inferenceMonitor.noStockingInfo') : undefined}
                >
                  {tr(RANGE_LABELS[r])}
                </button>
              ))}
            </div>
          </div>

          <div className="text-xs text-gray-500">
            {tr('inferenceMonitor.camerasLabel')} <span className="text-gray-300 font-mono">{cameraRows.length}</span>{tr('inferenceMonitor.camerasAll')}
          </div>
        </div>

        {/* 차트 N개 — 수조 순서대로 세로 나열 */}
        {loading ? (
          <div className="space-y-4">
            <Skeleton className="h-72 w-full" />
            <Skeleton className="h-72 w-full" />
          </div>
        ) : error ? (
          <div className="px-3 py-2 bg-destructive/10 border border-destructive/30 rounded text-sm text-destructive font-mono">
            {error}
          </div>
        ) : cameraRows.length === 0 ? (
          <p className="text-sm text-gray-500 py-12 text-center">{tr('inferenceMonitor.noCameras')}</p>
        ) : (
          <div className="space-y-4">
            {cameraRows.map(row => (
              <CameraSection
                key={row.camera.camera_id}
                row={row}
                observations={observations}
                scoreKey={scoreKey}
                range={range}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function CameraSection({
  row, observations, scoreKey, range,
}: {
  row: CameraRow;
  observations: VisionObservation[];
  scoreKey: string;
  range: TimeRange;
}) {
  const { tr } = useLanguage();
  const { camera, stockingAtMs, feedCycles } = row;
  const ageDays = stockingAtMs
    ? Math.floor((Date.now() - stockingAtMs) / 86_400_000)
    : null;

  return (
    <div className="p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg space-y-3">
      <div className="flex items-baseline justify-between gap-3 flex-wrap">
        <div className="flex items-baseline gap-2 min-w-0">
          {camera.tank_id && (
            <span className="text-sm font-semibold text-white">{tr('inferenceMonitor.tank')} {camera.tank_id}</span>
          )}
          <span className="text-xs text-gray-500 font-mono">{camera.camera_id}</span>
        </div>
        <div className="text-xs text-gray-500">
          {stockingAtMs ? (
            <>
              {tr('inferenceMonitor.stocking')} <span className="text-gray-300 font-mono">
                {new Date(stockingAtMs).toLocaleDateString('ko-KR')}
              </span>
              <span className="text-gray-600"> · D+{ageDays}</span>
            </>
          ) : (
            <span className="text-gray-600">{tr('inferenceMonitor.noStockingInfo')}</span>
          )}
          <span className="text-gray-600"> · {tr('inferenceMonitor.feedCyclePrefix')} {feedCycles.length}{tr('inferenceMonitor.feedCycleSuffix')}</span>
        </div>
      </div>

      <CandleScoreChart
        observations={observations}
        feedCycles={feedCycles}
        cameraId={camera.camera_id}
        scoreKey={scoreKey}
        range={range}
        stockingAtMs={stockingAtMs}
        height={240}
      />
    </div>
  );
}
