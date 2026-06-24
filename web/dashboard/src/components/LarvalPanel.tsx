import { useState, useEffect, useCallback } from 'react';
import { Plus, X, Trash2 } from 'lucide-react';
import type { LarvalBatch, NewLarvalBatchBody, LiveFeedCulture, NewLiveFeedBody, SpawnBatch, Tank } from '../lib/types';
import { LarvalBatches, LiveFeed, SpawnBatches, Groups, ApiError } from '../lib/api';
import { useLanguage } from '../lib/language-context';
import { ConfirmDialog } from './ui/confirm-dialog';

const FIELD = 'w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-green-500/60';
const LBL = 'text-xs text-gray-400 font-mono';
const DEV_STAGES = ['yolk_sac', 'first_feeding', 'metamorphosis', 'juvenile'];
const FEED_TYPES = ['rotifer', 'artemia', 'microalgae', 'copepod', 'other'];
const LARVAL_STATUS_CLS: Record<string, string> = {
  rearing: 'border-amber-500/40 bg-amber-500/10 text-amber-300',
  graduated: 'border-green-500/40 bg-green-500/10 text-green-300',
  discarded: 'border-gray-600 bg-gray-700/20 text-gray-400',
};
const FEED_STATUS_CLS: Record<string, string> = {
  culturing: 'border-green-500/40 bg-green-500/10 text-green-300',
  harvesting: 'border-sky-500/40 bg-sky-500/10 text-sky-300',
  crashed: 'border-red-500/40 bg-red-500/10 text-red-300',
  ended: 'border-gray-600 bg-gray-700/20 text-gray-400',
};

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

function Actions({ saving, canSubmit, onClose, onSubmit, tr }: {
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

// ───────────────────────── Larval (자어 육성) ─────────────────────────
export function LarvalPanel({ groupId, tanks }: { groupId: string; tanks: Tank[] }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<LarvalBatch[]>([]);
  const [eggLots, setEggLots] = useState<SpawnBatch[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState<LarvalBatch | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    LarvalBatches.list(groupId).then(r => setItems(r.items)).catch(() => setItems([])).finally(() => setLoading(false));
  }, [groupId]);
  useEffect(() => { reload(); }, [reload]);

  // 부모 알 lot 선택용 — spawning 단계 그룹들의 spawn batch 수집.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const gs = await Groups.list();
        const sgroups = gs.items.filter(g => (g.metadata as { stage_role?: string } | undefined)?.stage_role === 'spawning');
        const all: SpawnBatch[] = [];
        for (const g of sgroups) {
          try { const r = await SpawnBatches.list(g.group_id); all.push(...r.items); } catch { /* skip */ }
        }
        if (!cancelled) setEggLots(all);
      } catch { if (!cancelled) setEggLots([]); }
    })();
    return () => { cancelled = true; };
  }, []);

  const handleDelete = async () => {
    if (!confirmTarget) return;
    try { await LarvalBatches.delete(confirmTarget.batch_id); } catch { /* ignore */ }
    setConfirmTarget(null); reload();
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500 font-mono">{items.length} {tr('larval.batchUnit')}</span>
        <button onClick={() => setShowCreate(true)} className="flex items-center gap-1 px-3 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white">
          <Plus className="w-3.5 h-3.5" /> {tr('larval.add')}
        </button>
      </div>
      {loading ? <div className="py-10 text-center text-sm text-gray-500 font-mono">…</div>
        : items.length === 0 ? <div className="py-12 text-center text-sm text-gray-500 italic">{tr('larval.empty')}</div>
        : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {items.map(b => <LarvalCard key={b.batch_id} batch={b} tanks={tanks} onDelete={() => setConfirmTarget(b)} tr={tr} />)}
          </div>
        )}
      {showCreate && <LarvalModal groupId={groupId} tanks={tanks} eggLots={eggLots}
        onClose={() => setShowCreate(false)} onSaved={() => { setShowCreate(false); reload(); }} />}
      <ConfirmDialog open={confirmTarget !== null} title={tr('larval.deleteTitle')} message={tr('larval.deleteConfirm')}
        onConfirm={() => void handleDelete()} onCancel={() => setConfirmTarget(null)} />
    </div>
  );
}

function LarvalCard({ batch, tanks, onDelete, tr }: { batch: LarvalBatch; tanks: Tank[]; onDelete: () => void; tr: (k: string) => string }) {
  const tankName = tanks.find(t => t.tank_id === batch.tank_id)?.display_name ?? batch.tank_id;
  const cls = LARVAL_STATUS_CLS[batch.status] ?? LARVAL_STATUS_CLS.rearing;
  const origin = batch.origin_type === 'wild' ? tr('broodstock.originWild') : batch.origin_type === 'domestic' ? tr('broodstock.originDomestic') : '';
  return (
    <div className="border border-gray-700/60 rounded-lg p-3 bg-black/30">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-white truncate">{batch.species}</span>
            <span className={`px-2 py-0.5 rounded text-[11px] font-mono border ${cls}`}>{tr(`larval.status.${batch.status}`)}</span>
            {batch.dev_stage && <span className="px-2 py-0.5 rounded text-[11px] font-mono border border-gray-600 text-gray-300">{tr(`larval.stage.${batch.dev_stage}`)}</span>}
          </div>
          <div className="text-xs text-gray-400 font-mono mt-1 space-y-0.5">
            {(origin || batch.generation || batch.origin_region) && (
              <div>{tr('spawn.pedigree')}: {[origin, batch.generation, batch.origin_region].filter(Boolean).join(' · ')}</div>
            )}
            {batch.source_lot_code && <div>← {batch.source_lot_code}</div>}
            <div>
              {tr('larval.count')} {batch.current_count.toLocaleString()}/{batch.initial_count.toLocaleString()}
              {batch.initial_count > 0 && <> ({batch.survival_rate.toFixed(1)}%)</>}
              {batch.density_per_l > 0 && <> · {batch.density_per_l}/L</>}
            </div>
            {batch.tank_id && <div>{tr('broodstock.tank')}: {tankName}{batch.start_date ? ` · ${batch.start_date}` : ''}</div>}
          </div>
        </div>
        <button onClick={onDelete} className="p-1 text-gray-500 hover:text-red-400 flex-shrink-0" aria-label="delete"><Trash2 className="w-3.5 h-3.5" /></button>
      </div>
    </div>
  );
}

function LarvalModal({ groupId, tanks, eggLots, onClose, onSaved }: {
  groupId: string; tanks: Tank[]; eggLots: SpawnBatch[]; onClose: () => void; onSaved: () => void;
}) {
  const { tr } = useLanguage();
  const [source, setSource] = useState('');
  const [species, setSpecies] = useState('');
  const [tankId, setTankId] = useState('');
  const [startDate, setStartDate] = useState('');
  const [initial, setInitial] = useState('');
  const [current, setCurrent] = useState('');
  const [devStage, setDevStage] = useState('');
  const [density, setDensity] = useState('');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onSource = (lot: string) => {
    setSource(lot);
    const e = eggLots.find(x => x.lot_code === lot);
    if (e) { if (!species) setSpecies(e.species); if (!initial) setInitial(String(e.hatched_count || '')); }
  };
  const canSubmit = (species.trim() !== '' || source !== '') && !saving;

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true); setError(null);
    const body: NewLarvalBatchBody = {
      group_id: groupId, tank_id: tankId || undefined, species: species.trim() || undefined,
      source_lot_code: source || undefined, start_date: startDate || undefined,
      initial_count: Number(initial) || 0, current_count: Number(current) || 0,
      dev_stage: devStage || undefined, density_per_l: Number(density) || 0, notes: notes.trim() || undefined,
    };
    try { await LarvalBatches.create(body); onSaved(); }
    catch (err) { setError(err instanceof ApiError ? `${err.code}: ${err.message}` : err instanceof Error ? err.message : 'error'); setSaving(false); }
  };

  return (
    <ModalShell title={tr('larval.addTitle')} onClose={onClose}>
      <label className="block"><span className={LBL}>{tr('larval.source')}</span>
        <select value={source} onChange={e => onSource(e.target.value)} className={FIELD}>
          <option value="">—</option>
          {eggLots.map(e => <option key={e.batch_id} value={e.lot_code}>{e.lot_code} · {e.species}</option>)}
        </select></label>
      <div className="grid grid-cols-2 gap-3">
        <label className="block"><span className={LBL}>{tr('broodstock.species')}</span>
          <input type="text" value={species} onChange={e => setSpecies(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('broodstock.tank')}</span>
          <select value={tankId} onChange={e => setTankId(e.target.value)} className={FIELD}>
            <option value="">—</option>
            {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name ?? t.tank_id}</option>)}
          </select></label>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <label className="block"><span className={LBL}>{tr('larval.initial')}</span>
          <input type="number" min={0} value={initial} onChange={e => setInitial(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('larval.current')}</span>
          <input type="number" min={0} value={current} onChange={e => setCurrent(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('larval.density')}</span>
          <input type="number" min={0} step="0.1" value={density} onChange={e => setDensity(e.target.value)} className={FIELD} /></label>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <label className="block"><span className={LBL}>{tr('larval.devStage')}</span>
          <select value={devStage} onChange={e => setDevStage(e.target.value)} className={FIELD}>
            <option value="">—</option>
            {DEV_STAGES.map(s => <option key={s} value={s}>{tr(`larval.stage.${s}`)}</option>)}
          </select></label>
        <label className="block"><span className={LBL}>{tr('larval.startDate')}</span>
          <input type="date" value={startDate} onChange={e => setStartDate(e.target.value)} className={FIELD} /></label>
      </div>
      <label className="block"><span className={LBL}>{tr('broodstock.notes')}</span>
        <textarea value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={`${FIELD} resize-none`} /></label>
      {error && <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">{error}</div>}
      <Actions saving={saving} canSubmit={canSubmit} onClose={onClose} onSubmit={() => void submit()} tr={tr} />
    </ModalShell>
  );
}

// ───────────────────────── Live Feed (먹이배양실) ─────────────────────────
export function LiveFeedPanel({ groupId, tanks }: { groupId: string; tanks: Tank[] }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<LiveFeedCulture[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState<LiveFeedCulture | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    LiveFeed.list(groupId).then(r => setItems(r.items)).catch(() => setItems([])).finally(() => setLoading(false));
  }, [groupId]);
  useEffect(() => { reload(); }, [reload]);

  const handleDelete = async () => {
    if (!confirmTarget) return;
    try { await LiveFeed.delete(confirmTarget.culture_id); } catch { /* ignore */ }
    setConfirmTarget(null); reload();
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500 font-mono">{items.length} {tr('liveFeed.unit')}</span>
        <button onClick={() => setShowCreate(true)} className="flex items-center gap-1 px-3 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white">
          <Plus className="w-3.5 h-3.5" /> {tr('liveFeed.add')}
        </button>
      </div>
      {loading ? <div className="py-10 text-center text-sm text-gray-500 font-mono">…</div>
        : items.length === 0 ? <div className="py-12 text-center text-sm text-gray-500 italic">{tr('liveFeed.empty')}</div>
        : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {items.map(c => <FeedCard key={c.culture_id} culture={c} tanks={tanks} onDelete={() => setConfirmTarget(c)} tr={tr} />)}
          </div>
        )}
      {showCreate && <FeedModal groupId={groupId} tanks={tanks}
        onClose={() => setShowCreate(false)} onSaved={() => { setShowCreate(false); reload(); }} />}
      <ConfirmDialog open={confirmTarget !== null} title={tr('liveFeed.deleteTitle')} message={tr('liveFeed.deleteConfirm')}
        onConfirm={() => void handleDelete()} onCancel={() => setConfirmTarget(null)} />
    </div>
  );
}

function FeedCard({ culture, tanks, onDelete, tr }: { culture: LiveFeedCulture; tanks: Tank[]; onDelete: () => void; tr: (k: string) => string }) {
  const tankName = tanks.find(t => t.tank_id === culture.tank_id)?.display_name ?? culture.tank_id;
  const cls = FEED_STATUS_CLS[culture.status] ?? FEED_STATUS_CLS.culturing;
  return (
    <div className="border border-gray-700/60 rounded-lg p-3 bg-black/30">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-white truncate">{tr(`liveFeed.type.${culture.feed_type}`)}</span>
            <span className={`px-2 py-0.5 rounded text-[11px] font-mono border ${cls}`}>{tr(`liveFeed.status.${culture.status}`)}</span>
            {culture.strain && <span className="text-[11px] text-gray-500 font-mono">{culture.strain}</span>}
          </div>
          <div className="text-xs text-gray-400 font-mono mt-1 space-y-0.5">
            <div>
              {culture.density_per_ml > 0 && <>{tr('liveFeed.density')} {culture.density_per_ml}/ml</>}
              {culture.volume_l > 0 && <> · {culture.volume_l}L</>}
              {culture.start_date && <> · {culture.start_date}</>}
            </div>
            {(culture.last_harvest_date || culture.harvest_amount) && (
              <div>{tr('liveFeed.harvest')}: {[culture.harvest_amount, culture.last_harvest_date].filter(Boolean).join(' · ')}</div>
            )}
            {culture.tank_id && <div>{tr('broodstock.tank')}: {tankName}</div>}
          </div>
        </div>
        <button onClick={onDelete} className="p-1 text-gray-500 hover:text-red-400 flex-shrink-0" aria-label="delete"><Trash2 className="w-3.5 h-3.5" /></button>
      </div>
    </div>
  );
}

function FeedModal({ groupId, tanks, onClose, onSaved }: {
  groupId: string; tanks: Tank[]; onClose: () => void; onSaved: () => void;
}) {
  const { tr } = useLanguage();
  const [feedType, setFeedType] = useState('rotifer');
  const [strain, setStrain] = useState('');
  const [tankId, setTankId] = useState('');
  const [startDate, setStartDate] = useState('');
  const [volume, setVolume] = useState('');
  const [density, setDensity] = useState('');
  const [harvestDate, setHarvestDate] = useState('');
  const [harvestAmt, setHarvestAmt] = useState('');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (saving) return;
    setSaving(true); setError(null);
    const body: NewLiveFeedBody = {
      group_id: groupId, tank_id: tankId || undefined, feed_type: feedType,
      strain: strain.trim() || undefined, start_date: startDate || undefined,
      volume_l: Number(volume) || 0, density_per_ml: Number(density) || 0,
      last_harvest_date: harvestDate || undefined, harvest_amount: harvestAmt.trim() || undefined,
      notes: notes.trim() || undefined,
    };
    try { await LiveFeed.create(body); onSaved(); }
    catch (err) { setError(err instanceof ApiError ? `${err.code}: ${err.message}` : err instanceof Error ? err.message : 'error'); setSaving(false); }
  };

  return (
    <ModalShell title={tr('liveFeed.addTitle')} onClose={onClose}>
      <div className="grid grid-cols-2 gap-3">
        <label className="block"><span className={LBL}>{tr('liveFeed.feedType')}</span>
          <select value={feedType} onChange={e => setFeedType(e.target.value)} className={FIELD}>
            {FEED_TYPES.map(f => <option key={f} value={f}>{tr(`liveFeed.type.${f}`)}</option>)}
          </select></label>
        <label className="block"><span className={LBL}>{tr('liveFeed.strain')}</span>
          <input type="text" value={strain} onChange={e => setStrain(e.target.value)} className={FIELD} /></label>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <label className="block"><span className={LBL}>{tr('liveFeed.volume')}</span>
          <input type="number" min={0} step="0.1" value={volume} onChange={e => setVolume(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('liveFeed.density')}</span>
          <input type="number" min={0} step="0.1" value={density} onChange={e => setDensity(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('broodstock.tank')}</span>
          <select value={tankId} onChange={e => setTankId(e.target.value)} className={FIELD}>
            <option value="">—</option>
            {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name ?? t.tank_id}</option>)}
          </select></label>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <label className="block"><span className={LBL}>{tr('liveFeed.harvestDate')}</span>
          <input type="date" value={harvestDate} onChange={e => setHarvestDate(e.target.value)} className={FIELD} /></label>
        <label className="block"><span className={LBL}>{tr('liveFeed.harvestAmount')}</span>
          <input type="text" value={harvestAmt} onChange={e => setHarvestAmt(e.target.value)} className={FIELD} placeholder="ex) 2.0e9" /></label>
      </div>
      <label className="block"><span className={LBL}>{tr('liveFeed.startDate')}</span>
        <input type="date" value={startDate} onChange={e => setStartDate(e.target.value)} className={FIELD} /></label>
      <label className="block"><span className={LBL}>{tr('broodstock.notes')}</span>
        <textarea value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={`${FIELD} resize-none`} /></label>
      {error && <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">{error}</div>}
      <Actions saving={saving} canSubmit={!saving} onClose={onClose} onSubmit={() => void submit()} tr={tr} />
    </ModalShell>
  );
}
