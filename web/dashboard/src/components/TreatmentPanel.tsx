import { useState, useEffect, useCallback } from 'react';
import { Plus, X, Trash2 } from 'lucide-react';
import type {
  HatcheryTreatment, NewHatcheryTreatmentBody, HatcheryTreatmentType,
  SpawnBatch, LarvalBatch, Tank,
} from '../lib/types';
import { HatcheryTreatments as TreatmentApi, SpawnBatches, LarvalBatches, Groups, ApiError } from '../lib/api';
import { useLanguage } from '../lib/language-context';
import { ConfirmDialog } from './ui/confirm-dialog';

const FIELD = 'w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-green-500/60';
const LBL = 'text-xs text-gray-400 font-mono';
const TREATMENT_TYPES: HatcheryTreatmentType[] = [
  'sex_reversal', 'disinfection', 'antibiotic', 'vaccine', 'chemical', 'probiotic', 'anesthetic', 'other',
];
const TYPE_CLS: Record<string, string> = {
  sex_reversal: 'border-purple-500/40 bg-purple-500/10 text-purple-300',
  disinfection: 'border-sky-500/40 bg-sky-500/10 text-sky-300',
  antibiotic: 'border-red-500/40 bg-red-500/10 text-red-300',
};

export function TreatmentPanel({ groupId, tanks }: { groupId: string; tanks: Tank[] }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<HatcheryTreatment[]>([]);
  const [spawnLots, setSpawnLots] = useState<SpawnBatch[]>([]);
  const [larvalLots, setLarvalLots] = useState<LarvalBatch[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState<HatcheryTreatment | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    TreatmentApi.list(groupId).then(r => setItems(r.items)).catch(() => setItems([])).finally(() => setLoading(false));
  }, [groupId]);
  useEffect(() => { reload(); }, [reload]);

  // 처치 대상 배치 선택용 — spawning/larval 단계 그룹의 batch 수집.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const gs = await Groups.list();
        const sg = gs.items.filter(g => (g.metadata as { stage_role?: string } | undefined)?.stage_role === 'spawning');
        const lg = gs.items.filter(g => (g.metadata as { stage_role?: string } | undefined)?.stage_role === 'larval');
        const spawns: SpawnBatch[] = [];
        for (const g of sg) { try { spawns.push(...(await SpawnBatches.list(g.group_id)).items); } catch { /* skip */ } }
        const larvals: LarvalBatch[] = [];
        for (const g of lg) { try { larvals.push(...(await LarvalBatches.list(g.group_id)).items); } catch { /* skip */ } }
        if (!cancelled) { setSpawnLots(spawns); setLarvalLots(larvals); }
      } catch { if (!cancelled) { setSpawnLots([]); setLarvalLots([]); } }
    })();
    return () => { cancelled = true; };
  }, []);

  const handleDelete = async () => {
    if (!confirmTarget) return;
    try { await TreatmentApi.delete(confirmTarget.treatment_id); } catch { /* ignore */ }
    setConfirmTarget(null); reload();
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500 font-mono">{items.length} {tr('treatment.unit')}</span>
        <button onClick={() => setShowCreate(true)} className="flex items-center gap-1 px-3 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white">
          <Plus className="w-3.5 h-3.5" /> {tr('treatment.add')}
        </button>
      </div>
      {loading ? <div className="py-10 text-center text-sm text-gray-500 font-mono">…</div>
        : items.length === 0 ? <div className="py-12 text-center text-sm text-gray-500 italic">{tr('treatment.empty')}</div>
        : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {items.map(t => <TreatmentCard key={t.treatment_id} item={t} onDelete={() => setConfirmTarget(t)} tr={tr} />)}
          </div>
        )}
      {showCreate && <TreatmentModal groupId={groupId} tanks={tanks} spawnLots={spawnLots} larvalLots={larvalLots}
        onClose={() => setShowCreate(false)} onSaved={() => { setShowCreate(false); reload(); }} />}
      <ConfirmDialog open={confirmTarget !== null} title={tr('treatment.deleteTitle')} message={tr('treatment.deleteConfirm')}
        onConfirm={() => void handleDelete()} onCancel={() => setConfirmTarget(null)} />
    </div>
  );
}

function TreatmentCard({ item, onDelete, tr }: { item: HatcheryTreatment; onDelete: () => void; tr: (k: string) => string }) {
  const cls = TYPE_CLS[item.treatment_type] ?? 'border-gray-600 bg-gray-700/20 text-gray-300';
  return (
    <div className="border border-gray-700/60 rounded-lg p-3 bg-black/30">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-white truncate">{item.substance}</span>
            <span className={`px-2 py-0.5 rounded text-[11px] font-mono border ${cls}`}>{tr(`treatment.type.${item.treatment_type}`)}</span>
            {item.lot_code && <span className="text-[11px] text-gray-500 font-mono">{item.lot_code}</span>}
          </div>
          <div className="text-xs text-gray-400 font-mono mt-1 space-y-0.5">
            <div>
              {(item.dose ?? 0) > 0 && <>{tr('treatment.dose')} {item.dose}{item.dose_unit ? ` ${item.dose_unit}` : ''}</>}
              {item.route && <> · {item.route}</>}
              {item.administered_at && <> · {item.administered_at.slice(0, 10)}</>}
            </div>
            {item.withdrawal_until && (
              <div className="text-amber-300/80">{tr('treatment.withdrawal')}: {item.withdrawal_until.slice(0, 10)}</div>
            )}
            {item.reason && <div>{item.reason}</div>}
          </div>
        </div>
        <button onClick={onDelete} className="p-1 text-gray-500 hover:text-red-400 flex-shrink-0" aria-label="delete"><Trash2 className="w-3.5 h-3.5" /></button>
      </div>
    </div>
  );
}

function TreatmentModal({ groupId, tanks, spawnLots, larvalLots, onClose, onSaved }: {
  groupId: string; tanks: Tank[]; spawnLots: SpawnBatch[]; larvalLots: LarvalBatch[];
  onClose: () => void; onSaved: () => void;
}) {
  const { tr } = useLanguage();
  const [subjectKind, setSubjectKind] = useState<'spawn' | 'larval'>('spawn');
  const [batchId, setBatchId] = useState('');
  const [species, setSpecies] = useState('');
  const [type, setType] = useState<HatcheryTreatmentType>('disinfection');
  const [substance, setSubstance] = useState('');
  const [dose, setDose] = useState('');
  const [doseUnit, setDoseUnit] = useState('');
  const [route, setRoute] = useState('');
  const [reason, setReason] = useState('');
  const [withdrawal, setWithdrawal] = useState('');
  const [administeredAt, setAdministeredAt] = useState('');
  const [tankId, setTankId] = useState('');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const batches: { id: string; label: string; species: string }[] =
    subjectKind === 'spawn'
      ? spawnLots.map(b => ({ id: b.batch_id, label: `${b.lot_code ?? b.batch_id} · ${b.species}`, species: b.species }))
      : larvalLots.map(b => ({ id: b.batch_id, label: `${b.source_lot_code ?? b.batch_id} · ${b.species}`, species: b.species }));

  const onSelectBatch = (id: string) => {
    setBatchId(id);
    const b = batches.find(x => x.id === id);
    if (b && !species) setSpecies(b.species);
  };

  const canSubmit = substance.trim() !== '' && !saving;

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true); setError(null);
    const body: NewHatcheryTreatmentBody = {
      group_id: groupId,
      subject_kind: subjectKind,
      batch_id: batchId || undefined,
      tank_id: tankId || undefined,
      species: species.trim() || undefined,
      treatment_type: type,
      substance: substance.trim(),
      dose: Number(dose) || undefined,
      dose_unit: doseUnit.trim() || undefined,
      route: route.trim() || undefined,
      reason: reason.trim() || undefined,
      withdrawal_until: withdrawal || undefined,
      administered_at: administeredAt ? new Date(administeredAt).toISOString() : new Date().toISOString(),
      notes: notes.trim() || undefined,
    };
    try { await TreatmentApi.create(body); onSaved(); }
    catch (err) { setError(err instanceof ApiError ? `${err.code}: ${err.message}` : err instanceof Error ? err.message : 'error'); setSaving(false); }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-lg mx-4 bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-5 shadow-2xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4 pb-3 border-b border-green-500/20">
          <h4 className="font-medium text-white">{tr('treatment.addTitle')}</h4>
          <button onClick={onClose} className="text-gray-500 hover:text-white"><X className="w-4 h-4" /></button>
        </div>
        <div className="space-y-3">
          <div>
            <span className={LBL}>{tr('treatment.subject')}</span>
            <div className="flex gap-1.5 mt-1">
              {(['spawn', 'larval'] as const).map(k => (
                <button key={k} onClick={() => { setSubjectKind(k); setBatchId(''); }}
                  className={`px-3 py-1 rounded text-xs font-mono border transition-all ${
                    subjectKind === k ? 'border-green-500/60 bg-green-500/15 text-green-300' : 'border-gray-700 text-gray-400 hover:text-white'
                  }`}>
                  {tr(`treatment.subject.${k}`)}
                </button>
              ))}
            </div>
          </div>

          <label className="block"><span className={LBL}>{tr('treatment.batch')}</span>
            <select value={batchId} onChange={e => onSelectBatch(e.target.value)} className={FIELD}>
              <option value="">—</option>
              {batches.map(b => <option key={b.id} value={b.id}>{b.label}</option>)}
            </select></label>

          <div className="grid grid-cols-2 gap-3">
            <label className="block"><span className={LBL}>{tr('treatment.typeLabel')}</span>
              <select value={type} onChange={e => setType(e.target.value as HatcheryTreatmentType)} className={FIELD}>
                {TREATMENT_TYPES.map(t => <option key={t} value={t}>{tr(`treatment.type.${t}`)}</option>)}
              </select></label>
            <label className="block"><span className={LBL}>{tr('treatment.substance')}</span>
              <input type="text" value={substance} onChange={e => setSubstance(e.target.value)} className={FIELD} placeholder="ex) 17a-MT, formalin" /></label>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <label className="block"><span className={LBL}>{tr('treatment.dose')}</span>
              <input type="number" min={0} step="0.01" value={dose} onChange={e => setDose(e.target.value)} className={FIELD} /></label>
            <label className="block"><span className={LBL}>{tr('treatment.doseUnit')}</span>
              <input type="text" value={doseUnit} onChange={e => setDoseUnit(e.target.value)} className={FIELD} placeholder="ppm / mg/L" /></label>
            <label className="block"><span className={LBL}>{tr('treatment.route')}</span>
              <input type="text" value={route} onChange={e => setRoute(e.target.value)} className={FIELD} placeholder="immersion" /></label>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="block"><span className={LBL}>{tr('treatment.administeredAt')}</span>
              <input type="datetime-local" value={administeredAt} onChange={e => setAdministeredAt(e.target.value)} className={FIELD} /></label>
            <label className="block"><span className={LBL}>{tr('treatment.withdrawal')}</span>
              <input type="date" value={withdrawal} onChange={e => setWithdrawal(e.target.value)} className={FIELD} /></label>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="block"><span className={LBL}>{tr('treatment.reason')}</span>
              <input type="text" value={reason} onChange={e => setReason(e.target.value)} className={FIELD} /></label>
            <label className="block"><span className={LBL}>{tr('broodstock.tank')}</span>
              <select value={tankId} onChange={e => setTankId(e.target.value)} className={FIELD}>
                <option value="">—</option>
                {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name ?? t.tank_id}</option>)}
              </select></label>
          </div>

          <label className="block"><span className={LBL}>{tr('broodstock.notes')}</span>
            <textarea value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={`${FIELD} resize-none`} /></label>

          {error && <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">{error}</div>}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button onClick={onClose} className="px-3 py-1.5 text-sm text-gray-400 hover:text-white" disabled={saving}>{tr('broodstock.cancel')}</button>
            <button onClick={() => void submit()} disabled={!canSubmit}
              className="px-4 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white disabled:opacity-40 disabled:cursor-not-allowed">
              {saving ? '…' : tr('broodstock.save')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
