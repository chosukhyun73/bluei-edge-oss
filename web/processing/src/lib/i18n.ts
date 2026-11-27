// 경량 i18n — JSON+tr 패턴(의존성 0).
//
// 언어 = 빌드에 조립된 로케일 팩.
//   - `en` = 코어 베이스(항상 포함, 공개 저장소).
//   - 그 외(`ko` 등) = `lang-packs/<code>/dashboard.json` 을 prepare 단계
//     (web/dashboard/scripts/prepare-locales.mjs)가 `src/locales/<code>.json` 으로 복사.
// 아래 glob 가 `src/locales/*.json` 을 자동 등록하므로, 언어팩을 넣고/빼는 것이
// 곧 언어를 추가/게이팅하는 것과 같다(재빌드 시 반영). 베이스 외 로케일은 생성물.
const MODULES = import.meta.glob('../locales/*.json', {
  eager: true,
  import: 'default',
}) as Record<string, Record<string, string>>;

export type AppLanguage = string;

/// 항상 존재하는 코어 베이스 언어(궁극 폴백).
export const BASE_LANGUAGE: AppLanguage = 'en';

const PACKS: Record<string, Record<string, string>> = {};
for (const [path, pack] of Object.entries(MODULES)) {
  const code = path.split('/').pop()!.replace(/\.json$/, '');
  PACKS[code] = pack;
}

// 알려진 언어 표시명(팩에 없으면 코드 그대로 노출).
const KNOWN_LABELS: Record<string, string> = {
  en: 'English', ko: '한국어', ja: '日本語', zh: '中文', vi: 'Tiếng Việt', es: 'Español',
};

// 노출 순서 — 선호 순서에 있는 것 우선, 나머지는 알파벳.
const PREFERRED_ORDER = ['ko', 'en', 'ja', 'zh'];
export const SUPPORTED_LANGUAGES: AppLanguage[] = Object.keys(PACKS).sort((a, b) => {
  const ia = PREFERRED_ORDER.indexOf(a);
  const ib = PREFERRED_ORDER.indexOf(b);
  if (ia !== -1 || ib !== -1) return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib);
  return a.localeCompare(b);
});

export const LANGUAGE_LABELS: Record<string, string> = Object.fromEntries(
  SUPPORTED_LANGUAGES.map((c) => [c, KNOWN_LABELS[c] ?? c]),
);

/// 기본 언어 — ko 팩이 있으면 ko, 없으면 베이스(en). (공개 미러=en 단독에서도 안전)
export const DEFAULT_LANGUAGE: AppLanguage =
  PACKS['ko'] ? 'ko' : PACKS[BASE_LANGUAGE] ? BASE_LANGUAGE : SUPPORTED_LANGUAGES[0] ?? 'en';

/// 보간 변수 — 번역문의 {name} 자리를 vars[name] 으로 치환한다.
export type TrVars = Record<string, string | number>;

/// key 의 번역을 lang 에서 찾고, 없으면 베이스(en) → 그래도 없으면 key 자체로
/// 폴백한다. 미번역이 크래시 대신 키로 노출돼 점진 외부화에 안전하다.
/// vars 가 주어지면 번역문 내 {name} 토큰을 vars[name] 로 치환한다.
export function tr(key: string, lang: AppLanguage, vars?: TrVars): string {
  let s = PACKS[lang]?.[key] ?? PACKS[BASE_LANGUAGE]?.[key] ?? key;
  if (vars) {
    for (const name of Object.keys(vars)) {
      s = s.split(`{${name}}`).join(String(vars[name]));
    }
  }
  return s;
}

export function isSupportedLanguage(v: unknown): v is AppLanguage {
  return typeof v === 'string' && v in PACKS;
}

/// 모듈 레벨 현재 언어 — 훅을 쓸 수 없는 순수 헬퍼(lib/format.ts 등)가 참조한다.
/// LanguageProvider 가 언어 변경 시 setActiveLanguage 로 동기화한다.
let activeLang: AppLanguage = DEFAULT_LANGUAGE;

export function setActiveLanguage(lang: AppLanguage): void {
  activeLang = lang;
}

export function getActiveLanguage(): AppLanguage {
  return activeLang;
}

/// 현재 언어로 바인딩된 tr — 컴포넌트 밖(순수 함수)에서 t('key') 처럼 쓴다.
/// 컴포넌트 안에서는 useLanguage().tr 을 써야 언어 전환 시 리렌더된다.
export function t(key: string, vars?: TrVars): string {
  return tr(key, activeLang, vars);
}
