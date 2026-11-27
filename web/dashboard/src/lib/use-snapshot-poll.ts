import { useState, useRef, useEffect } from 'react';
import { Cameras } from './api';

// 카메라 라이브 표시를 위한 스냅샷 폴링 훅.
//
// MJPEG(live.mjpeg)는 하나의 HTTP 연결을 무한히 붙잡는 스트림이라, 카메라 타일이
// 여러 개면 브라우저의 호스트당 연결 한계(~6)를 소진해 이후 모든 fetch 가 큐에서
// 멈춘다(대시보드 전체 로딩 hang). 대신 snapshot.jpg 를 주기적으로 단발 요청한다.
// 폴링은 fetch 완료가 구동한다(요청당 최대 1개 in-flight 보장, 누적 방지).
//
// ⚠ 프레임 표시는 <img> 가 아니라 <canvas> 직접 그리기다:
//   fetch → blob → createImageBitmap → drawImage → bitmap.close()
// 1차(?t= URL 을 img.src 에): URL 마다 디코드 캐시 누적 → ~180MB/분 누수.
// 2차(blob objectURL + revoke): revoke 해도 WebKitGTK 가 objectURL 별 디코드
// 비트맵을 실질 보유 — 렌더러가 2시간에 16.5GB 도달, 자체 kill threshold(16GB)로
// 사망 (2026-07-05 실측, "Unable to shrink memory footprint" 로그).
// canvas + ImageBitmap.close() 는 URL/이미지 캐시를 아예 경유하지 않아 프레임
// 수명이 명시적이다.
//
// ⚠ 2d 컨텍스트는 반드시 {willReadFrequently:true} (software canvas 강제):
// WebKitGTK 의 GPU 가속 2D canvas 는 drawImage 마다 백킹 버퍼를 잔류시킨다
// (2026-07-09 격리 실측: 소스가 ImageBitmap 이든 HTMLImageElement 든 동일하게
// 프레임당 디코드 크기만큼 누적, 30fps 에서 917MB/분 — 주간 대시보드 +20MB/분
// 누수의 진범. software canvas 는 동일 조건 6MB/분으로 GC 노이즈 수준).
export function useSnapshotPoll(
  cameraId: string,
  enabled: boolean,
  opts?: { profile?: 'main' | 'sub'; intervalMs?: number },
) {
  const profile = opts?.profile ?? 'sub';
  const intervalMs = opts?.intervalMs ?? 1000;

  const [online, setOnline] = useState(false);
  // 프레임 원본 크기 — 소비자(확대 뷰)의 표시 배율 계산용. 크기 변화 시에만 갱신.
  const [dims, setDims] = useState<{ w: number; h: number } | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    if (!enabled) {
      setOnline(false);
      return;
    }
    let disposed = false;
    let errCount = 0;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const ctrl = new AbortController();

    const drawBitmap = (bmp: ImageBitmap) => {
      const canvas = canvasRef.current;
      if (!canvas) return;
      if (canvas.width !== bmp.width || canvas.height !== bmp.height) {
        canvas.width = bmp.width;
        canvas.height = bmp.height;
        setDims({ w: bmp.width, h: bmp.height });
      }
      canvas.getContext('2d', { willReadFrequently: true })?.drawImage(bmp, 0, 0);
    };

    // 폴백(createImageBitmap 미지원): objectURL + Image — draw 직후 즉시 revoke.
    const drawViaImage = async (blob: Blob) => {
      const url = URL.createObjectURL(blob);
      try {
        const img = new Image();
        await new Promise<void>((resolve, reject) => {
          img.onload = () => resolve();
          img.onerror = () => reject(new Error('decode'));
          img.src = url;
        });
        if (disposed) return;
        const canvas = canvasRef.current;
        if (!canvas) return;
        if (canvas.width !== img.naturalWidth || canvas.height !== img.naturalHeight) {
          canvas.width = img.naturalWidth;
          canvas.height = img.naturalHeight;
          setDims({ w: img.naturalWidth, h: img.naturalHeight });
        }
        canvas.getContext('2d', { willReadFrequently: true })?.drawImage(img, 0, 0);
        img.src = '';
      } finally {
        URL.revokeObjectURL(url);
      }
    };

    const schedule = (delay: number) => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => void fetchOnce(), delay);
    };

    const fetchOnce = async () => {
      try {
        // cache:'no-store' + 고정 URL(t=0) — ?t= 유니크 URL 폴링은 WebKit 렌더러
        // MemoryCache 가 응답을 URL 별로 무한 축적(+160MB/분, canvas 전환 후에도 지속
        // 실측 2026-07-05). no-store 는 캐시 조회/저장 자체를 우회한다.
        const res = await fetch(Cameras.snapshotURL(cameraId, profile, 0), {
          signal: ctrl.signal,
          cache: 'no-store',
        });
        if (!res.ok) throw new Error(`snapshot ${res.status}`);
        const blob = await res.blob();
        if (disposed) return;
        if (typeof createImageBitmap === 'function') {
          const bmp = await createImageBitmap(blob);
          if (disposed) {
            bmp.close();
            return;
          }
          drawBitmap(bmp);
          bmp.close();
        } else {
          await drawViaImage(blob);
          if (disposed) return;
        }
        errCount = 0;
        setOnline(true);
        schedule(intervalMs);
      } catch {
        if (disposed) return;
        // 일시 실패는 offline 로 영구 이탈시키지 않고 백오프 후 계속 재시도 → 라이브 자동 복구.
        errCount += 1;
        if (errCount >= 3) setOnline(false);
        schedule(Math.min(intervalMs * 2 ** Math.min(errCount, 3), 8000));
      }
    };

    void fetchOnce();
    return () => {
      disposed = true;
      if (timer) clearTimeout(timer);
      ctrl.abort();
    };
  }, [enabled, cameraId, profile, intervalMs]);

  return { online, dims, canvasRef };
}
