import { useState } from 'react';
import { createPortal } from 'react-dom';
import { Power, AlertTriangle } from 'lucide-react';
import { System, ApiError } from '../lib/api';
import { useLanguage } from '../lib/language-context';

// 백엔드 안전 종료 버튼 — GX10 전원 끄기 직전 운영자가 graceful 종료시킨다.
// 흐름: 버튼 → 확인 모달(경고) → System.shutdown() → 종료 완료 화면 → Tauri 창 닫기 시도.
type Phase = 'idle' | 'confirm' | 'shutting' | 'done' | 'error';

export function ShutdownButton() {
  const { tr } = useLanguage();
  const [phase, setPhase] = useState<Phase>('idle');
  const [err, setErr] = useState<string | null>(null);

  const doShutdown = async () => {
    setPhase('shutting');
    setErr(null);
    try {
      await System.shutdown();
      setPhase('done');
      // 잠시 후 Tauri 창 닫기 시도 (브라우저 환경이면 무시됨).
      setTimeout(() => {
        void closeWindowIfTauri();
      }, 1500);
    } catch (e) {
      // 응답 직후 백엔드가 내려가며 연결이 끊기면 fetch 가 실패할 수 있음 → 그래도 종료는 진행된 것.
      if (e instanceof ApiError && e.status >= 500) {
        setErr(`${e.code}: ${e.message}`);
        setPhase('error');
        return;
      }
      // 네트워크 단절(TypeError 등)은 종료 성공으로 간주.
      setPhase('done');
      setTimeout(() => {
        void closeWindowIfTauri();
      }, 1500);
    }
  };

  return (
    <>
      <button
        type="button"
        onClick={() => setPhase('confirm')}
        title={tr('shutdown.buttonTitle')}
        className="flex items-center gap-1.5 px-3 py-2 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 hover:bg-red-500/20 transition-colors"
      >
        <Power className="w-4 h-4" />
        <span className="text-sm font-medium">{tr('header.shutdown')}</span>
      </button>

      {phase !== 'idle' && createPortal(
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 backdrop-blur-sm">
          <div className="max-w-md w-full mx-4 bg-gradient-to-br from-gray-900 to-black border border-red-500/40 rounded-xl p-6 space-y-4">
            {phase === 'confirm' && (
              <>
                <div className="flex items-center gap-2 text-red-400">
                  <AlertTriangle className="w-5 h-5" />
                  <h2 className="text-base font-bold">{tr('shutdown.confirmTitle')}</h2>
                </div>
                <p className="text-sm text-gray-300 leading-relaxed">
                  {tr('shutdown.confirmBodyPrefix')} <b className="text-red-300">{tr('shutdown.confirmBodyStops')}</b><br />
                  <span className="text-gray-400">{tr('shutdown.confirmBodyWarning')}</span> {tr('shutdown.confirmBodySuffix')}
                </p>
                <div className="flex gap-2 justify-end pt-2">
                  <button
                    type="button"
                    onClick={() => setPhase('idle')}
                    className="px-4 py-2 text-sm rounded-lg border border-gray-600 text-gray-300 hover:bg-gray-800"
                  >
                    {tr('shutdown.cancel')}
                  </button>
                  <button
                    type="button"
                    onClick={() => void doShutdown()}
                    className="px-4 py-2 text-sm rounded-lg bg-red-600 text-white hover:bg-red-500 font-medium"
                  >
                    {tr('shutdown.shutdown')}
                  </button>
                </div>
              </>
            )}

            {phase === 'shutting' && (
              <div className="text-center py-4">
                <Power className="w-8 h-8 text-red-400 mx-auto mb-3 animate-pulse" />
                <p className="text-sm text-gray-300">{tr('shutdown.shutting')}</p>
              </div>
            )}

            {phase === 'done' && (
              <div className="text-center py-4 space-y-3">
                <Power className="w-10 h-10 text-green-400 mx-auto" />
                <p className="text-base font-bold text-white">{tr('shutdown.doneTitle')}</p>
                <p className="text-sm text-gray-400">
                  {tr('shutdown.donePowerOff')}<br />
                  {tr('shutdown.doneRestartPrefix')} <b className="text-gray-300">bluei-edge Dashboard</b> {tr('shutdown.doneRestartSuffix')}
                </p>
              </div>
            )}

            {phase === 'error' && (
              <div className="space-y-3">
                <div className="flex items-center gap-2 text-red-400">
                  <AlertTriangle className="w-5 h-5" />
                  <h2 className="text-base font-bold">{tr('shutdown.errorTitle')}</h2>
                </div>
                <p className="text-sm text-red-300 font-mono break-all">{err}</p>
                <div className="flex justify-end">
                  <button
                    type="button"
                    onClick={() => setPhase('idle')}
                    className="px-4 py-2 text-sm rounded-lg border border-gray-600 text-gray-300 hover:bg-gray-800"
                  >
                    {tr('shutdown.close')}
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>,
        document.body,
      )}
    </>
  );
}

// Tauri v2 창 닫기 — 패키지가 없거나 브라우저면 조용히 무시.
async function closeWindowIfTauri() {
  try {
    const mod = await import('@tauri-apps/api/window');
    await mod.getCurrentWindow().close();
  } catch {
    /* 브라우저 환경 — 무시 (종료 완료 화면 유지) */
  }
}
