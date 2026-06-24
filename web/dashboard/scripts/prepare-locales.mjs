// 빌드 전 로케일 조립 — lang-packs/<code>/dashboard.json → src/locales/<code>.json.
// en.json(코어 베이스)은 저장소에 커밋돼 있고, 그 외 언어는 언어팩에서 생성된다.
// 언어팩이 없으면(공개 미러 등) 아무것도 복사하지 않아 en 단독으로 동작한다.
import { readdirSync, existsSync, copyFileSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));               // web/dashboard/scripts
const langPacksDir = join(here, '..', '..', '..', 'lang-packs');   // <repo>/lang-packs
const localesDir = join(here, '..', 'src', 'locales');             // web/dashboard/src/locales

let copied = [];
if (existsSync(langPacksDir)) {
  for (const code of readdirSync(langPacksDir)) {
    const packDir = join(langPacksDir, code);
    if (!statSync(packDir).isDirectory()) continue;
    if (code === 'en') continue; // en 은 베이스(커밋본) — 덮어쓰지 않음
    const src = join(packDir, 'dashboard.json');
    if (existsSync(src)) {
      copyFileSync(src, join(localesDir, `${code}.json`));
      copied.push(code);
    }
  }
}
console.log(`[prepare-locales] base=en + packs: ${copied.length ? copied.join(', ') : '(none)'}`);
