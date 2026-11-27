import { useEffect, useRef, useState } from 'react';
import { dateLocale } from '../lib/format';

// 시계열 캔버스 차트 — 그룹개요 recharts(SVG) 대체.
//
// ⚠ WebKitGTK 는 화면에 있는 SVG 를 repaint 할 때마다 메모리를 잔류시킨다
// (2026-07-09 격리 실측: 차트 4개 표시 +19~45MB/분, 전부 숨김 +3MB/분 —
// 매일 저녁 대시보드 멈춤의 근본 원인). software canvas(willReadFrequently)
// 경로는 동일 조건에서 결백이 실측 확인돼(30fps 연속 그리기 6MB/분) SVG 를
// 아예 화면에 두지 않는 것이 근본 수정이다.
//
// recharts 대비 기능: 멀티 라인(connectNulls)/에어리어, 시간 x축(HH:MM),
// y 자동 범위+단위, 대시 그리드, 적정범위 밴드+한계선, hover 툴팁, 범례.

export type CanvasSeries = {
  key: string;   // data row 에서 값을 읽을 필드
  name: string;  // 범례/툴팁 표기
  color: string;
  area?: boolean; // 라인 아래 면 채우기 (ChillerEnergyChart 스타일)
};

export type CanvasPoint = { ts: number } & Record<string, number | undefined>;

interface Props {
  data: CanvasPoint[];
  series: CanvasSeries[];
  xDomain: [number, number];
  yMin?: number | 'auto';
  yMax?: number | 'auto';
  unit?: string;
  height?: number;
  refBand?: { min: number; max: number } | null; // 적정범위 밴드(초록) + 상/하한 점선(빨강)
  legend?: boolean;
  fmtTime?: (ts: number) => string;
  // y 자동 범위의 최소 폭 — 데이터 변동이 이보다 작아도 이 폭까지만 줌 (노이즈 과확대 방지).
  // y 는 데이터 중심으로 잡는다 (밴드는 범위 산정에서 제외 — 밴드까지 포함하면 예: 수온
  // 8~16°C 밴드가 12.4~12.8°C 실변동을 평평하게 눌러 변화가 안 보임, 2026-07-11 운영자).
  minSpan?: number;
  // 수직 마커(점선) — 예: 급이 사이클 시작 시각. 데이터 뒤(그리드 위)에 그린다.
  markers?: { ts: number; color?: string }[];
  // 수직 밴드(음영) — 예: 펄스 반응창. 그리드보다 먼저 그려 데이터 뒤에 깔린다.
  // 창 밖의 봉우리(사람 접근·소음 등)를 눈으로 걸러내기 위한 것.
  bands?: { from: number; to: number; color?: string }[];
}

const M = { top: 6, right: 12, bottom: 18, left: 58 }; // plot 여백 (left = "10.0mg/L" 폭)
const X_TICK_STEPS_MS = [
  60_000, 5 * 60_000, 10 * 60_000, 30 * 60_000,
  3_600_000, 2 * 3_600_000, 3 * 3_600_000, 6 * 3_600_000, 12 * 3_600_000,
  // 일·주·월 — 장기 환경(최대 2년) 차트용
  86_400_000, 2 * 86_400_000, 7 * 86_400_000, 14 * 86_400_000, 30 * 86_400_000, 61 * 86_400_000, 91 * 86_400_000,
];

function defaultFmtTime(ts: number): string {
  return new Date(ts).toLocaleTimeString(dateLocale(), { hour: '2-digit', minute: '2-digit' });
}

// 5개 내외의 "깔끔한" y 눈금 (1/2/5 × 10^n 간격)
function yTicks(min: number, max: number): number[] {
  const span = max - min;
  if (span <= 0) return [min];
  const raw = span / 4;
  const pow = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = [1, 2, 5, 10].map(m => m * pow).find(s => span / s <= 5) ?? 10 * pow;
  const ticks: number[] = [];
  for (let v = Math.ceil(min / step) * step; v <= max + 1e-9; v += step) ticks.push(v);
  return ticks;
}

function fmtVal(v: number): string {
  const a = Math.abs(v);
  if (a >= 100) return v.toFixed(0);
  if (a >= 10) return v.toFixed(1);
  return v.toFixed(2);
}

export function CanvasTimeChart({
  data, series, xDomain, yMin = 'auto', yMax = 'auto', unit = '',
  height = 190, refBand = null, legend = false, fmtTime = defaultFmtTime,
  minSpan = 0, markers, bands,
}: Props) {
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [width, setWidth] = useState(0);
  // hover: 가장 가까운 data row index (없으면 null). 툴팁 + 크로스헤어.
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const [hoverPx, setHoverPx] = useState<{ x: number; y: number } | null>(null);

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      const w = entries[0]?.contentRect.width ?? 0;
      setWidth(Math.floor(w));
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // y 범위: 명시값 우선, auto 는 데이터(+밴드) 에서 산출 + 5% 패딩
  const values: number[] = [];
  for (const row of data) {
    for (const s of series) {
      const v = row[s.key];
      if (typeof v === 'number') values.push(v);
    }
  }
  let lo = yMin === 'auto' ? (values.length ? Math.min(...values) : 0) : yMin;
  let hi = yMax === 'auto' ? (values.length ? Math.max(...values) : 1) : yMax;
  if (hi === lo) { hi = lo + Math.max(minSpan, 1); }
  if (minSpan > 0 && hi - lo < minSpan) {
    const mid = (hi + lo) / 2;
    if (yMin === 'auto') lo = mid - minSpan / 2;
    if (yMax === 'auto') hi = yMin === 'auto' ? mid + minSpan / 2 : lo + minSpan;
  }
  if (yMin === 'auto') lo -= (hi - lo) * 0.05;
  if (yMax === 'auto') hi += (hi - lo) * 0.05;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || width <= 0) return;
    const dpr = window.devicePixelRatio || 1;
    if (canvas.width !== width * dpr || canvas.height !== height * dpr) {
      canvas.width = width * dpr;
      canvas.height = height * dpr;
    }
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);

    const [x0, x1] = xDomain;
    const pw = width - M.left - M.right;   // plot width
    const ph = height - M.top - M.bottom;  // plot height
    if (pw <= 0 || ph <= 0 || x1 <= x0) return;
    const px = (ts: number) => M.left + ((ts - x0) / (x1 - x0)) * pw;
    const py = (v: number) => M.top + (1 - (v - lo) / (hi - lo)) * ph;

    // 수직 밴드 (펄스 반응창 등) — 가장 먼저 깔아 그리드·데이터가 위에 오게 한다.
    if (bands && bands.length > 0) {
      for (const b of bands) {
        const bx0 = Math.max(M.left, px(b.from));
        const bx1 = Math.min(M.left + pw, px(b.to));
        if (bx1 <= bx0) continue;
        ctx.fillStyle = b.color ?? 'rgba(34,197,94,0.10)';
        // 창이 1px 미만이면 안 보이므로 최소 폭을 준다 (넓은 시간범위에서도 위치는 보이게).
        ctx.fillRect(bx0, M.top, Math.max(1, bx1 - bx0), ph);
      }
    }

    // 적정범위 밴드 + 상/하한 점선
    if (refBand) {
      const yTop = Math.max(M.top, Math.min(py(refBand.max), M.top + ph));
      const yBot = Math.max(M.top, Math.min(py(refBand.min), M.top + ph));
      ctx.fillStyle = 'rgba(34,197,94,0.07)';
      ctx.fillRect(M.left, yTop, pw, yBot - yTop);
      ctx.strokeStyle = 'rgba(239,68,68,0.55)';
      ctx.lineWidth = 1;
      ctx.setLineDash([4, 4]);
      for (const v of [refBand.min, refBand.max]) {
        if (v < lo || v > hi) continue;
        ctx.beginPath();
        ctx.moveTo(M.left, py(v));
        ctx.lineTo(M.left + pw, py(v));
        ctx.stroke();
      }
      ctx.setLineDash([]);
    }

    // 그리드 + 눈금
    ctx.font = '10px ui-monospace, monospace';
    ctx.setLineDash([3, 3]);
    ctx.strokeStyle = 'rgba(55,65,81,0.3)';
    ctx.lineWidth = 1;
    const yt = yTicks(lo, hi);
    for (const v of yt) {
      ctx.beginPath();
      ctx.moveTo(M.left, py(v));
      ctx.lineTo(M.left + pw, py(v));
      ctx.stroke();
    }
    // x 눈금: 간격 ≥48px 이 되는 최소 step
    const minGapMs = ((x1 - x0) / pw) * 48;
    const stepMs = X_TICK_STEPS_MS.find(s => s >= minGapMs) ?? X_TICK_STEPS_MS[X_TICK_STEPS_MS.length - 1];
    const xt: number[] = [];
    for (let t = Math.ceil(x0 / stepMs) * stepMs; t <= x1; t += stepMs) xt.push(t);
    for (const t of xt) {
      ctx.beginPath();
      ctx.moveTo(px(t), M.top);
      ctx.lineTo(px(t), M.top + ph);
      ctx.stroke();
    }
    ctx.setLineDash([]);

    // 수직 마커 (급이 시작 등) — 데이터 뒤, 점선
    if (markers && markers.length > 0) {
      ctx.setLineDash([4, 4]);
      ctx.lineWidth = 1;
      for (const m of markers) {
        if (m.ts < x0 || m.ts > x1) continue;
        ctx.strokeStyle = m.color ?? '#3b82f6';
        ctx.beginPath();
        ctx.moveTo(px(m.ts), M.top);
        ctx.lineTo(px(m.ts), M.top + ph);
        ctx.stroke();
      }
      ctx.setLineDash([]);
    }

    // 축 레이블
    ctx.fillStyle = '#9ca3af';
    ctx.textAlign = 'right';
    ctx.textBaseline = 'middle';
    for (const v of yt) ctx.fillText(fmtVal(v) + (unit ? unit : ''), M.left - 4, py(v));
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    for (const t of xt) ctx.fillText(fmtTime(t), px(t), M.top + ph + 4);

    // 시리즈 (라인 + 선택적 area)
    for (const s of series) {
      // connectNulls: 값 있는 포인트만 이어서 그림
      const pts: { x: number; y: number }[] = [];
      for (const row of data) {
        const v = row[s.key];
        if (typeof v !== 'number') continue;
        if (row.ts < x0 || row.ts > x1) continue;
        pts.push({ x: px(row.ts), y: py(v) });
      }
      if (pts.length === 0) continue;
      if (s.area && pts.length >= 2) {
        ctx.beginPath();
        ctx.moveTo(pts[0].x, py(Math.max(lo, 0)));
        for (const p of pts) ctx.lineTo(p.x, p.y);
        ctx.lineTo(pts[pts.length - 1].x, py(Math.max(lo, 0)));
        ctx.closePath();
        ctx.fillStyle = s.color + '26'; // ~15% alpha
        ctx.fill();
      }
      ctx.beginPath();
      ctx.strokeStyle = s.color;
      ctx.lineWidth = 1.5;
      pts.forEach((p, i) => (i === 0 ? ctx.moveTo(p.x, p.y) : ctx.lineTo(p.x, p.y)));
      ctx.stroke();
    }

    // hover 크로스헤어
    if (hoverIdx != null && data[hoverIdx]) {
      const hx = px(data[hoverIdx].ts);
      if (hx >= M.left && hx <= M.left + pw) {
        ctx.strokeStyle = 'rgba(156,163,175,0.5)';
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(hx, M.top);
        ctx.lineTo(hx, M.top + ph);
        ctx.stroke();
      }
    }
  }, [data, series, width, height, xDomain[0], xDomain[1], lo, hi, unit, refBand?.min, refBand?.max, hoverIdx, fmtTime, markers, bands]);

  const onMove = (e: React.MouseEvent) => {
    const rect = canvasRef.current?.getBoundingClientRect();
    if (!rect || data.length === 0) return;
    const mx = e.clientX - rect.left;
    const [x0, x1] = xDomain;
    const pw = width - M.left - M.right;
    if (pw <= 0) return;
    const ts = x0 + ((mx - M.left) / pw) * (x1 - x0);
    // 가장 가까운 row (data 는 ts 오름차순)
    let best = 0;
    let bestD = Infinity;
    for (let i = 0; i < data.length; i++) {
      const d = Math.abs(data[i].ts - ts);
      if (d < bestD) { bestD = d; best = i; }
    }
    setHoverIdx(best);
    setHoverPx({ x: mx, y: e.clientY - rect.top });
  };

  const hoverRow = hoverIdx != null ? data[hoverIdx] : null;

  return (
    <div ref={wrapRef} className="relative w-full" style={{ height: height + (legend ? 22 : 0) }}>
      <canvas
        ref={canvasRef}
        style={{ width: '100%', height }}
        onMouseMove={onMove}
        onMouseLeave={() => { setHoverIdx(null); setHoverPx(null); }}
      />
      {legend && (
        <div className="flex items-center justify-center gap-3 text-[11px] text-gray-300" style={{ height: 22 }}>
          {series.map(s => (
            <span key={s.key} className="flex items-center gap-1">
              <span className="inline-block w-2.5 h-0.5" style={{ background: s.color }} />
              {s.name}
            </span>
          ))}
        </div>
      )}
      {hoverRow && hoverPx && (
        <div
          className="absolute pointer-events-none z-10 px-2 py-1 rounded text-xs font-mono"
          style={{
            left: Math.min(hoverPx.x + 10, Math.max(0, width - 150)),
            top: 8,
            background: '#111827',
            border: '1px solid #374151',
          }}
        >
          <div className="text-gray-400">{fmtTime(hoverRow.ts)}</div>
          {series.map(s => {
            const v = hoverRow[s.key];
            return typeof v === 'number' ? (
              <div key={s.key} style={{ color: s.color }}>
                {s.name}: {fmtVal(v)}{unit}
              </div>
            ) : null;
          })}
        </div>
      )}
    </div>
  );
}
