import { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './styles/globals.css';
import App from './App';
import { DeviceLoginScreen } from './components/DeviceLoginScreen';
import { api } from './lib/api';
import { LanguageProvider } from './lib/language-context';

// StrictMode 의도적 비활성 — MJPEG <img> 가 mount/unmount 두 번 사이클에서
// camera viewer slot 을 누적 점유. 운영 환경에선 영향 없음 (StrictMode 는 dev 전용).

// 최초 로그인 게이트: device 세션(/v1/device/session)이 없으면 DeviceLoginScreen 표시.
// 백엔드(엣지 /v1/device/*) 미구현 시 세션 조회가 실패 → 로그인 화면 노출(의도).
// 같은 탭 세션에서 로그인/건너뛰기 후엔 재게이트하지 않음(sessionStorage).
function Root() {
  const [authed, setAuthed] = useState<boolean | null>(null);

  useEffect(() => {
    if (sessionStorage.getItem('bluei_device_session') === '1') {
      setAuthed(true);
      return;
    }
    api<{ authenticated: boolean }>('/v1/device/session')
      .then((r) => setAuthed(!!r.authenticated))
      .catch(() => setAuthed(false));
  }, []);

  if (authed === null) return null; // 세션 확인 중(짧음)
  if (!authed) {
    return (
      <DeviceLoginScreen
        onAuthenticated={() => {
          sessionStorage.setItem('bluei_device_session', '1');
          setAuthed(true);
        }}
      />
    );
  }
  return <App />;
}

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('#root element not found');

createRoot(rootEl).render(
  <LanguageProvider>
    <Root />
  </LanguageProvider>,
);
