import { useCallback, useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../lib/language-context';
import { Cameras, Vision } from '../../lib/api';
import type {
  VisionTrainingStatus,
  Camera,
  VisionBootstrapBox,
} from '../../lib/types';

type BoxClass = 'fish' | 'food' | 'exclude';

const CLASS_COLORS: Record<BoxClass, string> = {
  fish: '#44d18a',
  food: '#ffd166',
  exclude: '#ff6b6b',
};

/**
 * 5/9 ai-training.js 의 ① 처음 알려주기 (박스 그리기) — React useRef + canvas.
 *
 * 사용자가 카메라 스냅샷에서 마우스 드래그 → 박스 → [💾 저장] → POST bootstrap labels.
 * 좌표는 normalized [0,1] 로 저장 (5/9 원본과 동일).
 */
export function BootstrapTab({
  status, onSaved,
}: {
  status: VisionTrainingStatus | null;
  onSaved: () => void;
}) {
  const { tr } = useLanguage();

  const imgRef = useRef<HTMLImageElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const drawingRef = useRef<{ startX: number; startY: number } | null>(null);

  const [cameras, setCameras] = useState<Camera[]>([]);
  const [cameraId, setCameraId] = useState<string>('');
  const [boxClass, setBoxClass] = useState<BoxClass>('fish');
  const [boxes, setBoxes] = useState<VisionBootstrapBox[]>([]);
  const [snapshotKey, setSnapshotKey] = useState<number>(Date.now());
  const [statusText, setStatusText] = useState<string>(() => tr('bootstrapTab.selectCameraPrompt'));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // 카메라 목록 로드 (5/9 loadCameras)
  useEffect(() => {
    (async () => {
      try {
        const res = await Cameras.list();
        const items = (res.items ?? []).filter(c => (c.status ?? '') !== 'disabled');
        setCameras(items);
        if (items.length > 0) {
          setCameraId(items[0].camera_id);
        }
      } catch (e) {
        setError(tr('bootstrapTab.errorLoadCameras') + (e instanceof Error ? e.message : 'unknown'));
      }
    })();
  }, []);

  // 카메라 변경 → 새 스냅샷 (5/9 loadSnapshot)
  useEffect(() => {
    if (!cameraId) {
      setStatusText(tr('bootstrapTab.selectCameraPrompt'));
      return;
    }
    setBoxes([]);
    setSnapshotKey(Date.now());
    setStatusText(tr('bootstrap.statusSnapshotLoaded', { cameraId }));
  }, [cameraId]);

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
      setBoxes(prev => [...prev, { x, y, w, h, class: boxClass }]);
    }
    drawingRef.current = null;
  }

  // 5/9 captureSnapshotJpeg
  function captureSnapshotJpeg(): string | null {
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
  }

  async function handleSave() {
    if (boxes.length === 0) {
      alert(tr('bootstrapTab.alertDrawBox'));
      return;
    }
    if (!cameraId) {
      alert(tr('bootstrapTab.alertSelectCamera'));
      return;
    }
    const image = captureSnapshotJpeg();
    if (!image) {
      alert(tr('bootstrapTab.alertSnapshotNotReady'));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const res = await Vision.saveBootstrapLabel({
        camera_id: cameraId,
        snapshot_ref: `${cameraId}_${Date.now()}`,
        operator_id: 'operator',
        boxes,
        image,
      });
      setStatusText(
        '✅ ' + tr('bootstrap.statusSaved', { count: boxes.length, sequence: res.sequence }),
      );
      setBoxes([]);
      onSaved();
    } catch (e) {
      setError(tr('bootstrapTab.errorSaveFailed') + (e instanceof Error ? e.message : 'unknown'));
    } finally {
      setSaving(false);
    }
  }

  function handleRefresh() {
    setBoxes([]);
    setSnapshotKey(Date.now());
    setStatusText(tr('bootstrap.statusNewSnapshot', { cameraId }));
  }

  function handleClear() {
    setBoxes([]);
  }

  const boot = status?.bootstrap;
  const countTag = boxes.length === 0
    ? tr('bootstrapTab.noBoxesYet')
    : tr('bootstrap.currentBoxes', { count: boxes.length });
  const totalBoxesLine = boot
    ? tr('aiStatus.progress', {
        boxes: boot.box_count,
        reqBoxes: boot.required_boxes,
        frames: boot.frame_count,
        reqFrames: boot.required_frames,
      })
    : '-';

  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-sm font-semibold text-gray-100">
          {tr('bootstrapTab.sectionTitle')}
        </h3>
        <span className="px-2 py-0.5 rounded text-xs bg-blue-500/20 text-blue-300 border border-blue-500/40">
          {countTag}
        </span>
      </div>

      {/* 안내문 */}
      <div className="p-3 bg-blue-900/15 border border-blue-500/30 rounded space-y-1 text-xs">
        <p className="text-sm font-semibold text-blue-200">{tr('bootstrapTab.howToTitle')}</p>
        <ol className="list-decimal list-inside text-gray-300 space-y-0.5 pl-1">
          <li>{tr('bootstrap.help1Pre')}<b>{tr('bootstrap.help1Bold')}</b>{tr('bootstrap.help1Post')}</li>
          <li>{tr('bootstrap.help2Pre')}<b>{tr('bootstrap.help2Bold')}</b>{tr('bootstrap.help2Post')}</li>
          <li><b>{tr('bootstrap.help3Bold1')}</b>{tr('bootstrap.help3Mid')}<b>{tr('bootstrap.help3Bold2')}</b>{tr('bootstrap.help3Post')}</li>
          <li>{tr('bootstrap.help4Pre')}<b>{tr('bootstrap.help4Bold')}</b>{tr('bootstrap.help4Post')}</li>
          <li><b>{tr('bootstrap.help5Bold')}</b>{tr('bootstrap.help5Post')}</li>
        </ol>
      </div>

      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-3 p-2.5 bg-gray-900/40 border border-gray-700/40 rounded">
        <label className="text-xs text-gray-400 flex items-center gap-1.5">
          {tr('bootstrapTab.labelCameraSelect')}
          <select
            value={cameraId}
            onChange={e => setCameraId(e.target.value)}
            className="px-2 py-1 text-sm bg-gray-900 border border-gray-600 rounded text-white [&>option]:bg-gray-900 [&>option]:text-white"
          >
            <option value="">{tr('bootstrapTab.optionSelectCamera')}</option>
            {cameras.map(c => (
              <option key={c.camera_id} value={c.camera_id}>
                {c.display_name ?? c.camera_id}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-gray-400 flex items-center gap-1.5">
          {tr('bootstrapTab.labelClass')}
          <select
            value={boxClass}
            onChange={e => setBoxClass(e.target.value as BoxClass)}
            className="px-2 py-1 text-sm bg-gray-900 border border-gray-600 rounded text-white [&>option]:bg-gray-900 [&>option]:text-white"
          >
            <option value="fish">{tr('bootstrapTab.classFish')}</option>
            <option value="food">{tr('bootstrapTab.classFood')}</option>
            <option value="exclude">{tr('bootstrapTab.classExclude')}</option>
          </select>
        </label>
        <button
          onClick={handleRefresh}
          className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 text-gray-200 transition-colors"
        >
          {tr('bootstrapTab.btnNextScene')}
        </button>
        <button
          onClick={handleClear}
          className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 text-gray-200 transition-colors"
        >
          {tr('bootstrapTab.btnClearBoxes')}
        </button>
        <button
          onClick={handleSave}
          disabled={saving || boxes.length === 0 || !cameraId}
          className="px-4 py-1.5 text-xs rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
        >
          {tr('bootstrapTab.btnSave')}
        </button>
      </div>

      {/* Canvas wrap */}
      <div className="relative bg-black border border-gray-700/40 rounded overflow-hidden" style={{ minHeight: 240 }}>
        {cameraId && (
          <img
            key={snapshotKey}
            ref={imgRef}
            src={Cameras.snapshotURL(cameraId, 'sub', snapshotKey)}
            alt={tr('bootstrapTab.altSnapshot')}
            className="block w-full h-auto"
            crossOrigin="anonymous"
            onLoad={() => redraw()}
          />
        )}
        <canvas
          ref={canvasRef}
          className="absolute inset-0 cursor-crosshair"
          onMouseDown={onMouseDown}
          onMouseMove={onMouseMove}
          onMouseUp={onMouseUp}
        />
      </div>

      {error && (
        <div className="px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
          {error}
        </div>
      )}

      <div className="text-xs text-gray-500">
        <span>{statusText}</span>
      </div>

      <div className="flex items-baseline justify-between text-xs pt-2 border-t border-gray-700/30">
        <b className="text-gray-300">{tr('bootstrapTab.totalBoxesLabel')}</b>
        <span className="text-gray-300 font-mono">{totalBoxesLine}</span>
      </div>
    </div>
  );
}
