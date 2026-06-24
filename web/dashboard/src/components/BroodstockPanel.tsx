import { useState, useEffect, useCallback } from 'react';
import { Plus, X, Pencil, Trash2 } from 'lucide-react';
import type { BroodstockCohort, NewBroodstockBody, BroodstockOrigin, Tank } from '../lib/types';
import { Broodstock, ApiError } from '../lib/api';
import { useLanguage } from '../lib/language-context';
import { ConfirmDialog } from './ui/confirm-dialog';

interface Props {
  groupId: string;
  tanks: Tank[];
}

const MATURITY_OPTIONS = ['immature', 'maturing', 'mature', 'spent'];

export function BroodstockPanel({ groupId, tanks }: Props) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<BroodstockCohort[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<BroodstockCohort | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState<BroodstockCohort | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    Broodstock.list(groupId)
      .then(r => setItems(r.items))
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  }, [groupId]);

  useEffect(() => { reload(); }, [reload]);

  const openAdd = () => { setEditing(null); setShowModal(true); };
  const openEdit = (c: BroodstockCohort) => { setEditing(c); setShowModal(true); };

  const handleDelete = async () => {
    if (!confirmTarget) return;
    try {
      await Broodstock.delete(confirmTarget.cohort_id);
      setConfirmTarget(null);
      reload();
    } catch {
      setConfirmTarget(null);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500 font-mono">
          {items.length} {tr('broodstock.title')}
        </span>
        <button
          onClick={openAdd}
          className="flex items-center gap-1 px-3 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white"
        >
          <Plus className="w-3.5 h-3.5" /> {tr('broodstock.add')}
        </button>
      </div>

      {loading ? (
        <div className="py-10 text-center text-sm text-gray-500 font-mono">…</div>
      ) : items.length === 0 ? (
        <div className="py-12 text-center text-sm text-gray-500 italic">{tr('broodstock.empty')}</div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {items.map(c => (
            <CohortCard key={c.cohort_id} cohort={c} tanks={tanks}
              onEdit={() => openEdit(c)} onDelete={() => setConfirmTarget(c)} />
          ))}
        </div>
      )}

      {showModal && (
        <CohortModal
          groupId={groupId}
          tanks={tanks}
          cohorts={items}
          editing={editing}
          onClose={() => setShowModal(false)}
          onSaved={() => { setShowModal(false); reload(); }}
        />
      )}

      <ConfirmDialog
        open={confirmTarget !== null}
        title={tr('broodstock.deleteTitle')}
        message={tr('broodstock.deleteConfirm')}
        onConfirm={() => void handleDelete()}
        onCancel={() => setConfirmTarget(null)}
      />
    </div>
  );
}

function CohortCard({ cohort, tanks, onEdit, onDelete }: {
  cohort: BroodstockCohort; tanks: Tank[];
  onEdit: () => void; onDelete: () => void;
}) {
  const { tr } = useLanguage();
  const tankName = tanks.find(t => t.tank_id === cohort.tank_id)?.display_name ?? cohort.tank_id;
  const originLabel = cohort.origin_type === 'wild' ? tr('broodstock.originWild') : tr('broodstock.originDomestic');
  const originCls = cohort.origin_type === 'wild'
    ? 'border-sky-500/40 bg-sky-500/10 text-sky-300'
    : 'border-amber-500/40 bg-amber-500/10 text-amber-300';
  return (
    <div className="border border-gray-700/60 rounded-lg p-3 bg-black/30">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-medium text-white truncate">{cohort.species}</span>
            <span className={`px-2 py-0.5 rounded text-[11px] font-mono border ${originCls}`}>{originLabel}</span>
            {cohort.generation && (
              <span className="px-2 py-0.5 rounded text-[11px] font-mono border border-gray-600 text-gray-300">{cohort.generation}</span>
            )}
          </div>
          <div className="text-xs text-gray-400 font-mono mt-1 space-y-0.5">
            {(cohort.origin_region || cohort.supplier) && (
              <div>{[cohort.origin_region, cohort.supplier].filter(Boolean).join(' · ')}</div>
            )}
            <div>
              {tr('broodstock.maleCount')} {cohort.male_count} · {tr('broodstock.femaleCount')} {cohort.female_count}
              {cohort.maturity && <> · {tr(`broodstock.maturity.${cohort.maturity}`)}</>}
            </div>
            {cohort.tank_id && <div>{tr('broodstock.tank')}: {tankName}</div>}
          </div>
        </div>
        <div className="flex items-center gap-1 flex-shrink-0">
          <button onClick={onEdit} className="p-1 text-gray-500 hover:text-white" aria-label="edit">
            <Pencil className="w-3.5 h-3.5" />
          </button>
          <button onClick={onDelete} className="p-1 text-gray-500 hover:text-red-400" aria-label="delete">
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
}

interface ModalProps {
  groupId: string;
  tanks: Tank[];
  cohorts: BroodstockCohort[];
  editing: BroodstockCohort | null;
  onClose: () => void;
  onSaved: () => void;
}

function CohortModal({ groupId, tanks, cohorts, editing, onClose, onSaved }: ModalProps) {
  const { tr } = useLanguage();
  const [species, setSpecies] = useState(editing?.species ?? '');
  const [origin, setOrigin] = useState<BroodstockOrigin>(editing?.origin_type ?? 'wild');
  const [region, setRegion] = useState(editing?.origin_region ?? '');
  const [supplier, setSupplier] = useState(editing?.supplier ?? '');
  const [generation, setGeneration] = useState(editing?.generation ?? '');
  const [parentCohort, setParentCohort] = useState(editing?.parent_cohort_id ?? '');
  const [acquiredDate, setAcquiredDate] = useState(editing?.acquired_date ?? '');
  const [maleCount, setMaleCount] = useState(String(editing?.male_count ?? 0));
  const [femaleCount, setFemaleCount] = useState(String(editing?.female_count ?? 0));
  const [maturity, setMaturity] = useState(editing?.maturity ?? '');
  const [tankId, setTankId] = useState(editing?.tank_id ?? '');
  const [notes, setNotes] = useState(editing?.notes ?? '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = species.trim().length > 0 && !saving;

  const submit = async () => {
    if (!canSubmit) return;
    setSaving(true);
    setError(null);
    const body: NewBroodstockBody = {
      group_id: groupId,
      tank_id: tankId || undefined,
      species: species.trim(),
      origin_type: origin,
      origin_region: region.trim() || undefined,
      supplier: supplier.trim() || undefined,
      generation: generation.trim() || undefined,
      parent_cohort_id: parentCohort || undefined,
      acquired_date: acquiredDate || undefined,
      male_count: Number(maleCount) || 0,
      female_count: Number(femaleCount) || 0,
      maturity: maturity || undefined,
      notes: notes.trim() || undefined,
    };
    try {
      if (editing) await Broodstock.update(editing.cohort_id, body);
      else await Broodstock.create(body);
      onSaved();
    } catch (err) {
      setError(err instanceof ApiError ? `${err.code}: ${err.message}` : err instanceof Error ? err.message : 'error');
      setSaving(false);
    }
  };

  const field = 'w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-green-500/60';
  const lbl = 'text-xs text-gray-400 font-mono';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-lg mx-4 bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-5 shadow-2xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4 pb-3 border-b border-green-500/20">
          <h4 className="font-medium text-white">{editing ? tr('broodstock.editTitle') : tr('broodstock.addTitle')}</h4>
          <button onClick={onClose} className="text-gray-500 hover:text-white"><X className="w-4 h-4" /></button>
        </div>

        <div className="space-y-3">
          <label className="block">
            <span className={lbl}>{tr('broodstock.species')}</span>
            <input type="text" value={species} onChange={e => setSpecies(e.target.value)} className={field} placeholder="ex) atlantic_salmon" />
          </label>

          <div>
            <span className={lbl}>{tr('broodstock.origin')}</span>
            <div className="flex gap-1.5 mt-1">
              {(['wild', 'domestic'] as BroodstockOrigin[]).map(o => (
                <button key={o} onClick={() => setOrigin(o)}
                  className={`px-3 py-1 rounded text-xs font-mono border transition-all ${
                    origin === o ? 'border-green-500/60 bg-green-500/15 text-green-300' : 'border-gray-700 text-gray-400 hover:text-white'
                  }`}>
                  {o === 'wild' ? tr('broodstock.originWild') : tr('broodstock.originDomestic')}
                </button>
              ))}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="block"><span className={lbl}>{tr('broodstock.region')}</span>
              <input type="text" value={region} onChange={e => setRegion(e.target.value)} className={field} /></label>
            <label className="block"><span className={lbl}>{tr('broodstock.supplier')}</span>
              <input type="text" value={supplier} onChange={e => setSupplier(e.target.value)} className={field} /></label>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="block"><span className={lbl}>{tr('broodstock.generation')}</span>
              <input type="text" value={generation} onChange={e => setGeneration(e.target.value)} className={field} placeholder="F0 / F1" /></label>
            <label className="block"><span className={lbl}>{tr('broodstock.acquiredDate')}</span>
              <input type="date" value={acquiredDate} onChange={e => setAcquiredDate(e.target.value)} className={field} /></label>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <label className="block"><span className={lbl}>{tr('broodstock.maleCount')}</span>
              <input type="number" min={0} value={maleCount} onChange={e => setMaleCount(e.target.value)} className={field} /></label>
            <label className="block"><span className={lbl}>{tr('broodstock.femaleCount')}</span>
              <input type="number" min={0} value={femaleCount} onChange={e => setFemaleCount(e.target.value)} className={field} /></label>
            <label className="block"><span className={lbl}>{tr('broodstock.maturity')}</span>
              <select value={maturity} onChange={e => setMaturity(e.target.value)} className={field}>
                <option value="">—</option>
                {MATURITY_OPTIONS.map(m => <option key={m} value={m}>{tr(`broodstock.maturity.${m}`)}</option>)}
              </select></label>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="block"><span className={lbl}>{tr('broodstock.tank')}</span>
              <select value={tankId} onChange={e => setTankId(e.target.value)} className={field}>
                <option value="">—</option>
                {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name ?? t.tank_id}</option>)}
              </select></label>
            <label className="block"><span className={lbl}>{tr('broodstock.parentCohort')}</span>
              <select value={parentCohort} onChange={e => setParentCohort(e.target.value)} className={field}>
                <option value="">—</option>
                {cohorts.filter(c => c.cohort_id !== editing?.cohort_id).map(c => (
                  <option key={c.cohort_id} value={c.cohort_id}>{c.species} {c.generation}</option>
                ))}
              </select></label>
          </div>

          <label className="block"><span className={lbl}>{tr('broodstock.notes')}</span>
            <textarea value={notes} onChange={e => setNotes(e.target.value)} rows={2} className={`${field} resize-none`} /></label>

          {error && (
            <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">{error}</div>
          )}

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
