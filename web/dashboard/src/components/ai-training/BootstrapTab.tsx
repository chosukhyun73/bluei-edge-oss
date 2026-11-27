import { useCallback, useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../lib/language-core';
import { Vision } from '../../lib/api';
import type {
  VisionTrainingStatus,
  VisionBootstrapBox,
} from '../../lib/types';
import {
  BoxLabelCanvas, BoxClassSelect,
  type BoxClass, type BoxLabelCanvasHandle,
} from './BoxLabelCanvas';

/**
 * 5/9 ai-training.js 의 ① 처음 알려주기 (박스 그리기).
 * 캔버스/클래스 셀렉터는 BoxLabelCanvas 공용 (③ 바닥 스틸 탭과 공유).
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

  const canvasRef = useRef<BoxLabelCanvasHandle>(null);

  // 저장된 clip 의 poster 프레임 1장을 라벨 대상으로 사용 (라이브 아님).
  // 실제 급이는 라이브로 못 보므로 저장 영상을 사후에 확인·라벨링한다.
  const [frame, setFrame] = useState<{ clipId: string; cameraId: string; tankId: string; offsetSec: number } | null>(null);
  const [boxClass, setBoxClass] = useState<BoxClass>('fish');
  const [boxes, setBoxes] = useState<VisionBootstrapBox[]>([]);
  const [statusText, setStatusText] = useState<string>(() => tr('bootstrapTab.loadingFrame'));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // 저장된 clip 에서 랜덤 프레임 로드 — 급이영상 우선(사후 확인 목적), 없으면 안정성 영상.
  // 시점 토글(수중/탑뷰): 7-05 에는 탑뷰 박스를 노이즈로 보고 수중 전용으로 제한했으나,
  // 2026-07-09 운영자 결정으로 개방 — "라벨링은 하나 안하나 일단 자료가 모여야" (탑뷰
  // 어체 마스킹 검출기 대비 축적). 시점별 라벨은 export 가 camera position 으로 분리
  // 저장하므로 서로 오염되지 않는다 (export_labels.py --camera-position).
  const [viewpoint, setViewpoint] = useState<'underwater' | 'top'>('underwater');
  const loadRandomFrame = useCallback(async () => {
    setError(null);
    try {
      // 서버가 셔플해 주므로 넉넉히 받아 선택 시점의 클립만 고른다.
      let pick = (await Vision.trainingPool('feeding', 50)).items.find(it =>
        it.payload.camera_id.includes(viewpoint));
      if (!pick) {
        pick = (await Vision.trainingPool('baseline', 50)).items.find(it =>
          it.payload.camera_id.includes(viewpoint));
      }
      if (!pick) {
        setFrame(null);
        setStatusText(tr('bootstrapTab.noUnderwaterClips'));
        return;
      }
      const p = pick.payload;
      // 클립 내 랜덤 시점 — 포스터(녹화 시작 고정 1장)만 쓰면 같은 장면이 반복돼
      // 프레임 다양성이 안 나온다 (2026-07-17). 가장자리 2초는 피한다.
      let offsetSec = 0;
      const durSec = (Date.parse(p.ended_at) - Date.parse(p.started_at)) / 1000;
      if (Number.isFinite(durSec) && durSec > 6) {
        offsetSec = Math.round((2 + Math.random() * (durSec - 4)) * 10) / 10;
      }
      setBoxes([]);
      setFrame({ clipId: p.clip_id, cameraId: p.camera_id, tankId: p.tank_id ?? '', offsetSec });
      setStatusText(tr('bootstrap.statusFrameLoaded', { tank: p.tank_id ?? '', cam: p.camera_id }));
    } catch (e) {
      setError(tr('bootstrapTab.errorLoadFrame') + (e instanceof Error ? e.message : 'unknown'));
    }
  }, [tr, viewpoint]);

  // 최초 진입 시 랜덤 프레임 로드.
  useEffect(() => {
    void loadRandomFrame();
  }, [loadRandomFrame]);

  async function handleSave() {
    if (boxes.length === 0) {
      alert(tr('bootstrapTab.alertDrawBox'));
      return;
    }
    if (!frame) {
      alert(tr('bootstrapTab.noClipsYet'));
      return;
    }
    const image = canvasRef.current?.captureJpeg();
    if (!image) {
      alert(tr('bootstrapTab.alertSnapshotNotReady'));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const res = await Vision.saveBootstrapLabel({
        camera_id: frame.cameraId,
        snapshot_ref: frame.clipId,
        origin_clip_id: frame.clipId,
        frame_offset_sec: frame.offsetSec,
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
    void loadRandomFrame();
  }

  // 최근에 그린 박스 1개만 제거 (undo). 전체 삭제 아님.
  function handleClear() {
    setBoxes(prev => prev.slice(0, -1));
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
        <span className="text-xs text-gray-400 font-mono">
          {frame ? `📹 ${frame.tankId} · ${frame.cameraId}` : tr('bootstrapTab.noClipsYet')}
        </span>
        <div className="flex rounded overflow-hidden border border-gray-600/60">
          {(['underwater', 'top'] as const).map(vp => (
            <button
              key={vp}
              onClick={() => setViewpoint(vp)}
              className={`px-2.5 py-1 text-xs transition-colors ${
                viewpoint === vp
                  ? 'bg-cyan-700 text-white'
                  : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
              }`}
            >
              {vp === 'underwater' ? tr('bootstrapTab.viewUnderwater') : tr('bootstrapTab.viewTop')}
            </button>
          ))}
        </div>
        <BoxClassSelect value={boxClass} onChange={setBoxClass} />
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
          disabled={saving || boxes.length === 0 || !frame}
          className="px-4 py-1.5 text-xs rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
        >
          {tr('bootstrapTab.btnSave')}
        </button>
      </div>

      {/* Canvas */}
      {frame ? (
        <BoxLabelCanvas
          ref={canvasRef}
          src={`/v1/media/clips/${encodeURIComponent(frame.clipId)}/poster.jpg${frame.offsetSec > 0 ? `?offset=${frame.offsetSec}` : ''}`}
          imgKey={`${frame.clipId}@${frame.offsetSec}`}
          alt={tr('bootstrapTab.altSnapshot')}
          boxClass={boxClass}
          boxes={boxes}
          onAddBox={box => setBoxes(prev => [...prev, box])}
        />
      ) : (
        <div className="bg-black border border-gray-700/40 rounded" style={{ minHeight: 240 }} />
      )}

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
