import { useEffect, useMemo, useRef, useState } from 'react';
import { dateLocale } from '../../lib/format';
import { Activity } from 'lucide-react';
import { Vision, Cameras, FeedCycles, Tanks } from '../../lib/api';
import type {
  VisionObservation, VisionAlgorithm, FeedCycle, Camera,
} from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';
import { CandleScoreChart } from './CandleScoreChart';
import { LineScoreChart, type LinePoint } from './LineScoreChart';
import { RANGE_CONFIG, RANGE_LABELS, type TimeRange } from './candleScoreRanges';
import { useLanguage } from '../../lib/language-core';
import { stretchR3 } from '../../lib/r3-stretch';

const RANGES: TimeRange[] = ['1h', '6h', '12h', '1d', '7d', '30d', 'since-stocking'];

// 요동곡선 기반 활동성 유사 점수 키 — 모델 점수가 아닌 수면 요동 프록시.
const AGITATION_KEY = 'agitation_activity';

// 표시 여유 — 시선별 P90 을 화면 0.71 에 놓아 위쪽 여유를 남긴다. 종전에는 P90 이
// 곧 1.0 이라 평소 급이도 천장에 붙어(실측 tank_01 최고 1.000) 봉우리 크기를
// 눈으로 비교할 수 없었다.
const RISE_HEADROOM = 1.4;

// 카메라 시선 구분 — 탑뷰와 수중은 장면 스케일이 9배 가까이 달라(절대 상승폭 P90
// 5.2 vs 45.9) 같은 자를 쓸 수 없다. 판별 관행은 백엔드 isUnderwaterCamera 와 동일.
function cameraLine(cameraID: string): string {
  return cameraID.toLowerCase().includes('underwater') ? 'underwater' : 'top';
}

// 평소 활동성 키 — baseline 상시 클립 요동 대표값을 카메라별 중앙값(평소 수준)
// 대비 비율로 정규화 (1.0 = 평소). 선형 차트(LineScoreChart)로 표시.
const BASELINE_KEY = 'baseline_activity';

// 펄스별 r3 채점 키 — vision.segment.model_scores.recorded 를 펄스 시각에 찍는다.
// 2026-07-24: 점수 종류 드롭다운에서는 제거(agitation_activity 위 r3 오버레이로 표시되어
// 중복). 단 데이터(pulseScoreObs)는 그 오버레이용으로 계속 사용한다.
const PULSE_SCORE_KEY = 'feeding_score_pulse';

// 점수 종류 드롭다운에서 숨길 관측 키 — feeding_activity(통클립 LRCN)는
// agitation_activity + r3 오버레이에 이미 담겨 중복이라 숨긴다 (2026-07-24 운영자).
const HIDDEN_SCORE_KEYS = new Set(['feeding_activity']);

type CameraRow = {
  camera: Camera;
  stockingAtMs: number | null;
  feedCycles: FeedCycle[];
};

export function InferenceMonitor() {
  const { tr } = useLanguage();
  const [observations, setObservations] = useState<VisionObservation[]>([]);
  const [agitationObs, setAgitationObs] = useState<VisionObservation[]>([]);
  // 급이 중 실시간 요동 (진행 사이클 in-memory 세션 폴링). 사이클 종료 후 나오는
  // agitationObs(클립)와 별도로 합쳐 그린다.
  const [liveAgi, setLiveAgi] = useState<VisionObservation[]>([]);
  // 카메라별 절대 상승폭 정규화 계수 — fetchAll 에서 산출, 실시간 폴링이 같은 계수로 정규화.
  const riseScaleRef = useRef<Record<string, number>>({});
  const [pulseScoreObs, setPulseScoreObs] = useState<VisionObservation[]>([]);
  const [baselinePoints, setBaselinePoints] = useState<Record<string, LinePoint[]>>({});
  const [cameraRows, setCameraRows] = useState<CameraRow[]>([]);
  const [algorithms, setAlgorithms] = useState<VisionAlgorithm[]>([]);

  const [scoreKey, setScoreKey] = useState<string>(AGITATION_KEY);
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
      // 화면이 안 보이면 받지 않는다 — 다른 탭·최소화 상태에서도 30초마다 전량을
      // 재구성하던 것을 멈춘다 (2026-08-14). 복귀 시 아래 visibilitychange 가 즉시 채운다.
      if (typeof document !== 'undefined' && document.hidden) return;
      try {
        // 곡선은 화면이 보는 기간만 받는다 (2026-08-14). 종전에는 range 와 무관하게 항상
        // 최근 500건(실측 23일치 · 1.5MB · series 표본 25만)을 30초마다 받아 관측 객체를
        // 5만 개씩 새로 만들었고, 렌더러가 60~80MB/분 늘다 WebKit 이 죽었다.
        // 여유 1일 — 캔들 첫 구간이 잘리지 않게. spanMs=null(입식 이후)은 전체.
        const span = RANGE_CONFIG[range].spanMs;
        const sinceIso = span === null
          ? undefined
          : new Date(Date.now() - span - 24 * 3600_000).toISOString();
        const [camRes, obsRes, agiRes, agiCalibRes, agiBaseRes, pulseRes] = await Promise.all([
          Cameras.list(),
          Vision.observations({ limit: 5000 }),
          Vision.agitationRecent(500, { since: sinceIso }).catch(() => null),
          // 정규화 계수 전용 — 앵커 없는 카메라의 7일 P90 폴백은 기간을 좁히면 자가
          // 바뀌므로 종전 창(7일)을 그대로 쓴다. series 는 필요 없어 meta_only.
          Vision.agitationRecent(500, {
            since: new Date(Date.now() - 7 * 24 * 3600_000).toISOString(),
            metaOnly: true,
          }).catch(() => null),
          Vision.agitationBaseline(3000).catch(() => null),
          Vision.modelScoresRecent(200).catch(() => null),
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
        // API 는 {event_id, recorded_at, payload:{camera_id, scores, ...}} 로 감싸서 반환 —
        // 차트는 camera_id/scores 를 최상위에서 읽으므로 payload 를 펼친다.
        const flatObs = (obsRes.items ?? []).map(it => {
          const payload = it.payload as VisionObservation | undefined;
          return payload ? { ...payload, recorded_at: it.recorded_at ?? payload.recorded_at } : it;
        });
        setObservations(flatObs);
        // 요동곡선 → 활동성 유사 관측: series(초 단위)를 5초 간격으로 샘플링.
        // 정규화 = 절대 상승폭(s − 기저)을 **같은 시선(탑뷰/수중) 카메라가 공유하는**
        // 스케일로 나눈다. 종전에는 상승률((s−기저)/기저)을 카메라별 P90 으로 나눠
        // 두 탑뷰가 서로 다른 자를 써서 화면 높이를 수조 간 비교할 수 없었다.
        // 실측(7-27~8-03, 카메라당 56클립): 상승률 P90 은 0.830 vs 0.502 로 1.65배
        // 벌어지지만 절대 상승폭 P90 은 5.22 vs 4.44 로 1.18배까지 좁혀진다 —
        // 카메라 차이가 덧셈형(노이즈 바닥)이라 뺄셈에서 이미 상쇄되기 때문이다.
        // 기저로 다시 나누면 기저 높은 카메라(tank_02 9.4 vs tank_01 6.5)만 눌린다.
        // ⚠ 정규화 계수는 표시용(agiRes)이 아니라 보정용(agiCalibRes, 7일 meta_only)에서
        //   낸다. 표시 창이 1시간이어도 자는 7일 분포로 고정돼야 화면 높이가 안 흔들린다.
        const calibItems = agiCalibRes?.items ?? agiRes?.items ?? [];
        const lineRises: Record<string, number[]> = {};
        const calibSince = Date.now() - 7 * 24 * 3600_000; // 백엔드와 동일 7일 창
        for (const item of calibItems) {
          if (!(item.baseline > 0)) continue;
          if (new Date(item.captured_at).getTime() < calibSince) continue;
          (lineRises[cameraLine(item.camera_id)] ??= []).push(item.peak - item.baseline);
        }
        const lineScale: Record<string, number> = {};
        for (const [line, rises] of Object.entries(lineRises)) {
          if (rises.length < 10) continue;
          const sorted = [...rises].sort((a, b) => a - b);
          const p90 = sorted[Math.floor(sorted.length * 0.9)];
          // 여러 카메라·7일을 함께 모아 P90 을 내므로, 한 수조의 활동이 떨어지면
          // 그 카메라 표시가 오히려 커지던 되먹임(tank_02 P90 0.885→0.454, 증폭
          // 1.13→2.20배)이 크게 완화된다. 그래서 하한(0.70) 패치도 쓰지 않는다.
          if (p90 > 1e-6) lineScale[line] = p90 * RISE_HEADROOM;
        }
        // 카메라별 앵커가 있으면 그것이 자다 (2026-08-05). 없는 카메라만 위 P90 폴백.
        //
        // 왜 앵커가 우선인가. P90 은 최근 7일 분포 통계라 (1) 날마다 움직이고 (2) 활동이
        // 낮아지면 자도 같이 낮아지는 되먹임이 있으며 (3) 애초에 "이 수조가 낼 수 있는
        // 최대"와 무관하다. 앵커는 단식 후 재개일(캘리브레이션 날)에 관측한 그 카메라의
        // 최고 반응을 고정한 값이라 1.00 이 "천장"이라는 물리적 의미를 갖는다.
        // 실측 효과: 축 사용률 27%→50%, 무배출↔배출 간격이 축의 3%→6% (1.9배).
        //
        // ⚠ 백엔드 섀도 판정(shadow_stop.go)과 **같은 값**이어야 한다. 그래서 프론트가
        // 상수로 들지 않고 API 응답(rise_anchor_by_camera)에서 받는다.
        const anchors = agiCalibRes?.rise_anchor_by_camera ?? agiRes?.rise_anchor_by_camera ?? {};
        const riseScale: Record<string, number> = {};
        for (const item of calibItems) {
          const a = anchors[item.camera_id];
          if (a && a > 0) { riseScale[item.camera_id] = a; continue; }
          const s = lineScale[cameraLine(item.camera_id)];
          if (s) riseScale[item.camera_id] = s;
        }
        riseScaleRef.current = riseScale; // 실시간 폴링이 동일 계수로 정규화하도록 공유
        const agiObs: VisionObservation[] = [];
        for (const item of agiRes?.items ?? []) {
          if (!item.captured_at || !(item.baseline > 0)) continue;
          const t0 = new Date(item.captured_at).getTime();
          if (!Number.isFinite(t0)) continue;
          const scale = riseScale[item.camera_id] ?? 1;
          for (let i = 0; i < (item.series?.length ?? 0); i += 5) {
            const rise = item.series[i] - item.baseline;
            const v = Math.max(0, Math.min(1, rise / scale));
            agiObs.push({
              camera_id: item.camera_id,
              recorded_at: new Date(t0 + i * 1000).toISOString(),
              scores: { [AGITATION_KEY]: v },
            });
          }
        }
        setAgitationObs(agiObs);
        // 평소 활동성 — 카메라별 요동 대표값을 그 카메라의 중앙값(평소 수준) 대비
        // 비율로 정규화 (카메라마다 절대 스케일이 달라 원시값 비교 불가).
        // 야간(IR 흑백)은 주간과 스케일이 또 달라 주/야 각각의 중앙값으로 정규화.
        const rawByRegime: Record<string, { cam: string; t: number; raw: number }[]> = {};
        for (const item of agiBaseRes?.items ?? []) {
          if (!item.captured_at || !(item.mean > 0)) continue;
          const t = new Date(item.captured_at).getTime();
          if (!Number.isFinite(t)) continue;
          const key = `${item.camera_id}|${item.night ? 'night' : 'day'}`;
          (rawByRegime[key] ??= []).push({ cam: item.camera_id, t, raw: item.mean });
        }
        const basePts: Record<string, LinePoint[]> = {};
        for (const arr of Object.values(rawByRegime)) {
          const sorted = arr.map(p => p.raw).sort((a, b) => a - b);
          const median = sorted[Math.floor(sorted.length / 2)];
          if (!(median > 0)) continue;
          for (const p of arr) (basePts[p.cam] ??= []).push({ t: p.t, v: p.raw / median });
        }
        for (const arr of Object.values(basePts)) arr.sort((a, b) => a.t - b.t);
        setBaselinePoints(basePts);
        // 펄스별 r3 채점 — 펄스 시각(pulse_at)에 유사 관측으로 찍는다.
        const psObs: VisionObservation[] = [];
        for (const item of pulseRes?.items ?? []) {
          if (!item.pulse_at || typeof item.feeding_score !== 'number') continue;
          const t = new Date(item.pulse_at).getTime();
          if (!Number.isFinite(t)) continue;
          psObs.push({
            camera_id: item.camera_id,
            recorded_at: item.pulse_at,
            // 표시 전용 스트레치 — 저장/프리필/ai_score 는 원값 유지(r3-stretch.ts 참조).
            scores: { [PULSE_SCORE_KEY]: stretchR3(item.feeding_score) },
          });
        }
        setPulseScoreObs(psObs);
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
    // 화면 복귀 즉시 1회 — 숨은 동안 건너뛴 폴링을 메운다.
    const onVisible = () => { if (!document.hidden) void fetchAll(); };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [range]);

  // 실시간 요동 폴링 (8초) — 진행 중 사이클의 in-memory series 를 클립과 동일한 P90
  // 정규화로 그려, 급이가 끝나기 전에도 녹색 곡선이 실시간으로 흐르게 한다.
  useEffect(() => {
    let cancelled = false;
    async function pollLive() {
      if (typeof document !== 'undefined' && document.hidden) return; // 안 보이면 폴링 정지
      try {
        const res = await Vision.agitationLive();
        if (cancelled) return;
        const obs: VisionObservation[] = [];
        for (const item of res.items ?? []) {
          if (!item.started_at || !(item.baseline > 0)) continue;
          const t0 = new Date(item.started_at).getTime();
          if (!Number.isFinite(t0)) continue;
          const scale = riseScaleRef.current[item.camera_id] ?? 1;
          for (let i = 0; i < (item.series?.length ?? 0); i += 3) {
            const rise = item.series[i] - item.baseline;
            const v = Math.max(0, Math.min(1, rise / scale));
            obs.push({
              camera_id: item.camera_id,
              recorded_at: new Date(t0 + i * 1000).toISOString(),
              scores: { [AGITATION_KEY]: v },
            });
          }
        }
        setLiveAgi(obs);
      } catch {
        if (!cancelled) setLiveAgi([]); // 실패 시 실시간 오버레이만 비움 (사후 클립은 유지)
      }
    }
    void pollLive();
    const t = setInterval(pollLive, 8_000);
    return () => { cancelled = true; clearInterval(t); };
  }, []);

  // 실제 관측에 존재하는 점수 키 우선 — 레지스트리 outputs 는 선언만 있고
  // 실적재가 없는 키(activity_score 등)를 포함하므로 관측이 있으면 관측 기준.
  const scoreOptions = useMemo<string[]>(() => {
    const observed = new Set<string>();
    for (const o of observations) {
      for (const k of Object.keys(o.scores ?? {})) {
        if (!HIDDEN_SCORE_KEYS.has(k)) observed.add(k);
      }
    }
    const opts = Array.from(observed).sort();
    // feeding_score_pulse(r3)는 드롭다운 제외 — agitation_activity 위 r3 오버레이로 표시(중복).
    if (agitationObs.length > 0) opts.push(AGITATION_KEY); // 요동 (기본)
    if (Object.keys(baselinePoints).length > 0) opts.push(BASELINE_KEY);
    if (opts.length > 0) return opts;

    const declared = new Set<string>();
    for (const a of algorithms) {
      for (const o of a.outputs ?? []) declared.add(o);
    }
    if (declared.size === 0) return ['activity_score', 'feeding_response_score'];
    return Array.from(declared);
  }, [observations, agitationObs, pulseScoreObs, baselinePoints, algorithms]);

  // 선택된 점수 종류에 해당하는 관측 소스만 차트에 공급.
  // 요동은 사후 클립(agitationObs) + 진행 중 실시간(liveAgi)을 합친다 — 차트가 ts 로
  // 병합하므로 중복 무해(진행 사이클은 클립 미완결이라 겹치지 않음).
  const chartObservations = useMemo(
    () => (scoreKey === AGITATION_KEY ? [...agitationObs, ...liveAgi]
      : scoreKey === PULSE_SCORE_KEY ? pulseScoreObs
      : observations),
    [scoreKey, agitationObs, liveAgi, pulseScoreObs, observations],
  );

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
            {tr('inferenceMonitor.lastRefresh')} {lastRefresh.toLocaleTimeString(dateLocale())} · {tr('inferenceMonitor.refreshInterval')}
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
                observations={chartObservations}
                baselinePoints={baselinePoints[row.camera.camera_id] ?? []}
                scoreKey={scoreKey}
                range={range}
                r3PulseObs={pulseScoreObs}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function CameraSection({
  row, observations, baselinePoints, scoreKey, range, r3PulseObs,
}: {
  row: CameraRow;
  observations: VisionObservation[];
  baselinePoints: LinePoint[];
  scoreKey: string;
  range: TimeRange;
  r3PulseObs: VisionObservation[];
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
                {new Date(stockingAtMs).toLocaleDateString(dateLocale())}
              </span>
              <span className="text-gray-600"> · D+{ageDays}</span>
            </>
          ) : (
            <span className="text-gray-600">{tr('inferenceMonitor.noStockingInfo')}</span>
          )}
          <span className="text-gray-600"> · {tr('inferenceMonitor.feedCyclePrefix')} {feedCycles.length}{tr('inferenceMonitor.feedCycleSuffix')}</span>
        </div>
      </div>

      {scoreKey === BASELINE_KEY ? (
        <LineScoreChart
          points={baselinePoints}
          range={range}
          stockingAtMs={stockingAtMs}
          height={240}
        />
      ) : (
        <CandleScoreChart
          observations={observations}
          feedCycles={feedCycles}
          cameraId={camera.camera_id}
          scoreKey={scoreKey}
          range={range}
          stockingAtMs={stockingAtMs}
          height={240}
          overlayObservations={scoreKey === AGITATION_KEY ? r3PulseObs : undefined}
          overlayScoreKey={PULSE_SCORE_KEY}
          overlayLabel="r3"
        />
      )}
    </div>
  );
}
