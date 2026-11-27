// useLanguage 훅 + context — LanguageProvider(컴포넌트)와 파일 분리.
// 컴포넌트+훅 혼합 export 는 Fast Refresh 비호환이라 HMR invalidate 가 임포터
// 전체(82파일)→main.tsx 재실행(앱 2벌 마운트)까지 전파된다 (2026-07-19 실측).
// i18n 은 type-only import — 로케일 JSON 변경이 이 모듈을 invalidate 하지 않는다.
import { createContext, useContext } from 'react';
import type { AppLanguage, TrVars } from './i18n';

export interface LanguageContextValue {
  lang: AppLanguage;
  setLang: (l: AppLanguage) => void;
  /// 현재 언어로 바인딩된 tr — tr('header.title'), tr('x.y', { count: 3 }) 처럼 쓴다.
  tr: (key: string, vars?: TrVars) => string;
}

export const LanguageContext = createContext<LanguageContextValue | null>(null);

export function useLanguage(): LanguageContextValue {
  const ctx = useContext(LanguageContext);
  if (!ctx) throw new Error('useLanguage must be used within LanguageProvider');
  return ctx;
}
