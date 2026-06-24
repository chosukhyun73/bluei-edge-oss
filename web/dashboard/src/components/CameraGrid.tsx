import { useState, useEffect } from 'react';
import { Cameras } from '../lib/api';
import type { Camera, Tank } from '../lib/types';
import { CameraTile } from './CameraTile';
import { useLanguage } from '../lib/language-context';

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
  const ungrouped: Camera[] = [];

  for (const cam of cameras) {
    const tid = resolveTankId(cam);
    if (tid && tankIds.includes(tid)) {
      if (!grouped.has(tid)) grouped.set(tid, []);
      grouped.get(tid)!.push(cam);
    } else {
      ungrouped.push(cam);
    }
  }

  const tankDisplayName = (tid: string) =>
    tanks.find(t => t.tank_id === tid)?.display_name ?? tid;

  return (
    <div className="space-y-4">
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
              <CameraTile key={cam.camera_id} camera={cam} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
