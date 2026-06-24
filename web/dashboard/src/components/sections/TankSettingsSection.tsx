import { useCallback, useEffect, useMemo, useState } from 'react';
import { useLanguage } from '../../lib/language-context';
import { Trash2, Plus, ChevronDown, ChevronRight, Fish } from 'lucide-react';
import {
  Tanks, SensorDevices, Actuators, Cameras, CameraModels, SensorModels, ActuatorModels, WTGs, Controllers, ApiError,
} from '../../lib/api';
import type { DiscoveredCamera } from '../../lib/api';
import type {
  Tank, Sensor, Actuator, Camera, TankFormFactor,
  TankLifecycleResponse,
  NewSensorBody, NewCameraBody, NewWTGBody,
  CameraModel, MountLocation, ViewAngle, CameraPurpose,
  NewCameraModelBody, CameraLensType,
  SensorModel, NewSensorModelBody,
  MeasurementType, SensorMountLocation, MeasurementRole,
  ActuatorModel, ActuatorMountLocation, SafetyRole, OperatingMode,
  DeviceCategory, NewActuatorBodyWithMeta, Controller,
} from '../../lib/types';
import {
  MOUNT_LOCATION_LABELS, VIEW_ANGLE_LABELS, VIEW_ANGLE_AI_HINT,
  CAMERA_PURPOSE_LABELS, CAMERA_PURPOSE_TOOLTIPS,
  MEASUREMENT_TYPE_LABELS, MOUNT_LOCATION_SENSOR_LABELS,
  MEASUREMENT_ROLE_LABELS, MEASUREMENT_ROLE_TOOLTIPS,
  ACTUATOR_MOUNT_LOCATION_LABELS, SAFETY_ROLE_LABELS, SAFETY_ROLE_HINTS, OPERATING_MODE_LABELS,
  DEVICE_CATEGORY_LABELS, CONTROL_METHOD_LABELS,
} from '../../lib/types';
import { Button } from '../ui/button';
import { CameraTile } from '../CameraTile';
import { computeVolumeFromStrings, volumeHint } from '../../lib/volume';

// ─────────────────────────────────────────────────────────────────────────────
// TankSettingsSection — TankDetail "설정" 탭의 본체.
// 3 sub-section: 1) 기본 정보 (편집), 2) 자원 매핑, 3) 입식 이벤트.
// ─────────────────────────────────────────────────────────────────────────────

interface Props {
  tankId: string;
}

function friendlyError(err: unknown, tr: (k: string) => string): string {
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return tr('tankSettings.unknownError');
}

const FORM_FACTOR_LABEL_KEY: Record<string, string> = {
  '': 'tankSettings.unspecified',
  round: 'tankSettings.shapeRound',
  square: 'tankSettings.shapeSquare',
  rectangular: 'tankSettings.shapeRectangular',
};

export function TankSettingsSection({ tankId }: Props) {
  const { tr } = useLanguage();
  const [tank, setTank] = useState<Tank | null>(null);
  const [lifecycle, setLifecycle] = useState<TankLifecycleResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setError(null);
    try {
      const [tk, lc] = await Promise.all([
        Tanks.list().then(r => r.items.find(t => t.tank_id === tankId) ?? null),
        Tanks.lifecycle(tankId).catch(() => null),
      ]);
      setTank(tk);
      setLifecycle(lc);
    } catch (err) {
      setError(friendlyError(err, tr));
    }
  }, [tankId]);

  useEffect(() => { void reload(); }, [reload]);

  if (error) {
    return (
      <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
        {error}
      </div>
    );
  }
  if (!tank) {
    return <div className="text-sm text-gray-500 italic">{tr('tankSettings.loading')}</div>;
  }

  return (
    <div className="space-y-4">
      <BasicInfoCard tank={tank} onSaved={() => void reload()} />
      <ResourceMappingCard tankId={tankId} />
      <LifecycleEventCard lifecycle={lifecycle} />
    </div>
  );
}

// ── 1) 기본 정보 (편집) ────────────────────────────────────────────────────────

function BasicInfoCard({ tank, onSaved }: { tank: Tank; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [editing, setEditing] = useState(false);
  const [displayName, setDisplayName] = useState(tank.display_name);
  const [species, setSpecies] = useState(tank.species);
  const [systemType, setSystemType] = useState(tank.system_type);
  const [formFactor, setFormFactor] = useState<'' | TankFormFactor>(
    (tank.form_factor as TankFormFactor | '' | null | undefined) ?? '',
  );
  const [diameter, setDiameter] = useState(String(tank.diameter_m ?? ''));
  const [length, setLength] = useState(String(tank.length_m ?? ''));
  const [width, setWidth] = useState(String(tank.width_m ?? ''));
  const [depth, setDepth] = useState(String(tank.depth_m ?? ''));
  const [volume, setVolume] = useState(String(tank.volume_m3 ?? ''));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const cancel = () => {
    setEditing(false);
    setDisplayName(tank.display_name);
    setSpecies(tank.species);
    setSystemType(tank.system_type);
    setFormFactor((tank.form_factor as TankFormFactor | '' | null | undefined) ?? '');
    setDiameter(String(tank.diameter_m ?? ''));
    setLength(String(tank.length_m ?? ''));
    setWidth(String(tank.width_m ?? ''));
    setDepth(String(tank.depth_m ?? ''));
    setVolume(String(tank.volume_m3 ?? ''));
    setErr(null);
  };

  // C-10 — 용적 자동 계산. 형태/치수 입력에 따라 즉시 산출.
  const autoVolume = useMemo(
    () => computeVolumeFromStrings(formFactor, diameter, length, width, depth),
    [formFactor, diameter, length, width, depth],
  );
  const autoVolumeHint = useMemo(() => {
    return volumeHint({
      formFactor,
      diameterM: diameter ? Number(diameter) : undefined,
      lengthM: length ? Number(length) : undefined,
      widthM: width ? Number(width) : undefined,
      depthM: depth ? Number(depth) : undefined,
    });
  }, [formFactor, diameter, length, width, depth]);

  const save = async () => {
    setBusy(true);
    setErr(null);
    try {
      // 운영자가 volume 비워두면 autoVolume 사용 (있을 때). 명시 입력 시 그 값을 우선.
      const effectiveVolume = volume.trim() !== ''
        ? Number(volume)
        : (autoVolume ?? undefined);
      // 기존 POST /v1/tanks (UpsertTankProfile) 가 idempotent — tank_id 동일하면 덮어쓰기.
      await Tanks.create({
        tank_id: tank.tank_id,
        display_name: displayName.trim(),
        species: species.trim(),
        system_type: systemType.trim(),
        ...(effectiveVolume != null ? { volume_m3: effectiveVolume } : {}),
        ...(formFactor ? { form_factor: formFactor } : {}),
        ...(diameter ? { diameter_m: Number(diameter) } : {}),
        ...(length ? { length_m: Number(length) } : {}),
        ...(width ? { width_m: Number(width) } : {}),
        ...(depth ? { depth_m: Number(depth) } : {}),
        // 기존 group_id 보존 (편집 폼에선 변경 안 함).
        ...(tank.group_id ? { group_id: tank.group_id } : {}),
        ...(tank.site_id ? { site_id: tank.site_id } : {}),
        ...(tank.wtg_id ? { wtg_id: tank.wtg_id } : {}),
      });
      setEditing(false);
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/20 rounded-lg p-4 space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-white">{tr('tankSettings.basicInfo')}</h3>
        {!editing ? (
          <Button size="sm" variant="outline" onClick={() => setEditing(true)}>{tr('tankSettings.edit')}</Button>
        ) : (
          <div className="flex gap-2">
            <Button size="sm" onClick={() => void save()} disabled={busy}>
              {busy ? tr('tankSettings.saving') : tr('tankSettings.save')}
            </Button>
            <Button size="sm" variant="outline" onClick={cancel} disabled={busy}>{tr('tankSettings.cancel')}</Button>
          </div>
        )}
      </div>

      {!editing ? (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
          <InfoRow label={tr('tankSettings.displayName')} value={tank.display_name} />
          <InfoRow label={tr('tankSettings.species')} value={tank.species} />
          <InfoRow label={tr('tankSettings.systemType')} value={tank.system_type} />
          <InfoRow label={tr('tankSettings.shape')} value={tr(FORM_FACTOR_LABEL_KEY[tank.form_factor ?? ''] ?? 'tankSettings.unspecified')} />
          {tank.form_factor === 'round' && (
            <InfoRow label={tr('tankSettings.diameter')} value={tank.diameter_m ? `${tank.diameter_m} m` : '—'} />
          )}
          {tank.form_factor === 'square' && (
            <InfoRow label={tr('tankSettings.oneSide')} value={tank.length_m ? `${tank.length_m} m` : '—'} />
          )}
          {tank.form_factor === 'rectangular' && (
            <>
              <InfoRow label={tr('tankSettings.length')} value={tank.length_m ? `${tank.length_m} m` : '—'} />
              <InfoRow label={tr('tankSettings.width')} value={tank.width_m ? `${tank.width_m} m` : '—'} />
            </>
          )}
          <InfoRow label={tr('tankSettings.depth')} value={tank.depth_m ? `${tank.depth_m} m` : '—'} />
          <InfoRow label={tr('tankSettings.volume')} value={tank.volume_m3 ? `${tank.volume_m3} m³` : '—'} />
        </dl>
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('tankSettings.displayName')} value={displayName} onChange={setDisplayName} />
            <Field label={tr('tankSettings.species')} value={species} onChange={setSpecies} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <SelectField label={tr('tankSettings.systemType')} value={systemType} onChange={setSystemType}
              options={[
                { value: 'land_based_ras', label: tr('tankSettings.systemLandRas') },
                { value: 'marine_cage', label: tr('tankSettings.systemMarineCage') },
                { value: 'flow_through', label: tr('tankSettings.systemFlowThrough') },
              ]} />
            <SelectField label={tr('tankSettings.shape')} value={formFactor}
              onChange={v => setFormFactor(v as typeof formFactor)}
              options={[
                { value: '', label: tr('tankSettings.unspecifiedDash') },
                { value: 'round', label: tr('tankSettings.shapeRound') },
                { value: 'square', label: tr('tankSettings.shapeSquare') },
                { value: 'rectangular', label: tr('tankSettings.shapeRectangular') },
              ]} />
          </div>
          {formFactor === 'round' && (
            <Field label={tr('tankSettings.diameterM')} type="number" value={diameter} onChange={setDiameter} />
          )}
          {formFactor === 'square' && (
            <Field label={tr('tankSettings.oneSideM')} type="number" value={length} onChange={setLength} />
          )}
          {formFactor === 'rectangular' && (
            <div className="grid grid-cols-2 gap-3">
              <Field label={tr('tankSettings.lengthM')} type="number" value={length} onChange={setLength} />
              <Field label={tr('tankSettings.widthM')} type="number" value={width} onChange={setWidth} />
            </div>
          )}
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('tankSettings.depthM')} type="number" value={depth} onChange={setDepth} />
            <div className="flex flex-col gap-1">
              <label className="text-xs text-gray-400 font-medium">
                {tr('tankSettings.volumeM3')}
                <span className="text-gray-600 ml-1">{tr('tankSettings.volumeAutoHint')}</span>
              </label>
              <input
                type="number"
                value={volume}
                onChange={e => setVolume(e.target.value)}
                placeholder={autoVolume != null ? String(autoVolume) : ''}
                className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
              />
              {autoVolumeHint && (
                <span className="text-[11px] text-gray-500 font-mono">{autoVolumeHint}</span>
              )}
            </div>
          </div>
          {err && (
            <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
              {err}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="text-xs text-gray-500">{label}</dt>
      <dd className="text-sm text-white font-mono">{value}</dd>
    </>
  );
}

export function Field({ label, value, onChange, type = 'text' }: {
  label: string; value: string; onChange: (v: string) => void;
  type?: 'text' | 'number' | 'password' | 'date';
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-gray-400 font-medium">{label}</label>
      <input
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
      />
    </div>
  );
}

export function SelectField({ label, value, onChange, options }: {
  label: string; value: string; onChange: (v: string) => void;
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
        {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    </div>
  );
}

// ── 2) 자원 매핑 ──────────────────────────────────────────────────────────────

function ResourceMappingCard({ tankId }: { tankId: string }) {
  const { tr } = useLanguage();
  const [sensors, setSensors] = useState<Sensor[]>([]);
  const [actuators, setActuators] = useState<Actuator[]>([]);
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [openSection, setOpenSection] = useState<Record<string, boolean>>({
    sensors: true, actuators: true, cameras: true,
  });
  // C-10 — inline 등록 폼 펼침 상태. 기본은 모두 접힘 (운영자가 명시적으로 + 버튼 클릭).
  const [showForm, setShowForm] = useState<Record<string, boolean>>({
    sensor: false, actuator: false, camera: false,
  });
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    try {
      const [s, a, c] = await Promise.all([
        SensorDevices.list({ tankId }),
        Actuators.list({ tankId }),
        Cameras.list({ tankId }),
      ]);
      setSensors(s.items);
      setActuators(a.items);
      setCameras(c.items);
    } catch {
      // 단순화: 부분 실패 시 빈 배열 유지
    }
  }, [tankId]);

  useEffect(() => { void reload(); }, [reload]);

  const deleteSensor = async (id: string) => {
    if (!confirm(tr('tankSettings.confirmDeleteSensor', { id }))) return;
    setBusy(true);
    try { await SensorDevices.delete(id); await reload(); }
    finally { setBusy(false); }
  };
  const deleteActuator = async (id: string) => {
    if (!confirm(tr('tankSettings.confirmDeleteActuator', { id }))) return;
    setBusy(true);
    try { await Actuators.delete(id); await reload(); }
    finally { setBusy(false); }
  };
  const deleteCamera = async (id: string) => {
    if (!confirm(tr('tankSettings.confirmDeleteCamera', { id }))) return;
    setBusy(true);
    try { await Cameras.delete(id); await reload(); }
    finally { setBusy(false); }
  };

  const toggle = (key: string) => setOpenSection(p => ({ ...p, [key]: !p[key] }));
  const toggleForm = (key: string) =>
    setShowForm(p => ({ ...p, [key]: !p[key] }));
  const closeForm = (key: string) =>
    setShowForm(p => ({ ...p, [key]: false }));

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/20 rounded-lg p-4 space-y-3">
      <h3 className="text-sm font-medium text-white">{tr('tankSettings.resourceMapping')}</h3>

      <ResourceGroup
        title={tr('tankSettings.sensors')}
        count={sensors.length}
        open={!!openSection.sensors}
        onToggle={() => toggle('sensors')}
        emptyMsg={tr('tankSettings.sensorsEmpty')}
        actionLabel={showForm.sensor ? tr('tankSettings.closeForm') : tr('tankSettings.addSensor')}
        onAction={() => toggleForm('sensor')}
      >
        {sensors.map(s => (
          <ResourceRow
            key={s.sensor_id}
            label={s.sensor_id}
            sub={`${s.sensor_type}${s.position ? ` · ${s.position}` : ''}`}
            onDelete={() => void deleteSensor(s.sensor_id)}
            busy={busy}
          />
        ))}
        {showForm.sensor && (
          <InlineSensorForm
            tankId={tankId}
            onSaved={() => { void reload(); closeForm('sensor'); }}
            onCancel={() => closeForm('sensor')}
          />
        )}
      </ResourceGroup>

      <ResourceGroup
        title={tr('tankSettings.actuators')}
        count={actuators.length}
        open={!!openSection.actuators}
        onToggle={() => toggle('actuators')}
        emptyMsg={tr('tankSettings.actuatorsEmpty')}
        actionLabel={showForm.actuator ? tr('tankSettings.closeForm') : tr('tankSettings.addActuator')}
        onAction={() => toggleForm('actuator')}
      >
        {actuators.map(a => (
          <ResourceRow
            key={a.device_id}
            label={a.device_id}
            sub={`${a.device_type}${a.controller_id ? ` · ${a.controller_id}` : ''}`}
            onDelete={() => void deleteActuator(a.device_id)}
            busy={busy}
          />
        ))}
        {showForm.actuator && (
          <InlineActuatorForm
            tankId={tankId}
            onSaved={() => { void reload(); closeForm('actuator'); }}
            onCancel={() => closeForm('actuator')}
          />
        )}
      </ResourceGroup>

      <ResourceGroup
        title={tr('tankSettings.cameras')}
        count={cameras.length}
        open={!!openSection.cameras}
        onToggle={() => toggle('cameras')}
        emptyMsg={tr('tankSettings.camerasEmpty')}
        actionLabel={showForm.camera ? tr('tankSettings.closeForm') : tr('tankSettings.addCamera')}
        onAction={() => toggleForm('camera')}
      >
        <div className="grid grid-cols-2 gap-2">
          {cameras.map(cam => (
            <CameraInstanceCard
              key={cam.camera_id}
              camera={cam}
              busy={busy}
              onDelete={() => void deleteCamera(cam.camera_id)}
            />
          ))}
        </div>
        {showForm.camera && (
          <InlineCameraForm
            tankId={tankId}
            onSaved={() => { void reload(); closeForm('camera'); }}
            onCancel={() => closeForm('camera')}
          />
        )}
      </ResourceGroup>

      <p className="text-[11px] text-gray-600">
        {tr('tankSettings.resourceAutoMapping')} tank_id=
        <span className="font-mono text-green-400 mx-1">{tankId}</span> {tr('tankSettings.autoMapped')}.
      </p>
    </div>
  );
}

function ResourceGroup({
  title, count, open, onToggle, children, emptyMsg, actionLabel, onAction,
}: {
  title: string; count: number; open: boolean; onToggle: () => void;
  children: React.ReactNode; emptyMsg: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  const { tr } = useLanguage();
  return (
    <div className="rounded-md border border-gray-700">
      <div className="w-full flex items-center justify-between px-3 py-2 hover:bg-gray-800/40">
        <button
          type="button"
          onClick={onToggle}
          className="flex items-center gap-2 text-sm text-white flex-1 text-left"
          aria-expanded={open}
        >
          {open ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
          {title}
        </button>
        <div className="flex items-center gap-3">
          <span className="text-xs text-gray-400 font-mono">{count}{tr('tankSettings.countSuffix')}</span>
          {actionLabel && onAction && (
            <button
              type="button"
              onClick={(e) => { e.stopPropagation(); onAction(); }}
              className="text-xs px-2 py-0.5 rounded border border-green-500/40 text-green-400 hover:bg-green-500/10"
            >
              {actionLabel}
            </button>
          )}
        </div>
      </div>
      {open && (
        <div className="px-3 py-2 border-t border-gray-700 space-y-1.5">
          {count === 0 && (
            <div className="text-xs text-gray-500 italic py-1">{emptyMsg}</div>
          )}
          {/* children 은 항상 렌더 — 자원 row 와 인라인 폼이 섞임. */}
          {children}
        </div>
      )}
    </div>
  );
}

function ResourceRow({
  label, sub, onDelete, busy,
}: {
  label: string; sub: string; onDelete: () => void; busy?: boolean;
}) {
  const { tr } = useLanguage();
  return (
    <div className="group flex items-center justify-between gap-2 px-2 py-1 bg-gray-800/40 rounded text-xs">
      <div className="min-w-0">
        <div className="text-sm text-white font-mono truncate">{label}</div>
        <div className="text-xs text-gray-500 truncate">{sub}</div>
      </div>
      <button
        type="button"
        onClick={onDelete}
        disabled={busy}
        className="opacity-0 group-hover:opacity-100 p-1 text-gray-500 hover:text-red-400 disabled:opacity-30"
        aria-label={tr('tankSettings.deleteAria', { label })}
        title={tr('tankSettings.delete')}
      >
        <Trash2 className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

// C-10 — Inline 등록 폼 3종 (센서/액추에이터/카메라).
// 사용자가 수조 설정 탭 안에서 완결되도록. tank_id 자동 매핑.

// SENSOR_TYPES — C-13a 이후 InlineSensorForm 은 sensor_models 의 measurement_type 을 사용.
// 기존 자유 문자열 sensor_type 입력은 deprecated (모델 라이브러리 강제).
const ACTUATOR_TYPES = [
  'feeder', 'pump', 'aerator', 'oxygen_cone',
  'heater', 'uv_sterilizer', 'light', 'biofilter',
];

function InlineFormShell({
  title, children, onSubmit, onCancel, busy, err, ok, canSubmit,
}: {
  title: string;
  children: React.ReactNode;
  onSubmit: () => void;
  onCancel: () => void;
  busy: boolean;
  err: string | null;
  ok: string | null;
  canSubmit: boolean;
}) {
  const { tr } = useLanguage();
  return (
    <div className="mt-2 rounded-md border border-green-500/30 bg-gray-900/40 p-3 space-y-2">
      <div className="text-xs text-green-300 font-medium">{title}</div>
      {children}
      {err && (
        <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">
          {err}
        </div>
      )}
      {ok && (
        <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">
          {ok}
        </div>
      )}
      <div className="flex items-center gap-2 pt-1">
        <Button size="sm" onClick={onSubmit} disabled={busy || !canSubmit}>
          <Plus className="w-3.5 h-3.5 mr-1" />
          {busy ? tr('tankSettings.registering') : tr('tankSettings.register')}
        </Button>
        <Button size="sm" variant="outline" onClick={onCancel} disabled={busy}>{tr('tankSettings.cancel')}</Button>
      </div>
    </div>
  );
}

// device_type 또는 sensor_type 같은 select+직접입력 — enum dropdown + "직접 입력" 선택 시 free text.
function TypeSelectField({
  label, value, onChange, options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: string[];
}) {
  const { tr } = useLanguage();
  const [custom, setCustom] = useState(false);
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-gray-400 font-medium">{label} *</label>
      {!custom ? (
        <select
          value={value}
          onChange={e => {
            if (e.target.value === '__custom__') { setCustom(true); onChange(''); }
            else onChange(e.target.value);
          }}
          className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
        >
          {options.map(o => <option key={o} value={o}>{o}</option>)}
          <option value="__custom__">{tr('tankSettings.customInput')}</option>
        </select>
      ) : (
        <div className="flex gap-1">
          <input
            value={value}
            onChange={e => onChange(e.target.value)}
            placeholder={tr('tankSettings.customInputPlaceholder')}
            className="flex-1 h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          />
          <button type="button" onClick={() => { setCustom(false); onChange(options[0]); }}
            className="text-xs text-gray-400 hover:text-white px-2">▼</button>
        </div>
      )}
    </div>
  );
}

// C-13a — InlineSensorForm: 모델 라이브러리 강제 + mount_location dropdown + role multi.
// 카메라 InlineCameraForm 의 4 패턴을 동일하게 따른다 (모델 dropdown 강제, 자동 ID, 위치≠의도 분리).
// AdminRegistry / WTGDetail 에서도 reuse 할 수 있도록 export.
export function InlineSensorForm({ tankId, wtgId, onSaved, onCancel }: {
  tankId?: string;
  wtgId?: string;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const ctxLabel = tankId ? `tank: ${tankId}` : wtgId ? `wtg: ${wtgId}` : tr('tankSettings.noContext');
  // 모델 라이브러리 시드 + dropdown.
  const [models, setModels] = useState<SensorModel[]>([]);
  const [modelsLoaded, setModelsLoaded] = useState(false);
  const [showAddModel, setShowAddModel] = useState(false);
  const [modelId, setModelId] = useState<string>('');

  // sensor_id 자동 생성 — 같은 tank 의 기존 sensor 수 + 1.
  const [sensorId, setSensorId] = useState('');
  const [sensorIdOverride, setSensorIdOverride] = useState(false);

  // 위치(어디) — dropdown.
  const [mountLocation, setMountLocation] = useState<SensorMountLocation | ''>('');
  // 설치 깊이 — 수면 기준 (양수=위, 음수=아래).
  const [installedDepthM, setInstalledDepthM] = useState('');
  // 운영자 의도 — 다중 선택.
  const [measurementRole, setMeasurementRole] = useState<MeasurementRole[]>([]);
  // 교정 일자 — 마지막 (운영자 입력) + 다음 (자동 계산 read-only).
  const [calibrationLastAt, setCalibrationLastAt] = useState('');

  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  const reloadModels = useCallback(async () => {
    try {
      const r = await SensorModels.list();
      setModels(r.items);
    } catch {
      setModels([]);
    } finally {
      setModelsLoaded(true);
    }
  }, []);
  useEffect(() => { void reloadModels(); }, [reloadModels]);

  // 자동 sensor_id 생성. tank context 우선, 없으면 wtg context.
  useEffect(() => {
    if (sensorIdOverride) return;
    (async () => {
      try {
        if (tankId) {
          const r = await SensorDevices.list({ tankId });
          const seq = String(r.items.length + 1).padStart(2, '0');
          setSensorId(`sensor_${tankId}_${seq}`);
        } else if (wtgId) {
          const r = await SensorDevices.list({ wtgId });
          const seq = String(r.items.length + 1).padStart(2, '0');
          setSensorId(`sensor_${wtgId}_${seq}`);
        }
      } catch {
        if (tankId) setSensorId(`sensor_${tankId}_01`);
        else if (wtgId) setSensorId(`sensor_${wtgId}_01`);
      }
    })();
  }, [tankId, wtgId, sensorIdOverride]);

  const selectedModel = useMemo(
    () => models.find(m => m.model_id === modelId) ?? null,
    [models, modelId],
  );

  // 다음 교정 예정일 — model.calibration_interval_days + last 기반 자동 계산.
  const calibrationDueAt = useMemo(() => {
    if (!selectedModel?.calibration_interval_days || !calibrationLastAt) return '';
    const last = new Date(calibrationLastAt);
    if (Number.isNaN(last.getTime())) return '';
    const due = new Date(last.getTime() + selectedModel.calibration_interval_days * 86400000);
    return due.toISOString().slice(0, 10);
  }, [selectedModel, calibrationLastAt]);

  const toggleRole = (r: MeasurementRole) => {
    setMeasurementRole(prev => prev.includes(r) ? prev.filter(x => x !== r) : [...prev, r]);
  };

  // 모델 미선택이면 등록 불가 — 카메라 인스턴스 폼과 동일 강제.
  const canSubmit = sensorId.trim() !== '' && modelId.trim() !== '';

  const submit = async () => {
    setBusy(true); setErr(null); setOk(null);
    try {
      const trimmedId = sensorId.trim();
      const body: NewSensorBody & {
        model_id?: string;
        mount_location?: SensorMountLocation;
        installed_depth_m?: number;
        measurement_role?: MeasurementRole[];
        calibration_last_at?: string;
        calibration_due_at?: string;
      } = {
        sensor_id: trimmedId,
        // 모델 link 가 sensor_type 의 source of truth — backend 가 필수로 받음.
        sensor_type: selectedModel?.measurement_type ?? 'other',
      };
      if (tankId) body.tank_id = tankId;
      if (wtgId) body.wtg_id = wtgId;
      if (modelId) body.model_id = modelId;
      if (mountLocation) body.mount_location = mountLocation;
      if (installedDepthM.trim()) body.installed_depth_m = Number(installedDepthM);
      if (measurementRole.length > 0) body.measurement_role = measurementRole;
      if (calibrationLastAt) body.calibration_last_at = calibrationLastAt;
      if (calibrationDueAt) body.calibration_due_at = calibrationDueAt;
      await SensorDevices.create(body);
      setOk(tr('tankSettings.sensorRegistered', { id: trimmedId }));
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };

  return (
    <InlineFormShell
      title={tr('tankSettings.addSensorTitle', { ctx: ctxLabel })}
      onSubmit={() => void submit()} onCancel={onCancel}
      busy={busy} err={err} ok={ok}
      canSubmit={canSubmit}
    >
      {/* C-13a — 모델 라이브러리 강제. 모델 선택 안 하면 등록 불가. */}
      <div className="flex flex-col gap-2">
        <label className="text-xs text-gray-400 font-medium">
          {tr('tankSettings.sensorModel')} * <span className="text-[10px] text-gray-500">({tr('tankSettings.library')})</span>
        </label>
        {modelsLoaded && models.length === 0 && !showAddModel && (
          <div className="px-3 py-2 rounded border border-amber-500/30 bg-amber-500/5 text-xs space-y-1.5">
            <div className="text-amber-300">{tr('tankSettings.noSensorModels')}</div>
            <Button type="button" size="sm" variant="outline"
              onClick={() => setShowAddModel(true)}
            >
              {tr('tankSettings.registerModelFirst')}
            </Button>
          </div>
        )}
        {(models.length > 0 || showAddModel) && (
          <select
            value={modelId}
            onChange={e => {
              const v = e.target.value;
              if (v === '__add_new__') {
                setShowAddModel(true);
              } else {
                setModelId(v);
              }
            }}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          >
            <option value="">{tr('tankSettings.selectModelFirst')}</option>
            {models.map(m => (
              <option key={m.model_id} value={m.model_id}>
                {m.display_name} · {m.vendor} · {MEASUREMENT_TYPE_LABELS[m.measurement_type] ? tr(MEASUREMENT_TYPE_LABELS[m.measurement_type]) : m.measurement_type}
              </option>
            ))}
            <option value="__add_new__">{tr('tankSettings.addNewModel')}</option>
          </select>
        )}
        {showAddModel && (
          <InlineSensorModelMiniForm
            onSaved={async (newModelId) => {
              await reloadModels();
              setModelId(newModelId);
              setShowAddModel(false);
            }}
            onCancel={() => setShowAddModel(false)}
          />
        )}
        {selectedModel && (
          <div className="px-2 py-1.5 rounded bg-gray-800/50 text-[11px] text-gray-300 font-mono space-y-0.5">
            <div>{tr('tankSettings.measurement')}: <span className="text-cyan-300">{MEASUREMENT_TYPE_LABELS[selectedModel.measurement_type] ? tr(MEASUREMENT_TYPE_LABELS[selectedModel.measurement_type]) : selectedModel.measurement_type}</span>
              · {tr('tankSettings.unit')}: {selectedModel.unit}
              {selectedModel.range_min != null && (
                <> · {tr('tankSettings.range')}: {selectedModel.range_min}~{selectedModel.range_max ?? '?'}</>
              )}
            </div>
            <div>
              {selectedModel.accuracy_value != null && (
                <>{tr('tankSettings.accuracy')}: ±{selectedModel.accuracy_value}{selectedModel.accuracy_unit ?? ''}</>
              )}
              {selectedModel.protocol && <> · {selectedModel.protocol}</>}
              {selectedModel.calibration_interval_days && <> · {tr('tankSettings.calibration')} {selectedModel.calibration_interval_days}{tr('tankSettings.days')}</>}
            </div>
          </div>
        )}
        {!modelId && modelsLoaded && models.length > 0 && (
          <div className="text-[10px] text-amber-400">{tr('tankSettings.pleaseSelectModel')}</div>
        )}
      </div>

      {/* 자동 sensor_id */}
      <div className="flex flex-col gap-1">
        <label className="text-xs text-gray-400 font-medium">
          {tr('tankSettings.sensorId')} *
          <button type="button"
            onClick={() => setSensorIdOverride(true)}
            className={`ml-2 text-[10px] ${sensorIdOverride ? 'text-cyan-400' : 'text-gray-500 hover:text-white'}`}
          >
            {sensorIdOverride ? tr('tankSettings.editMode') : tr('tankSettings.autoClickToEdit')}
          </button>
        </label>
        <input
          type="text"
          value={sensorId}
          onChange={e => setSensorId(e.target.value)}
          disabled={!sensorIdOverride}
          className={`h-8 px-3 rounded-md border bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 ${
            sensorIdOverride ? 'border-gray-700' : 'border-gray-800 text-gray-400'
          }`}
        />
      </div>

      {/* 위치(어디) + 설치 깊이 — 의도와 분리. */}
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">
            {tr('tankSettings.installLocation')} <span className="text-[10px] text-gray-500">({tr('tankSettings.whereInstalled')})</span>
          </label>
          <select
            value={mountLocation}
            onChange={e => setMountLocation(e.target.value as SensorMountLocation | '')}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          >
            <option value="">{tr('tankSettings.unspecifiedDash')}</option>
            {(Object.keys(MOUNT_LOCATION_SENSOR_LABELS) as SensorMountLocation[]).map(v => (
              <option key={v} value={v}>{tr(MOUNT_LOCATION_SENSOR_LABELS[v])}</option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.installDepthM')}</label>
          <input
            type="number" step="0.1"
            value={installedDepthM}
            onChange={e => setInstalledDepthM(e.target.value)}
            placeholder={tr('tankSettings.depthPlaceholderSensor')}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          />
          <p className="text-[10px] text-gray-500">{tr('tankSettings.posNegWaterHint')}</p>
        </div>
      </div>

      {/* 운영자 의도 — multi chip toggle. */}
      <div className="flex flex-col gap-1.5">
        <label className="text-xs text-gray-400 font-medium">
          {tr('tankSettings.measurementRole')} <span className="text-[10px] text-gray-500">({tr('tankSettings.multiSelectAiIntent')})</span>
        </label>
        <div className="flex flex-wrap gap-1.5">
          {(Object.keys(MEASUREMENT_ROLE_LABELS) as MeasurementRole[]).map(role => {
            const active = measurementRole.includes(role);
            return (
              <button
                key={role}
                type="button"
                title={tr(MEASUREMENT_ROLE_TOOLTIPS[role])}
                onClick={() => toggleRole(role)}
                className={`px-2 py-1 rounded text-[11px] border transition-colors ${
                  active
                    ? 'bg-cyan-500/20 border-cyan-500 text-cyan-300'
                    : 'bg-gray-900 border-gray-700 text-gray-400 hover:text-white'
                }`}
              >
                {tr(MEASUREMENT_ROLE_LABELS[role])}
              </button>
            );
          })}
        </div>
      </div>

      {/* 교정 일자. */}
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.lastCalibrationDate')}</label>
          <input
            type="date"
            value={calibrationLastAt}
            onChange={e => setCalibrationLastAt(e.target.value)}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.nextCalibrationDate')}</label>
          <input
            type="text"
            value={calibrationDueAt || tr('tankSettings.calibrationDuePlaceholder')}
            readOnly
            className="h-8 px-3 rounded-md border border-gray-800 bg-gray-900 text-sm text-gray-400 font-mono"
          />
        </div>
      </div>
    </InlineFormShell>
  );
}

// C-13a — 인라인 mini 모델 등록 폼 (Inline 폼 안에서 "+ 새 모델" 클릭 시 펼침).
function InlineSensorModelMiniForm({
  onSaved, onCancel,
}: {
  onSaved: (modelId: string) => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const [vendor, setVendor] = useState('');
  const [productCode, setProductCode] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [measurementType, setMeasurementType] = useState<MeasurementType>('water_temperature');
  const [unit, setUnit] = useState('°C');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const autoModelId = useMemo(() => {
    const v = vendor.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    const p = productCode.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    if (!v || !p) return '';
    return `${v}_${p}`;
  }, [vendor, productCode]);

  const canSubmit = !!(vendor.trim() && productCode.trim() && displayName.trim() && unit.trim() && autoModelId);

  const submit = async () => {
    if (!canSubmit) return;
    setBusy(true); setErr(null);
    try {
      const body: NewSensorModelBody = {
        model_id: autoModelId,
        vendor: vendor.trim(),
        product_code: productCode.trim(),
        display_name: displayName.trim(),
        measurement_type: measurementType,
        unit: unit.trim(),
      };
      await SensorModels.create(body);
      onSaved(autoModelId);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };

  return (
    <div className="p-3 rounded border border-cyan-500/30 bg-cyan-500/5 space-y-2">
      <div className="flex items-center justify-between">
        <div className="text-xs text-cyan-300 font-medium">{tr('tankSettings.newSensorModelTitle')}</div>
        <button type="button" onClick={onCancel}
          className="text-[10px] text-gray-400 hover:text-white">{tr('tankSettings.cancel')}</button>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <Field label={tr('tankSettings.vendorRequired')} value={vendor} onChange={setVendor} />
        <Field label={tr('tankSettings.productCodeRequired')} value={productCode} onChange={setProductCode} />
      </div>
      <Field label={tr('tankSettings.displayNameRequired')} value={displayName} onChange={setDisplayName} />
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.measurementTypeRequired')}</label>
          <select
            value={measurementType}
            onChange={e => setMeasurementType(e.target.value as MeasurementType)}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-cyan-500/50"
          >
            {(Object.keys(MEASUREMENT_TYPE_LABELS) as MeasurementType[]).map(v => (
              <option key={v} value={v}>{tr(MEASUREMENT_TYPE_LABELS[v])}</option>
            ))}
          </select>
        </div>
        <Field label={tr('tankSettings.unitRequired')} value={unit} onChange={setUnit} />
      </div>
      {autoModelId && (
        <div className="text-[10px] text-gray-500">
          model_id ({tr('tankSettings.auto')}): <span className="font-mono text-cyan-300">{autoModelId}</span>
        </div>
      )}
      {err && <div className="text-[11px] text-red-400 font-mono">{err}</div>}
      <Button type="button" size="sm" onClick={() => void submit()}
        disabled={busy || !canSubmit}
      >
        {busy ? tr('tankSettings.registering') : tr('tankSettings.registerModelAutoSelect')}
      </Button>
      <p className="text-[10px] text-gray-500">
        {tr('tankSettings.sensorModelDetailHint')}
      </p>
    </div>
  );
}

// C-13b — InlineActuatorForm: 모델 라이브러리 dropdown 강제 + mount_location + safety_role chips + operating_mode.
// 카메라 / 센서 4 패턴 동일 적용 (모델 dropdown 강제, 자동 ID, 위치≠의도 분리, 안전 의도 chip).
// AdminRegistry / WTGDetail 에서도 reuse 할 수 있도록 export.
export function InlineActuatorForm({ tankId, wtgId, onSaved, onCancel }: {
  tankId?: string;
  wtgId?: string;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const ctxLabel = tankId ? `tank: ${tankId}` : wtgId ? `wtg: ${wtgId}` : tr('tankSettings.noContext');

  // 모델 라이브러리 시드 + dropdown.
  const [models, setModels] = useState<ActuatorModel[]>([]);
  const [modelsLoaded, setModelsLoaded] = useState(false);
  const [showAddModel, setShowAddModel] = useState(false);
  const [modelId, setModelId] = useState<string>('');

  // device_id 자동 생성 — 카테고리 prefix + 같은 컨텍스트의 기존 actuator 수 + 1.
  const [deviceId, setDeviceId] = useState('');
  const [deviceIdOverride, setDeviceIdOverride] = useState(false);

  const [deviceType, setDeviceType] = useState(ACTUATOR_TYPES[0]);
  const [controllerId, setControllerId] = useState('');
  // 등록된 컨트롤러(ESP32 자동 register) 목록 — controller_id dropdown 용.
  const [controllers, setControllers] = useState<Controller[]>([]);
  useEffect(() => {
    void (async () => {
      try { const r = await Controllers.list(); setControllers(r.items); }
      catch { setControllers([]); }
    })();
  }, []);
  const [model, setModel] = useState('');
  const [ratedW, setRatedW] = useState('');

  const [mountLocation, setMountLocation] = useState<ActuatorMountLocation | ''>('');
  const [safetyRoles, setSafetyRoles] = useState<SafetyRole[]>([]);
  const [operatingMode, setOperatingMode] = useState<OperatingMode>('auto');
  const [alarmThresholdsStr, setAlarmThresholdsStr] = useState('');
  const [lastMaintAt, setLastMaintAt] = useState('');

  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  const reloadModels = useCallback(async () => {
    try {
      const r = await ActuatorModels.list();
      setModels(r.items);
    } catch {
      setModels([]);
    } finally {
      setModelsLoaded(true);
    }
  }, []);
  useEffect(() => { void reloadModels(); }, [reloadModels]);

  // 모델 선택 시 자동 채움 (device_type / rated_power_w / model).
  const selectedModel = useMemo(() => models.find(m => m.model_id === modelId) ?? null, [models, modelId]);
  useEffect(() => {
    if (!selectedModel) return;
    setDeviceType(selectedModel.device_category);
    if (selectedModel.rated_power_w != null) setRatedW(String(selectedModel.rated_power_w));
    if (!model) setModel(`${selectedModel.vendor} ${selectedModel.product_code}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedModel]);

  // 다음 정비 예정일 자동 계산 — 모델 consumable_replacement_days 기준.
  const nextMaintenanceDueAt = useMemo(() => {
    if (!selectedModel?.consumable_replacement_days || !lastMaintAt) return '';
    try {
      const base = new Date(lastMaintAt);
      base.setDate(base.getDate() + selectedModel.consumable_replacement_days);
      return base.toISOString().slice(0, 10);
    } catch { return ''; }
  }, [selectedModel, lastMaintAt]);

  // 자동 device_id 생성 — 카테고리 prefix + 시퀀스.
  useEffect(() => {
    if (deviceIdOverride) return;
    void (async () => {
      try {
        const prefix = selectedModel?.device_category ?? deviceType ?? 'act';
        if (tankId) {
          const r = await Actuators.list({ tankId });
          const seq = String(r.items.length + 1).padStart(2, '0');
          setDeviceId(`${prefix}_${tankId}_${seq}`);
        } else if (wtgId) {
          const r = await Actuators.list({ wtgId });
          const seq = String(r.items.length + 1).padStart(2, '0');
          setDeviceId(`${prefix}_${wtgId}_${seq}`);
        }
      } catch {
        /* non-fatal — 운영자가 직접 입력 가능 */
      }
    })();
  }, [tankId, wtgId, selectedModel, deviceType, deviceIdOverride]);

  function toggleSafetyRole(r: SafetyRole) {
    setSafetyRoles(prev => prev.includes(r) ? prev.filter(x => x !== r) : [...prev, r]);
  }

  const submit = async () => {
    setBusy(true); setErr(null); setOk(null);
    try {
      const body: NewActuatorBodyWithMeta = {
        device_id: deviceId.trim(),
        device_type: deviceType.trim(),
      };
      if (tankId) body.tank_id = tankId;
      if (wtgId) body.wtg_id = wtgId;
      if (model) body.model = model.trim();
      if (controllerId) body.controller_id = controllerId.trim();
      if (ratedW) body.rated_power_w = Number(ratedW);
      if (modelId) body.model_id = modelId;
      if (mountLocation) body.mount_location = mountLocation;
      if (safetyRoles.length > 0) body.safety_role = safetyRoles;
      if (operatingMode) body.operating_mode = operatingMode;
      if (alarmThresholdsStr.trim()) {
        try {
          body.alarm_thresholds = JSON.parse(alarmThresholdsStr);
        } catch {
          throw new Error(tr('tankSettings.invalidAlarmJson'));
        }
      }
      if (lastMaintAt) body.last_maintenance_at = lastMaintAt;
      if (nextMaintenanceDueAt) body.next_maintenance_due_at = nextMaintenanceDueAt;
      await Actuators.create(body);
      setOk(tr('tankSettings.actuatorRegistered', { id: body.device_id }));
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };

  const canSubmit = deviceId.trim() !== '' && deviceType.trim() !== '';

  return (
    <InlineFormShell
      title={tr('tankSettings.addActuatorTitle', { ctx: ctxLabel })}
      onSubmit={() => void submit()} onCancel={onCancel}
      busy={busy} err={err} ok={ok}
      canSubmit={canSubmit}
    >
      {/* 모델 라이브러리 dropdown — 강제는 아님 (수동 fallback 유지). */}
      <div>
        <div className="flex items-center justify-between mb-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.actuatorModel')}</label>
          <button type="button" onClick={() => setShowAddModel(s => !s)}
            className="text-[11px] text-green-400 hover:text-green-300">
            {showAddModel ? tr('tankSettings.closeForm') : tr('tankSettings.addNewModelBtn')}
          </button>
        </div>
        <select value={modelId} onChange={e => setModelId(e.target.value)}
          className="h-8 px-3 w-full rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50">
          <option value="">{tr('tankSettings.modelUnspecifiedManual')}</option>
          {models.map(m => (
            <option key={m.model_id} value={m.model_id}>
              {m.display_name} ({m.vendor} · {DEVICE_CATEGORY_LABELS[m.device_category as DeviceCategory] ? tr(DEVICE_CATEGORY_LABELS[m.device_category as DeviceCategory]) : m.device_category})
            </option>
          ))}
        </select>
        {!modelsLoaded && (
          <div className="text-[10px] text-gray-500 italic mt-1">{tr('tankSettings.modelLibraryLoading')}</div>
        )}
        {modelsLoaded && models.length === 0 && (
          <div className="text-[10px] text-amber-400 italic mt-1">
            {tr('tankSettings.noActuatorModels')}
          </div>
        )}
      </div>

      {showAddModel && (
        <InlineActuatorModelMini onCreated={async () => {
          setShowAddModel(false);
          await reloadModels();
        }} />
      )}

      {selectedModel && (
        <div className="px-2 py-1.5 rounded bg-gray-800/40 border border-gray-700 text-[11px] text-gray-400">
          {tr('tankSettings.vendor')}: {selectedModel.vendor} · {tr('tankSettings.code')}: {selectedModel.product_code}
          {' · '}{tr('tankSettings.category')}: {DEVICE_CATEGORY_LABELS[selectedModel.device_category as DeviceCategory] ? tr(DEVICE_CATEGORY_LABELS[selectedModel.device_category as DeviceCategory]) : selectedModel.device_category}
          {selectedModel.control_method && ` · ${tr('tankSettings.control')}: ${CONTROL_METHOD_LABELS[selectedModel.control_method] ? tr(CONTROL_METHOD_LABELS[selectedModel.control_method]) : selectedModel.control_method}`}
        </div>
      )}

      {/* device_id 자동 — 클릭해서 수정. */}
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.deviceId')} *</label>
          {!deviceIdOverride && (
            <button type="button" onClick={() => setDeviceIdOverride(true)}
              className="text-[10px] text-gray-500 hover:text-white">{tr('tankSettings.autoClickToEdit')}</button>
          )}
        </div>
        <input value={deviceId} onChange={e => setDeviceId(e.target.value)}
          readOnly={!deviceIdOverride}
          className={`h-8 px-3 rounded-md border bg-gray-900 text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 ${
            deviceIdOverride ? 'border-gray-700 text-white' : 'border-gray-800 text-gray-400 cursor-pointer'
          }`}
          onClick={() => !deviceIdOverride && setDeviceIdOverride(true)} />
      </div>

      <div className="grid grid-cols-2 gap-2">
        <TypeSelectField label={tr('tankSettings.deviceType')} value={deviceType} onChange={setDeviceType} options={ACTUATOR_TYPES} />
        <SelectField label={tr('tankSettings.installLocationMount')}
          value={mountLocation} onChange={v => setMountLocation(v as ActuatorMountLocation | '')}
          options={[
            { value: '', label: tr('tankSettings.unspecifiedDash') },
            ...(Object.keys(ACTUATOR_MOUNT_LOCATION_LABELS) as ActuatorMountLocation[]).map(v => ({
              value: v, label: tr(ACTUATOR_MOUNT_LOCATION_LABELS[v]),
            })),
          ]}
        />
      </div>

      <div>
        <div className="text-xs text-gray-400 font-medium mb-1">{tr('tankSettings.safetyRole')}</div>
        <div className="flex flex-wrap gap-1">
          {(Object.keys(SAFETY_ROLE_LABELS) as SafetyRole[]).map(r => {
            const active = safetyRoles.includes(r);
            const isCritical = r === 'oxygen_critical' || r === 'circulation_critical';
            return (
              <button
                key={r}
                type="button"
                onClick={() => toggleSafetyRole(r)}
                title={tr(SAFETY_ROLE_HINTS[r])}
                className={`text-[11px] px-2 py-0.5 rounded border transition-colors ${
                  active
                    ? (isCritical
                        ? 'bg-red-500/20 border-red-500/50 text-red-300'
                        : 'bg-green-500/20 border-green-500/50 text-green-300')
                    : 'bg-gray-800/40 border-gray-700 text-gray-400 hover:text-gray-200'
                }`}
              >
                {tr(SAFETY_ROLE_LABELS[r])}
              </button>
            );
          })}
        </div>
        {safetyRoles.length > 0 && (
          <div className="mt-1 text-[10px] text-gray-500 italic">
            {safetyRoles.map(r => tr(SAFETY_ROLE_HINTS[r])).join(' · ')}
          </div>
        )}
      </div>

      <div className="grid grid-cols-2 gap-2">
        <SelectField label={tr('tankSettings.operatingMode')}
          value={operatingMode} onChange={v => setOperatingMode(v as OperatingMode)}
          options={(Object.keys(OPERATING_MODE_LABELS) as OperatingMode[]).map(v => ({
            value: v, label: tr(OPERATING_MODE_LABELS[v]),
          }))}
        />
        <SelectField label={controllers.length ? tr('tankSettings.controllerOptional') : tr('tankSettings.controllerNoEsp32')}
          value={controllerId} onChange={setControllerId}
          options={[
            { value: '', label: controllers.length ? tr('tankSettings.unspecifiedDash') : tr('tankSettings.noControllerAutoRegister') },
            ...controllers.map(c => ({
              value: c.controller_id,
              label: `${c.controller_id}${c.tank_id ? ` · ${c.tank_id}` : ''}${c.status ? ` (${c.status})` : ''}`,
            })),
          ]}
        />
      </div>

      <div className="grid grid-cols-2 gap-2">
        <Field label={tr('tankSettings.modelFreeText')} value={model} onChange={setModel} />
        <Field label={tr('tankSettings.ratedPowerW')} type="number" value={ratedW} onChange={setRatedW} />
      </div>

      <div className="flex flex-col gap-1">
        <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.alarmThresholds')}</label>
        <textarea
          value={alarmThresholdsStr}
          onChange={e => setAlarmThresholdsStr(e.target.value)}
          rows={2}
          placeholder='{"pressure_min_kpa":30,"pressure_max_kpa":80}'
          className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900 text-xs text-white font-mono focus:outline-none focus:ring-2 focus:ring-green-500/50"
        />
      </div>

      <div className="grid grid-cols-2 gap-2">
        <Field label={tr('tankSettings.lastMaintenanceDate')} type="date" value={lastMaintAt} onChange={setLastMaintAt} />
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.nextMaintenanceDue')}</label>
          <div className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900/50 text-xs text-gray-500 font-mono h-8 flex items-center">
            {nextMaintenanceDueAt || tr('tankSettings.maintenanceDuePlaceholder')}
          </div>
        </div>
      </div>
    </InlineFormShell>
  );
}

// 모델 등록 미니 폼 — InlineActuatorForm 내부 inline 모델 추가용. AdminRegistry 의 전체 폼 대비 최소화.
function InlineActuatorModelMini({ onCreated }: { onCreated: () => void | Promise<void> }) {
  const { tr } = useLanguage();
  const [vendor, setVendor] = useState('');
  const [productCode, setProductCode] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [deviceCategory, setDeviceCategory] = useState<DeviceCategory>('pump');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const autoModelId = useMemo(() => {
    const v = vendor.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    const p = productCode.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    if (!v || !p) return '';
    return `${v}_${p}`;
  }, [vendor, productCode]);
  const submit = async () => {
    setBusy(true); setErr(null);
    try {
      await ActuatorModels.create({
        model_id: autoModelId,
        vendor: vendor.trim(),
        product_code: productCode.trim(),
        display_name: displayName.trim(),
        device_category: deviceCategory,
      });
      await onCreated();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };
  return (
    <div className="rounded border border-cyan-500/30 bg-cyan-500/5 p-2 space-y-2">
      <div className="text-[11px] text-cyan-300 font-medium">{tr('tankSettings.newActuatorModelTitle')}</div>
      <div className="grid grid-cols-3 gap-1">
        <Field label={tr('tankSettings.vendorRequired')} value={vendor} onChange={setVendor} />
        <Field label={tr('tankSettings.productCodeRequired')} value={productCode} onChange={setProductCode} />
        <SelectField label={tr('tankSettings.categoryRequired')}
          value={deviceCategory} onChange={v => setDeviceCategory(v as DeviceCategory)}
          options={(Object.keys(DEVICE_CATEGORY_LABELS) as DeviceCategory[]).map(v => ({
            value: v, label: tr(DEVICE_CATEGORY_LABELS[v]),
          }))}
        />
      </div>
      <Field label={tr('tankSettings.displayNameRequired')} value={displayName} onChange={setDisplayName} />
      {err && (
        <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[10px] text-red-400 font-mono">
          {err}
        </div>
      )}
      <Button size="sm" type="button"
        disabled={busy || !vendor.trim() || !productCode.trim() || !displayName.trim()}
        onClick={() => void submit()}>
        {busy ? tr('tankSettings.registering') : tr('tankSettings.registerModel')}
      </Button>
    </div>
  );
}

// C-11 — 등록된 카메라 카드. 휴지통 (기존 hotfix) + 연결 테스트 버튼.
function CameraInstanceCard({ camera, busy, onDelete }: {
  camera: Camera; busy: boolean; onDelete: () => void;
}) {
  const { tr } = useLanguage();
  const [testBusy, setTestBusy] = useState(false);
  const [testMsg, setTestMsg] = useState<string | null>(null);

  const runTest = async () => {
    setTestBusy(true); setTestMsg(null);
    try {
      const r = await Cameras.test(camera.camera_id);
      if (r.snapshot_ok) {
        setTestMsg(tr('tankSettings.cameraTestOk'));
      } else if (r.snapshot_error) {
        setTestMsg(`${tr('tankSettings.cameraConnFail')}: ${r.snapshot_error}`);
      } else {
        setTestMsg(tr('tankSettings.testDone'));
      }
    } catch (e) {
      setTestMsg(`${tr('tankSettings.testFail')}: ${friendlyError(e, tr)}`);
    } finally { setTestBusy(false); }
  };

  return (
    <div className="group relative">
      <CameraTile camera={camera} />
      <button
        type="button"
        onClick={onDelete}
        disabled={busy}
        className="absolute top-1.5 right-1.5 opacity-0 group-hover:opacity-100 p-1.5 rounded bg-black/70 hover:bg-red-500/90 text-white disabled:opacity-30 transition-opacity"
        title={tr('tankSettings.deleteCameraMapping')}
        aria-label={tr('tankSettings.deleteCameraAria', { id: camera.camera_id })}
      >
        <Trash2 className="w-4 h-4" />
      </button>
      <div className="flex items-center justify-between gap-2 mt-1">
        <div className="text-xs text-gray-500 font-mono truncate flex-1">{camera.camera_id}</div>
        <button
          type="button"
          onClick={() => void runTest()}
          disabled={testBusy}
          className="text-[10px] px-2 py-0.5 rounded border border-cyan-500/40 text-cyan-300 hover:bg-cyan-500/10 disabled:opacity-50"
          title={tr('tankSettings.connTest')}
        >
          {testBusy ? tr('tankSettings.testingEllipsis') : tr('tankSettings.test')}
        </button>
      </div>
      {testMsg && (
        <div className={`text-[10px] font-mono mt-0.5 ${testMsg === tr('tankSettings.cameraTestOk') ? 'text-green-400' : 'text-amber-400'}`}>
          {testMsg}
        </div>
      )}
    </div>
  );
}

// C-12 — InlineCameraForm 을 AdminRegistry 에서도 reuse 할 수 있도록 export.
// 벤더별 기본 RTSP sub 경로 (백엔드 vendorRTSPPaths 와 동기화). 벤더 선택 시 자동 채움.
const VENDOR_RTSP_SUB: Record<string, string> = {
  hikvision: '/Streaming/Channels/102',
  dahua: '/cam/realmonitor?channel=1&subtype=1',
  hanwha: '/profile5/media.smp',
  samsung: '/profile5/media.smp',
  axis: '/axis-media/media.amp?resolution=320x240',
  reolink: '/h264Preview_01_sub',
  balus: '/1',
};

export function InlineCameraForm({ tankId, onSaved, onCancel }: {
  tankId: string; onSaved: () => void; onCancel: () => void;
}) {
  const { tr } = useLanguage();
  // C-12 — 모델 라이브러리 강제 + mount_location/view_angle 분리 + 수면 기준 높이 + purpose 다중선택.
  const [models, setModels] = useState<CameraModel[]>([]);
  const [modelsLoaded, setModelsLoaded] = useState(false);
  const [showAddModel, setShowAddModel] = useState(false);
  const [modelId, setModelId] = useState<string>('');
  const [cameraId, setCameraId] = useState('');
  const [cameraIdOverride, setCameraIdOverride] = useState(false);
  const [displayName, setDisplayName] = useState('');
  const [vendor, setVendor] = useState('hikvision');
  const [host, setHost] = useState('');
  const [rtspPort, setRtspPort] = useState('554');
  const [httpPort, setHttpPort] = useState('80');
  const [rtspPath, setRtspPath] = useState('');
  const [rtspPathEdited, setRtspPathEdited] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [mountLocation, setMountLocation] = useState<MountLocation | ''>('');
  const [viewAngle, setViewAngle] = useState<ViewAngle | ''>('');
  const [heightFromWaterM, setHeightFromWaterM] = useState('');
  const [tiltDeg, setTiltDeg] = useState('');
  const [purpose, setPurpose] = useState<CameraPurpose[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  // 카메라 자동 탐지 (ONVIF + 포트스캔).
  const [discovering, setDiscovering] = useState(false);
  const [discovered, setDiscovered] = useState<DiscoveredCamera[]>([]);
  const [discoverMsg, setDiscoverMsg] = useState<string | null>(null);
  // 등록 전 연결 테스트 결과.
  const [testBusy, setTestBusy] = useState(false);
  const [testThumbnailURL, setTestThumbnailURL] = useState<string | null>(null);
  const [testMsg, setTestMsg] = useState<string | null>(null);

  async function runDiscover() {
    setDiscovering(true);
    setDiscoverMsg(null);
    try {
      const r = await Cameras.discover();
      setDiscovered(r.cameras);
      setDiscoverMsg(
        r.cameras.length === 0
          ? tr('tankSettings.noCamerasFound')
          : `${r.cameras.length}${tr('tankSettings.camerasFoundSuffix')}`,
      );
    } catch (e) {
      setDiscoverMsg(e instanceof Error ? e.message : tr('tankSettings.searchFailed'));
    } finally {
      setDiscovering(false);
    }
  }

  function applyDiscovered(c: DiscoveredCamera) {
    setHost(c.ip);
    if (c.rtsp_port) setRtspPort(String(c.rtsp_port));
    if (c.http_port) setHttpPort(String(c.http_port));
    if (c.vendor) setVendor(c.vendor.toLowerCase());
    setRtspPathEdited(false); // 새 카메라 선택 시 벤더 기본 경로 재유도
  }

  // 벤더가 알려진 종류면 RTSP 경로 자동 채움 (사용자가 직접 편집하기 전까지).
  useEffect(() => {
    if (rtspPathEdited) return;
    setRtspPath(VENDOR_RTSP_SUB[vendor.trim().toLowerCase()] ?? '');
  }, [vendor, rtspPathEdited]);

  const reloadModels = useCallback(async () => {
    try {
      const r = await CameraModels.list();
      setModels(r.items);
    } catch {
      setModels([]);
    } finally {
      setModelsLoaded(true);
    }
  }, []);

  // 모델 라이브러리 시드.
  useEffect(() => { void reloadModels(); }, [reloadModels]);

  // 같은 수조의 카메라 수 + 1 로 자동 ID 생성 — 운영자 override 가능.
  useEffect(() => {
    if (cameraIdOverride) return;
    (async () => {
      try {
        const r = await Cameras.list({ tankId });
        const seq = String(r.count + 1).padStart(2, '0');
        setCameraId(`cam_${tankId}_${seq}`);
      } catch {
        setCameraId(`cam_${tankId}_01`);
      }
    })();
  }, [tankId, cameraIdOverride]);

  // 모델 선택 시 vendor / displayName 자동 채움.
  const selectedModel = useMemo(
    () => models.find(m => m.model_id === modelId) ?? null,
    [models, modelId],
  );
  useEffect(() => {
    if (selectedModel) {
      setVendor(selectedModel.vendor.toLowerCase());
      if (!displayName.trim()) {
        setDisplayName(selectedModel.display_name);
      }
    }
    // displayName 은 다시 안 덮어쓰기 — 운영자가 손댄 다음에는 보존.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedModel]);

  // 클린업 — blob URL revoke.
  useEffect(() => {
    return () => { if (testThumbnailURL) URL.revokeObjectURL(testThumbnailURL); };
  }, [testThumbnailURL]);

  // C-12 size_estimation 호환성 hint — hard block 아님, 경고만 (운영자 override 자유).
  const sizeWarning = useMemo(() => {
    if (!purpose.includes('size_estimation')) return null;
    const needsDual = !selectedModel || selectedModel.lens_type !== 'dual';
    const needsUWSide = viewAngle !== 'underwater_side';
    if (!needsDual && !needsUWSide) return null;
    const parts: string[] = [];
    if (needsDual) parts.push(tr('tankSettings.dualLensModel'));
    if (needsUWSide) parts.push(tr('tankSettings.uwSideView'));
    return `${tr('tankSettings.sizeEstimationRecommend')} ${parts.join(' + ')} ${tr('tankSettings.recommended')}`;
  }, [purpose, viewAngle, selectedModel]);

  const togglePurpose = (p: CameraPurpose) => {
    setPurpose(prev => prev.includes(p) ? prev.filter(x => x !== p) : [...prev, p]);
  };

  // C-12 — 모델 미선택이면 인스턴스 등록 불가. submit guard.
  const canSubmit = cameraId.trim() !== '' && displayName.trim() !== '' && modelId.trim() !== '';

  const runConnectionTest = async () => {
    setTestBusy(true); setTestMsg(null);
    if (testThumbnailURL) { URL.revokeObjectURL(testThumbnailURL); setTestThumbnailURL(null); }
    try {
      const blob = await Cameras.testProfileSnapshot({
        camera_id: cameraId.trim() || `cam_${tankId}_test`,
        tank_id: tankId,
        vendor: vendor.trim() || undefined,
        host: host.trim(),
        rtsp_port: rtspPort ? Number(rtspPort) : undefined,
        http_port: httpPort ? Number(httpPort) : undefined,
        username: username.trim() || undefined,
        password: password || undefined,
        stream_profiles: rtspPath.trim() ? { sub: { path: rtspPath.trim() } } : undefined,
      });
      setTestThumbnailURL(URL.createObjectURL(blob));
      setTestMsg(tr('tankSettings.connSuccessSnapshot'));
    } catch (e) {
      setTestMsg(`${tr('tankSettings.connFail')}: ${friendlyError(e, tr)}`);
    } finally { setTestBusy(false); }
  };

  const submit = async () => {
    setBusy(true); setErr(null); setOk(null);
    try {
      const trimmedId = cameraId.trim();
      const body: NewCameraBody = {
        camera_id: trimmedId,
        tank_id: tankId,
        display_name: displayName.trim(),
      };
      if (modelId) body.model_id = modelId;
      if (vendor) body.vendor = vendor.trim();
      if (host) body.host = host.trim();
      if (rtspPort) body.rtsp_port = Number(rtspPort);
      if (httpPort) body.http_port = Number(httpPort);
      if (username) body.username = username.trim();
      if (mountLocation) body.mount_location = mountLocation;
      if (viewAngle) body.view_angle = viewAngle;
      if (heightFromWaterM.trim()) body.height_from_water_m = Number(heightFromWaterM);
      if (tiltDeg.trim()) body.tilt_deg = Number(tiltDeg);
      if (purpose.length > 0) body.purpose = purpose;
      if (rtspPath.trim()) body.stream_profiles = { sub: { path: rtspPath.trim() } };
      // password 입력 시 password_secret_ref 는 backend 가 자동 채움. 입력 없으면 placeholder ref.
      if (!password) body.password_secret_ref = `secrets://camera_${trimmedId}`;
      await Cameras.create(body);
      if (password) {
        await Cameras.setSecret(trimmedId, password);
        setOk(tr('tankSettings.cameraRegisteredWithPassword', { id: trimmedId }));
      } else {
        setOk(tr('tankSettings.cameraRegisteredNoPassword', { id: trimmedId }));
      }
      setPassword(''); // 화면에 평문이 남아 있지 않도록 즉시 비움
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };

  return (
    <InlineFormShell
      title={tr('tankSettings.addCameraTitle', { ctx: `tank: ${tankId}` })}
      onSubmit={() => void submit()} onCancel={onCancel}
      busy={busy} err={err} ok={ok}
      canSubmit={canSubmit}
    >
      {/* C-12 — 모델 라이브러리 강제. 모델 선택 안 하면 인스턴스 등록 불가. */}
      <div className="flex flex-col gap-2">
        <label className="text-xs text-gray-400 font-medium">
          {tr('tankSettings.cameraModel')} * <span className="text-[10px] text-gray-500">({tr('tankSettings.library')})</span>
        </label>

        {/* 모델 없을 때 안내 + "+ 새 모델 등록" 인라인 폼 */}
        {modelsLoaded && models.length === 0 && !showAddModel && (
          <div className="px-3 py-2 rounded border border-amber-500/30 bg-amber-500/5 text-xs space-y-1.5">
            <div className="text-amber-300">{tr('tankSettings.noCameraModels')}</div>
            <Button type="button" size="sm" variant="outline"
              onClick={() => setShowAddModel(true)}
            >
              {tr('tankSettings.registerModelFirst')}
            </Button>
          </div>
        )}

        {/* 모델 dropdown — 모델이 1개 이상 있을 때 또는 추가 폼 표시 중일 때 */}
        {(models.length > 0 || showAddModel) && (
          <select
            value={modelId}
            onChange={e => {
              const v = e.target.value;
              if (v === '__add_new__') {
                setShowAddModel(true);
              } else {
                setModelId(v);
              }
            }}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          >
            <option value="">{tr('tankSettings.selectModelFirst')}</option>
            {models.map(m => (
              <option key={m.model_id} value={m.model_id}>
                {m.display_name} · {m.vendor} · {m.lens_type}
              </option>
            ))}
            <option value="__add_new__">{tr('tankSettings.addNewModel')}</option>
          </select>
        )}

        {/* 인라인 모델 등록 폼 (펼침/접힘) */}
        {showAddModel && (
          <InlineCameraModelMiniForm
            onSaved={async (newModelId) => {
              await reloadModels();
              setModelId(newModelId);
              setShowAddModel(false);
            }}
            onCancel={() => setShowAddModel(false)}
          />
        )}

        {/* 선택된 모델 정보 표시 (read-only) */}
        {selectedModel && (
          <div className="px-2 py-1.5 rounded bg-gray-800/50 text-[11px] text-gray-300 font-mono space-y-0.5">
            <div>{tr('tankSettings.vendor')}: <span className="text-cyan-300">{selectedModel.vendor}</span> · {tr('tankSettings.code')}: {selectedModel.product_code}</div>
            <div>{tr('tankSettings.lens')}: <span className="text-cyan-300">{selectedModel.lens_type}</span>
              {selectedModel.lens_type === 'dual' && selectedModel.baseline_mm != null && (
                <> · baseline: {selectedModel.baseline_mm}mm</>
              )}
              {selectedModel.resolution_w && selectedModel.resolution_h && (
                <> · {selectedModel.resolution_w}x{selectedModel.resolution_h}</>
              )}
              {selectedModel.fps && <> · {selectedModel.fps}fps</>}
              {selectedModel.night_mode && <> · {tr('tankSettings.nightMode')}</>}
            </div>
            {selectedModel.lens_type === 'dual' && (
              <div className="text-[10px] text-cyan-400">
                {tr('tankSettings.dualLensSizeCapable')}
              </div>
            )}
          </div>
        )}

        {!modelId && modelsLoaded && models.length > 0 && (
          <div className="text-[10px] text-amber-400">{tr('tankSettings.pleaseSelectModel')}</div>
        )}
      </div>

      {/* 자동 ID + 표시 이름 */}
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">
            {tr('tankSettings.cameraId')} *
            <button type="button"
              onClick={() => setCameraIdOverride(true)}
              className={`ml-2 text-[10px] ${cameraIdOverride ? 'text-cyan-400' : 'text-gray-500 hover:text-white'}`}
            >
              {cameraIdOverride ? tr('tankSettings.editMode') : tr('tankSettings.autoClickToEdit')}
            </button>
          </label>
          <input
            type="text"
            value={cameraId}
            onChange={e => setCameraId(e.target.value)}
            disabled={!cameraIdOverride}
            className={`h-8 px-3 rounded-md border bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 ${
              cameraIdOverride ? 'border-gray-700' : 'border-gray-800 text-gray-400'
            }`}
          />
        </div>
        <Field label={tr('tankSettings.displayNameRequired')} value={displayName} onChange={setDisplayName} />
      </div>

      {/* 카메라 자동 탐지 — IP 를 직접 외우지 않고 네트워크에서 찾아 채움. */}
      <div className="rounded-md border border-cyan-500/20 bg-cyan-500/5 p-2 space-y-2">
        <div className="flex items-center gap-2">
          <Button type="button" size="sm" variant="outline"
            onClick={() => void runDiscover()} disabled={discovering}>
            {discovering ? tr('tankSettings.discovering') : tr('tankSettings.discoverCamera')}
          </Button>
          {discoverMsg && <span className="text-[11px] text-gray-400">{discoverMsg}</span>}
        </div>
        {discovered.length > 0 && (
          <div className="space-y-1 max-h-40 overflow-auto">
            {discovered.map(c => (
              <button key={c.ip} type="button" onClick={() => applyDiscovered(c)}
                className={`w-full flex items-center justify-between gap-2 px-2 py-1 rounded border text-left text-xs transition-colors ${
                  host === c.ip
                    ? 'bg-cyan-500/15 border-cyan-500/40 text-cyan-200'
                    : 'bg-gray-800/40 border-gray-700/40 text-gray-300 hover:border-gray-600'
                }`}>
                <span className="font-mono">{c.ip}</span>
                <span className="truncate text-gray-400">
                  {[c.name, c.model, c.vendor].filter(Boolean).join(' · ') || (c.mac ?? '')}
                </span>
                <span className={`shrink-0 px-1.5 py-0.5 rounded text-[10px] ${
                  c.source === 'onvif' ? 'bg-green-500/15 text-green-300' : 'bg-gray-600/30 text-gray-400'
                }`}>
                  {c.source === 'onvif' ? 'ONVIF' : tr('tankSettings.portScan')}
                </span>
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="grid grid-cols-3 gap-2">
        <Field label={tr('tankSettings.vendor')} value={vendor} onChange={setVendor} />
        <Field label={tr('tankSettings.hostIp')} value={host} onChange={setHost} />
        <Field label="username" value={username} onChange={setUsername} />
      </div>

      <div className="grid grid-cols-2 gap-2">
        <Field label={tr('tankSettings.rtspPort')} type="number" value={rtspPort} onChange={setRtspPort} />
        <Field label={tr('tankSettings.httpPort')} type="number" value={httpPort} onChange={setHttpPort} />
      </div>
      <div className="flex flex-col gap-1">
        <Field
          label={`${tr('tankSettings.rtspPathLabel')}${VENDOR_RTSP_SUB[vendor.trim().toLowerCase()] ? ` — ${tr('tankSettings.vendorDefaultAuto')}` : ` — ${tr('tankSettings.unknownVendorManual')}`}`}
          value={rtspPath}
          onChange={v => { setRtspPathEdited(true); setRtspPath(v); }}
        />
        <span className="text-[10px] text-gray-500">
          {tr('tankSettings.rtspPathHint')}
        </span>
      </div>
      <Field label="password" type="password" value={password} onChange={setPassword} />

      {/* C-12 — 위치 (어디 위) + 시점/구도 (어떻게 보는가) 분리 */}
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">
            {tr('tankSettings.installLocation')} <span className="text-[10px] text-gray-500">({tr('tankSettings.whereOnTop')})</span>
          </label>
          <select
            value={mountLocation}
            onChange={e => setMountLocation(e.target.value as MountLocation | '')}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          >
            <option value="">{tr('tankSettings.unspecifiedDash')}</option>
            {(Object.keys(MOUNT_LOCATION_LABELS) as MountLocation[]).map(v => (
              <option key={v} value={v}>{tr(MOUNT_LOCATION_LABELS[v])}</option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">
            {tr('tankSettings.viewAngle')} <span className="text-[10px] text-gray-500">({tr('tankSettings.aiAlgorithmSelect')})</span>
          </label>
          <select
            value={viewAngle}
            onChange={e => setViewAngle(e.target.value as ViewAngle | '')}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          >
            <option value="">{tr('tankSettings.unspecifiedDash')}</option>
            {(Object.keys(VIEW_ANGLE_LABELS) as ViewAngle[]).map(v => (
              <option key={v} value={v}>
                {tr(VIEW_ANGLE_LABELS[v])} ({tr(VIEW_ANGLE_AI_HINT[v])})
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* C-12 — 설치 기하 (수면 기준 명시) */}
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.heightFromWaterM')}</label>
          <input
            type="number" step="0.1"
            value={heightFromWaterM}
            onChange={e => setHeightFromWaterM(e.target.value)}
            placeholder={tr('tankSettings.depthPlaceholderCamera')}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          />
          <p className="text-[10px] text-gray-500">{tr('tankSettings.posNegWaterHint')}</p>
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.tiltDeg')}</label>
          <input
            type="number" step="1"
            value={tiltDeg}
            onChange={e => setTiltDeg(e.target.value)}
            placeholder={tr('tankSettings.tiltPlaceholder')}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          />
        </div>
      </div>

      {/* C-12 — AI 사용 목적 (purpose, 다중 선택) */}
      <div className="flex flex-col gap-1.5">
        <label className="text-xs text-gray-400 font-medium">
          {tr('tankSettings.aiPurpose')} <span className="text-[10px] text-gray-500">({tr('tankSettings.multiSelectGx10Resource')})</span>
        </label>
        <div className="flex flex-wrap gap-1.5">
          {(Object.keys(CAMERA_PURPOSE_LABELS) as CameraPurpose[]).map(p => {
            const active = purpose.includes(p);
            return (
              <button
                key={p}
                type="button"
                title={tr(CAMERA_PURPOSE_TOOLTIPS[p])}
                onClick={() => togglePurpose(p)}
                className={`px-2 py-1 rounded text-[11px] border transition-colors ${
                  active
                    ? 'bg-cyan-500/20 border-cyan-500 text-cyan-300'
                    : 'bg-gray-900 border-gray-700 text-gray-400 hover:text-white'
                }`}
              >
                {tr(CAMERA_PURPOSE_LABELS[p])}
              </button>
            );
          })}
        </div>
        {sizeWarning && (
          <div className="text-[10px] text-amber-400">⚠ {sizeWarning}</div>
        )}
        {purpose.length === 1 && purpose[0] === 'operator_view' && (
          <div className="text-[10px] text-gray-500">{tr('tankSettings.monitoringOnlyHint')}</div>
        )}
      </div>

      {/* 등록 전 연결 테스트 */}
      <div className="space-y-2 p-2 rounded border border-cyan-500/20 bg-cyan-500/5">
        <div className="flex items-center justify-between">
          <div className="text-xs text-cyan-300 font-medium">{tr('tankSettings.connTestBeforeRegister')}</div>
          <Button
            type="button" size="sm" variant="outline"
            onClick={() => void runConnectionTest()}
            disabled={testBusy || !host.trim()}
          >
            {testBusy ? tr('tankSettings.testing') : tr('tankSettings.getSnapshot')}
          </Button>
        </div>
        {testMsg && (
          <div className={`text-[11px] font-mono ${testThumbnailURL ? 'text-green-400' : 'text-amber-400'}`}>
            {testMsg}
          </div>
        )}
        {testThumbnailURL && (
          <img
            src={testThumbnailURL}
            alt="snapshot preview"
            className="w-full max-h-32 object-contain rounded border border-cyan-500/30 bg-black"
          />
        )}
        {!host.trim() && (
          <div className="text-[10px] text-gray-500">{tr('tankSettings.enterHostFirst')}</div>
        )}
      </div>

      <p className="text-[10px] text-gray-600">
        {tr('tankSettings.passwordStorageHint')}
      </p>
    </InlineFormShell>
  );
}

// C-12 — 카메라 인스턴스 폼 내부에서 "+ 새 모델" 클릭 시 펼쳐지는 미니 폼.
// AdminRegistry 의 CameraModelCard 와 동일 backend (CameraModels.create) 사용.
// 최소 필드만 노출 — vendor / product_code / display_name / lens_type / (dual 일 때 baseline_mm).
// 등록 완료 시 새 model_id 를 callback 으로 전달 → 부모가 자동 선택.
function InlineCameraModelMiniForm({
  onSaved, onCancel,
}: {
  onSaved: (modelId: string) => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const [vendor, setVendor] = useState('');
  const [productCode, setProductCode] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [lensType, setLensType] = useState<CameraLensType>('single');
  const [baselineMM, setBaselineMM] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // model_id 자동 추천.
  const autoModelId = useMemo(() => {
    const v = vendor.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    const p = productCode.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
    if (!v || !p) return '';
    return `${v}_${p}`;
  }, [vendor, productCode]);

  const canSubmit = vendor.trim() && productCode.trim() && displayName.trim() && autoModelId;

  const submit = async () => {
    if (!canSubmit) return;
    setBusy(true); setErr(null);
    try {
      const body: NewCameraModelBody = {
        model_id: autoModelId,
        vendor: vendor.trim(),
        product_code: productCode.trim(),
        display_name: displayName.trim(),
        lens_type: lensType,
        night_mode: false,
      };
      if (lensType === 'dual' && baselineMM) body.baseline_mm = Number(baselineMM);
      await CameraModels.create(body);
      onSaved(autoModelId);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally { setBusy(false); }
  };

  return (
    <div className="p-3 rounded border border-cyan-500/30 bg-cyan-500/5 space-y-2">
      <div className="flex items-center justify-between">
        <div className="text-xs text-cyan-300 font-medium">{tr('tankSettings.newCameraModelTitle')}</div>
        <button type="button" onClick={onCancel}
          className="text-[10px] text-gray-400 hover:text-white">{tr('tankSettings.cancel')}</button>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <Field label={tr('tankSettings.vendorRequired')} value={vendor} onChange={setVendor} />
        <Field label={tr('tankSettings.productCodeRequired')} value={productCode} onChange={setProductCode} />
      </div>
      <Field label={tr('tankSettings.displayNameRequired')} value={displayName} onChange={setDisplayName} />
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">{tr('tankSettings.lensTypeRequired')}</label>
          <select
            value={lensType}
            onChange={e => setLensType(e.target.value as CameraLensType)}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-cyan-500/50"
          >
            <option value="single">{tr('tankSettings.lensSingle')}</option>
            <option value="dual">{tr('tankSettings.lensDual')}</option>
            <option value="fisheye">{tr('tankSettings.lensFisheye')}</option>
            <option value="ptz">PTZ</option>
            <option value="other">{tr('tankSettings.lensOther')}</option>
          </select>
        </div>
        {lensType === 'dual' && (
          <Field label={tr('tankSettings.baselineMm')} type="number" value={baselineMM} onChange={setBaselineMM} />
        )}
      </div>
      {autoModelId && (
        <div className="text-[10px] text-gray-500">
          model_id ({tr('tankSettings.auto')}): <span className="font-mono text-cyan-300">{autoModelId}</span>
        </div>
      )}
      {err && <div className="text-[11px] text-red-400 font-mono">{err}</div>}
      <Button type="button" size="sm" onClick={() => void submit()}
        disabled={busy || !canSubmit}
      >
        {busy ? tr('tankSettings.registering') : tr('tankSettings.registerModelAutoSelect')}
      </Button>
      <p className="text-[10px] text-gray-500">
        {tr('tankSettings.cameraModelDetailHint')}
      </p>
    </div>
  );
}

// ── 3) 입식 이벤트 (lifecycle) ────────────────────────────────────────────────

function LifecycleEventCard({
  lifecycle,
}: {
  lifecycle: TankLifecycleResponse | null;
}) {
  const { tr } = useLanguage();
  const current = lifecycle?.current;
  const active = current && current.status === 'active';

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/20 rounded-lg p-4 space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-white flex items-center gap-2">
          <Fish className="w-4 h-4 text-green-500" />
          {tr('tankSettings.stockingEvent')}
        </h3>
        <span className={`text-xs px-2 py-0.5 rounded font-mono ${
          active
            ? 'bg-green-500/20 text-green-400 border border-green-500/30'
            : 'bg-gray-700 text-gray-300'
        }`}>
          {active ? tr('tankSettings.activeRunning') : tr('tankSettings.inactive')}
        </span>
      </div>

      {active && current ? (
        <ActiveLifecyclePanel current={current} />
      ) : (
        <p className="text-xs text-gray-500 italic">
          {tr('tankSettings.stockingHarvestHint')}
        </p>
      )}
    </div>
  );
}

function ActiveLifecyclePanel({ current }: { current: NonNullable<TankLifecycleResponse['current']> }) {
  const { tr } = useLanguage();
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
      <InfoRow label={tr('tankSettings.stockingId')} value={current.active_stocking_id} />
      <InfoRow label={tr('tankSettings.species')} value={current.species} />
      <InfoRow label={tr('tankSettings.growthStage')} value={current.growth_stage} />
      <InfoRow label={tr('tankSettings.initialCount')} value={current.initial_count.toLocaleString()} />
      <InfoRow label={tr('tankSettings.initialAvgWeight')} value={`${current.initial_avg_weight_g} g`} />
      <InfoRow label={tr('tankSettings.stockedAt')} value={current.stocked_at} />
      {current.target_harvest_weight_g != null && (
        <InfoRow label={tr('tankSettings.targetHarvestWeight')} value={`${current.target_harvest_weight_g} g`} />
      )}
      {current.target_harvest_date && (
        <InfoRow label={tr('tankSettings.targetHarvestDate')} value={current.target_harvest_date} />
      )}
      {current.source_hatchery && (
        <InfoRow label={tr('tankSettings.sourceHatchery')} value={current.source_hatchery} />
      )}
      {current.lot_no && (
        <InfoRow label={tr('tankSettings.lotNo')} value={current.lot_no} />
      )}
      {current.parent_lot_no && (
        <InfoRow label={tr('tankSettings.parentLotNo')} value={current.parent_lot_no} />
      )}
    </dl>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// NOTE: TreatmentForm, MortalityForm, TransferForm, TraceabilityTimeline,
//       StockingForm, HarvestForm, and cteType* helpers have been moved to
//       src/components/production/ProductionEventLog.tsx
// ─────────────────────────────────────────────────────────────────────────────


// ─────────────────────────────────────────────────────────────────────────────
// InlineWtgForm — Site 단계 inline 등록 (위임 2).
// 사용자 원칙 "각 단계에서 처리": Site selector 옆에서 직접 WTG 추가 가능.
// site_id 는 props 로 자동 매핑, wtg_id 는 기존 WTG 수 + 1 로 자동 생성.
// 동일 패턴: InlineSensorForm / InlineActuatorForm / InlineCameraForm.
// ─────────────────────────────────────────────────────────────────────────────
export function InlineWtgForm({ siteId, onSaved, onCancel }: {
  siteId: string;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const [wtgId, setWtgId] = useState('');
  const [wtgIdOverride, setWtgIdOverride] = useState(false);
  const [name, setName] = useState('');
  const [volumeM3, setVolumeM3] = useState('');
  const [flowRate, setFlowRate] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  // 자동 wtg_id 생성 — 같은 site 의 기존 WTG 수 + 1 (wtg_a, wtg_b, ...).
  useEffect(() => {
    if (wtgIdOverride || !siteId) return;
    (async () => {
      try {
        const r = await WTGs.list(siteId);
        const nextLetter = String.fromCharCode(97 + r.items.length); // 'a','b',...
        setWtgId(`wtg_${nextLetter}`);
      } catch {
        setWtgId('wtg_a');
      }
    })();
  }, [siteId, wtgIdOverride]);

  const canSubmit = !!siteId && !!wtgId.trim() && !!name.trim();

  async function submit() {
    if (!canSubmit) return;
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      const body: NewWTGBody = {
        wtg_id: wtgId.trim(),
        site_id: siteId,
        name: name.trim(),
      };
      if (volumeM3 || flowRate) {
        body.capacity = {};
        if (volumeM3) body.capacity.volume_m3 = Number(volumeM3);
        if (flowRate) body.capacity.flow_rate_m3_per_h = Number(flowRate);
      }
      await WTGs.create(body);
      setOk(tr('tankSettings.wtgRegistered', { id: body.wtg_id }));
      onSaved();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <InlineFormShell
      title={tr('tankSettings.addWtgTitle', { ctx: `site: ${siteId || tr('tankSettings.noneSelected')}` })}
      onSubmit={() => void submit()}
      onCancel={onCancel}
      busy={busy}
      err={err}
      ok={ok}
      canSubmit={canSubmit}
    >
      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-1">
          <label className="text-xs text-gray-400 font-medium">WTG ID *</label>
          <input
            value={wtgId}
            onChange={e => { setWtgId(e.target.value); setWtgIdOverride(true); }}
            placeholder="wtg_a"
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50"
          />
        </div>
        <Field label={tr('tankSettings.nameRequired')} value={name} onChange={setName} />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field label={tr('tankSettings.volumeM3Optional')} type="number" value={volumeM3} onChange={setVolumeM3} />
        <Field label={tr('tankSettings.flowRateOptional')} type="number" value={flowRate} onChange={setFlowRate} />
      </div>
      <p className="text-[11px] text-gray-500">
        {tr('tankSettings.wtgSensorMappingHint')}
      </p>
    </InlineFormShell>
  );
}
