import { useState } from 'react';
import { PlantPanel } from './components/plant/PlantPanel';
import { api } from './lib/api';
import { useLanguage } from './lib/language-core';
import { SUPPORTED_LANGUAGES, LANGUAGE_LABELS } from './lib/i18n';

// processing-bluei — 가공공장 관리. 열면 바로 공장 설정 화면이 나온다.
export default function App() {
  const { tr, lang, setLang } = useLanguage();
  // 종료는 두 번 눌러야 한다. 끝난 뒤에는 화면이 더 갱신되지 않는다.
  const [quit, setQuit] = useState<'idle' | 'confirm' | 'done'>('idle');
  if (quit === 'done') {
    return <div className="min-h-screen bg-background flex items-center justify-center text-gray-300">{tr('app.stopped')}</div>;
  }
  return (
    <div className="min-h-screen bg-background">
      <header className="border-b border-gray-800">
        <div className="max-w-[1920px] mx-auto px-6 py-4 flex items-center gap-4">
          <div>
            <h1 className="text-xl text-white font-semibold">processing-bluei</h1>
            <p className="text-xs text-green-400 font-mono">{tr('app.subtitle')}</p>
          </div>
          <div className="ml-auto flex items-center gap-2">
            {quit === 'confirm' && <span className="text-xs text-gray-400">{tr('app.quitConfirm')}</span>}
            <button type="button"
              onClick={() => (quit === 'idle' ? setQuit('confirm') : api('/v1/system/shutdown', { method: 'POST' }).finally(() => setQuit('done')))}
              className={'h-8 px-3 rounded-md border text-sm ' + (quit === 'confirm' ? 'border-red-500 text-red-300' : 'border-gray-700 text-gray-400 hover:text-white')}>
              {tr('app.quit')}
            </button>
            {quit === 'confirm' && (
              <button type="button" onClick={() => setQuit('idle')} className="h-8 px-3 rounded-md border border-gray-700 text-sm text-gray-400">{tr('app.cancel')}</button>
            )}
          </div>
          {SUPPORTED_LANGUAGES.length > 1 && (
            <select value={lang} onChange={e => setLang(e.target.value)}
              className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white">
              {SUPPORTED_LANGUAGES.map(c => <option key={c} value={c}>{LANGUAGE_LABELS[c]}</option>)}
            </select>
          )}
        </div>
      </header>
      <main className="max-w-[1920px] mx-auto px-6 py-6">
        <PlantPanel />
      </main>
    </div>
  );
}
