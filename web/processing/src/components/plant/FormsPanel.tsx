import { useCallback, useEffect, useState } from 'react';
import { ApiError } from '../../lib/api';
import { Forms } from '../../lib/plant';
import type { FormPack, FormSummary } from '../../lib/plant';
import { Button } from '../ui/button';
import { useLanguage } from '../../lib/language-core';

// 양식 — 공장이 쓰는 기록 양식(점검표·일지·서식). 기본 양식에서 골라 받고, 빈 양식을 PDF 로 뽑는다.
// 양식을 고치는 것과 쓰던 양식을 가져오는 것은 다음 단계다.

function errText(e: unknown): string {
  if (e instanceof ApiError) return `${e.code}: ${e.message}`;
  return e instanceof Error ? e.message : String(e);
}

export function FormsPanel({ siteId }: { siteId: string }) {
  const { tr } = useLanguage();
  const [forms, setForms] = useState<FormSummary[]>([]);
  const [packs, setPacks] = useState<FormPack[]>([]);
  const [picked, setPicked] = useState<string[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try { setForms((await Forms.list(siteId)).forms); } catch (e) { setErr(errText(e)); }
  }, [siteId]);

  useEffect(() => {
    reload();
    Forms.defaults().then(r => setPacks(r.packs)).catch(e => setErr(errText(e)));
  }, [reload]);

  const have = new Set(forms.map(f => f.form_id));
  const pack = packs[0];
  const open = pack ? pack.forms.filter(f => !have.has(f.id)) : [];

  const adopt = async () => {
    if (!pack) return;
    try {
      const r = await Forms.adopt(siteId, pack.id, picked);
      setNote(tr('form.adopted', { n: r.adopted.length }));
      setPicked([]);
      setErr(null);
      reload();
    } catch (e) { setErr(errText(e)); setNote(null); }
  };

  return (
    <div className="space-y-5">
      <p className="text-xs text-gray-500">{tr('form.hint')}</p>

      <div>
        <h3 className="text-sm text-white font-medium mb-2">{tr('form.mine')} ({forms.length})</h3>
        {forms.length === 0 && <p className="text-sm text-gray-500">{tr('form.none')}</p>}
        <div className="space-y-2">
          {forms.map(f => (
            <div key={f.form_id} className="p-3 rounded-lg border border-gray-800 flex flex-wrap items-center gap-x-4 gap-y-1">
              <span className="px-1.5 py-0.5 rounded bg-gray-700 text-gray-200 text-xs">{tr(`form.kind.${f.kind}`)}</span>
              <span className="text-sm text-white">{f.name}</span>
              <span className="text-xs text-gray-500">{f.cycle} · {f.owner}</span>
              <span className="text-xs text-gray-500">{tr(`form.source.${f.source}`)} · {tr('form.revision', { n: f.revision })}</span>
              {f.covers.length > 0 && <span className="text-xs text-gray-600">{tr('form.covers')} {f.covers.join(' ')}</span>}
              <a className="ml-auto text-sm text-sky-300 underline" href={Forms.blankUrl(siteId, f.form_id)} target="_blank" rel="noreferrer">{tr('form.blank')}</a>
            </div>
          ))}
        </div>
      </div>

      <div className="p-4 rounded-lg border border-gray-800 space-y-4">
        <h3 className="text-sm text-white font-medium">{tr('form.add')}</h3>
        <div className="space-y-2">
          <h4 className="text-sm text-gray-200">{tr('form.add.default')}{pack ? ` — ${pack.name}` : ''}</h4>
          <p className="text-xs text-gray-500">{tr('form.add.default.hint')}</p>
          {pack && open.length === 0 && <p className="text-xs text-gray-400">{tr('form.allAdopted')}</p>}
          {open.length > 0 && <>
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-1">
              {open.map(f => (
                <label key={f.id} className="flex items-center gap-2 text-sm text-gray-300">
                  <input type="checkbox" checked={picked.includes(f.id)}
                    onChange={e => setPicked(e.target.checked ? [...picked, f.id] : picked.filter(id => id !== f.id))} />
                  <span>{f.name}</span>
                  <span className="text-xs text-gray-600">{tr(`form.kind.${f.kind}`)}</span>
                </label>
              ))}
            </div>
            <div className="flex items-center gap-3">
              <Button size="sm" variant="outline" onClick={() => setPicked(open.map(f => f.id))}>{tr('form.selectAll')}</Button>
              <Button disabled={picked.length === 0} onClick={adopt}>{tr('form.adopt', { n: picked.length })}</Button>
            </div>
          </>}
        </div>
        <div className="space-y-1 border-t border-gray-800 pt-3">
          <h4 className="text-sm text-gray-200">{tr('form.add.import')}</h4>
          <p className="text-xs text-gray-500">{tr('form.add.import.hint')}</p>
        </div>
        {note && <p className="text-xs text-green-300">{note}</p>}
        {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      </div>
    </div>
  );
}
