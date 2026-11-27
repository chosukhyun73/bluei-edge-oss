import { useState, useEffect } from 'react';
import { X } from 'lucide-react';
import { Cameras } from '../lib/api';
import type { Camera, Tank, CommonCameraArea } from '../lib/types';
import { cameraViewScope, cameraCommonArea, COMMON_CAMERA_AREA_OPTIONS } from '../lib/types';
import { CameraTile } from './CameraTile';
import { FeedQuickControl } from './multitank/FeedQuickControl';
import { useLanguage } from '../lib/language-core';
import { useSnapshotPoll } from '../lib/use-snapshot-poll';

export type CameraGridColumns = 1 | 2 | 4;

interface Props {
  tanks: Tank[];
  // 운영자가 선택한 컬럼 수. 미지정 시 2 (default).
  columns?: CameraGridColumns;
}

// columns → Tailwind grid-cols-X 매핑. md 미만은 자동 1열 (좁은 창 대응).
const COLS_CLASS: Record<CameraGridColumns, string> = {
  1: 'grid-cols-1',
  2: 'grid-cols-1 md:grid-cols-2',
  4: 'grid-cols-1 md:grid-cols-2 2xl:grid-cols-4',
};

// tank_id 직접 필드 우선, 없으면 camera_id 에서 /tank_([^_]+)/ 패턴으로 추출
function resolveTankId(camera: Camera): string | undefined {
  if (camera.tank_id) return camera.tank_id;
  const m = camera.camera_id.match(/tank_([^_]+)/);
  return m ? `tank_${m[1]}` : undefined;
}

export function CameraGrid({ tanks, columns = 2 }: Props) {
  const { tr } = useLanguage();
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<Camera | null>(null);

  useEffect(() => {
    Cameras.list()
      .then(res => setCameras(res.items))
      .catch(() => setCameras([]))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="py-6 text-center text-sm text-gray-500 font-mono">{tr('cameraGrid.loading')}</div>
    );
  }

  if (cameras.length === 0) {
    return (
      <div className="py-6 text-center text-sm text-gray-500 italic">
        {tr('cameraGrid.empty')}
      </div>
    );
  }

  // 탱크별 그룹핑 — 탱크 순서를 group tanks 배열 순서로 고정
  const tankIds = tanks.map(t => t.tank_id);
  const grouped = new Map<string, Camera[]>();
  const common: Camera[] = [];     // 수중 등 탑뷰가 아닌 카메라 — 수조 셀에서 빼서 공통 영역에 표시
  const ungrouped: Camera[] = [];

  for (const cam of cameras) {
    // 공통(수조 비종속) 카메라는 공통 영역으로 분리 → 수조 셀은 탑뷰로 채워지고
    // 수중·수처리·보안 등 공통 카메라는 구역별로 한 곳에 모인다.
    if (cameraViewScope(cam) === 'common') {
      common.push(cam);
      continue;
    }
    const tid = resolveTankId(cam);
    if (tid && tankIds.includes(tid)) {
      if (!grouped.has(tid)) grouped.set(tid, []);
      grouped.get(tid)!.push(cam);
    } else {
      ungrouped.push(cam);
    }
  }

  // 공통 카메라를 구역(수중/수처리/보안/기타)별로 묶는다.
  const commonByArea = new Map<CommonCameraArea, Camera[]>();
  for (const cam of common) {
    const area = cameraCommonArea(cam);
    if (!commonByArea.has(area)) commonByArea.set(area, []);
    commonByArea.get(area)!.push(cam);
  }

  const tankDisplayName = (tid: string) =>
    tanks.find(t => t.tank_id === tid)?.display_name ?? tid;

  return (
    <div className="space-y-4">
      {/* 공통(수조 비종속) 카메라 — 수조 셀에서 빼서 상단에 구역별로 모아 표시 */}
      {common.length > 0 && (
        <div className="space-y-3">
          <div className="text-xs text-gray-400 font-mono">{tr('cameraGrid.commonCameras')}</div>
          {COMMON_CAMERA_AREA_OPTIONS.filter(o => commonByArea.has(o.value)).map(o => (
            <div key={o.value}>
              <div className="text-xs text-gray-500 font-mono mb-2">{tr(o.label)}</div>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                {commonByArea.get(o.value)!.map(cam => (
                  <CameraTile key={cam.camera_id} camera={cam} onExpand={() => setExpanded(cam)} />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {/* 탱크들을 옆으로. 컬럼 수는 운영자가 선택 (Props.columns: 1|2|4).
          좁은 창 (< md) 은 자동 1열로 떨어짐.
          gap-3 = SensorMatrix 와 통일. */}
      <div className={`grid ${COLS_CLASS[columns]} gap-3`}>
        {tankIds.map(tid => {
          const cams = grouped.get(tid);
          if (!cams || cams.length === 0) return null;
          return (
            <div key={tid} className="min-w-0">
              <div className="text-xs text-gray-500 font-mono mb-2">{tankDisplayName(tid)}</div>
              {/* 카메라가 1개면 부모 wrapper 폭 100%, 2개+ 면 2 cols (탑뷰+측면 등). */}
              <div className={cams.length > 1 ? 'grid grid-cols-1 sm:grid-cols-2 gap-3' : ''}>
                {cams.map(cam => (
                  <CameraTile
                    key={cam.camera_id}
                    camera={cam}
                    tank={tanks.find(t => t.tank_id === resolveTankId(cam))}
                    onExpand={() => setExpanded(cam)}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>
      {ungrouped.length > 0 && (
        <div>
          <div className="text-xs text-gray-500 font-mono mb-2">{tr('cameraGrid.ungrouped')}</div>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {ungrouped.map(cam => (
              <CameraTile key={cam.camera_id} camera={cam} onExpand={() => setExpanded(cam)} />
            ))}
          </div>
        </div>
      )}

      {expanded && (
        <CameraModal
          camera={expanded}
          tank={tanks.find(t => t.tank_id === resolveTankId(expanded))}
          onClose={() => setExpanded(null)}
        />
      )}
    </div>
  );
}

// 단독 확대 뷰 — 영상 클릭 시 전체화면 오버레이. 배경/Esc/X 로 닫기.
function CameraModal({ camera, tank, onClose }: { camera: Camera; tank?: Tank; onClose: () => void }) {
  // 확대 뷰는 snapshot 폴링 100ms(≈10fps) — 단발요청이라 연결 점유/누수 없이 라이브.
  const { online, dims, canvasRef } = useSnapshotPoll(camera.camera_id, true, {
    intervalMs: 100,
  });
  // 확대 표시는 실제 입력 해상도의 2배(대각선 2배)로. 소스가 저해상(320~640px)이라
  // 그대로 두면 그리드 타일과 크기가 비슷해 "확대 안 됨"처럼 보임. viewport 초과 시 CSS max로 축소.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/90 p-4"
      onClick={onClose}
    >
      <div className="relative max-w-[96vw] max-h-[96vh]" onClick={e => e.stopPropagation()}>
        <button
          onClick={onClose}
          className="absolute -top-3 -right-3 z-10 w-9 h-9 rounded-full bg-gray-800 border border-gray-600 text-white flex items-center justify-center hover:bg-gray-700"
          aria-label="close"
        >
          <X className="w-5 h-5" />
        </button>
        <div className="relative">
          <canvas
            ref={canvasRef}
            aria-label={camera.display_name}
            style={dims ? { width: dims.w * 2, height: dims.h * 2 } : undefined}
            className={`max-w-[96vw] max-h-[92vh] object-contain rounded-lg bg-black transition-opacity ${
              online ? 'opacity-100' : 'opacity-40'
            }`}
          />
          {!online && (
            <div className="absolute inset-0 flex items-center justify-center text-gray-400 font-mono">
              connecting…
            </div>
          )}
        </div>
        <div className="mt-2 text-center text-sm text-gray-300 font-mono">
          {camera.display_name} · {camera.camera_id}
        </div>
        {/* 이 카메라가 수조에 매핑돼 있으면 사이클 급이 컨트롤(시작=마이크로펄스 사이클, 정지=전체 정지).
            원시 run/stop 컨트롤은 사이클 재점화 혼동으로 제거(2026-07-04) — 정비용 원시 제어는 API 로만. */}
        {tank && (
          <div className="mt-2 max-w-md mx-auto" onClick={e => e.stopPropagation()}>
            <FeedQuickControl tank={tank} variant="compact" immediate />
          </div>
        )}
      </div>
    </div>
  );
}
