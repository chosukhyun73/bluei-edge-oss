// 언어 전환 드롭다운. useLanguage 로 현재 언어를 읽고 setLang 으로 전환(localStorage 영속).
import { LANGUAGE_LABELS, SUPPORTED_LANGUAGES, type AppLanguage } from '../lib/i18n';
import { useLanguage } from '../lib/language-context';

export function LanguageSelector() {
  const { lang, setLang, tr } = useLanguage();

  return (
    <select
      aria-label={tr('common.language')}
      value={lang}
      onChange={(e) => setLang(e.target.value as AppLanguage)}
      className="text-sm font-mono text-gray-300 bg-green-500/10 border border-green-500/30 rounded-lg px-3 py-2 focus:outline-none focus:border-green-500/60"
    >
      {SUPPORTED_LANGUAGES.map((code) => (
        <option key={code} value={code} className="bg-gray-900 text-white">
          {LANGUAGE_LABELS[code]}
        </option>
      ))}
    </select>
  );
}
