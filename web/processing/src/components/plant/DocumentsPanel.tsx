import { useCallback, useEffect, useState } from 'react';
import { ApiError } from '../../lib/api';
import { Documents, Haccp, fmtTime } from '../../lib/plant';
import type { HaccpPlan, IssuedDocument, LocalUser, Signature } from '../../lib/plant';
import { SelectField } from '../fields';
import { Button } from '../ui/button';
import { useLanguage } from '../../lib/language-core';
import { SignerBar } from './ProductionPanel';

// 문서 — 게시된 계획서를 발행하고, 발행 대장을 본다. lot 기록서는 생산 화면에서 마감 뒤 발행한다.
// PDF 는 이 기기가 직접 만든다. 보관 중인 파일이 발행 때와 같은지는 해시로 대조한다.

function errText(e: unknown): string {
  if (e instanceof ApiError) return `${e.code}: ${e.message}`;
  return e instanceof Error ? e.message : String(e);
}

export function DocumentsPanel({ siteId, plans, users }: { siteId: string; plans: HaccpPlan[]; users: LocalUser[] }) {
  const { tr } = useLanguage();
  const [docs, setDocs] = useState<IssuedDocument[]>([]);
  const [versions, setVersions] = useState<{ value: string; label: string }[]>([]);
  const [versionId, setVersionId] = useState('');
  const [sig, setSig] = useState<Signature>({ by: '', pin: '' });
  const [err, setErr] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try { setDocs((await Documents.list(siteId)).documents); } catch (e) { setErr(errText(e)); }
  }, [siteId]);

  useEffect(() => {
    reload();
    // 발행할 수 있는 것: 게시됐거나 게시됐던 계획 버전.
    Promise.all(plans.map(p => Haccp.plan(p.plan_id))).then(all => {
      const opts = all.flatMap(d => d.versions.filter(v => v.status !== 'draft')
        .map(v => ({ value: v.version_id, label: `${d.plan.name} v${v.version_no} (${tr(`plant.status.${v.status}`)})` })));
      setVersions(opts);
      setVersionId(cur => cur || opts[opts.length - 1]?.value || '');
    }).catch(e => setErr(errText(e)));
  }, [reload, plans, tr]);

  const issue = async () => {
    try {
      const r = await Documents.issue('plan', versionId, sig);
      setNote(r.reissued ? tr('doc.reissued') : tr('doc.issued'));
      setErr(null);
      reload();
    } catch (e) { setErr(errText(e)); setNote(null); }
  };

  return (
    <div className="space-y-5">
      <div className="p-4 rounded-lg border border-gray-800 space-y-3">
        <h3 className="text-sm text-white font-medium">{tr('doc.issuePlan')}</h3>
        <p className="text-xs text-gray-500">{tr('doc.issuePlan.hint')}</p>
        <div className="flex flex-wrap items-end gap-3">
          <SelectField label={tr('doc.version')} value={versionId} onChange={setVersionId} options={versions.length ? versions : [{ value: '', label: tr('doc.noVersion') }]} />
          <SignerBar users={users} value={sig} onChange={setSig} />
          <Button disabled={!versionId || !sig.by} onClick={issue}>{tr('doc.issue')}</Button>
        </div>
        {note && <p className="text-xs text-green-300">{note}</p>}
        {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      </div>

      <div>
        <h3 className="text-sm text-white font-medium mb-2">{tr('doc.ledger')} ({docs.length})</h3>
        {docs.length === 0 && <p className="text-sm text-gray-500">{tr('doc.none')}</p>}
        <div className="space-y-2">
          {docs.map(d => (
            <div key={d.doc_id} className="p-3 rounded-lg border border-gray-800 flex flex-wrap items-center gap-x-4 gap-y-1">
              <span className="px-1.5 py-0.5 rounded bg-gray-700 text-gray-200 text-xs">{tr(`doc.kind.${d.kind}`)}</span>
              <span className="text-sm text-white">{d.title}</span>
              <span className="text-xs text-gray-500">{fmtTime(d.issued_at)} · {d.issued_by_name}</span>
              <span className="text-xs text-gray-600 font-mono" title={d.sha256}>{d.sha256.slice(0, 16)}…</span>
              <span className={'text-xs ' + (d.intact ? 'text-green-300' : 'text-red-300')}>{d.intact ? tr('doc.intact') : tr('doc.changed')}</span>
              <span className="ml-auto flex gap-3 text-sm">
                <a className="text-sky-300 underline" href={Documents.fileUrl(d.doc_id)} target="_blank" rel="noreferrer">{tr('doc.openPdf')}</a>
                <a className="text-gray-400 underline" href={Documents.dataUrl(d.doc_id)} target="_blank" rel="noreferrer">{tr('doc.openData')}</a>
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
