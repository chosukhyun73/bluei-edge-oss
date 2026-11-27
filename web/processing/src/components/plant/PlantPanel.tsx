import { useCallback, useEffect, useState } from 'react';
import { CheckCircle2, Circle, Plus } from 'lucide-react';
import { ApiError } from '../../lib/api';
import { Plant, Haccp } from '../../lib/plant';
import type {
  PlantSite, PlantProfile, PlantZone, PlantEquipment, PlantSensor, LocalUser, PlantCertificate,
  ComplianceView, ComplianceProfileInfo, ComplianceStatus, HaccpPlan,
} from '../../lib/plant';
import { Field, SelectField } from '../fields';
import { Button } from '../ui/button';
import { useLanguage } from '../../lib/language-core';
import { HaccpPlans } from './HaccpPlanEditor';
import { ProductionPanel } from './ProductionPanel';
import { DocumentsPanel } from './DocumentsPanel';
import { FormsPanel } from './FormsPanel';

// 가공공장 관리 — 최초 설정을 순서대로 진행한다:
// 공장 정보 → 계정 → 구역·설비·센서 → 적용 제도·인증서 → HACCP 계획.
// 각 단계의 완료 여부는 등록된 내용에서 읽는다.

type SectionId = 'profile' | 'users' | 'layout' | 'compliance' | 'plans';
const SECTIONS: SectionId[] = ['profile', 'users', 'layout', 'compliance', 'plans'];

function errText(e: unknown): string {
  if (e instanceof ApiError) return `${e.code}: ${e.message}`;
  return e instanceof Error ? e.message : String(e);
}

interface PlantData {
  profile: PlantProfile; users: LocalUser[]; zones: PlantZone[]; equipment: PlantEquipment[]; sensors: PlantSensor[];
  certificates: PlantCertificate[]; compliance: ComplianceView; publishedPlans: number; plans: HaccpPlan[];
}

async function loadPlant(siteId: string): Promise<PlantData> {
  const [profile, users, zones, equipment, sensors, certificates, compliance, plans] = await Promise.all([
    Plant.profile(siteId), Plant.users(siteId), Plant.zones(siteId), Plant.equipment(siteId), Plant.sensors(siteId),
    Plant.certificates(siteId), Plant.compliance(siteId), Haccp.plans(siteId),
  ]);
  const details = await Promise.all(plans.plans.map(p => Haccp.plan(p.plan_id)));
  return {
    profile: profile.profile, users: users.users, zones: zones.zones, equipment: equipment.equipment,
    sensors: sensors.sensors, certificates: certificates.certificates, compliance,
    publishedPlans: details.filter(d => d.versions.some(v => v.status === 'published')).length,
    plans: details.filter(d => d.versions.some(v => v.status === 'published')).map(d => d.plan),
  };
}

export function PlantPanel() {
  const { tr } = useLanguage();
  const [sites, setSites] = useState<PlantSite[] | null>(null);
  const [siteId, setSiteId] = useState('');
  const [data, setData] = useState<PlantData | null>(null);
  const [section, setSection] = useState<SectionId>('profile');
  const [view, setView] = useState<'setup' | 'production' | 'forms' | 'documents'>('setup');
  const [err, setErr] = useState<string | null>(null);

  const reloadSites = useCallback(async () => {
    try {
      const all = (await Plant.list()).plants;
      setSites(all);
      setSiteId(cur => (all.some(s => s.site_id === cur) ? cur : all[0]?.site_id ?? ''));
    } catch (e) { setErr(errText(e)); }
  }, []);
  useEffect(() => { reloadSites(); }, [reloadSites]);

  const reload = useCallback(async () => {
    if (!siteId) { setData(null); return; }
    try { setData(await loadPlant(siteId)); setErr(null); } catch (e) { setErr(errText(e)); }
  }, [siteId]);
  useEffect(() => { reload(); }, [reload]);

  if (sites === null) return <p className="text-sm text-gray-500">{err ?? tr('plant.loading')}</p>;
  if (sites.length === 0) return <NewPlantForm onCreated={reloadSites} />;

  const done: Record<SectionId, boolean> = data ? {
    profile: data.profile.missing_for_traceability.length === 0,
    users: data.users.some(u => u.active && u.role === 'admin'),
    layout: data.zones.length > 0,
    compliance: data.compliance.profile_ids.length > 0,
    plans: data.publishedPlans > 0,
  } : { profile: false, users: false, layout: false, compliance: false, plans: false };

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-lg text-white font-semibold">{tr('plant.title')}</h2>
        {sites.length > 1 && (
          <select value={siteId} onChange={e => setSiteId(e.target.value)}
            className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white">
            {sites.map(s => <option key={s.site_id} value={s.site_id}>{s.name}</option>)}
          </select>
        )}
        {sites.length === 1 && <span className="text-sm text-gray-400">{sites[0].name}</span>}
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      {sites.find(s => s.site_id === siteId)?.name.includes('시연용') && (
        <p className="text-xs text-amber-200/90 px-3 py-2 rounded border border-amber-500/30 bg-amber-500/5">{tr('plant.demo.hint')}</p>
      )}

      <div className="flex gap-0 border-b border-green-500/20">
        {(['setup', 'production', 'forms', 'documents'] as const).map(v => (
          <button key={v} type="button" onClick={() => setView(v)}
            className={'px-5 py-2.5 text-sm font-medium border-b-2 -mb-px ' + (view === v
              ? 'text-green-400 border-green-500' : 'text-gray-400 border-transparent hover:text-white')}>
            {tr(`plant.view.${v}`)}
          </button>
        ))}
      </div>

      {data && view === 'production' && (
        data.plans.length > 0
          ? <ProductionPanel key={siteId} siteId={siteId} plans={data.plans} users={data.users} zones={data.zones} />
          : <p className="text-sm text-gray-500">{tr('proc.needPlan')}</p>
      )}
      {data && view === 'forms' && <FormsPanel key={siteId} siteId={siteId} />}
      {data && view === 'documents' && <DocumentsPanel key={siteId} siteId={siteId} plans={data.plans} users={data.users} />}

      {view === 'setup' && <>
      {/* 설정 순서 — 카드를 누르면 그 단계로 간다 */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
        {SECTIONS.map((id, i) => (
          <button
            key={id} type="button" onClick={() => setSection(id)}
            className={'flex items-start gap-2 p-3 rounded-lg border text-left ' + (section === id
              ? 'border-green-500/60 bg-green-500/10' : 'border-gray-800 bg-gray-900 hover:border-gray-600')}
          >
            {done[id] ? <CheckCircle2 className="size-5 text-green-400 shrink-0" /> : <Circle className="size-5 text-gray-600 shrink-0" />}
            <span>
              <span className="block text-sm text-white">{i + 1}. {tr(`plant.section.${id}`)}</span>
              <span className="block text-xs text-gray-500">{tr(`plant.section.${id}.hint`)}</span>
            </span>
          </button>
        ))}
      </div>

      {data && (
        <div className="p-4 rounded-xl border border-gray-800 bg-gray-950/40">
          {section === 'profile' && <ProfileSection key={siteId} siteId={siteId} profile={data.profile} onSaved={reload} />}
          {section === 'users' && <UsersSection siteId={siteId} users={data.users} onSaved={reload} />}
          {section === 'layout' && <LayoutSection siteId={siteId} data={data} onSaved={reload} />}
          {section === 'compliance' && <ComplianceSection siteId={siteId} data={data} onSaved={reload} />}
          {section === 'plans' && (
            <HaccpPlans siteId={siteId} onChanged={reload}
              refs={{ zones: data.zones, equipment: data.equipment, sensors: data.sensors, users: data.users }} />
          )}
        </div>
      )}
      </>}
    </div>
  );
}

// ── 공장 등록 ────────────────────────────────────────────────────────────────

function NewPlantForm({ onCreated }: { onCreated: () => void }) {
  const { tr } = useLanguage();
  const [name, setName] = useState('');
  const [address, setAddress] = useState('');
  const [err, setErr] = useState<string | null>(null);

  const submit = async () => {
    if (!name.trim()) { setErr(tr('plant.new.nameRequired')); return; }
    try {
      await Plant.register({ name: name.trim(), address: address.trim() });
      onCreated();
    } catch (e) { setErr(errText(e)); }
  };

  return (
    <div className="max-w-xl p-5 rounded-xl border border-gray-800 space-y-3">
      <h2 className="text-lg text-white font-semibold">{tr('plant.new.title')}</h2>
      <p className="text-sm text-gray-400">{tr('plant.new.hint')}</p>
      <Field label={tr('plant.new.name')} value={name} onChange={setName} />
      <Field label={tr('plant.new.address')} value={address} onChange={setAddress} />
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={submit}><Plus className="size-4" />{tr('plant.new.submit')}</Button>
        <Button variant="outline" onClick={async () => {
          try { await Plant.createDemo(); onCreated(); } catch (e) { setErr(errText(e)); }
        }}>{tr('plant.demo.create')}</Button>
      </div>
      <p className="text-xs text-gray-500">{tr('plant.demo.note')}</p>
    </div>
  );
}

// ── 1. 공장 정보 ─────────────────────────────────────────────────────────────

function ProfileSection({ siteId, profile, onSaved }: { siteId: string; profile: PlantProfile; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [p, setP] = useState(profile);
  const [err, setErr] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const set = (patch: Partial<PlantProfile>) => { setP(prev => ({ ...prev, ...patch })); setSaved(false); };

  const save = async () => {
    try { await Plant.saveProfile(siteId, p); setErr(null); setSaved(true); onSaved(); } catch (e) { setErr(errText(e)); }
  };

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <Field label={tr('plant.profile.businessNo')} value={p.business_no} onChange={v => set({ business_no: v })} />
        <Field label={tr('plant.profile.licenseName')} value={p.license_name} onChange={v => set({ license_name: v })} />
        <Field label={tr('plant.profile.licenseNo')} value={p.license_no} onChange={v => set({ license_no: v })} />
        <Field label={tr('plant.profile.licenseAuthority')} value={p.license_authority} onChange={v => set({ license_authority: v })} />
        <Field label={tr('plant.profile.haccpCertNo')} value={p.haccp_cert_no} onChange={v => set({ haccp_cert_no: v })} />
        <SelectField label={tr('plant.profile.welfare')} value={p.human_welfare_policy}
          onChange={v => set({ human_welfare_policy: v as PlantProfile['human_welfare_policy'] })}
          options={['', 'none', 'internal_policy', 'third_party_audit'].map(v => ({ value: v, label: v ? tr(`plant.enum.${v}`) : '—' }))} />
        <Field label={tr('plant.profile.retention')} type="number" value={String(p.retention_years)} onChange={v => set({ retention_years: Number(v) || 0 })} />
      </div>
      <p className="text-xs text-gray-500">{tr('plant.profile.hint')}</p>
      {profile.missing_for_traceability.length > 0 && (
        <p className="text-xs text-amber-300">
          {tr('plant.profile.missing')}: {profile.missing_for_traceability.map(m => tr(`plant.missing.${m}`)).join(', ')}
        </p>
      )}
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      <div className="flex items-center gap-3">
        <Button size="sm" onClick={save}>{tr('plant.save')}</Button>
        {saved && <span className="text-xs text-green-400">{tr('plant.saved')}</span>}
      </div>
    </div>
  );
}

// ── 2. 계정 ──────────────────────────────────────────────────────────────────

function UsersSection({ siteId, users, onSaved }: { siteId: string; users: LocalUser[]; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [name, setName] = useState('');
  const [role, setRole] = useState('admin');
  const [pin, setPin] = useState('');
  const [err, setErr] = useState<string | null>(null);

  const add = async () => {
    try {
      await Plant.createUser(siteId, { name: name.trim(), role, pin });
      setName(''); setPin(''); setErr(null); onSaved();
    } catch (e) { setErr(errText(e)); }
  };

  return (
    <div className="space-y-3">
      <p className="text-xs text-gray-500">{tr('plant.users.hint')}</p>
      <ul className="space-y-1">
        {users.map(u => (
          <li key={u.user_id} className="text-sm text-gray-200">
            {u.name} <span className="text-xs text-gray-500">· {tr(`plant.role.${u.role}`)}</span>
          </li>
        ))}
        {users.length === 0 && <li className="text-sm text-gray-500">{tr('plant.users.none')}</li>}
      </ul>
      <div className="flex flex-wrap items-end gap-2">
        <Field label={tr('plant.users.name')} value={name} onChange={setName} />
        <SelectField label={tr('plant.users.role')} value={role} onChange={setRole}
          options={['admin', 'reviewer', 'monitor'].map(v => ({ value: v, label: tr(`plant.role.${v}`) }))} />
        <Field label={tr('plant.users.pin')} type="password" value={pin} onChange={setPin} />
        <Button size="sm" onClick={add}><Plus className="size-4" />{tr('plant.add')}</Button>
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
    </div>
  );
}

// ── 3. 구역·설비·센서 ────────────────────────────────────────────────────────

const EQUIPMENT_KINDS = ['cold_room', 'freezer', 'cooker', 'washer', 'metal_detector', 'scale', 'tag_reader', 'other'];
const METRICS = ['temperature', 'core_temperature', 'flow', 'weight', 'count', 'zone_presence'];

function LayoutSection({ siteId, data, onSaved }: { siteId: string; data: PlantData; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [err, setErr] = useState<string | null>(null);
  const [zone, setZone] = useState({ name: '', hygiene_class: 'general', temp_class: 'ambient' });
  const [eq, setEq] = useState({ name: '', kind: 'cold_room', zone_id: '' });
  const [sn, setSn] = useState({ sensor_id: '', metric: 'temperature', unit: '℃', zone_id: '', equipment_id: '', is_mock: true });

  const run = async (fn: () => Promise<unknown>, reset: () => void) => {
    try { await fn(); reset(); setErr(null); onSaved(); } catch (e) { setErr(errText(e)); }
  };
  const none = { value: '', label: '—' };
  const zoneOptions = [none, ...data.zones.map(z => ({ value: z.zone_id, label: z.name }))];
  const zoneName = (id: string) => data.zones.find(z => z.zone_id === id)?.name ?? '';
  const eqName = (id: string) => data.equipment.find(e => e.equipment_id === id)?.name ?? '';

  return (
    <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
      <div className="space-y-2">
        <h4 className="text-sm text-gray-300 font-medium">{tr('plant.zones')} ({data.zones.length})</h4>
        {data.zones.map(z => (
          <div key={z.zone_id} className="text-sm text-gray-200">
            {z.name} <span className="text-xs text-gray-500">· {tr(`plant.enum.${z.hygiene_class}`)} · {tr(`plant.enum.${z.temp_class}`)}</span>
          </div>
        ))}
        <div className="grid grid-cols-3 gap-2 items-end pt-2">
          <Field label={tr('plant.zone.name')} value={zone.name} onChange={v => setZone({ ...zone, name: v })} />
          <SelectField label={tr('plant.zone.hygiene')} value={zone.hygiene_class} onChange={v => setZone({ ...zone, hygiene_class: v })}
            options={['general', 'clean'].map(v => ({ value: v, label: tr(`plant.enum.${v}`) }))} />
          <SelectField label={tr('plant.zone.temp')} value={zone.temp_class} onChange={v => setZone({ ...zone, temp_class: v })}
            options={['ambient', 'chilled', 'frozen'].map(v => ({ value: v, label: tr(`plant.enum.${v}`) }))} />
        </div>
        <Button size="sm" variant="outline" onClick={() => run(() => Plant.saveZone(siteId, zone), () => setZone({ ...zone, name: '' }))}>
          <Plus className="size-4" />{tr('plant.add')}
        </Button>
      </div>

      <div className="space-y-2">
        <h4 className="text-sm text-gray-300 font-medium">{tr('plant.equipmentList')} ({data.equipment.length})</h4>
        {data.equipment.map(e => (
          <div key={e.equipment_id} className="text-sm text-gray-200">
            {e.name} <span className="text-xs text-gray-500">· {tr(`plant.enum.${e.kind}`)}{e.zone_id ? ` · ${zoneName(e.zone_id)}` : ''}</span>
          </div>
        ))}
        <div className="grid grid-cols-3 gap-2 items-end pt-2">
          <Field label={tr('plant.equipment.name')} value={eq.name} onChange={v => setEq({ ...eq, name: v })} />
          <SelectField label={tr('plant.equipment.kind')} value={eq.kind} onChange={v => setEq({ ...eq, kind: v })}
            options={EQUIPMENT_KINDS.map(v => ({ value: v, label: tr(`plant.enum.${v}`) }))} />
          <SelectField label={tr('plant.zone')} value={eq.zone_id} onChange={v => setEq({ ...eq, zone_id: v })} options={zoneOptions} />
        </div>
        <Button size="sm" variant="outline" onClick={() => run(() => Plant.saveEquipment(siteId, eq), () => setEq({ ...eq, name: '' }))}>
          <Plus className="size-4" />{tr('plant.add')}
        </Button>
      </div>

      <div className="space-y-2">
        <h4 className="text-sm text-gray-300 font-medium">{tr('plant.sensors')} ({data.sensors.length})</h4>
        {data.sensors.map(s => (
          <div key={s.sensor_id} className="text-sm text-gray-200">
            <span className="font-mono text-xs">{s.sensor_id}</span>
            <span className="text-xs text-gray-500"> · {s.metric} {s.unit} · {zoneName(s.zone_id) || eqName(s.equipment_id)}{s.is_mock ? ` · ${tr('plant.sensor.mock')}` : ''}</span>
          </div>
        ))}
        <div className="grid grid-cols-3 gap-2 items-end pt-2">
          <Field label={tr('plant.sensor.id')} value={sn.sensor_id} onChange={v => setSn({ ...sn, sensor_id: v })} />
          <SelectField label={tr('plant.sensor.metric')} value={sn.metric} onChange={v => setSn({ ...sn, metric: v })}
            options={METRICS.map(v => ({ value: v, label: v }))} />
          <Field label={tr('plant.limit.unit')} value={sn.unit} onChange={v => setSn({ ...sn, unit: v })} />
          <SelectField label={tr('plant.zone')} value={sn.zone_id} onChange={v => setSn({ ...sn, zone_id: v })} options={zoneOptions} />
          <SelectField label={tr('plant.equipment')} value={sn.equipment_id} onChange={v => setSn({ ...sn, equipment_id: v })}
            options={[none, ...data.equipment.map(e => ({ value: e.equipment_id, label: e.name }))]} />
          <label className="flex items-center gap-1.5 text-xs text-gray-300 h-8">
            <input type="checkbox" checked={sn.is_mock} onChange={e => setSn({ ...sn, is_mock: e.target.checked })} />
            {tr('plant.sensor.mock')}
          </label>
        </div>
        <p className="text-xs text-gray-500">{tr('plant.sensor.hint')}</p>
        <Button size="sm" variant="outline" onClick={() => run(() => Plant.saveSensor(siteId, sn), () => setSn({ ...sn, sensor_id: '' }))}>
          <Plus className="size-4" />{tr('plant.add')}
        </Button>
      </div>
      {err && <p className="xl:col-span-3 text-xs text-red-400 font-mono">{err}</p>}
    </div>
  );
}

// ── 4. 적용 제도와 인증서 ────────────────────────────────────────────────────

const CERT_SCHEMES = ['haccp_kr', 'haccp_kr_export', 'msc_coc', 'asc_coc', 'bap', 'brcgs', 'ifs', 'sqf', 'fssc22000', 'iso22000', 'other'];
const STATUS_CLASS: Record<ComplianceStatus, string> = {
  met: 'bg-green-500/20 text-green-300', unmet: 'bg-red-500/20 text-red-300',
  later: 'bg-gray-700/60 text-gray-300', manual: 'bg-blue-500/20 text-blue-300',
};

function ComplianceSection({ siteId, data, onSaved }: { siteId: string; data: PlantData; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [all, setAll] = useState<ComplianceProfileInfo[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const emptyCert = { scheme: 'haccp_kr', cert_no: '', scope: '', holder_kind: 'own' as 'own' | 'supplier', holder_name: '', valid_from: '', valid_until: '', verified_at: '' };
  const [cert, setCert] = useState(emptyCert);
  useEffect(() => { Plant.complianceProfiles().then(r => setAll(r.profiles)).catch(e => setErr(errText(e))); }, []);

  const chosen = data.compliance.profile_ids;
  const toggle = async (id: string) => {
    try {
      await Plant.setCompliance(siteId, chosen.includes(id) ? chosen.filter(c => c !== id) : [...chosen, id]);
      setErr(null); onSaved();
    } catch (e) { setErr(errText(e)); }
  };
  const addCert = async () => {
    try { await Plant.saveCertificate(siteId, cert); setCert(emptyCert); setErr(null); onSaved(); } catch (e) { setErr(errText(e)); }
  };
  const r = data.compliance.retention;

  return (
    <div className="space-y-5">
      <div>
        <h4 className="text-sm text-gray-300 font-medium mb-2">{tr('plant.compliance.choose')}</h4>
        <div className="flex flex-wrap gap-2">
          {all.map(p => (
            <button key={p.id} type="button" onClick={() => toggle(p.id)} title={`${p.version} · ${p.checked_at}`}
              className={'px-3 py-1.5 text-sm rounded border ' + (chosen.includes(p.id)
                ? 'bg-green-600/20 border-green-500/60 text-green-200' : 'border-gray-700 text-gray-400 hover:text-white')}>
              {p.name}
            </button>
          ))}
        </div>
        {chosen.length > 0 && (
          <p className={'mt-2 text-xs ' + (r.ok ? 'text-gray-500' : 'text-red-300')}>
            {tr('plant.compliance.retention', { set: r.set_years, required: r.required_years })}
          </p>
        )}
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
        {data.compliance.profiles.map(p => (
          <div key={p.id} className="p-3 rounded-lg border border-gray-800">
            <div className="flex items-baseline gap-2 mb-2">
              <span className="text-sm text-white font-medium">{p.name}</span>
              <span className="text-xs text-gray-500">{p.version}</span>
              <span className={'ml-auto text-xs ' + (p.required_unmet ? 'text-red-300' : 'text-green-400')}>
                {tr('plant.compliance.requiredUnmet', { n: p.required_unmet })}
              </span>
            </div>
            <ul className="space-y-1.5">
              {p.items.map(it => (
                <li key={it.id} className="flex items-start gap-2 text-xs">
                  <span className={'shrink-0 px-1.5 py-0.5 rounded ' + STATUS_CLASS[it.status]}>{tr(`plant.cstatus.${it.status}`)}</span>
                  <span className="text-gray-200">
                    {it.title}
                    {it.clause && <span className="text-gray-500"> · {it.clause}</span>}
                    {!it.required && <span className="text-gray-500"> · {tr('plant.compliance.optional')}</span>}
                    {it.note && <span className="block text-gray-500">{it.note}</span>}
                  </span>
                </li>
              ))}
            </ul>
            {p.retention_note && <p className="mt-2 text-xs text-gray-500">{p.retention_note}</p>}
          </div>
        ))}
      </div>

      <div className="space-y-2">
        <h4 className="text-sm text-gray-300 font-medium">{tr('plant.certs')} ({data.certificates.length})</h4>
        {data.certificates.map(c => (
          <div key={c.cert_id} className="text-sm text-gray-200">
            {tr(`plant.scheme.${c.scheme}`)} <span className="font-mono text-xs">{c.cert_no}</span>
            <span className="text-xs text-gray-500">
              {' '}· {c.holder_kind === 'own' ? tr('plant.cert.own') : c.holder_name}
              {c.valid_until ? ` · ~${c.valid_until}` : ''}
            </span>
            {c.expired && <span className="ml-2 text-xs text-red-300">{tr('plant.cert.expired')}</span>}
          </div>
        ))}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2 items-end pt-2">
          <SelectField label={tr('plant.cert.scheme')} value={cert.scheme} onChange={v => setCert({ ...cert, scheme: v })}
            options={CERT_SCHEMES.map(v => ({ value: v, label: tr(`plant.scheme.${v}`) }))} />
          <Field label={tr('plant.cert.no')} value={cert.cert_no} onChange={v => setCert({ ...cert, cert_no: v })} />
          <Field label={tr('plant.cert.scope')} value={cert.scope} onChange={v => setCert({ ...cert, scope: v })} />
          <SelectField label={tr('plant.cert.holder')} value={cert.holder_kind} onChange={v => setCert({ ...cert, holder_kind: v as 'own' | 'supplier' })}
            options={[{ value: 'own', label: tr('plant.cert.own') }, { value: 'supplier', label: tr('plant.cert.supplier') }]} />
          {cert.holder_kind === 'supplier' && (
            <Field label={tr('plant.cert.holderName')} value={cert.holder_name} onChange={v => setCert({ ...cert, holder_name: v })} />
          )}
          <Field label={tr('plant.cert.validFrom')} type="date" value={cert.valid_from} onChange={v => setCert({ ...cert, valid_from: v })} />
          <Field label={tr('plant.cert.validUntil')} type="date" value={cert.valid_until} onChange={v => setCert({ ...cert, valid_until: v })} />
          <Field label={tr('plant.cert.verifiedAt')} type="date" value={cert.verified_at} onChange={v => setCert({ ...cert, verified_at: v })} />
        </div>
        <Button size="sm" variant="outline" onClick={addCert}><Plus className="size-4" />{tr('plant.add')}</Button>
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
    </div>
  );
}
