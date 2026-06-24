import { useState, useEffect, useCallback } from 'react';
import { Plus, X, Trash2 } from 'lucide-react';
import type { SpawnBatch, NewSpawnBatchBody, BroodstockCohort, Tank } from '../lib/types';
import { SpawnBatches, Broodstock, Groups, ApiError } from '../lib/api';
import { useLanguage } from '../lib/language-context';
import { ConfirmDialog } from './ui/confirm-dialog';

// QR 발행·표시는 GX10 아닌 Flutter 앱(landBased)에서 수행(2026-06-15 방향). GX10은
// 데이터 push(플랫폼 발행)까지만 → 대시보드 QR 표시 제거.

interface Props {
  groupId: string;
  tanks: Tank[];
  mode: 'spawning' | 'hatching';
}

const STATUS_CLS: Record<string, string> = {
  incubating: 'border-amber-500/40 bg-amber-500/10 text-amber-300',
  hatched: 'border-green-500/40 bg-green-500/10 text-green-300',
  discarded: 'border-gray-600 bg-gray-700/20 text-gray-400',
  sold: 'border-sky-500/40 bg-sky-500/10 text-sky-300',
};

function toBody(b: SpawnBatch): NewSpawnBatchBody {
  return {
    group_id: b.group_id, tank_id: b.tank_id, species: b.species,
    female_cohort_id: b.female_cohort_id, male_cohort_id: b.male_cohort_id,
    spawn_date: b.spawn_date, egg_count: b.egg_count, egg_volume_ml: b.egg_volume_ml,
    fertilization_rate: b.fertilization_rate, hatch_date: b.hatch_date,
    hatched_count: b.hatched_count, hatch_rate: b.hatch_rate, status: b.status,
    buyer: b.buyer, notes: b.notes,
  };
}

export function SpawnPanel({ groupId, tanks, mode }: Props) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<SpawnBatch[]>([]);
  const [cohorts, setCohorts] = useState<BroodstockCohort[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [hatchTarget, setHatchTarget] = useState<SpawnBatch | null>(null);
  const [confirmTarget, setConfirmTarget] = useState<SpawnBatch | null>(null);
  const [publishing, setPublishing] = useState<string | null>(null);
  const [publishErr, setPublishErr] = useState<string | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    SpawnBatches.list(groupId).then(r => setItems(r.items)).catch(() => setItems([])).finally(() => setLoading(false));
  }, [groupId]);
  useEffect(() => { reload(); }, [reload]);

  // 산란 모드: 부모 어미 계군 선택용으로 broodstock 단계 그룹들의 cohort 수집.
  useEffect(() => {
    if (mode !== 'spawning') return;
    let cancelled = false;
    (async () => {
      try {
        const gs = await Groups.list();
        const bgroups = gs.items.filter(g => (g.metadata as { stage_role?: string } | undefined)?.stage_role === 'broodstock');
        const all: BroodstockCohort[] = [];
        for (const g of bgroups) {
          try { const r = await Broodstock.list(g.group_id); all.push(...r.items); } catch { /* skip */ }
        }
        if (!cancelled) setCohorts(all);
      } catch { if (!cancelled) setCohorts([]); }
    })();
    return () => { cancelled = true; };
  }, [mode]);

  const handleDelete = async () => {
    if (!confirmTarget) return;
    try { await SpawnBatches.delete(confirmTarget.batch_id); } catch { /* ignore */ }
    setConfirmTarget(null);
    reload();
  };

  const handlePublish = async (b: SpawnBatch) => {
    setPublishing(b.batch_id); setPublishErr(null);
    try { await SpawnBatches.publish(b.batch_id); reload(); }
    catch (err) { setPublishErr(err instanceof ApiError ? err.message : err instanceof Error ? err.message : 'error'); }
    finally { setPublishing(null); }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500 font-mono">{items.length} {tr('spawn.batchUnit')}</span>
        {mode === 'spawning' && (
          <button onClick={() => setShowCreate(true)}
            className="flex items-center gap-1 px-3 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white">
            <Plus className="w-3.5 h-3.5" /> {tr('spawn.add')}
          </button>
        )}
      </div>

      {publishErr && (
        <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
          {tr('spawn.publishFailed')}: {publishErr}
        </div>
      )}

      {loading ? (
        <div className="py-10 text-center text-sm text-gray-500 font-mono">…</div>
      ) : items.length === 0 ? (
        <div className="py-12 text-center text-sm text-gray-500 italic">
          {mode === 'spawning' ? tr('spawn.empty') : tr('spawn.emptyHatch')}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {items.map(b => (
            <BatchCard key={b.batch_id} batch={b} tanks={tanks} mode={mode}
              onHatch={() => setHatchTarget(b)} onDelete={() => setConfirmTarget(b)}
              onPublish={() => void handlePublish(b)} publishing={publishing === b.batch_id}
              tr={tr} />
          ))}
        </div>
      )}

      {showCreate && (
        <SpawnModal groupId={groupId} tanks={tanks} cohorts={cohorts}
          onClose={() => setShowCreate(false)} onSaved={() => { setShowCreate(false); reload(); }} />
      )}
      {hatchTarget && (
        <HatchModal batch={hatchTarget}
          onClose={() => setHatchTarget(null)} onSaved={() => { setHatchTarget(null); reload(); }} />
      )}
      <ConfirmDialog open={confirmTarget !== null} title={tr('spawn.deleteTitle')} message={tr('spawn.deleteConfirm')}
        onConfirm={() => void handleDelete()} onCancel={() => setConfirmTarget(null)} />
    </div>
  );
}

function BatchCard({ batch, tanks, mode, onHatch, onDelete, onPublish, publishing, tr }: {
  batch: SpawnBatch; tanks: Tank[]; mode: 'spawning' | 'hatching';
  onHatch: () => void; onDelete: () => void; onPublish: () => void; publishing: boolean;
  tr: (k: string) => string;
}) {
  const tankName = tanks.find(t => t.tank_id === batch.tank_id)?.display_name ?? batch.tank_id;
  const statusCls = STATUS_CLS[batch.status] ?? STATUS_CLS.incubating;
  const origin = batch.origin_type === 'wild' ? tr('broodstock.originWild')
    : batch.origin_type === 'domestic' ? tr('broodstock.originDomestic') : '';
  const published = (batch.metadata as { published?: boolean } | undefined)?.published === true;
  return (
    <div className="border border-gray-700/60 rounded-lg p-3 bg-black/30">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-white truncate">{batch.species}</span>
            <span className={`px-2 py-0.5 rounded text-[11px] font-mono border ${statusCls}`}>{tr(`spawn.status.${batch.status}`)}</span>
            {published && (
              <span className="px-2 py-0.5 rounded text-[11px] font-mono border border-emerald-500/40 bg-emerald-500/10 text-emerald-300">{tr('spawn.published')}</span>
            )}
            {batch.lot_code && <span className="text-[11px] text-gray-500 font-mono">{batch.lot_code}</span>}
          </div>
          <div className="text-xs text-gray-400 font-mono mt-1 space-y-0.5">
            {/* 족보(어미에서 이어진 출신성분) — GDST 추적성 */}
            {(origin || batch.generation || batch.origin_region) && (
              <div>{tr('spawn.pedigree')}: {[origin, batch.generation, batch.origin_region].filter(Boolean).join(' · ')}</div>
            )}
            <div>
              {batch.egg_count > 0 && <>{tr('spawn.eggCount')} {batch.egg_count.toLocaleString()}</>}
              {batch.egg_volume_ml > 0 && <> · {batch.egg_volume_ml}ml</>}
              {batch.spawn_date && <> · {batch.spawn_date}</>}
            </div>
            {(batch.hatched_count > 0 || batch.hatch_date) && (
              <div>{tr('spawn.hatched')} {batch.hatched_count.toLocaleString()} ({batch.hatch_rate.toFixed(1)}%){batch.hatch_date ? ` · ${batch.hatch_date}` : ''}</div>
            )}
            {batch.tank_id && <div>{tr('broodstock.tank')}: {tankName}</div>}
          </div>
          {mode === 'hatching' && batch.status === 'incubating' && (
            <button onClick={onHatch} className="mt-2 px-2.5 py-1 text-xs rounded border border-green-500/50 text-green-300 hover:bg-green-500/10">
              {tr('spawn.recordHatch')}
            </button>
          )}
          {mode === 'spawning' && !published && (
            <button onClick={onPublish} disabled={publishing}
              className="mt-2 px-2.5 py-1 text-xs rounded border border-emerald-500/50 text-emerald-300 hover:bg-emerald-500/10 disabled:opacity-40">
              {publishing ? '…' : tr('spawn.publish')}
            </button>
          )}
        </div>
        {mode === 'spawning' && (
          <button onClick={onDelete} className="p-1 text-gray-500 hover:text-red-400 flex-shrink-0" aria-label={tr('spawnPanel.deleteAriaLabel')}>
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

const FIELD = 'w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-green-500/60';
const LBL = 'text-xs text-gray-400 font-mono';

function SpawnModal({ groupId, tanks, cohorts, onClose, onSaved }: {
  groupId: string; tanks: Tank[]; cohorts: BroodstockCohort[];
  onClose: () => void; onSaved: () => void;
}) {
  const { tr } = useLanguage();
  const [species, setSpecies] = useState('');
  const [female, setFemale] = useState('');
  const [male, setMale] = useState('');
  const [spawnDate, setSpawnDate] = useState('');
  const [eggCount, setEggCount] = useState('');
  const [eggVolume, setEggVolume] = useState('');
  const [fertRate, setFertRate] = useState('');
  const [tankId, setTankId] = useState('');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 모계 선택 시 어종 자동 채움(비어있을 때).
  const onSelectFemale = (id: string) => {
    setFemale(id);
    const c = cohorts.find(x => x.cohort_id === id);
    if (c && !species) setSpecies(c.species);
  };

  const canSubmit = species.trim().length > 0 && (Number(eggCount) > 0 || Number(eggVolume) > 0) && !saving;

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true); setError(null);
    const body: NewSpawnBatchBody = {
      group_id: groupId, tank_id: tankId || undefined, species: species.trim(),
      female_cohort_id: female || undefined, male_cohort_id: male || undefined,
      spawn_date: spawnDate || undefined,
      egg_count: Number(eggCount) || 0, egg_volume_ml: Number(eggVolume) || 0,
      fertilization_rate: Number(fertRate) || 0, notes: notes.trim() || undefined,
    };
    try { await SpawnBatches.create(body); onSaved(); }
    catch (err) { setError(err instanceof ApiError ? `${err.code}: ${err.message}` : err instanceof Error ? err.message : 'error'); setSaving(false); }
  };

  return (
    <ModalShell title={tr('spawn.addTitle')} onClose={onClose}>
      <label className="block"><span className={LBL}>{tr('broodstock.species')}</span>
        <input type="text" value={species} onChange={e => setSpecies(e.target.value)} className={FIELD} placeholder="ex) atlantic_salmon" /></label>
      <div className="grid grid-cols-2 gap-3">
        <label className="block"><span className={LBL}>{tr('spawn.female')}</span>
          <select value={female} onChange={e => onSelectFemale(e.target.value)} className={FIELD}>
            <option value="">—</option>
            {cohorts.map(c => <option key={c.cohort_id} value={c.cohort_id}>{c.species} {c.generation} ({c.origin_type})</option>)}
          </select></label>
        <label className="block"><span className={LBL}>{tr('spawn.male')}</span>
          <select value={male} onChange={e => setMale(e.target.value)} className={FIELD}>
            <option value="">—</option>
            {cohorts.map(c => <option key={c.cohort_id} value={c.cohort_id}>{c.species} {c.generation} ({c.origin_type})</option>)}
          </select></label>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <label className="block"><span className={LBL}>{tr('spawn.eggCount')}</span>
          <input type="number" min={0} value={eggCount} onChange={e => setEggCount(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('spawn.eggVolume')}</span>
          <input type="number" min={0} step="0.1" value={eggVolume} onChange={e => setEggVolume(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('spawn.fertRate')}</span>
          <input type="number" min={0} max={100} value={fertRate} onChange={e => setFertRate(e.target.value)} className={FIELD} /></label>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <label className="block"><span className={LBL}>{tr('spawn.spawnDate')}</span>
          <input type="date" value={spawnDate} onChange={e => setSpawnDate(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('broodstock.tank')}</span>
          <select value={tankId} onChange={e => setTankId(e.target.value)} className={FIELD}>
            <option value="">—</option>
            {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name ?? t.tank_id}</option>)}
          </select></label>
      </div>
      <label className="block"><span className={LBL}>{tr('broodstock.notes')}</span>
        <textarea value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={`${FIELD} resize-none`} /></label>
      {error && <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">{error}</div>}
      <ModalActions saving={saving} canSubmit={canSubmit} onClose={onClose} onSubmit={() => void submit()} tr={tr} />
    </ModalShell>
  );
}

function HatchModal({ batch, onClose, onSaved }: { batch: SpawnBatch; onClose: () => void; onSaved: () => void; }) {
  const { tr } = useLanguage();
  const [hatchDate, setHatchDate] = useState(batch.hatch_date ?? '');
  const [hatchedCount, setHatchedCount] = useState(String(batch.hatched_count || ''));
  const [hatchRate, setHatchRate] = useState(String(batch.hatch_rate || ''));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 부화율 자동 제안(개수 입력 시).
  const onCount = (v: string) => {
    setHatchedCount(v);
    if (batch.egg_count > 0 && Number(v) >= 0) setHatchRate(((Number(v) / batch.egg_count) * 100).toFixed(1));
  };
  const canSubmit = Number(hatchedCount) >= 0 && !saving;

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true); setError(null);
    const body: NewSpawnBatchBody = {
      ...toBody(batch),
      hatch_date: hatchDate || undefined,
      hatched_count: Number(hatchedCount) || 0,
      hatch_rate: Number(hatchRate) || 0,
      status: 'hatched',
    };
    try { await SpawnBatches.update(batch.batch_id, body); onSaved(); }
    catch (err) { setError(err instanceof ApiError ? `${err.code}: ${err.message}` : err instanceof Error ? err.message : 'error'); setSaving(false); }
  };

  return (
    <ModalShell title={tr('spawn.recordHatch')} onClose={onClose}>
      <div className="text-xs text-gray-500 font-mono">{batch.species} · {batch.lot_code} · {tr('spawn.eggCount')} {batch.egg_count.toLocaleString()}</div>
      <div className="grid grid-cols-3 gap-3">
        <label className="block"><span className={LBL}>{tr('spawn.hatchDate')}</span>
          <input type="date" value={hatchDate} onChange={e => setHatchDate(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('spawn.hatchedCount')}</span>
          <input type="number" min={0} value={hatchedCount} onChange={e => onCount(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('spawn.hatchRate')}</span>
          <input type="number" min={0} max={100} value={hatchRate} onChange={e => setHatchRate(e.target.value)} className={FIELD} /></label>
      </div>
      {error && <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">{error}</div>}
      <ModalActions saving={saving} canSubmit={canSubmit} onClose={onClose} onSubmit={() => void submit()} tr={tr} />
    </ModalShell>
  );
}

function ModalShell({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-lg mx-4 bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-5 shadow-2xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4 pb-3 border-b border-green-500/20">
          <h4 className="font-medium text-white">{title}</h4>
          <button onClick={onClose} className="text-gray-500 hover:text-white"><X className="w-4 h-4" /></button>
        </div>
        <div className="space-y-3">{children}</div>
      </div>
    </div>
  );
}

function ModalActions({ saving, canSubmit, onClose, onSubmit, tr }: {
  saving: boolean; canSubmit: boolean; onClose: () => void; onSubmit: () => void; tr: (k: string) => string;
}) {
  return (
    <div className="flex items-center justify-end gap-2 pt-2">
      <button onClick={onClose} className="px-3 py-1.5 text-sm text-gray-400 hover:text-white" disabled={saving}>{tr('broodstock.cancel')}</button>
      <button onClick={onSubmit} disabled={!canSubmit}
        className="px-4 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white disabled:opacity-40 disabled:cursor-not-allowed">
        {saving ? '…' : tr('broodstock.save')}
      </button>
    </div>
  );
}
