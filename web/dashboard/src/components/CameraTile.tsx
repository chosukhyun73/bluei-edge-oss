import { memo } from 'react';
import { VideoOff, Maximize2 } from 'lucide-react';
import type { Camera, Tank } from '../lib/types';
import { FeedQuickControl } from './multitank/FeedQuickControl';
import { useLanguage } from '../lib/language-core';
import { useSnapshotPoll } from '../lib/use-snapshot-poll';

interface Props {
  camera: Camera;
  tank?: Tank;          // 매핑된 수조 — 사료 quick 컨트롤용
  onExpand?: () => void; // 영상 클릭 시 단독 확대 뷰 열기
}

// verified 카메라만 라이브 스트림 표시 — snapshot 폴링(useSnapshotPoll)으로 최신 프레임을
// 주기 갱신한다. MJPEG 지속연결을 쓰지 않으므로 다중 타일에서도 브라우저 연결 풀을 점유/누수하지
// 않는다. 일시 실패 시 자동 재시도(placeholder 표시)로 라이브를 유지한다.
// memo 로 부모 polling re-render 가 stream 을 reset 하지 않게 안정화.
function CameraTileImpl({ camera, tank, onExpand }: Props) {
  const { tr } = useLanguage();
  const verified = camera.status === 'verified';
  const { online, canvasRef } = useSnapshotPoll(camera.camera_id, verified, {
    intervalMs: 200, // 5fps 그리드 표시 — 짧은 단발요청이라 연결을 점유하지 않아 프리즈 안전
  });
  const isLive = verified && online;

  // 크롭된 탑뷰는 세로로 긴(≈2:3) 프레임 → 타일도 세로 비율로 두고 object-contain 으로
  // 전체 높이를 잘림 없이 표시. 미크롭(수중/전체 탑뷰)은 4:3.
  const meta = camera.metadata as Record<string, unknown> | undefined;
  const cropped = !!meta && ((Number(meta.crop_left) || 0) > 0 || (Number(meta.crop_right) || 0) > 0);
  const padBottom = cropped ? '150%' : '75%';

  return (
    <div className="relative rounded-lg overflow-hidden bg-gray-900 border border-gray-700/40 group">
      {/* 16:9 aspect ratio wrapper — live 일 때 클릭하면 단독 확대 뷰 */}
      <div
        className={`relative w-full bg-black ${verified && onExpand ? 'cursor-pointer' : ''}`}
        style={{ paddingBottom: padBottom }}
        onClick={verified && onExpand ? onExpand : undefined}
      >
        {verified ? (
          <>
            {/* 스냅샷 폴링 — 훅이 blob→ImageBitmap 을 canvas 에 직접 그림(WebKit 이미지캐시 우회). */}
            <canvas
              ref={canvasRef}
              aria-label={camera.display_name}
              className={`absolute inset-0 w-full h-full object-contain transition-opacity ${
                online ? 'opacity-100' : 'opacity-0'
              }`}
            />
            {/* 최초 로딩 / 일시 연결 실패 — 회색 플레이스홀더(폴링은 계속 재시도 중) */}
            {!online && (
              <div className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-gray-800/80">
                <VideoOff className="w-6 h-6 text-gray-600" />
                <span className="text-xs text-gray-500 font-mono">{tr('cameraTile.offline')}</span>
              </div>
            )}
          </>
        ) : (
          /* unverified — 회색 플레이스홀더 */
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-gray-800/80">
            <VideoOff className="w-6 h-6 text-gray-600" />
            <span className="text-xs text-gray-500 font-mono">{tr('cameraTile.unverified')}</span>
          </div>
        )}

        {/* LIVE 펄스 — verified 이고 스냅샷 수신 중일 때만 */}
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

        {/* 확대 힌트 — verified 이고 클릭 가능할 때 hover 시 표시 */}
        {verified && onExpand && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/0 group-hover:bg-black/30 opacity-0 group-hover:opacity-100 transition-all pointer-events-none">
            <Maximize2 className="w-7 h-7 text-white/90 drop-shadow" />
          </div>
        )}
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
