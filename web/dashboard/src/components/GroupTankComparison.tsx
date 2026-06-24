import { useState, useEffect, useMemo } from 'react';
import { Fish, Plus, Trash2 } from 'lucide-react';
import { Cameras, Tanks, SpeciesProfiles, ApiError } from '../lib/api';
import type { Camera, Tank, StateVector, NewTankBody, SpeciesProfile } from '../lib/types';
import { CameraTile } from './CameraTile';
import { Button } from './ui/button';
import { ConfirmDialog } from './ui/confirm-dialog';
import { relativeTime } from '../lib/format';
import { computeVolumeFromStrings, volumeHint } from '../lib/volume';
import { useLanguage } from '../lib/language-context';

interface Props {
  tanks: Tank[];
  stateVectors: Map<string, StateVector>;
  onSelectTank: (tankId: string) => void;
  groupId?: string;
  onTanksChanged?: () => void;
}

function friendlyError(err: unknown, tr: (key: string) => string): string {
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return tr('groupTankComparison.unknownError');
}

// tank_id 직접 필드 우선 → camera_id 패턴
function resolveTankId(cam: Camera): string | undefined {
  if (cam.tank_id) return cam.tank_id;
  const m = cam.camera_id.match(/tank_([^_]+)/);
  return m ? `tank_${m[1]}` : undefined;
}

// label 은 i18n 키(off 는 보편 토큰이라 그대로). 렌더 시 tr 로 해석한다.
const AUTONOMOUS_LABELS: Record<string, { label: string; cls: string }> = {
  off: { label: 'OFF', cls: 'bg-gray-700 text-gray-300' },
  observation: { label: 'groupTankComparison.autonomyObservation', cls: 'bg-blue-500/20 text-blue-400 border border-blue-500/30' },
  partial: { label: 'groupTankComparison.autonomyPartial', cls: 'bg-orange-500/20 text-orange-400 border border-orange-500/30' },
  full: { label: 'groupTankComparison.autonomyFull', cls: 'bg-green-500/20 text-green-400 border border-green-500/30' },
};

// 탱크별 비교 카드 — 개별 탱크 상세 진입 없이 주요 지표를 한눈에 파악하기 위한 비교 뷰
export function GroupTankComparison({ tanks, stateVectors, onSelectTank, groupId, onTanksChanged }: Props) {
  const { tr } = useLanguage();
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [showAddForm, setShowAddForm] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Tank | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  useEffect(() => {
    Cameras.list()
      .then(res => setCameras(res.items))
      .catch(() => setCameras([]));
  }, []);

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await Tanks.delete(deleteTarget.tank_id);
      setDeleteTarget(null);
      onTanksChanged?.();
    } catch (err) {
      setDeleteError(friendlyError(err, tr));
    } finally {
      setDeleting(false);
    }
  };

  const camsForTank = (tankId: string) =>
    cameras.filter(c => resolveTankId(c) === tankId).slice(0, 3);

  return (
    <div className="space-y-3 pt-1">
      {/* 상단 액션 바 */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500 font-mono">{tr('groupTankComparison.cageTankCount', { count: tanks.length })}</span>
        <Button size="sm" variant="outline" onClick={() => setShowAddForm(true)}>
          <Plus className="w-3.5 h-3.5 mr-1" />
          {tr('groupTankComparison.addTank')}
        </Button>
      </div>

      {showAddForm && (
        <TankAddForm
          groupId={groupId ?? ''}
          onClose={() => setShowAddForm(false)}
          onSaved={() => { setShowAddForm(false); onTanksChanged?.(); }}
        />
      )}

      {tanks.length === 0 ? (
        <div className="py-10 text-center text-sm text-gray-500 italic">
          {tr('groupTankComparison.noTanks')}
        </div>
      ) : (
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {tanks.map(tank => {
        const sv = stateVectors.get(tank.tank_id);
        const bio = sv?.biological_context;
        const autonomous = sv?.autonomous;
        const anomaly = sv?.anomaly;
        const openAlerts = anomaly?.open_alerts ?? [];

        const fishCount = bio?.fish_count ?? tank.fish_count ?? 0;
        const avgWeightG = bio?.avg_weight_g ?? tank.avg_weight_g ?? 0;
        const biomassKg = bio?.biomass_kg ?? ((fishCount * avgWeightG) / 1000);

        const autoInfo = AUTONOMOUS_LABELS[autonomous?.mode ?? ''] ?? {
          label: autonomous?.mode ?? '—',
          cls: 'bg-gray-700 text-gray-300',
        };

        const severityColor =
          openAlerts.some(a => a.severity === 'critical')
            ? 'text-red-400'
            : openAlerts.some(a => a.severity === 'warning')
            ? 'text-orange-400'
            : 'text-gray-500';

        const tankCameras = camsForTank(tank.tank_id);

        return (
          <div
            key={tank.tank_id}
            className="group bg-gradient-to-br from-gray-900 to-black border border-green-500/20 rounded-xl p-4 space-y-3 relative"
          >
            {/* 휴지통 — hover 시 표시 */}
            <button
              onClick={() => { setDeleteTarget(tank); setDeleteError(null); }}
              className="absolute top-2 right-2 p-1.5 rounded opacity-0 group-hover:opacity-100 text-gray-500 hover:text-red-400 hover:bg-red-500/10 transition-all"
              aria-label={tr('groupTankComparison.deleteAriaLabel', { name: tank.display_name })}
              title={tr('groupTankComparison.deleteTankTitle')}
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>

            {/* 헤더 */}
            <div className="flex items-center gap-2 pr-7">
              <Fish className="w-4 h-4 text-green-500 flex-shrink-0" />
              <div className="min-w-0 flex-1">
                <div className="font-medium text-white truncate">{tank.display_name}</div>
                <div className="text-xs text-gray-500 font-mono">{tank.tank_id}</div>
              </div>
              <span className={`text-xs px-2 py-0.5 rounded font-mono ${autoInfo.cls}`}>
                {tr(autoInfo.label)}
              </span>
            </div>

            {/* 통계 3열 */}
            <div className="grid grid-cols-3 gap-2 text-center">
              <div className="bg-black/30 rounded-lg p-2">
                <div className="text-xs text-gray-500 mb-0.5">{tr('groupTankComparison.fishCount')}</div>
                <div className="text-sm font-mono font-medium text-white">
                  {fishCount > 0 ? fishCount.toLocaleString() : '—'}
                </div>
              </div>
              <div className="bg-black/30 rounded-lg p-2">
                <div className="text-xs text-gray-500 mb-0.5">{tr('groupTankComparison.avgWeight')}</div>
                <div className="text-sm font-mono font-medium text-white">
                  {avgWeightG > 0 ? `${avgWeightG.toFixed(0)}g` : '—'}
                </div>
              </div>
              <div className="bg-black/30 rounded-lg p-2">
                <div className="text-xs text-gray-500 mb-0.5">{tr('groupTankComparison.biomass')}</div>
                <div className="text-sm font-mono font-medium text-white">
                  {biomassKg > 0 ? `${biomassKg.toFixed(1)}kg` : '—'}
                </div>
              </div>
            </div>

            {/* 생물학적 컨텍스트 */}
            {bio && (
              <div className="text-xs text-gray-500 font-mono">
                {bio.source === 'tank_profile' ? tr('groupTankComparison.tankProfileBased') : bio.source}
                {' · '}{tank.species}
              </div>
            )}

            {/* 알림 요약 */}
            <div className={`text-xs font-mono ${severityColor}`}>
              {openAlerts.length === 0
                ? <span className="text-green-500">{tr('groupTankComparison.noAlerts')}</span>
                : tr('groupTankComparison.alertCount', { count: openAlerts.length })}
            </div>

            {/* 카메라 썸네일 스트립 */}
            {tankCameras.length > 0 && (
              <div className="flex gap-2 overflow-x-auto">
                {tankCameras.map(cam => (
                  <div key={cam.camera_id} className="flex-shrink-0 w-28">
                    <CameraTile camera={cam} />
                  </div>
                ))}
              </div>
            )}

            {/* 마지막 업데이트 */}
            {sv && (
              <div className="text-xs text-gray-600 font-mono">
                {tr('groupTankComparison.updated')} {relativeTime(sv.timestamp)}
              </div>
            )}

            {/* 상세 보기 버튼 */}
            <button
              onClick={() => onSelectTank(tank.tank_id)}
              className="w-full text-xs text-green-400 hover:text-green-300 font-mono py-1 border border-green-500/20 hover:border-green-500/50 rounded transition-colors"
            >
              {tr('groupTankComparison.viewDetail')}
            </button>
          </div>
        );
      })}
      </div>
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        title={tr('groupTankComparison.deleteTankConfirmTitle', { name: deleteTarget?.display_name ?? '' })}
        message={
          <div className="space-y-2">
            <p>
              <span className="font-mono text-red-300">{deleteTarget?.tank_id ?? ''}</span> {tr('groupTankComparison.deleteConfirmBody')}
            </p>
            <p className="text-xs text-gray-400">
              {tr('groupTankComparison.deleteConfirmNote')}
            </p>
            {deleteError && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {deleteError}
              </div>
            )}
          </div>
        }
        busy={deleting}
        onConfirm={() => void handleDelete()}
        onCancel={() => { if (!deleting) { setDeleteTarget(null); setDeleteError(null); } }}
      />
    </div>
  );
}

// ── 수조 추가 인라인 폼 ────────────────────────────────────────────────────────

function TankAddForm({ groupId, onClose, onSaved }: {
  groupId: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [tankId, setTankId] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [species, setSpecies] = useState('atlantic_salmon');
  const [systemType, setSystemType] = useState('land_based_ras');
  const [fishCount, setFishCount] = useState('');
  const [avgWeightG, setAvgWeightG] = useState('');
  const [biomassKg, setBiomassKg] = useState('');
  const [volumeM3, setVolumeM3] = useState('');
  // C-9 — 물리 정보.
  const [formFactor, setFormFactor] = useState<'' | 'round' | 'square' | 'rectangular'>('');
  const [diameterM, setDiameterM] = useState('');
  const [lengthM, setLengthM] = useState('');
  const [widthM, setWidthM] = useState('');
  const [depthM, setDepthM] = useState('');
  const [speciesOpts, setSpeciesOpts] = useState<SpeciesProfile[]>([]);
  const { tr } = useLanguage();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    SpeciesProfiles.list()
      .then(r => setSpeciesOpts(r.items))
      .catch(() => setSpeciesOpts([]));
  }, []);

  // C-10 — 용적 자동 계산.
  const autoVolume = useMemo(
    () => computeVolumeFromStrings(formFactor, diameterM, lengthM, widthM, depthM),
    [formFactor, diameterM, lengthM, widthM, depthM],
  );
  const autoVolumeHintText = useMemo(() => volumeHint({
    formFactor,
    diameterM: diameterM ? Number(diameterM) : undefined,
    lengthM: lengthM ? Number(lengthM) : undefined,
    widthM: widthM ? Number(widthM) : undefined,
    depthM: depthM ? Number(depthM) : undefined,
  }), [formFactor, diameterM, lengthM, widthM, depthM]);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const body: NewTankBody = {
        tank_id: tankId.trim(),
        display_name: displayName.trim(),
        species: species.trim(),
        system_type: systemType.trim(),
      };
      if (groupId) body.group_id = groupId;
      if (fishCount) body.fish_count = Number(fishCount);
      if (avgWeightG) body.avg_weight_g = Number(avgWeightG);
      if (biomassKg) body.biomass_kg = Number(biomassKg);
      // volume 비어있으면 자동값 사용.
      const effectiveVolume = volumeM3.trim() !== '' ? Number(volumeM3) : (autoVolume ?? undefined);
      if (effectiveVolume != null) body.volume_m3 = effectiveVolume;
      if (formFactor) body.form_factor = formFactor;
      if (diameterM) body.diameter_m = Number(diameterM);
      if (lengthM) body.length_m = Number(lengthM);
      if (widthM) body.width_m = Number(widthM);
      if (depthM) body.depth_m = Number(depthM);
      await Tanks.create(body);
      onSaved();
    } catch (err) {
      setError(friendlyError(err, tr));
      setBusy(false);
    }
  };

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-xl p-4 space-y-3">
      <div className="text-sm font-medium text-white">{tr('tankAddForm.heading')} ({tr('tankAddForm.group')}: {groupId || tr('tankAddForm.noGroup')})</div>
      <div className="grid grid-cols-2 gap-3">
        <FormInput label={tr('tankAddForm.tankId')} required value={tankId} onChange={setTankId} placeholder="ras_tank_05" />
        <FormInput label={tr('tankAddForm.displayName')} required value={displayName} onChange={setDisplayName} placeholder={tr('tankAddForm.displayNamePlaceholder')} />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <FormSelect label={tr('tankAddForm.species')} value={species} onChange={setSpecies}
          options={speciesOpts.length > 0
            ? speciesOpts.map(s => ({ value: s.species, label: s.display_name || s.species }))
            : [
              { value: 'atlantic_salmon', label: 'Atlantic Salmon' },
              { value: 'red_seabream', label: tr('tankAddForm.redSeabream') },
              { value: 'rainbow_trout', label: tr('tankAddForm.rainbowTrout') },
            ]}
        />
        <FormSelect label={tr('tankAddForm.systemType')} value={systemType} onChange={setSystemType}
          options={[
            { value: 'land_based_ras', label: tr('tankAddForm.landBasedRas') },
            { value: 'marine_cage', label: tr('tankAddForm.marineCage') },
            { value: 'flow_through', label: tr('tankAddForm.flowThrough') },
          ]}
        />
      </div>
      <div className="grid grid-cols-3 gap-3">
        <FormInput label={tr('tankAddForm.fishCountOpt')} type="number" value={fishCount} onChange={setFishCount} placeholder="1000" />
        <FormInput label={tr('tankAddForm.avgWeightOpt')} type="number" value={avgWeightG} onChange={setAvgWeightG} placeholder="100" />
        <FormInput label={tr('tankAddForm.biomassOpt')} type="number" value={biomassKg} onChange={setBiomassKg} placeholder="100" />
      </div>

      {/* C-9 — 물리 정보 (형태/직경·가로·세로/수심/용적). C-10 — 용적 자동. */}
      <div className="rounded-md border border-gray-700 p-3 space-y-2">
        <div className="text-xs text-gray-400 font-medium">{tr('tankAddForm.physicsOpt')}</div>
        <div className="grid grid-cols-3 gap-3">
          <FormSelect label={tr('tankAddForm.formFactor')} value={formFactor}
            onChange={v => setFormFactor(v as typeof formFactor)}
            options={[
              { value: '', label: tr('tankAddForm.noSelection') },
              { value: 'round', label: tr('tankAddForm.round') },
              { value: 'square', label: tr('tankAddForm.square') },
              { value: 'rectangular', label: tr('tankAddForm.rectangular') },
            ]}
          />
          <FormInput label={tr('tankAddForm.depthM')} type="number" value={depthM} onChange={setDepthM} placeholder="2.0" />
          <div className="flex flex-col gap-1">
            <label className="text-xs text-gray-400 font-medium">
              {tr('tankAddForm.volumeM3')} <span className="text-gray-600">{tr('tankAddForm.volumeAutoHint')}</span>
            </label>
            <input
              type="number"
              value={volumeM3}
              onChange={e => setVolumeM3(e.target.value)}
              placeholder={autoVolume != null ? String(autoVolume) : '12.5'}
              className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-green-500/50"
            />
            {autoVolumeHintText && (
              <span className="text-[11px] text-gray-500 font-mono">{autoVolumeHintText}</span>
            )}
          </div>
        </div>
        {formFactor === 'round' && (
          <FormInput label={tr('tankAddForm.diameterM')} type="number" value={diameterM} onChange={setDiameterM} placeholder="4.5" />
        )}
        {formFactor === 'square' && (
          <FormInput label={tr('tankAddForm.sideM')} type="number" value={lengthM} onChange={setLengthM} placeholder="3.0" />
        )}
        {formFactor === 'rectangular' && (
          <div className="grid grid-cols-2 gap-3">
            <FormInput label={tr('tankAddForm.lengthM')} type="number" value={lengthM} onChange={setLengthM} placeholder="5.0" />
            <FormInput label={tr('tankAddForm.widthM')} type="number" value={widthM} onChange={setWidthM} placeholder="3.0" />
          </div>
        )}
      </div>

      {error && (
        <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
          {error}
        </div>
      )}
      <div className="flex items-center gap-2 pt-1">
        <Button size="sm" onClick={() => void submit()} disabled={busy || !tankId.trim() || !displayName.trim()}>
          {busy ? tr('tankAddForm.registering') : tr('tankAddForm.register')}
        </Button>
        <Button size="sm" variant="outline" onClick={onClose} disabled={busy}>{tr('tankAddForm.cancel')}</Button>
      </div>
    </div>
  );
}

function FormInput({
  label, value, onChange, placeholder, required, type = 'text',
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  required?: boolean;
  type?: 'text' | 'number';
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-gray-400 font-medium">
        {label}{required && <span className="text-red-400 ml-0.5">*</span>}
      </label>
      <input
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-green-500/50"
      />
    </div>
  );
}

function FormSelect({
  label, value, onChange, options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-gray-400 font-medium">{label}</label>
      <select
        value={value}
        onChange={e => onChange(e.target.value)}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
      >
        {options.map(o => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  );
}
