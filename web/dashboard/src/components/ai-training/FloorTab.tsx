import { useCallback, useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../lib/language-core';
import { Vision, floorStillImageURL } from '../../lib/api';
import type { FloorStillItem } from '../../lib/api';
import type { VisionBootstrapBox } from '../../lib/types';
import { Skeleton } from '../ui/skeleton';
import {
  BoxLabelCanvas, BoxClassSelect,
  type BoxClass, type BoxLabelCanvasHandle,
} from './BoxLabelCanvas';

/**
 * ③ 바닥 상태 (수중 스틸) — 박스 라벨링 (yolo-lrcn-fusion 설계 3.2, 2026-07-08).
 *
 * (구) 4버튼 이미지 분류는 위치 정보가 없어 YOLO 학습에 쓸 수 없어 폐기 (운영자 확정).
 * 스틸을 클릭해 ① 탭과 같은 캔버스로 pellet(잔류 펠릿)·feces(배설물) 등 박스를 그리면
 * 기존 bootstrap 라벨 풀에 합류한다 — 라벨 1종, 학습 경로 1개.
 */
export function FloorTab({ operatorId }: { operatorId?: string }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<FloorStillItem[]>([]);
  const [totals, setTotals] = useState({ stills: 0, labeled: 0 });
  const [unlabeledOnly, setUnlabeledOnly] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // 라벨링 대상으로 선택된 스틸 (캔버스 편집 상태).
  const [selected, setSelected] = useState<FloorStillItem | null>(null);
  const [boxClass, setBoxClass] = useState<BoxClass>('pellet');
  const [boxes, setBoxes] = useState<VisionBootstrapBox[]>([]);
  const [saving, setSaving] = useState(false);
  const canvasRef = useRef<BoxLabelCanvasHandle>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await Vision.floorStills({ unlabeled: unlabeledOnly, limit: 60 });
      setItems(res.items);
      setTotals({ stills: res.total_stills, labeled: res.total_labeled });
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [unlabeledOnly]);

  useEffect(() => {
    setLoading(true);
    void refresh();
  }, [refresh]);

  function select(item: FloorStillItem) {
    setSelected(item);
    setBoxes([]);
  }

  async function handleSave() {
    if (!selected) return;
    if (boxes.length === 0) {
      alert(tr('bootstrapTab.alertDrawBox'));
      return;
    }
    const image = canvasRef.current?.captureJpeg();
    if (!image) {
      alert(tr('bootstrapTab.alertSnapshotNotReady'));
      return;
    }
    setSaving(true);
    try {
      await Vision.saveBootstrapLabel({
        camera_id: selected.camera_id,
        tank_id: selected.tank_id,
        snapshot_ref: selected.still_id,
        origin_still_id: selected.still_id,
        operator_id: operatorId || 'operator',
        boxes,
        image,
      });
      setSelected(null);
      setBoxes([]);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  if (loading && items.length === 0 && !selected) {
    return <Skeleton className="h-40 w-full" />;
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-gray-400">{tr('floorTab.hint')}</p>
        <div className="flex items-center gap-3 shrink-0">
          <span className="text-xs text-gray-500 font-mono">
            {tr('floorTab.progress', { labeled: totals.labeled, total: totals.stills })}
          </span>
          <label className="flex items-center gap-1.5 text-xs text-gray-300 cursor-pointer">
            <input
              type="checkbox"
              checked={unlabeledOnly}
              onChange={e => setUnlabeledOnly(e.target.checked)}
              className="accent-green-500"
            />
            {tr('floorTab.unlabeledOnly')}
          </label>
        </div>
      </div>

      {error && (
        <div className="px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
          {error}
        </div>
      )}

      {/* 선택된 스틸 — 박스 라벨링 캔버스 (① 탭과 공용) */}
      {selected && (
        <div className="space-y-2 p-3 bg-gray-900/40 border border-gray-700/50 rounded">
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-xs text-gray-400 font-mono">
              🖼 {selected.tank_id} · {tr(`floorTab.trigger_${selected.trigger}`)} ·{' '}
              {selected.captured_at.replace('T', ' ').slice(5, 16)}
            </span>
            <BoxClassSelect value={boxClass} onChange={setBoxClass} />
            <button
              onClick={() => setBoxes(prev => prev.slice(0, -1))}
              className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 text-gray-200 transition-colors"
            >
              {tr('bootstrapTab.btnClearBoxes')}
            </button>
            <button
              onClick={handleSave}
              disabled={saving || boxes.length === 0}
              className="px-4 py-1.5 text-xs rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
            >
              {tr('bootstrapTab.btnSave')}
            </button>
            <button
              onClick={() => { setSelected(null); setBoxes([]); }}
              className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 text-gray-200 transition-colors"
            >
              {tr('floorTab.btnClose')}
            </button>
          </div>
          <BoxLabelCanvas
            ref={canvasRef}
            src={floorStillImageURL(selected.still_id)}
            imgKey={selected.still_id}
            alt={selected.still_id}
            boxClass={boxClass}
            boxes={boxes}
            onAddBox={box => setBoxes(prev => [...prev, box])}
          />
        </div>
      )}

      {items.length === 0 ? (
        <div className="py-10 text-center text-sm text-gray-500">
          {unlabeledOnly ? tr('floorTab.emptyUnlabeled') : tr('floorTab.empty')}
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 3xl:grid-cols-6 gap-3">
          {items.map(item => (
            <button
              key={item.still_id}
              onClick={() => select(item)}
              className={
                'rounded border bg-gray-900/40 overflow-hidden text-left transition-colors ' +
                (selected?.still_id === item.still_id
                  ? 'border-green-500/60'
                  : 'border-gray-700/50 hover:border-gray-500')
              }
            >
              <img
                src={floorStillImageURL(item.still_id)}
                alt={item.still_id}
                loading="lazy"
                className="w-full aspect-video object-cover bg-black"
              />
              <div className="p-2 flex items-center justify-between text-[11px] text-gray-500 font-mono">
                <span>
                  {item.tank_id} · {tr(`floorTab.trigger_${item.trigger}`)}
                </span>
                <span className="flex items-center gap-2">
                  {item.box_labeled && (
                    <span className="text-green-400">{tr('floorTab.labeledBadge')}</span>
                  )}
                  {item.captured_at.replace('T', ' ').slice(5, 16)}
                </span>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
