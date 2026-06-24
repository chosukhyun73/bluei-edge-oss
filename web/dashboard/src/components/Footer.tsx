import { useLanguage } from '../lib/language-context';

export function Footer() {
  const { tr } = useLanguage();
  return (
    <footer className="bg-gradient-to-r from-black via-gray-900 to-black border-t border-green-500/30 mt-12">
      <div className="max-w-[1920px] mx-auto px-6 py-4">
        <p className="text-sm text-gray-400 text-center font-mono">
          © 2026 bluei-edge — {tr('footer.tagline')} · Offline-First · Phase 2
        </p>
      </div>
    </footer>
  );
}
