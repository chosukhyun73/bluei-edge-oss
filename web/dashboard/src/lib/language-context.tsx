// 현재 언어 상태 + 전환 + localStorage 영속. 접근은 lib/language-core 의 useLanguage().
// 이 파일은 컴포넌트(LanguageProvider)만 export — Fast Refresh 경계 유지
// (훅과 혼합 export 금지, language-core.ts 헤더 참조).
import { useCallback, useState, type ReactNode } from 'react';
import {
  type AppLanguage,
  type TrVars,
  DEFAULT_LANGUAGE,
  isSupportedLanguage,
  setActiveLanguage,
  tr as trBase,
} from './i18n';
import { LanguageContext, type LanguageContextValue } from './language-core';

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

  const value: LanguageContextValue = { lang, setLang, tr };
  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}
