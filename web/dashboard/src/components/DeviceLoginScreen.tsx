import { useEffect, useRef, useState } from 'react';
import { Loader2, Smartphone, ShieldCheck } from 'lucide-react';
import logoUrl from '../assets/bluei-logo.png';
import { api, ApiError } from '../lib/api';
import { useLanguage } from '../lib/language-context';

type Tr = (key: string, vars?: Record<string, string | number>) => string;

/// GX10 최초 로그인 화면 — 앱과 동일한 다크 테마 + bluei 브랜딩, 화면 중앙 정렬.
/// 비밀번호 입력이 아니라 device-authorization 흐름(폰 승인 + 번호 매칭):
///   [로그인] → /v1/device/login/start → 매칭번호 표시 → 폰 bluei 앱에서 승인
///   → /v1/device/login/status 폴링 → approved 시 엣지가 device 토큰 저장 → 대시보드.
/// 백엔드(엣지 /v1/device/* + 클라우드 device-auth)는 P-A 후속 작업. 미구현이면
/// 안내 문구를 보여주고, 개발 중에는 하단 "건너뛰기"로 대시보드 진입 가능.
type Phase = 'idle' | 'requesting' | 'awaiting' | 'success' | 'error';

interface StartResp {
  user_code: string;
  device_code: string;
  expires_in?: number;
  interval?: number;
}
interface StatusResp {
  status: 'pending' | 'approved' | 'expired' | 'denied';
  user_email?: string;
}

export function DeviceLoginScreen({ onAuthenticated }: { onAuthenticated: () => void }) {
  const { tr } = useLanguage();
  const [phase, setPhase] = useState<Phase>('idle');
  const [userCode, setUserCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const pollRef = useRef<number | null>(null);

  useEffect(
    () => () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    },
    [],
  );

  function stopPoll() {
    if (pollRef.current) {
      window.clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }

  async function startLogin() {
    setError(null);
    setPhase('requesting');
    try {
      const r = await api<StartResp>('/v1/device/login/start', { method: 'POST' });
      setUserCode(r.user_code);
      setPhase('awaiting');
      const interval = (r.interval && r.interval > 0 ? r.interval : 3) * 1000;
      stopPoll();
      pollRef.current = window.setInterval(() => void poll(r.device_code), interval);
    } catch (e) {
      setError(loginErr(e, tr));
      setPhase('error');
    }
  }

  async function poll(deviceCode: string) {
    try {
      const r = await api<StatusResp>(
        `/v1/device/login/status?device_code=${encodeURIComponent(deviceCode)}`,
      );
      if (r.status === 'approved') {
        stopPoll();
        setPhase('success');
        window.setTimeout(onAuthenticated, 700);
      } else if (r.status === 'expired' || r.status === 'denied') {
        stopPoll();
        setError(
          r.status === 'denied'
            ? tr('deviceLogin.denied')
            : tr('deviceLogin.expired'),
        );
        setPhase('error');
      }
      // pending → 계속 폴링
    } catch {
      // 일시 오류는 무시하고 다음 폴링에서 재시도
    }
  }

  function cancel() {
    stopPoll();
    setUserCode('');
    setPhase('idle');
  }

  return (
    <div className="min-h-screen w-full flex items-center justify-center bg-gradient-to-br from-black via-gray-900 to-black px-6">
      <div className="w-full max-w-sm flex flex-col items-center text-center">
        {/* 브랜드 (앱 로그인과 동일: 로고 배지 + bluei 워드마크) */}
        <img
          src={logoUrl}
          alt="bluei"
          className="w-[72px] h-[72px] rounded-2xl shadow-lg shadow-blue-500/20"
        />
        <h1 className="mt-3 text-4xl font-black tracking-tight text-blue-400">bluei</h1>
        <p className="mt-2 text-sm text-gray-400 font-mono">{tr('deviceLogin.tagline')}</p>

        {phase === 'idle' && (
          <>
            <p className="mt-8 text-gray-300 leading-relaxed">
              {tr('deviceLogin.idleLine1')}
              <br />
              {tr('deviceLogin.idleLine2')}
            </p>
            <button
              onClick={startLogin}
              className="mt-6 w-full py-3 rounded-xl bg-blue-600 hover:bg-blue-700 text-white font-semibold transition-colors"
            >
              {tr('deviceLogin.loginButton')}
            </button>
          </>
        )}

        {phase === 'requesting' && (
          <div className="mt-10 flex flex-col items-center gap-3 text-gray-400">
            <Loader2 className="w-6 h-6 animate-spin" />
            <span>{tr('deviceLogin.requesting')}</span>
          </div>
        )}

        {phase === 'awaiting' && (
          <div className="mt-8 w-full flex flex-col items-center">
            <div className="flex items-center gap-2 text-gray-400 text-sm">
              <Smartphone className="w-4 h-4" /> {tr('deviceLogin.awaitingInstruction')}
            </div>
            <div className="mt-4 px-6 py-4 rounded-2xl bg-gray-800/80 border border-blue-500/40">
              <span className="text-4xl font-black tracking-[0.3em] text-white tabular-nums">
                {userCode}
              </span>
            </div>
            <div className="mt-5 flex items-center gap-2 text-gray-500 text-sm">
              <Loader2 className="w-4 h-4 animate-spin" /> {tr('deviceLogin.awaitingApproval')}
            </div>
            <button onClick={cancel} className="mt-6 text-sm text-gray-500 hover:text-gray-300">
              {tr('deviceLogin.cancel')}
            </button>
          </div>
        )}

        {phase === 'success' && (
          <div className="mt-10 flex flex-col items-center gap-3 text-green-400">
            <ShieldCheck className="w-8 h-8" />
            <span>{tr('deviceLogin.success')}</span>
          </div>
        )}

        {phase === 'error' && (
          <div className="mt-8 w-full flex flex-col items-center gap-4">
            <p className="text-sm text-red-400">{error}</p>
            <button
              onClick={startLogin}
              className="w-full py-3 rounded-xl bg-blue-600 hover:bg-blue-700 text-white font-semibold transition-colors"
            >
              {tr('deviceLogin.retry')}
            </button>
          </div>
        )}

        {/* 개발용: device-auth 백엔드 준비 전 대시보드 접근용 임시 우회 */}
        <button
          onClick={onAuthenticated}
          className="mt-10 text-xs text-gray-600 hover:text-gray-400 underline underline-offset-4"
        >
          {tr('deviceLogin.skip')}
        </button>
      </div>
    </div>
  );
}

function loginErr(e: unknown, tr: Tr): string {
  if (e instanceof ApiError && e.status === 404) {
    return tr('deviceLogin.errorNotReady');
  }
  return tr('deviceLogin.errorRequestFailed');
}
