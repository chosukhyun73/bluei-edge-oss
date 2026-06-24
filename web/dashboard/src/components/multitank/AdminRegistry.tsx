import { useState, useEffect, useCallback, useMemo, type ReactNode } from 'react';
import { Trash2 } from 'lucide-react';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Button } from '../ui/button';
import { ConfirmDialog } from '../ui/confirm-dialog';
import {
  WTGs, SpeciesProfiles, CameraModels,
  SensorModels, ActuatorModels,
} from '../../lib/api';
import type {
  NewWTGBody, NewSpeciesProfileBody,
  CameraModel, NewCameraModelBody, CameraLensType,
  SensorModel, NewSensorModelBody,
  MeasurementType, SensorProtocol, SensorWetDry,
  ActuatorModel, NewActuatorModelBody,
  DeviceCategory, ControlMethod, PowerType,
} from '../../lib/types';
import {
  MEASUREMENT_TYPE_LABELS, DEVICE_CATEGORY_LABELS, CONTROL_METHOD_LABELS,
  POWER_TYPE_LABELS,
} from '../../lib/types';
import { useLanguage } from '../../lib/language-context';

// ─────────────────────────────────────────────────────────────────────────────
// AdminRegistry — 운영자 도메인 등록 화면 (C-6a)
//
// 7개 카드를 한 페이지에 표시. 가장 자주 사용하는 순서:
//   1) 수조 (Tank)
//   2) 센서 (Sensor)
//   3) 액추에이터 / 장비 (Actuator)
//   4) 수처리 그룹 (WTG)
//   5) 사이트 (Site)
//   6) Farm
//   7) 어종 프로필 (Species)
//
// 기본은 모두 접힘. 운영자가 카드 헤더 클릭해서 펼침. 단순함을 우선.
// ─────────────────────────────────────────────────────────────────────────────

type Status = { kind: 'idle' } | { kind: 'busy' } | { kind: 'ok'; message: string } | { kind: 'err'; message: string };

type TrFn = (key: string, vars?: Record<string, string | number>) => string;

function statusLine(s: Status, tr: TrFn): ReactNode {
  if (s.kind === 'idle') return null;
  if (s.kind === 'busy') return <p className="text-xs text-gray-400">{tr('common.registering')}</p>;
  if (s.kind === 'ok') return <p className="text-xs text-green-400">{s.message}</p>;
  return <p className="text-xs text-red-400">{s.message}</p>;
}

// Korean error mapping for common API codes (backend 가 반환하는 code → 친화적 메시지).
function friendlyError(err: unknown, tr: TrFn): string {
  if (err && typeof err === 'object' && 'code' in err && 'message' in err) {
    const e = err as { code: string; message: string; status?: number };
    if (e.status === 409) {
      // 409 메시지는 한국어로 backend 가 이미 잘 만들어줌 — 그대로 노출.
      return e.message;
    }
    if (e.code.startsWith('INVALID_') || e.code === 'MISSING_FIELDS') return e.message;
    return `${e.code}: ${e.message}`;
  }
  return err instanceof Error ? err.message : tr('adminRegistry.unknownError');
}

// ── EntityList — 등록 entity 목록 + 삭제 버튼 (공통) ──────────────────────────

type DeleteCandidate = { id: string; label: string };

function EntityList<T>({
  items, getId, getLabel, getSubLabel, onDelete, busy, emptyMsg,
}: {
  items: T[];
  getId: (it: T) => string;
  getLabel: (it: T) => string;
  getSubLabel?: (it: T) => string;
  onDelete: (cand: DeleteCandidate) => void;
  busy?: boolean;
  emptyMsg?: string;
}) {
  const { tr } = useLanguage();
  if (items.length === 0) {
    return (
      <div className="text-xs text-gray-500 italic py-2">
        {emptyMsg ?? tr('entityList.emptyDefault')}
      </div>
    );
  }
  return (
    <div className="space-y-1.5">
      <div className="text-xs text-gray-400 font-medium">{tr('entityList.registeredCount')} ({items.length})</div>
      <div className="space-y-1">
        {items.map(it => {
          const id = getId(it);
          return (
            <div
              key={id}
              className="group flex items-center justify-between gap-3 px-3 py-1.5 bg-gray-800/40 border border-gray-700/30 rounded text-xs"
            >
              <div className="min-w-0 flex-1">
                <div className="text-sm text-white truncate">{getLabel(it)}</div>
                {getSubLabel && (
                  <div className="text-xs text-gray-500 font-mono truncate">{getSubLabel(it)}</div>
                )}
              </div>
              <button
                onClick={() => onDelete({ id, label: getLabel(it) })}
                disabled={busy}
                className="opacity-0 group-hover:opacity-100 p-1 text-gray-500 hover:text-red-400 disabled:opacity-30 transition-opacity"
                aria-label={`${getLabel(it)} ${tr('entityList.deleteLabel')}`}
                title={tr('entityList.deleteLabel')}
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── Generic field components ─────────────────────────────────────────────────

function Field({
  id, label, value, onChange, placeholder, required, type = 'text',
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  required?: boolean;
  type?: 'text' | 'number' | 'date';
}) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs text-gray-400 font-medium">
        {label}{required && <span className="text-red-400 ml-0.5">*</span>}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
      />
    </div>
  );
}

function Select({
  id, label, value, onChange, options, required,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
  required?: boolean;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs text-gray-400 font-medium">
        {label}{required && <span className="text-red-400 ml-0.5">*</span>}
      </label>
      <select
        id={id}
        value={value}
        onChange={e => onChange(e.target.value)}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
      >
        {options.map(o => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  );
}

// ── Card shell with expand/collapse ──────────────────────────────────────────

function AdminCard({
  title, expanded, onToggle, children,
}: {
  title: string;
  expanded: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  const { tr } = useLanguage();
  return (
    <Card>
      <CardHeader
        className="cursor-pointer select-none"
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <CardTitle className="flex items-center justify-between text-base">
          <span>{title}</span>
          <span className="text-xs text-gray-400">{expanded ? tr('adminCard.collapse') : tr('adminCard.expand')}</span>
        </CardTitle>
      </CardHeader>
      {expanded && <CardContent className="space-y-3">{children}</CardContent>}
    </Card>
  );
}

// ── 공통 삭제 핸들러 ──────────────────────────────────────────────────────────

// useDeleteHandler — 카드별 delete 상태 + 액션 통합 훅.
// 단순함을 위해 카드 안에서 직접 호출 + ConfirmDialog 렌더.
function useDeleteHandler(
  apiDelete: (id: string) => Promise<unknown>,
  onAfter: () => Promise<void> | void,
  tr: TrFn,
) {
  const [cand, setCand] = useState<DeleteCandidate | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const open = (c: DeleteCandidate) => { setCand(c); setErr(null); };
  const cancel = () => { if (!busy) { setCand(null); setErr(null); } };
  const confirm = async () => {
    if (!cand) return;
    setBusy(true); setErr(null);
    try {
      await apiDelete(cand.id);
      setCand(null);
      await onAfter();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };
  return { cand, busy, err, open, cancel, confirm };
}


// ── WTG Card ─────────────────────────────────────────────────────────────────

function WTGCard({ refreshKey, onChanged }: { refreshKey: number; onChanged: () => void }) {
  const { tr } = useLanguage();
  type Item = { wtg_id: string; site_id?: string; name: string };
  const [items, setItems] = useState<Item[]>([]);
  const reload = useCallback(async () => {
    try { const r = await WTGs.list(); setItems(r.items as Item[]); }
    catch { setItems([]); }
  }, []);
  useEffect(() => { void reload(); }, [reload, refreshKey]);
  const del = useDeleteHandler((id) => WTGs.delete(id), async () => { await reload(); onChanged(); }, tr);

  const [wtgId, setWtgId] = useState('');
  const [siteId, setSiteId] = useState('');
  const [name, setName] = useState('');
  const [tankIdsStr, setTankIdsStr] = useState('');
  const [intake, setIntake] = useState('');
  const [outlet, setOutlet] = useState('');
  const [volumeM3, setVolumeM3] = useState('');
  const [nh3, setNh3] = useState('');
  const [flow, setFlow] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  async function submit() {
    setStatus({ kind: 'busy' });
    try {
      const body: NewWTGBody = {
        wtg_id: wtgId.trim(),
        site_id: siteId.trim(),
        name: name.trim(),
      };
      if (tankIdsStr) {
        body.tank_ids = tankIdsStr.split(',').map(s => s.trim()).filter(Boolean);
      }
      if (intake) body.intake_sensor = intake;
      if (outlet) body.outlet_sensor = outlet;
      if (volumeM3 || nh3 || flow) {
        body.capacity = {};
        if (volumeM3) body.capacity.volume_m3 = Number(volumeM3);
        if (nh3) body.capacity.nh3_processing_kg_per_h = Number(nh3);
        if (flow) body.capacity.flow_rate_m3_per_h = Number(flow);
      }
      await WTGs.create(body);
      setStatus({ kind: 'ok', message: `${tr('wtgCard.registeredOk')} ${body.wtg_id}` });
      setWtgId(''); setSiteId(''); setName(''); setTankIdsStr('');
      setIntake(''); setOutlet(''); setVolumeM3(''); setNh3(''); setFlow('');
      await reload();
      onChanged();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-3 gap-3">
        <Field id="w_id" label="WTG ID" value={wtgId} onChange={setWtgId} placeholder="wtg_a" required />
        <Field id="w_site" label="Site ID" value={siteId} onChange={setSiteId} placeholder="gangneung_ras" required />
        <Field id="w_name" label={tr('wtgCard.nameLabel')} value={name} onChange={setName} placeholder={tr('adminRegistry.wtgNamePlaceholder')} required />
      </div>
      <Field id="w_tanks" label={tr('wtgCard.tankIdsLabel')} value={tankIdsStr} onChange={setTankIdsStr}
        placeholder="ras_tank_01, ras_tank_02" />
      <div className="grid grid-cols-2 gap-3">
        <Field id="w_intake" label={tr('wtgCard.intakeSensorLabel')} value={intake} onChange={setIntake} placeholder="sensor_intake_01" />
        <Field id="w_outlet" label={tr('wtgCard.outletSensorLabel')} value={outlet} onChange={setOutlet} placeholder="sensor_outlet_01" />
      </div>
      <div className="grid grid-cols-3 gap-3">
        <Field id="w_vol" label={tr('wtgCard.volumeLabel')} type="number" value={volumeM3} onChange={setVolumeM3} placeholder="50" />
        <Field id="w_nh3" label={tr('wtgCard.nh3Label')} type="number" value={nh3} onChange={setNh3} placeholder="0.5" />
        <Field id="w_flow" label={tr('wtgCard.flowLabel')} type="number" value={flow} onChange={setFlow} placeholder="20" />
      </div>
      <div className="flex items-center gap-3">
        <Button type="button" size="sm" onClick={submit} disabled={status.kind === 'busy'}>
          {status.kind === 'busy' ? tr('common.registering') : tr('wtgCard.submitBtn')}
        </Button>
        {statusLine(status, tr)}
      </div>

      <EntityList
        items={items}
        getId={i => i.wtg_id}
        getLabel={i => `${i.name} (${i.wtg_id})`}
        getSubLabel={i => i.site_id ?? ''}
        onDelete={del.open}
        busy={del.busy}
        emptyMsg={tr('wtgCard.emptyMsg')}
      />
      <ConfirmDialog
        open={del.cand !== null}
        title={tr('wtgCard.deleteTitle')}
        message={
          <div className="space-y-2">
            <p><span className="font-mono text-red-300">{del.cand?.label ?? ''}</span> {tr('common.willDelete')}</p>
            {del.err && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {del.err}
              </div>
            )}
          </div>
        }
        busy={del.busy}
        onConfirm={() => void del.confirm()}
        onCancel={del.cancel}
      />
    </div>
  );
}


// ── Species Profile Card ─────────────────────────────────────────────────────

function SpeciesCard({ refreshKey, onChanged }: { refreshKey: number; onChanged: () => void }) {
  const { tr } = useLanguage();
  type Item = { species: string; display_name?: string };
  const [items, setItems] = useState<Item[]>([]);
  const reload = useCallback(async () => {
    try { const r = await SpeciesProfiles.list(); setItems(r.items as unknown as Item[]); }
    catch { setItems([]); }
  }, []);
  useEffect(() => { void reload(); }, [reload, refreshKey]);
  const del = useDeleteHandler((id) => SpeciesProfiles.delete(id), async () => { await reload(); onChanged(); }, tr);

  const [speciesKey, setSpeciesKey] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  async function submit() {
    setStatus({ kind: 'busy' });
    try {
      const body: NewSpeciesProfileBody = {
        species: speciesKey.trim(),
        display_name: displayName.trim(),
        source: 'override',
      };
      await SpeciesProfiles.create(body);
      setStatus({ kind: 'ok', message: `${tr('speciesCard.registeredOk')} ${body.species}` });
      setSpeciesKey(''); setDisplayName('');
      await reload();
      onChanged();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <Field id="sp_key" label={tr('speciesCard.keyLabel')} value={speciesKey} onChange={setSpeciesKey}
          placeholder="rainbow_trout" required />
        <Field id="sp_name" label={tr('speciesCard.displayNameLabel')} value={displayName} onChange={setDisplayName}
          placeholder={tr('adminRegistry.speciesPlaceholder')} required />
      </div>
      <p className="text-xs text-gray-500">
        {tr('speciesCard.seedHint')}
      </p>
      <div className="flex items-center gap-3">
        <Button type="button" size="sm" onClick={submit} disabled={status.kind === 'busy'}>
          {status.kind === 'busy' ? tr('common.registering') : tr('speciesCard.submitBtn')}
        </Button>
        {statusLine(status, tr)}
      </div>

      <EntityList
        items={items}
        getId={i => i.species}
        getLabel={i => i.display_name || i.species}
        getSubLabel={i => i.species}
        onDelete={del.open}
        busy={del.busy}
        emptyMsg={tr('speciesCard.emptyMsg')}
      />
      <ConfirmDialog
        open={del.cand !== null}
        title={tr('speciesCard.deleteTitle')}
        message={
          <div className="space-y-2">
            <p><span className="font-mono text-red-300">{del.cand?.label ?? ''}</span> {tr('common.willDelete')}</p>
            {del.err && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {del.err}
              </div>
            )}
          </div>
        }
        busy={del.busy}
        onConfirm={() => void del.confirm()}
        onCancel={del.cancel}
      />
    </div>
  );
}

// ── Camera Model Library Card (C-11) ─────────────────────────────────────────
// 카메라 특성 (vendor / lens / 해상도 / fps / 야간모드 / 프로토콜) 라이브러리.
// camera_profiles 의 model_id 가 이 카드의 model_id 를 가리킴.
// 단일 source of truth — TankSettings 의 InlineCameraForm 도 동일 list 사용.

// LENS_TYPE_OPTIONS labels are translated inline in CameraModelCard using tr()
const LENS_TYPE_OPTION_KEYS: { value: CameraLensType; labelKey: string }[] = [
  { value: 'single', labelKey: 'cameraModelCard.lensTypeSingle' },
  { value: 'dual', labelKey: 'cameraModelCard.lensTypeDual' },
  { value: 'fisheye', labelKey: 'cameraModelCard.lensTypeFisheye' },
  { value: 'ptz', labelKey: 'cameraModelCard.lensTypePtz' },
  { value: 'other', labelKey: 'cameraModelCard.lensTypeOther' },
];

function CameraModelCard({ refreshKey, onChanged }: { refreshKey: number; onChanged: () => void }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<CameraModel[]>([]);
  const reload = useCallback(async () => {
    try { const r = await CameraModels.list(); setItems(r.items); }
    catch { setItems([]); }
  }, []);
  useEffect(() => { void reload(); }, [reload, refreshKey]);
  const del = useDeleteHandler((id) => CameraModels.delete(id), async () => { await reload(); onChanged(); }, tr);

  const [modelId, setModelId] = useState('');
  const [vendor, setVendor] = useState('');
  const [productCode, setProductCode] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [lensType, setLensType] = useState<CameraLensType>('single');
  const [baselineMM, setBaselineMM] = useState('');
  const [stereoCalibJSON, setStereoCalibJSON] = useState('');
  const [resW, setResW] = useState('');
  const [resH, setResH] = useState('');
  const [fovDeg, setFovDeg] = useState('');
  const [fps, setFps] = useState('');
  const [nightMode, setNightMode] = useState(false);
  const [protocolsStr, setProtocolsStr] = useState('rtsp');
  const [notes, setNotes] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  // vendor + product_code 가 채워지면 model_id 가 비어있을 때 자동 추천.
  const autoModelId = useMemo(() => {
    const v = vendor.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    const p = productCode.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    if (!v || !p) return '';
    return `${v}_${p}`;
  }, [vendor, productCode]);

  async function submit() {
    setStatus({ kind: 'busy' });
    try {
      const body: NewCameraModelBody = {
        model_id: (modelId.trim() || autoModelId).trim(),
        vendor: vendor.trim(),
        product_code: productCode.trim(),
        display_name: displayName.trim(),
        lens_type: lensType,
        night_mode: nightMode,
      };
      if (lensType === 'dual') {
        if (baselineMM) body.baseline_mm = Number(baselineMM);
        if (stereoCalibJSON.trim()) body.stereo_calibration_json = stereoCalibJSON.trim();
      }
      if (resW) body.resolution_w = Number(resW);
      if (resH) body.resolution_h = Number(resH);
      if (fovDeg) body.fov_deg = Number(fovDeg);
      if (fps) body.fps = Number(fps);
      if (protocolsStr.trim()) {
        body.protocols = protocolsStr.split(',').map(s => s.trim()).filter(Boolean);
      }
      if (notes.trim()) body.notes = notes.trim();
      await CameraModels.create(body);
      setStatus({ kind: 'ok', message: `${tr('cameraModelCard.registeredOk')} ${body.model_id}` });
      setModelId(''); setVendor(''); setProductCode(''); setDisplayName('');
      setLensType('single'); setBaselineMM(''); setStereoCalibJSON('');
      setResW(''); setResH(''); setFovDeg(''); setFps('');
      setNightMode(false); setProtocolsStr('rtsp'); setNotes('');
      await reload();
      onChanged();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-gray-500">
        {tr('cameraModelCard.libraryHint')}
      </p>
      <div className="grid grid-cols-3 gap-3">
        <Field id="cm_id" label={tr('common.modelIdLabel')}
          value={modelId} onChange={setModelId}
          placeholder={autoModelId || 'hikvision_ds_2cd2347g2'} />
        <Field id="cm_vendor" label={tr('common.vendorLabel')} value={vendor} onChange={setVendor}
          placeholder="Hikvision" required />
        <Field id="cm_pc" label={tr('common.productCodeLabel')} value={productCode} onChange={setProductCode}
          placeholder="DS-2CD2347G2P-LSU/SL" required />
      </div>
      <Field id="cm_name" label={tr('common.displayNameLabel')} value={displayName} onChange={setDisplayName}
        placeholder="Hikvision ColorVu 4MP" required />
      <Select id="cm_lens" label={tr('cameraModelCard.lensTypeLabel')}
        value={lensType} onChange={v => setLensType(v as CameraLensType)}
        options={LENS_TYPE_OPTION_KEYS.map(o => ({ value: o.value, label: tr(o.labelKey) }))}
        required
      />
      {lensType === 'dual' && (
        <div className="space-y-2 p-2 rounded border border-cyan-500/20 bg-cyan-500/5">
          <div className="text-xs text-cyan-300">
            {tr('cameraModelCard.dualLensHint')}
          </div>
          <Field id="cm_baseline" type="number" label={tr('cameraModelCard.baselineLabel')}
            value={baselineMM} onChange={setBaselineMM} placeholder="60" />
          <div className="flex flex-col gap-1">
            <label className="text-xs text-gray-400 font-medium">{tr('cameraModelCard.stereoCalibLabel')}</label>
            <textarea
              value={stereoCalibJSON}
              onChange={e => setStereoCalibJSON(e.target.value)}
              rows={3}
              placeholder='{"K1": [[..]], "K2": [[..]], "R": [[..]], "T": [..]}'
              className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900 text-xs text-white font-mono focus:outline-none focus:ring-2 focus:ring-cyan-500/50"
            />
          </div>
        </div>
      )}
      <div className="grid grid-cols-4 gap-3">
        <Field id="cm_resW" type="number" label={tr('cameraModelCard.resWLabel')} value={resW} onChange={setResW} placeholder="1920" />
        <Field id="cm_resH" type="number" label={tr('cameraModelCard.resHLabel')} value={resH} onChange={setResH} placeholder="1080" />
        <Field id="cm_fov" type="number" label={tr('cameraModelCard.fovLabel')} value={fovDeg} onChange={setFovDeg} placeholder="95" />
        <Field id="cm_fps" type="number" label="fps" value={fps} onChange={setFps} placeholder="30" />
      </div>
      <div className="grid grid-cols-2 gap-3 items-end">
        <Field id="cm_proto" label={tr('common.protocolsLabel')}
          value={protocolsStr} onChange={setProtocolsStr} placeholder="rtsp, onvif" />
        <label className="flex items-center gap-2 text-xs text-gray-300 cursor-pointer pb-2">
          <input type="checkbox" checked={nightMode}
            onChange={e => setNightMode(e.target.checked)}
            className="h-4 w-4" />
          {tr('cameraModelCard.nightModeLabel')}
        </label>
      </div>
      <div className="flex flex-col gap-1">
        <label className="text-xs text-gray-400 font-medium">{tr('common.notesLabel')}</label>
        <textarea
          value={notes}
          onChange={e => setNotes(e.target.value)}
          rows={2}
          className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900 text-xs text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
        />
      </div>

      <div className="flex items-center gap-3">
        <Button type="button" size="sm" onClick={submit}
          disabled={status.kind === 'busy' || !vendor.trim() || !productCode.trim() || !displayName.trim()}>
          {status.kind === 'busy' ? tr('common.registering') : tr('cameraModelCard.submitBtn')}
        </Button>
        {statusLine(status, tr)}
      </div>

      <EntityList
        items={items}
        getId={i => i.model_id}
        getLabel={i => `${i.display_name} (${i.lens_type})`}
        getSubLabel={i => `${i.vendor} · ${i.product_code} · ${i.resolution_w ?? '?'}x${i.resolution_h ?? '?'} · ${i.fps ?? '?'}fps${i.night_mode ? ` · ${tr('cameraModelCard.nightShort')}` : ''}`}
        onDelete={del.open}
        busy={del.busy}
        emptyMsg={tr('cameraModelCard.emptyMsg')}
      />
      <ConfirmDialog
        open={del.cand !== null}
        title={tr('cameraModelCard.deleteTitle')}
        message={
          <div className="space-y-2">
            <p><span className="font-mono text-red-300">{del.cand?.label ?? ''}</span> {tr('common.modelWillDelete')}</p>
            <p className="text-xs text-gray-500">{tr('cameraModelCard.deleteWarning')}</p>
            {del.err && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {del.err}
              </div>
            )}
          </div>
        }
        busy={del.busy}
        onConfirm={() => void del.confirm()}
        onCancel={del.cancel}
      />
    </div>
  );
}

// ── Sensor Model Library Card (C-13a) ────────────────────────────────────────
// 센서 특성 (vendor / measurement_type / unit / range / accuracy / 프로토콜) 라이브러리.
// sensors 의 model_id 가 이 카드의 model_id 를 가리킴.
// 단일 source of truth — TankSettings 의 InlineSensorForm + WTGDetail 의 inline 폼 모두 동일 list 사용.

// SENSOR_PROTOCOL_OPTIONS and SENSOR_WET_DRY_OPTIONS labels are translated inline in SensorModelCard using tr()
const SENSOR_PROTOCOL_OPTION_KEYS: { value: SensorProtocol; label: string; labelKey?: string }[] = [
  { value: 'modbus', label: 'Modbus' },
  { value: 'rs485', label: 'RS-485' },
  { value: 'rs232', label: 'RS-232' },
  { value: '4-20ma', label: '4-20mA' },
  { value: '0-10v', label: '0-10V' },
  { value: 'i2c', label: 'I2C' },
  { value: 'sdi-12', label: 'SDI-12' },
  { value: 'http', label: 'HTTP' },
  { value: 'mqtt', label: 'MQTT' },
  { value: 'other', label: '', labelKey: 'common.other' },
];

const SENSOR_WET_DRY_OPTION_KEYS: { value: SensorWetDry; labelKey: string }[] = [
  { value: 'wet_probe', labelKey: 'sensorModelCard.wetProbe' },
  { value: 'inline', labelKey: 'sensorModelCard.inline' },
  { value: 'dry_mount', labelKey: 'sensorModelCard.dryMount' },
  { value: 'other', labelKey: 'common.other' },
];

function SensorModelCard({ refreshKey, onChanged }: { refreshKey: number; onChanged: () => void }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<SensorModel[]>([]);
  const reload = useCallback(async () => {
    try { const r = await SensorModels.list(); setItems(r.items); }
    catch { setItems([]); }
  }, []);
  useEffect(() => { void reload(); }, [reload, refreshKey]);
  const del = useDeleteHandler((id) => SensorModels.delete(id), async () => { await reload(); onChanged(); }, tr);

  const [modelId, setModelId] = useState('');
  const [vendor, setVendor] = useState('');
  const [productCode, setProductCode] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [measurementType, setMeasurementType] = useState<MeasurementType>('water_temperature');
  const [unit, setUnit] = useState('°C');
  const [rangeMin, setRangeMin] = useState('');
  const [rangeMax, setRangeMax] = useState('');
  const [accuracyValue, setAccuracyValue] = useState('');
  const [accuracyUnit, setAccuracyUnit] = useState('');
  const [responseTimeS, setResponseTimeS] = useState('');
  const [protocol, setProtocol] = useState<SensorProtocol | ''>('');
  const [calibIntervalDays, setCalibIntervalDays] = useState('');
  const [wetDry, setWetDry] = useState<SensorWetDry | ''>('');
  const [notes, setNotes] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  // vendor + product_code 채워지면 model_id 자동 추천.
  const autoModelId = useMemo(() => {
    const v = vendor.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    const p = productCode.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    if (!v || !p) return '';
    return `${v}_${p}`;
  }, [vendor, productCode]);

  async function submit() {
    setStatus({ kind: 'busy' });
    try {
      const body: NewSensorModelBody = {
        model_id: (modelId.trim() || autoModelId).trim(),
        vendor: vendor.trim(),
        product_code: productCode.trim(),
        display_name: displayName.trim(),
        measurement_type: measurementType,
        unit: unit.trim(),
      };
      if (rangeMin) body.range_min = Number(rangeMin);
      if (rangeMax) body.range_max = Number(rangeMax);
      if (accuracyValue) body.accuracy_value = Number(accuracyValue);
      if (accuracyUnit.trim()) body.accuracy_unit = accuracyUnit.trim();
      if (responseTimeS) body.response_time_s = Number(responseTimeS);
      if (protocol) body.protocol = protocol;
      if (calibIntervalDays) body.calibration_interval_days = Number(calibIntervalDays);
      if (wetDry) body.wet_dry = wetDry;
      if (notes.trim()) body.notes = notes.trim();
      await SensorModels.create(body);
      setStatus({ kind: 'ok', message: `${tr('sensorModelCard.registeredOk')} ${body.model_id}` });
      setModelId(''); setVendor(''); setProductCode(''); setDisplayName('');
      setMeasurementType('water_temperature'); setUnit('°C');
      setRangeMin(''); setRangeMax(''); setAccuracyValue(''); setAccuracyUnit('');
      setResponseTimeS(''); setProtocol(''); setCalibIntervalDays(''); setWetDry('');
      setNotes('');
      await reload();
      onChanged();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-gray-500">
        {tr('sensorModelCard.libraryHint')}
      </p>
      <div className="grid grid-cols-3 gap-3">
        <Field id="sm_id" label={tr('common.modelIdLabel')} value={modelId} onChange={setModelId}
          placeholder={autoModelId || 'ysi_proquatro_ph'} />
        <Field id="sm_vendor" label={tr('common.vendorLabel')} value={vendor} onChange={setVendor}
          placeholder="YSI" required />
        <Field id="sm_pc" label={tr('common.productCodeLabel')} value={productCode} onChange={setProductCode}
          placeholder="ProQuatro-pH" required />
      </div>
      <Field id="sm_name" label={tr('common.displayNameLabel')} value={displayName} onChange={setDisplayName}
        placeholder="YSI ProQuatro pH" required />
      <div className="grid grid-cols-2 gap-3">
        <Select id="sm_mt" label={tr('sensorModelCard.measurementTypeLabel')}
          value={measurementType} onChange={v => setMeasurementType(v as MeasurementType)}
          options={(Object.keys(MEASUREMENT_TYPE_LABELS) as MeasurementType[]).map(v => ({
            value: v, label: tr(MEASUREMENT_TYPE_LABELS[v]),
          }))}
          required
        />
        <Field id="sm_unit" label={tr('sensorModelCard.unitLabel')} value={unit} onChange={setUnit}
          placeholder="°C / pH / mg/L / NTU ..." required />
      </div>
      <div className="grid grid-cols-4 gap-3">
        <Field id="sm_rmin" type="number" label={tr('sensorModelCard.rangeMinLabel')} value={rangeMin} onChange={setRangeMin} placeholder="0" />
        <Field id="sm_rmax" type="number" label={tr('sensorModelCard.rangeMaxLabel')} value={rangeMax} onChange={setRangeMax} placeholder="14" />
        <Field id="sm_acc" type="number" label={tr('sensorModelCard.accuracyValueLabel')} value={accuracyValue} onChange={setAccuracyValue} placeholder="0.01" />
        <Field id="sm_acc_u" label={tr('sensorModelCard.accuracyUnitLabel')} value={accuracyUnit} onChange={setAccuracyUnit} placeholder="pH" />
      </div>
      <div className="grid grid-cols-3 gap-3">
        <Field id="sm_resp" type="number" label={tr('sensorModelCard.responseTimeLabel')} value={responseTimeS} onChange={setResponseTimeS} placeholder="2" />
        <Select id="sm_proto" label={tr('sensorModelCard.protocolLabel')}
          value={protocol} onChange={v => setProtocol(v as SensorProtocol | '')}
          options={[{ value: '', label: tr('common.unspecified') }, ...SENSOR_PROTOCOL_OPTION_KEYS.map(o => ({ value: o.value, label: o.labelKey ? tr(o.labelKey) : o.label }))]}
        />
        <Field id="sm_cal" type="number" label={tr('sensorModelCard.calibIntervalLabel')} value={calibIntervalDays} onChange={setCalibIntervalDays} placeholder="90" />
      </div>
      <div className="grid grid-cols-2 gap-3 items-end">
        <Select id="sm_wd" label={tr('sensorModelCard.installTypeLabel')}
          value={wetDry} onChange={v => setWetDry(v as SensorWetDry | '')}
          options={[{ value: '', label: tr('common.unspecified') }, ...SENSOR_WET_DRY_OPTION_KEYS.map(o => ({ value: o.value, label: tr(o.labelKey) }))]}
        />
      </div>
      <div className="flex flex-col gap-1">
        <label className="text-xs text-gray-400 font-medium">{tr('common.notesLabel')}</label>
        <textarea
          value={notes}
          onChange={e => setNotes(e.target.value)}
          rows={2}
          className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900 text-xs text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
        />
      </div>

      <div className="flex items-center gap-3">
        <Button type="button" size="sm" onClick={submit}
          disabled={status.kind === 'busy' || !vendor.trim() || !productCode.trim() || !displayName.trim() || !unit.trim()}>
          {status.kind === 'busy' ? tr('common.registering') : tr('sensorModelCard.submitBtn')}
        </Button>
        {statusLine(status, tr)}
      </div>

      <EntityList
        items={items}
        getId={i => i.model_id}
        getLabel={i => `${i.display_name} (${MEASUREMENT_TYPE_LABELS[i.measurement_type as MeasurementType] ? tr(MEASUREMENT_TYPE_LABELS[i.measurement_type as MeasurementType]) : i.measurement_type})`}
        getSubLabel={i => `${i.vendor} · ${i.product_code} · ${i.unit}${i.range_min != null ? ` · ${i.range_min}~${i.range_max ?? '?'}` : ''}${i.protocol ? ` · ${i.protocol}` : ''}`}
        onDelete={del.open}
        busy={del.busy}
        emptyMsg={tr('sensorModelCard.emptyMsg')}
      />
      <ConfirmDialog
        open={del.cand !== null}
        title={tr('sensorModelCard.deleteTitle')}
        message={
          <div className="space-y-2">
            <p><span className="font-mono text-red-300">{del.cand?.label ?? ''}</span> {tr('common.modelWillDelete')}</p>
            <p className="text-xs text-gray-500">{tr('sensorModelCard.deleteWarning')}</p>
            {del.err && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {del.err}
              </div>
            )}
          </div>
        }
        busy={del.busy}
        onConfirm={() => void del.confirm()}
        onCancel={del.cancel}
      />
    </div>
  );
}

// ── Actuator Model Library Card (C-13b) ──────────────────────────────────────
// 액추에이터 모델 특성 (카테고리/제어방식/용량/응답시간/제어범위/소모품 교체일) 라이브러리.
// actuators.model_id 가 이 카드의 model_id 를 가리킴.
// 카메라 CameraModelCard / 센서 SensorModelCard 패턴과 평행.

const DEVICE_CATEGORY_OPTIONS: { value: DeviceCategory; label: string }[] =
  (Object.keys(DEVICE_CATEGORY_LABELS) as DeviceCategory[]).map(v => ({
    value: v,
    label: DEVICE_CATEGORY_LABELS[v],
  }));

const CONTROL_METHOD_OPTIONS: { value: ControlMethod; label: string }[] =
  (Object.keys(CONTROL_METHOD_LABELS) as ControlMethod[]).map(v => ({
    value: v,
    label: CONTROL_METHOD_LABELS[v],
  }));

const POWER_TYPE_OPTIONS: { value: PowerType; label: string }[] =
  (Object.keys(POWER_TYPE_LABELS) as PowerType[]).map(v => ({
    value: v,
    label: POWER_TYPE_LABELS[v],
  }));

// ── C-13b 카테고리별 spec 스키마 ────────────────────────────────────────────
// docs/wip/equipment-category-spec-design.md "카테고리별 spec" + "필수 항목 +
// 작성 가이드" 표 기준. 필수 키는 backend (internal/api/actuator_models.go) 와 일치.
// number 필드는 제출 시 Number 변환. enum/string 필드는 그대로 전송.
//
// kind:
//   'number'      — 숫자 입력 (number input)
//   'enum'        — 고정 선택지 (select)
//   'flow_choice' — air_pump 풍량: 값 + 단위(m3min/lpm) dropdown. key 가 단위에 따라 전환.
type SpecField =
  | {
      kind: 'number';
      key: string;
      label: string;
      unit?: string;
      placeholder: string;
      required?: boolean;
      helper?: string;
    }
  | {
      kind: 'enum';
      key: string;
      label: string;
      placeholder: string; // 미선택 라벨
      required?: boolean;
      helper?: string;
      options: { value: string; label: string }[];
    }
  | {
      // air_pump 풍량 전용 — 값 입력 + 단위 dropdown(m3min/lpm). 단위에 따라 전송 key 전환.
      kind: 'flow_choice';
      label: string;
      placeholder: string;
      required?: boolean;
      helper?: string;
      units: { unitKey: 'air_flow_m3min' | 'air_flow_lpm'; unitLabel: string }[];
    };

// 카테고리 → spec 필드 셋. 여기 없는 카테고리(pump 등 기존)는 공통 폼만 (하위호환).
// label / helper / placeholder(enum) / option label 은 tr 키. 렌더 시 ActuatorModelCard 에서 tr() 로 해석.
const CATEGORY_SPEC_SCHEMA: Partial<Record<DeviceCategory, SpecField[]>> = {
  circulation_pump: [
    { kind: 'number', key: 'max_head_m', label: 'adminRegistry.specMaxHead', unit: 'm', placeholder: '15.3', required: true,
      helper: 'adminRegistry.specMaxHeadHelper' },
    { kind: 'number', key: 'max_flow_m3h', label: 'adminRegistry.specMaxFlow', unit: 'm³/h', placeholder: '11.4', required: true,
      helper: 'adminRegistry.specMaxFlowHelper' },
    { kind: 'number', key: 'rated_current_a', label: 'adminRegistry.specRatedCurrent', unit: 'A', placeholder: '1.6',
      helper: 'adminRegistry.specRatedCurrentHelper' },
  ],
  heat_pump: [
    { kind: 'number', key: 'cooling_capacity_kcal_h', label: 'adminRegistry.specCoolingCapacity', unit: 'kcal/h', placeholder: '12000', required: true,
      helper: 'adminRegistry.specCoolingCapacityHelper' },
    { kind: 'number', key: 'heating_capacity_kcal_h', label: 'adminRegistry.specHeatingCapacity', unit: 'kcal/h', placeholder: '14000',
      helper: 'adminRegistry.specHeatingCapacityHelper' },
  ],
  air_pump: [
    { kind: 'flow_choice', label: 'adminRegistry.specAirFlow', placeholder: '1.5', required: true,
      helper: 'adminRegistry.specAirFlowHelper',
      units: [
        { unitKey: 'air_flow_m3min', unitLabel: 'm³/min' },
        { unitKey: 'air_flow_lpm', unitLabel: 'L/min' },
      ] },
    { kind: 'number', key: 'air_pressure_kpa', label: 'adminRegistry.specAirPressure', unit: 'kPa', placeholder: '20', required: true,
      helper: 'adminRegistry.specAirPressureHelper' },
    { kind: 'number', key: 'port_diameter_mm', label: 'adminRegistry.specPortDiameter', unit: 'mm', placeholder: '40',
      helper: 'adminRegistry.specPortDiameterHelper' },
    { kind: 'number', key: 'control_hz_min', label: 'adminRegistry.specControlHzMin', unit: 'Hz', placeholder: '30',
      helper: 'adminRegistry.specControlHzHelper' },
    { kind: 'number', key: 'control_hz_max', label: 'adminRegistry.specControlHzMax', unit: 'Hz', placeholder: '60',
      helper: 'adminRegistry.specControlHzHelper' },
  ],
  uv_sterilizer: [
    { kind: 'number', key: 'lamp_wavelength_nm', label: 'adminRegistry.specLampWavelength', unit: 'nm', placeholder: '254.3', required: true,
      helper: 'adminRegistry.specLampWavelengthHelper' },
    { kind: 'number', key: 'lamp_power_w', label: 'adminRegistry.specLampPower', unit: 'W', placeholder: '87', required: true,
      helper: 'adminRegistry.specLampPowerHelper' },
    { kind: 'number', key: 'lamp_count', label: 'adminRegistry.specLampCount', unit: 'adminRegistry.unitPieces', placeholder: '6', required: true,
      helper: 'adminRegistry.specLampCountHelper' },
    { kind: 'number', key: 'treatment_flow_ton_h', label: 'adminRegistry.specTreatmentFlow', unit: 'ton/h', placeholder: '60', required: true,
      helper: 'adminRegistry.specTreatmentFlowHelper' },
  ],
  feeder: [
    // 공급속도(rpm)·살포거리는 측정 불가한 무의미 스펙이라 제외.
    // 살포방식만 카탈로그 참고용 (선택).
    { kind: 'enum', key: 'spray_method', label: 'adminRegistry.specSprayMethod', placeholder: 'adminRegistry.specUnspecifiedDash',
      helper: 'adminRegistry.specSprayMethodHelper',
      options: [
        { value: 'rotary_impact', label: 'adminRegistry.sprayRotaryImpact' },
        { value: 'screw', label: 'adminRegistry.sprayScrew' },
        { value: 'vibration', label: 'adminRegistry.sprayVibration' },
      ] },
  ],
};

// SpecHelper — spec 필드 아래 helper text (용도 / 작성 가이드). 필수 미입력 시 빨강.
function SpecHelper({ text, error }: { text?: string; error?: boolean }) {
  if (!text) return null;
  return <p className={`text-[11px] ${error ? 'text-red-400' : 'text-gray-500'}`}>{text}</p>;
}

function ActuatorModelCard({ refreshKey, onChanged }: { refreshKey: number; onChanged: () => void }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<ActuatorModel[]>([]);
  const reload = useCallback(async () => {
    try { const r = await ActuatorModels.list(); setItems(r.items); }
    catch { setItems([]); }
  }, []);
  useEffect(() => { void reload(); }, [reload, refreshKey]);
  const del = useDeleteHandler((id) => ActuatorModels.delete(id), async () => { await reload(); onChanged(); }, tr);

  const [modelId, setModelId] = useState('');
  const [vendor, setVendor] = useState('');
  const [productCode, setProductCode] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [deviceCategory, setDeviceCategory] = useState<DeviceCategory>('pump');
  const [powerType, setPowerType] = useState<PowerType | ''>('');
  const [ratedPower, setRatedPower] = useState(''); // 단위는 powerType 에 따라 W / kW
  const [capacityValue, setCapacityValue] = useState('');
  const [capacityUnit, setCapacityUnit] = useState('');
  const [controlMethod, setControlMethod] = useState<ControlMethod | ''>('');
  const [responseTimeS, setResponseTimeS] = useState('');
  const [ctrlMin, setCtrlMin] = useState('');
  const [ctrlMax, setCtrlMax] = useState('');
  const [ctrlUnit, setCtrlUnit] = useState('');
  const [consumDays, setConsumDays] = useState('');
  const [notes, setNotes] = useState('');
  // 카테고리별 spec 값 (모든 키 string state — 제출 시 number/enum 변환).
  const [specValues, setSpecValues] = useState<Record<string, string>>({});
  // air_pump 풍량 단위 (m3min / lpm). flow_choice 필드용.
  const [flowUnitKey, setFlowUnitKey] = useState<'air_flow_m3min' | 'air_flow_lpm'>('air_flow_m3min');
  // 제출 시도 후 필수 누락 표시용.
  const [attempted, setAttempted] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  // 현재 카테고리의 spec 스키마. 없으면 기존 공통 폼만 (하위호환).
  const specSchema = CATEGORY_SPEC_SCHEMA[deviceCategory];
  const hasSpec = !!specSchema;

  // 카테고리 변경 시 이전 카테고리 spec 값/단위 초기화.
  function changeCategory(v: DeviceCategory) {
    setDeviceCategory(v);
    setSpecValues({});
    setFlowUnitKey('air_flow_m3min');
    setAttempted(false);
  }
  const setSpec = (key: string, v: string) => setSpecValues(prev => ({ ...prev, [key]: v }));

  // 전력 단위 — DC = W, AC(단상/3상) = kW. power_type 미선택 시 기본 W (하위호환).
  const powerUnit = powerType === 'dc' || powerType === '' ? 'W' : 'kW';

  // 현재 카테고리의 누락된 필수 spec 키 목록.
  const missingRequired = useMemo(() => {
    if (!specSchema) return [];
    const miss: string[] = [];
    for (const f of specSchema) {
      if (!f.required) continue;
      if (f.kind === 'flow_choice') {
        if (!specValues['__flow']?.trim()) miss.push(tr(f.label));
      } else if (!specValues[f.key]?.trim()) {
        miss.push(tr(f.label));
      }
    }
    return miss;
  }, [specSchema, specValues, tr]);

  // vendor + product_code 가 채워지면 model_id 자동 추천.
  const autoModelId = useMemo(() => {
    const v = vendor.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    const p = productCode.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    if (!v || !p) return '';
    return `${v}_${p}`;
  }, [vendor, productCode]);

  async function submit() {
    // 카테고리 필수 spec 누락 시 제출 막고 인라인 에러.
    if (hasSpec && missingRequired.length > 0) {
      setAttempted(true);
      setStatus({ kind: 'err', message: `${tr('actuatorModelCard.missingRequired')} ${missingRequired.join(', ')}` });
      return;
    }
    setStatus({ kind: 'busy' });
    try {
      const body: NewActuatorModelBody = {
        model_id: (modelId.trim() || autoModelId).trim(),
        vendor: vendor.trim(),
        product_code: productCode.trim(),
        display_name: displayName.trim(),
        device_category: deviceCategory,
      };
      // 전력 — DC=W → rated_power_w, AC=kW → category_specs.rated_power_kw.
      // power_type 미선택(공통, 하위호환) 시 기존처럼 rated_power_w 로.
      if (ratedPower) {
        if (powerType === 'dc' || powerType === '') {
          body.rated_power_w = Number(ratedPower);
        }
      }
      if (controlMethod) body.control_method = controlMethod;

      if (hasSpec && specSchema) {
        // 카테고리별 spec → category_specs 객체. number 는 Number 변환.
        const specs: Record<string, number | string> = {};
        for (const f of specSchema) {
          if (f.kind === 'flow_choice') {
            const raw = specValues['__flow'];
            if (raw?.trim()) specs[flowUnitKey] = Number(raw);
          } else if (f.kind === 'enum') {
            const raw = specValues[f.key];
            if (raw?.trim()) specs[f.key] = raw;
          } else {
            const raw = specValues[f.key];
            if (raw?.trim()) specs[f.key] = Number(raw);
          }
        }
        // AC 전력은 category_specs.rated_power_kw 로 보냄.
        if (ratedPower && (powerType === 'single_phase' || powerType === 'three_phase')) {
          specs.rated_power_kw = Number(ratedPower);
        }
        if (powerType) specs.power_type = powerType;
        if (Object.keys(specs).length > 0) body.category_specs = specs;
      } else {
        // spec 없는 카테고리 (pump 등) — 기존 공통 필드 유지 (하위호환).
        if (capacityValue) body.capacity_value = Number(capacityValue);
        if (capacityUnit.trim()) body.capacity_unit = capacityUnit.trim();
        if (responseTimeS) body.response_time_s = Number(responseTimeS);
        if (ctrlMin) body.control_range_min = Number(ctrlMin);
        if (ctrlMax) body.control_range_max = Number(ctrlMax);
        if (ctrlUnit.trim()) body.control_range_unit = ctrlUnit.trim();
        // 공통 폼에서 AC 전력 선택 시에도 power_type/kw 는 category_specs 로 (검증 없음).
        if (powerType && (powerType === 'single_phase' || powerType === 'three_phase') && ratedPower) {
          body.category_specs = { rated_power_kw: Number(ratedPower), power_type: powerType };
        } else if (powerType === 'dc') {
          body.category_specs = { ...(body.category_specs ?? {}), power_type: powerType };
        }
      }
      if (consumDays) body.consumable_replacement_days = Number(consumDays);
      if (notes.trim()) body.notes = notes.trim();
      await ActuatorModels.create(body);
      setStatus({ kind: 'ok', message: `${tr('actuatorModelCard.registeredOk')} ${body.model_id}` });
      setModelId(''); setVendor(''); setProductCode(''); setDisplayName('');
      setDeviceCategory('pump'); setPowerType(''); setRatedPower(''); setCapacityValue(''); setCapacityUnit('');
      setControlMethod(''); setResponseTimeS(''); setCtrlMin(''); setCtrlMax(''); setCtrlUnit('');
      setConsumDays(''); setNotes(''); setSpecValues({}); setFlowUnitKey('air_flow_m3min'); setAttempted(false);
      await reload();
      onChanged();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-gray-500">
        {tr('actuatorModelCard.libraryHint')}
      </p>
      <div className="grid grid-cols-3 gap-3">
        <Field id="am_id" label={tr('common.modelIdLabel')}
          value={modelId} onChange={setModelId}
          placeholder={autoModelId || 'grundfos_cre_5_1'} />
        <Field id="am_vendor" label={tr('common.vendorLabel')} value={vendor} onChange={setVendor}
          placeholder={tr('adminRegistry.vendorPlaceholder')} required />
        <Field id="am_pc" label={tr('common.productCodeLabel')} value={productCode} onChange={setProductCode}
          placeholder="PU-S600U" required />
      </div>
      <Field id="am_name" label={tr('common.displayNameLabel')} value={displayName} onChange={setDisplayName}
        placeholder={tr('adminRegistry.productNamePlaceholder')} required />
      <div className="grid grid-cols-2 gap-3">
        <Select id="am_cat" label={tr('actuatorModelCard.categoryLabel')}
          value={deviceCategory} onChange={v => changeCategory(v as DeviceCategory)}
          options={DEVICE_CATEGORY_OPTIONS.map(o => ({ value: o.value, label: tr(o.label) }))}
          required
        />
        <Select id="am_ctrl" label={tr('actuatorModelCard.controlMethodLabel')}
          value={controlMethod} onChange={v => setControlMethod(v as ControlMethod | '')}
          options={[{ value: '', label: tr('common.unspecified') }, ...CONTROL_METHOD_OPTIONS.map(o => ({ value: o.value, label: tr(o.label) }))]}
        />
      </div>

      {/* 전원 — power_type 에 따라 전력 단위(W/kW) 전환. */}
      <div className="grid grid-cols-2 gap-3">
        <Select id="am_power_type" label={tr('actuatorModelCard.powerTypeLabel')}
          value={powerType} onChange={v => setPowerType(v as PowerType | '')}
          options={[{ value: '', label: tr('common.unspecified') }, ...POWER_TYPE_OPTIONS.map(o => ({ value: o.value, label: tr(o.label) }))]}
        />
        <div className="flex flex-col gap-1">
          <Field id="am_rated" type="number" label={`${tr('actuatorModelCard.ratedPowerLabel')} (${powerUnit}, ${tr('common.optional')})`}
            value={ratedPower} onChange={setRatedPower}
            placeholder={powerUnit === 'kW' ? '0.6' : '1500'} />
          <SpecHelper text={powerType === '' ? tr('actuatorModelCard.powerUnitHint') : `${tr('actuatorModelCard.powerLoadHint')} (${powerUnit})`} />
        </div>
      </div>

      {/* 카테고리별 spec 동적 폼. 스키마가 있는 카테고리만. */}
      {hasSpec && specSchema && (
        <div className="space-y-2 rounded-md border border-gray-700/50 bg-gray-900/30 p-3">
          <div className="text-xs text-gray-300 font-medium">
            {tr(DEVICE_CATEGORY_LABELS[deviceCategory])} {tr('actuatorModelCard.detailSpec')}
          </div>
          <div className="grid grid-cols-2 gap-3">
            {specSchema.map(f => {
              if (f.kind === 'flow_choice') {
                const err = attempted && !!f.required && !specValues['__flow']?.trim();
                return (
                  <div key="__flow" className="flex flex-col gap-1">
                    <label className="text-xs text-gray-400 font-medium">
                      {tr(f.label)}{f.required && <span className="text-red-400 ml-0.5">*</span>}
                    </label>
                    <div className="flex gap-2">
                      <input type="number"
                        value={specValues['__flow'] ?? ''}
                        onChange={e => setSpec('__flow', e.target.value)}
                        placeholder={f.placeholder}
                        className={`h-8 px-3 flex-1 rounded-md border bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-green-500/50 ${err ? 'border-red-500/60' : 'border-gray-700'}`}
                      />
                      <select
                        value={flowUnitKey}
                        onChange={e => setFlowUnitKey(e.target.value as 'air_flow_m3min' | 'air_flow_lpm')}
                        className="h-8 px-2 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
                      >
                        {f.units.map(u => <option key={u.unitKey} value={u.unitKey}>{u.unitLabel}</option>)}
                      </select>
                    </div>
                    <SpecHelper text={f.helper ? tr(f.helper) : undefined} error={err} />
                  </div>
                );
              }
              if (f.kind === 'enum') {
                return (
                  <div key={f.key} className="flex flex-col gap-1">
                    <Select id={`am_spec_${f.key}`}
                      label={tr(f.label)}
                      value={specValues[f.key] ?? ''}
                      onChange={v => setSpec(f.key, v)}
                      options={[{ value: '', label: tr(f.placeholder) }, ...f.options.map(o => ({ value: o.value, label: tr(o.label) }))]}
                      required={f.required}
                    />
                    <SpecHelper text={f.helper ? tr(f.helper) : undefined} />
                  </div>
                );
              }
              const err = attempted && !!f.required && !specValues[f.key]?.trim();
              return (
                <div key={f.key} className="flex flex-col gap-1">
                  <Field id={`am_spec_${f.key}`} type="number"
                    label={f.unit ? `${tr(f.label)} (${tr(f.unit)})` : tr(f.label)}
                    value={specValues[f.key] ?? ''}
                    onChange={v => setSpec(f.key, v)}
                    placeholder={f.placeholder}
                    required={f.required}
                  />
                  <SpecHelper text={f.helper ? tr(f.helper) : undefined} error={err} />
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* spec 없는 카테고리 (pump 등 기존) — 공통 용량/제어범위 필드 (하위호환). */}
      {!hasSpec && (
        <>
          <div className="grid grid-cols-3 gap-3">
            <Field id="am_cap" type="number" label={tr('actuatorModelCard.capacityValueLabel')}
              value={capacityValue} onChange={setCapacityValue} placeholder="20" />
            <Field id="am_capu" label={tr('actuatorModelCard.capacityUnitLabel')}
              value={capacityUnit} onChange={setCapacityUnit} placeholder="m3/h, L/min, ..." />
            <Field id="am_resp" type="number" label={tr('actuatorModelCard.responseTimeLabel')}
              value={responseTimeS} onChange={setResponseTimeS} placeholder="2" />
          </div>
          <div className="grid grid-cols-3 gap-3">
            <Field id="am_min" type="number" label={tr('actuatorModelCard.ctrlRangeMinLabel')}
              value={ctrlMin} onChange={setCtrlMin} placeholder="0" />
            <Field id="am_max" type="number" label={tr('actuatorModelCard.ctrlRangeMaxLabel')}
              value={ctrlMax} onChange={setCtrlMax} placeholder="100" />
            <Field id="am_ru" label={tr('actuatorModelCard.ctrlRangeUnitLabel')}
              value={ctrlUnit} onChange={setCtrlUnit} placeholder="%, Hz, ..." />
          </div>
        </>
      )}

      <Field id="am_consum" type="number" label={tr('actuatorModelCard.consumDaysLabel')}
        value={consumDays} onChange={setConsumDays} placeholder="365" />
      <div className="flex flex-col gap-1">
        <label className="text-xs text-gray-400 font-medium">{tr('common.notesLabel')}</label>
        <textarea
          value={notes}
          onChange={e => setNotes(e.target.value)}
          rows={2}
          className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900 text-xs text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
        />
      </div>

      <div className="flex items-center gap-3">
        <Button type="button" size="sm" onClick={submit}
          disabled={status.kind === 'busy' || !vendor.trim() || !productCode.trim() || !displayName.trim() || (hasSpec && missingRequired.length > 0)}>
          {status.kind === 'busy' ? tr('common.registering') : tr('actuatorModelCard.submitBtn')}
        </Button>
        {hasSpec && missingRequired.length > 0 && (
          <span className="text-[11px] text-gray-500">{tr('actuatorModelCard.specRequired')} {missingRequired.join(', ')}</span>
        )}
        {statusLine(status, tr)}
      </div>

      <EntityList
        items={items}
        getId={i => i.model_id}
        getLabel={i => `${i.display_name} (${DEVICE_CATEGORY_LABELS[i.device_category as DeviceCategory] ? tr(DEVICE_CATEGORY_LABELS[i.device_category as DeviceCategory]) : i.device_category})`}
        getSubLabel={i => `${i.vendor} · ${i.product_code}${i.rated_power_w ? ` · ${i.rated_power_w}W` : ''}${i.capacity_value ? ` · ${i.capacity_value}${i.capacity_unit ?? ''}` : ''}${i.control_method ? ` · ${i.control_method}` : ''}`}
        onDelete={del.open}
        busy={del.busy}
        emptyMsg={tr('actuatorModelCard.emptyMsg')}
      />
      <ConfirmDialog
        open={del.cand !== null}
        title={tr('actuatorModelCard.deleteTitle')}
        message={
          <div className="space-y-2">
            <p><span className="font-mono text-red-300">{del.cand?.label ?? ''}</span> {tr('common.modelWillDelete')}</p>
            <p className="text-xs text-gray-500">{tr('actuatorModelCard.deleteWarning')}</p>
            {del.err && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {del.err}
              </div>
            )}
          </div>
        }
        busy={del.busy}
        onConfirm={() => void del.confirm()}
        onCancel={del.cancel}
      />
    </div>
  );
}

// ── Main entry ───────────────────────────────────────────────────────────────

export function AdminRegistry() {
  const { tr } = useLanguage();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  // refreshKey — 한 카드에서 등록 시 다른 카드도 (예: WTG → Site list) 동기화 유도 가능.
  // 단순화를 위해 직접 invalidate; 각 카드는 useEffect 가 [refreshKey] 의존.
  const [refreshKey, setRefreshKey] = useState(0);
  const onChanged = () => setRefreshKey(k => k + 1);

  function toggle(key: string) {
    setExpanded(prev => ({ ...prev, [key]: !prev[key] }));
  }

  // 수조/센서/장비/사이트/Farm 등록은 "사이트 수조관리" 메인 탭에서 가능 — 중복 제거.
  // 여기엔 메인 탭에 없는 모델 라이브러리·WTG·어종 프로필만 둔다.
  const cards = [
    { key: 'sensor_model', title: tr('adminRegistry.sensorModelTitle'), render: () => <SensorModelCard refreshKey={refreshKey} onChanged={onChanged} /> },
    { key: 'actuator_model', title: tr('adminRegistry.actuatorModelTitle'), render: () => <ActuatorModelCard refreshKey={refreshKey} onChanged={onChanged} /> },
    { key: 'camera_model', title: tr('adminRegistry.cameraModelTitle'), render: () => <CameraModelCard refreshKey={refreshKey} onChanged={onChanged} /> },
    { key: 'wtg', title: tr('adminRegistry.wtgTitle'), render: () => <WTGCard refreshKey={refreshKey} onChanged={onChanged} /> },
    { key: 'species', title: tr('adminRegistry.speciesTitle'), render: () => <SpeciesCard refreshKey={refreshKey} onChanged={onChanged} /> },
  ];

  return (
    <div className="space-y-3">
      <div className="text-sm text-gray-400">
        {tr('adminRegistry.description')}
      </div>
      {cards.map(c => (
        <AdminCard
          key={c.key}
          title={c.title}
          expanded={!!expanded[c.key]}
          onToggle={() => toggle(c.key)}
        >
          {c.render()}
        </AdminCard>
      ))}
    </div>
  );
}
