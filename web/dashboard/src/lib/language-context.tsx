// 현재 언어 상태 + 전환 + localStorage 영속. App 전역에서 useLanguage() 로 접근.
import { createContext, useCallback, useContext, useState, type ReactNode } from 'react';
import {
  type AppLanguage,
  type TrVars,
  DEFAULT_LANGUAGE,
  isSupportedLanguage,
  setActiveLanguage,
  tr as trBase,
} from './i18n';

const STORAGE_KEY = 'bluei_edge_lang';

function readStored(): AppLanguage {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (isSupportedLanguage(v)) return v;
  } catch {
    // localStorage 접근 불가(프라이빗 모드 등) → 기본 언어.
  }
  return DEFAULT_LANGUAGE;
}

interface LanguageContextValue {
  lang: AppLanguage;
  setLang: (l: AppLanguage) => void;
  /// 현재 언어로 바인딩된 tr — tr('header.title'), tr('x.y', { count: 3 }) 처럼 쓴다.
  tr: (key: string, vars?: TrVars) => string;
}

const LanguageContext = createContext<LanguageContextValue | null>(null);

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<AppLanguage>(readStored);

  // 순수 헬퍼(lib/format.ts 등)가 참조하는 모듈 레벨 언어를 자식 렌더 전에 동기화.
  setActiveLanguage(lang);

  const setLang = useCallback((l: AppLanguage) => {
    setLangState(l);
    try {
      localStorage.setItem(STORAGE_KEY, l);
    } catch {
      // 영속 실패는 무시(세션 내 전환은 유지).
    }
  }, []);

  const tr = useCallback((key: string, vars?: TrVars) => trBase(key, lang, vars), [lang]);

  return (
    <LanguageContext.Provider value={{ lang, setLang, tr }}>
      {children}
    </LanguageContext.Provider>
  );
}

export function useLanguage(): LanguageContextValue {
  const ctx = useContext(LanguageContext);
  if (!ctx) throw new Error('useLanguage must be used within LanguageProvider');
  return ctx;
}
