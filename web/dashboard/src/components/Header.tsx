import { useState, useEffect } from 'react';
import { GlobalAlerts } from './GlobalAlerts';
import logoUrl from '../assets/bluei-logo.png';
import { ShutdownButton } from './ShutdownButton';
import { AssistantPanel } from './AssistantPanel';
import { LanguageSelector } from './LanguageSelector';
import { useLanguage } from '../lib/language-core';
import { dateLocale } from '../lib/format';

export function Header() {
  const { tr } = useLanguage();
  const [currentTime, setCurrentTime] = useState(new Date());

  useEffect(() => {
    const interval = setInterval(() => setCurrentTime(new Date()), 1000);
    return () => clearInterval(interval);
  }, []);

  return (
    <header className="bg-gradient-to-r from-black via-gray-900 to-black border-b border-green-500/30 sticky top-0 z-50 backdrop-blur-sm">
      <div className="max-w-[1920px] 3xl:max-w-[3200px] mx-auto px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <img src={logoUrl} alt="BlueI" className="w-11 h-11 rounded-lg shadow-lg" />
            <div>
              <h1 className="text-xl font-bold text-white">{tr('header.title')}</h1>
              <p className="text-sm text-green-400 font-mono">bluei-edge · Offline-Capable Edge Runtime</p>
            </div>
          </div>
          <div className="flex items-center gap-4">
            <div className="text-sm text-gray-400 font-mono">
              {currentTime.toLocaleString(dateLocale())}
            </div>
            <div className="flex items-center gap-2 px-4 py-2 bg-green-500/10 border border-green-500/30 rounded-lg">
              <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse shadow-lg shadow-green-500/50" />
              <span className="text-sm text-green-400 font-medium font-mono">{tr('header.systemOnline')}</span>
            </div>
            <LanguageSelector />
            <AssistantPanel />
            <ShutdownButton />
          </div>
        </div>
      </div>
      <GlobalAlerts />
    </header>
  );
}
