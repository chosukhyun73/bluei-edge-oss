import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef } from 'react';
import { useLanguage } from '../../lib/language-core';
import type { VisionBootstrapBox } from '../../lib/types';

export type BoxClass = VisionBootstrapBox['class'];

const CLASS_COLORS: Record<BoxClass, string> = {
  fish: '#44d18a',
  fish_abnormal: '#c471ed', // 이상 어체(기형·질병) — 보라
  food: '#ffd166',
  pellet: '#e8590c', // 바닥 잔류 펠릿(진한색) — 진한 주황
  feces: '#a0703c', // 배설물 — 갈색
  exclude: '#ff6b6b',
};

const CLASS_LABEL_KEYS: Record<BoxClass, string> = {
  fish: 'bootstrapTab.classFish',
  fish_abnormal: 'bootstrapTab.classFishAbnormal',
  food: 'bootstrapTab.classFood',
  pellet: 'bootstrapTab.classPellet',
  feces: 'bootstrapTab.classFeces',
  exclude: 'bootstrapTab.classExclude',
};

/** 박스 클래스 셀렉터 — ① 부트스트랩 탭과 ③ 바닥 스틸 탭 공용. */
export function BoxClassSelect({
  value, onChange,
}: {
  value: BoxClass;
  onChange: (c: BoxClass) => void;
}) {
  const { tr } = useLanguage();
  return (
    <label className="text-xs text-gray-400 flex items-center gap-1.5">
      {tr('bootstrapTab.labelClass')}
      <select
        value={value}
        onChange={e => onChange(e.target.value as BoxClass)}
        className="px-2 py-1 text-sm bg-gray-900 border border-gray-600 rounded text-white [&>option]:bg-gray-900 [&>option]:text-white"
      >
        {(Object.keys(CLASS_LABEL_KEYS) as BoxClass[]).map(c => (
          <option key={c} value={c}>{tr(CLASS_LABEL_KEYS[c])}</option>
        ))}
      </select>
    </label>
  );
}

export type BoxLabelCanvasHandle = {
  /** 라벨 시점의 원본 프레임을 JPEG data URL 로 캡처. CORS taint 시 null. */
  captureJpeg(): string | null;
};

/**
 * 이미지 + 박스 드로잉 캔버스 — 5/9 ai-training.js 의 박스 그리기를 공용화.
 * (yolo-lrcn-fusion 설계 3.2: ① 부트스트랩 탭과 ③ 바닥 스틸 탭이 같은 캔버스 재사용.)
 *
 * 마우스 드래그 → normalized [0,1] 박스 → onAddBox. 박스 상태는 부모 소유.
 */
export const BoxLabelCanvas = forwardRef<BoxLabelCanvasHandle, {
  src: string;
  /** 이미지 교체 시 리마운트용 key (clip_id / still_id). */
  imgKey: string;
  alt: string;
  boxClass: BoxClass;
  boxes: VisionBootstrapBox[];
  onAddBox: (box: VisionBootstrapBox) => void;
}>(function BoxLabelCanvas({ src, imgKey, alt, boxClass, boxes, onAddBox }, ref) {
  const imgRef = useRef<HTMLImageElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const drawingRef = useRef<{ startX: number; startY: number } | null>(null);

  // canvas 크기 동기화 + 박스 그리기 (5/9 redrawCanvas)
  const redraw = useCallback(() => {
    const img = imgRef.current;
    const cv = canvasRef.current;
    if (!img || !cv) return;
    const w = img.clientWidth || img.naturalWidth;
    const h = img.clientHeight || img.naturalHeight;
    cv.width = w;
    cv.height = h;
    cv.style.width = `${w}px`;
    cv.style.height = `${h}px`;
    const ctx = cv.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, w, h);
    for (const box of boxes) {
      drawBox(ctx, box, w, h);
    }
  }, [boxes]);

  // 박스 또는 스냅샷 변경 시 redraw
  useEffect(() => {
    redraw();
    const onResize = () => redraw();
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [redraw]);

  function drawBox(
    ctx: CanvasRenderingContext2D,
    box: VisionBootstrapBox,
    W: number, H: number,
  ) {
    const color = CLASS_COLORS[box.class];
    ctx.lineWidth = 3;
    ctx.strokeStyle = color;
    ctx.fillStyle = color + '33';
    ctx.fillRect(box.x * W, box.y * H, box.w * W, box.h * H);
    ctx.strokeRect(box.x * W, box.y * H, box.w * W, box.h * H);
    ctx.fillStyle = color;
    ctx.font = 'bold 14px Inter, sans-serif';
    ctx.fillText(box.class, box.x * W + 4, box.y * H + 16);
  }

  function canvasMouse(ev: React.MouseEvent<HTMLCanvasElement>) {
    const cv = canvasRef.current;
    if (!cv) return { x: 0, y: 0 };
    const rect = cv.getBoundingClientRect();
    const x = (ev.clientX - rect.left) / rect.width;
    const y = (ev.clientY - rect.top) / rect.height;
    return {
      x: Math.max(0, Math.min(1, x)),
      y: Math.max(0, Math.min(1, y)),
    };
  }

  function onMouseDown(ev: React.MouseEvent<HTMLCanvasElement>) {
    const p = canvasMouse(ev);
    drawingRef.current = { startX: p.x, startY: p.y };
  }

  function onMouseMove(ev: React.MouseEvent<HTMLCanvasElement>) {
    if (!drawingRef.current) return;
    const p = canvasMouse(ev);
    const cv = canvasRef.current;
    if (!cv) return;
    redraw();
    const ctx = cv.getContext('2d');
    if (!ctx) return;
    const W = cv.width, H = cv.height;
    const x = Math.min(drawingRef.current.startX, p.x);
    const y = Math.min(drawingRef.current.startY, p.y);
    const w = Math.abs(p.x - drawingRef.current.startX);
    const h = Math.abs(p.y - drawingRef.current.startY);
    drawBox(ctx, { x, y, w, h, class: boxClass }, W, H);
  }

  function onMouseUp(ev: React.MouseEvent<HTMLCanvasElement>) {
    if (!drawingRef.current) return;
    const p = canvasMouse(ev);
    const x = Math.min(drawingRef.current.startX, p.x);
    const y = Math.min(drawingRef.current.startY, p.y);
    const w = Math.abs(p.x - drawingRef.current.startX);
    const h = Math.abs(p.y - drawingRef.current.startY);
    if (w > 0.005 && h > 0.005) {
      onAddBox({ x, y, w, h, class: boxClass });
    }
    drawingRef.current = null;
  }

  // 5/9 captureSnapshotJpeg
  useImperativeHandle(ref, () => ({
    captureJpeg() {
      const img = imgRef.current;
      if (!img || !img.complete || img.naturalWidth === 0) return null;
      const cv = document.createElement('canvas');
      cv.width = img.naturalWidth;
      cv.height = img.naturalHeight;
      const ctx = cv.getContext('2d');
      if (!ctx) return null;
      try {
        ctx.drawImage(img, 0, 0);
        return cv.toDataURL('image/jpeg', 0.85);
      } catch {
        // canvas tainted (CORS) — backend 가 snapshot_ref 만으로 처리
        return null;
      }
    },
  }));

  return (
    <div className="relative bg-black border border-gray-700/40 rounded overflow-hidden" style={{ minHeight: 240 }}>
      <img
        key={imgKey}
        ref={imgRef}
        src={src}
        alt={alt}
        className="block w-full h-auto"
        crossOrigin="anonymous"
        onLoad={() => {
          // 과확대 상한 = 원본 4배 (운영자 2026-07-16). 탑뷰 크롭 프레임(~320px 폭)이
          // 컨테이너 전체로 늘어나며 뭉개지던 것 방지. 수중(640px)은 사실상 영향 없음.
          // ⚠ 중앙정렬 금지 — 캔버스가 wrapper 좌상단 기준이라 이미지도 좌상단 고정이어야 정렬.
          const img = imgRef.current;
          if (img && img.naturalWidth > 0) {
            img.style.maxWidth = `${img.naturalWidth * 4}px`;
          }
          redraw();
        }}
      />
      <canvas
        ref={canvasRef}
        className="absolute inset-0 cursor-crosshair"
        onMouseDown={onMouseDown}
        onMouseMove={onMouseMove}
        onMouseUp={onMouseUp}
      />
    </div>
  );
});
