import { useCallback, useEffect, useState } from 'react';
import { Video, Circle } from 'lucide-react';
import { Vision, type CaptureStatusResponse } from '../../lib/api';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { useLanguage } from '../../lib/language-core';

const POLL_MS = 10_000;

// 학습 데이터(상시 캡처) 수집을 대시보드에서 켜고 끄는 카드.
// config.Continuous 와 무관하게 운영자가 버튼으로 제어. mode=fixture 면 실 카메라 아님을 경고.
export function CaptureControlCard() {
  const { tr } = useLanguage();
  const [status, setStatus] = useState<CaptureStatusResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setStatus(await Vision.captureStatus());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'unknown');
    }
  }, []);

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), POLL_MS);
    return () => clearInterval(t);
  }, [load]);

  const toggle = async () => {
    if (!status) return;
    setBusy(true);
    try {
      if (status.running) await Vision.captureStop();
      else await Vision.captureStart();
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'unknown');
    } finally {
      setBusy(false);
    }
  };

  const running = status?.running ?? false;
  const isFixture = status?.mode === 'fixture';

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Video className="w-4 h-4" /> {tr('captureControl.title')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center gap-2 text-sm">
          <Circle className={`w-2.5 h-2.5 ${running ? 'fill-green-500 text-green-500 animate-pulse' : 'fill-gray-600 text-gray-600'}`} />
          <span className={running ? 'text-green-400' : 'text-gray-400'}>
            {running ? tr('captureControl.running') : tr('captureControl.stopped')}
          </span>
          {status && (
            <span className="text-xs text-gray-500 font-mono ml-auto">
              {tr('captureControl.statusLine', { mode: status.mode, s: status.spacing_seconds, tanks: (status.tanks ?? []).length })}
            </span>
          )}
        </div>

        {isFixture && (
          <div className="text-xs text-amber-400 bg-amber-500/10 border border-amber-500/30 rounded px-2 py-1.5">
            {tr('captureControl.fixtureWarnPre')}<span className="font-mono">mode: rtsp</span>{tr('captureControl.fixtureWarnPost')}
          </div>
        )}

        <button
          onClick={toggle}
          disabled={busy || !status?.available}
          className={`w-full px-4 py-2.5 text-sm rounded font-medium transition-colors disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed ${
            running ? 'bg-red-600 hover:bg-red-500 text-white' : 'bg-green-600 hover:bg-green-500 text-white'
          }`}
        >
          {busy ? tr('captureControl.busy') : running ? tr('captureControl.btnStop') : tr('captureControl.btnStart')}
        </button>

        {status && (
          <div className="text-[11px] text-gray-600 font-mono truncate">{tr('captureControl.savePathLabel')}: {status.temp_dir}</div>
        )}
        {error && <div className="text-xs text-red-400">{error}</div>}
      </CardContent>
    </Card>
  );
}
