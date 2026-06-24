import { useState, useMemo, memo } from 'react';
import { VideoOff } from 'lucide-react';
import { Cameras } from '../lib/api';
import type { Camera, Tank } from '../lib/types';
import { FeedQuickControl } from './multitank/FeedQuickControl';
import { useLanguage } from '../lib/language-context';

interface Props {
  camera: Camera;
  tank?: Tank;          // 매핑된 수조 — 사료 quick 컨트롤용
}

// verified 카메라만 MJPEG 스트림 마운트 — GStreamer 오류 시 offline placeholder 로 자동 전환.
// memo + useMemo 로 부모 polling re-render 가 stream 을 reset 하지 않게 안정화.
function CameraTileImpl({ camera, tank }: Props) {
  const { tr } = useLanguage();
  const [error, setError] = useState(false);
  const isLive = camera.status === 'verified' && !error;

  // mount 시점 cacheBust 한 번만 — re-render 시 URL 변경 X (stream 유지)
  const src = useMemo(
    () => Cameras.mjpegURL(camera.camera_id, 'sub'),
    [camera.camera_id],
  );

  return (
    <div className="relative rounded-lg overflow-hidden bg-gray-900 border border-gray-700/40 group">
      {/* 16:9 aspect ratio wrapper */}
      <div className="relative w-full" style={{ paddingBottom: '56.25%' }}>
        {isLive ? (
          <img
            src={src}
            alt={camera.display_name}
            className="absolute inset-0 w-full h-full object-cover"
            onError={() => setError(true)}
          />
        ) : (
          /* 오프라인 / unverified / GStreamer 없음 — 회색 플레이스홀더 */
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-gray-800/80">
            <VideoOff className="w-6 h-6 text-gray-600" />
            <span className="text-xs text-gray-500 font-mono">
              {camera.status !== 'verified' ? tr('cameraTile.unverified') : tr('cameraTile.offline')}
            </span>
          </div>
        )}

        {/* LIVE 펄스 — verified 이고 에러 없을 때만 */}
        {isLive && (
          <div className="absolute top-2 left-2 flex items-center gap-1 bg-black/60 rounded px-1.5 py-0.5">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse" />
            <span className="text-xs text-white font-mono leading-none">LIVE</span>
          </div>
        )}

        {/* 상태 배지 */}
        <div className="absolute top-2 right-2">
          <span
            className={`text-xs px-1.5 py-0.5 rounded font-mono ${
              camera.status === 'verified'
                ? 'bg-green-500/80 text-black'
                : camera.status === 'error'
                ? 'bg-red-500/80 text-white'
                : 'bg-gray-600/80 text-gray-200'
            }`}
          >
            {camera.status}
          </span>
        </div>
      </div>

      {/* 캡션 — 카메라 이름 + 사료 시작/멈춤 토글 */}
      <div className="px-2 py-1.5 flex items-center gap-2">
        <span className="text-xs text-gray-400 truncate flex-1">
          {camera.position ?? '—'}
        </span>
        <span className="text-xs text-gray-600 font-mono truncate">{camera.camera_id}</span>
        {tank && <FeedQuickControl tank={tank} variant="mini" />}
      </div>
    </div>
  );
}

// shallow 비교 — camera 기본 필드 + tank.tank_id 변경 시 re-render.
export const CameraTile = memo(CameraTileImpl, (prev, next) =>
  prev.camera.camera_id === next.camera.camera_id &&
  prev.camera.status === next.camera.status &&
  prev.camera.display_name === next.camera.display_name &&
  prev.tank?.tank_id === next.tank?.tank_id,
);
