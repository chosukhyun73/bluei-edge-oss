import { useCallback, useEffect, useState } from 'react';
import { Plus, Trash2, Save } from 'lucide-react';
import { ApiError } from '../../lib/api';
import { Haccp, PlanIssuesError, newPlanId } from '../../lib/plant';
import type {
  HaccpPlan, HaccpTemplate, HaccpVersion, PlanContent, PlanCCP, PlanHazard, PlanIssue, PlanLimit, PlanLink,
  PlantZone, PlantEquipment, PlantSensor, LocalUser,
} from '../../lib/plant';
import { Field, SelectField } from '../fields';
import { Button } from '../ui/button';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../ui/tabs';
import { useLanguage } from '../../lib/language-core';
import { FlowDiagram } from './FlowDiagram';
import type { FlowSelection } from './FlowDiagram';

// HACCP 계획 — 공정도(단계·연결), 위해요소, CCP·한계기준을 등록하고 게시한다.
// 게시된 버전은 고칠 수 없다. 고치려면 개정 사유를 적고 새 초안을 만든다.

export interface PlantRefs {
  zones: PlantZone[]; equipment: PlantEquipment[]; sensors: PlantSensor[]; users: LocalUser[];
}

function errText(e: unknown): string {
  if (e instanceof ApiError) return `${e.code}: ${e.message}`;
  return e instanceof Error ? e.message : String(e);
}

const numStr = (v: number | undefined) => (v == null ? '' : String(v));
const strNum = (v: string): number | undefined => (v.trim() === '' || Number.isNaN(Number(v)) ? undefined : Number(v));

function Check({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-1.5 text-xs text-gray-300 h-8 mt-5">
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      {label}
    </label>
  );
}

function Chips({ label, options, values, onChange }: {
  label: string; options: { value: string; label: string }[]; values: string[]; onChange: (v: string[]) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-gray-400 font-medium">{label}</span>
      <div className="flex flex-wrap gap-1">
        {options.map(o => {
          const on = values.includes(o.value);
          return (
            <button
              key={o.value} type="button"
              onClick={() => onChange(on ? values.filter(v => v !== o.value) : [...values, o.value])}
              className={'px-2 py-1 text-xs rounded border ' + (on
                ? 'bg-green-600/20 border-green-500/60 text-green-200'
                : 'border-gray-700 text-gray-400 hover:border-gray-500')}
            >
              {o.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}

const FLOW_KINDS = ['product', 'ingredient', 'packaging', 'water', 'waste', 'rework'];
const RULE_KINDS = ['instant', 'duration', 'cumulative_exposure', 'count', 'periodic_check'];
const SOURCE_KINDS = ['sensor', 'operator', 'derived'];
const AUTO_LEVELS = ['auto_measure', 'auto_observe', 'auto_record', 'manual'];
const RULE_PARAMS: Record<string, string[]> = {
  duration: ['min_duration_sec'],
  cumulative_exposure: ['threshold', 'max_minutes', 'high_threshold', 'max_minutes_high'],
  periodic_check: ['interval_min'],
};
const PRODUCT_FIELDS = ['name', 'food_type', 'form', 'composition', 'batch_unit', 'spec', 'storage', 'shelf_life',
  'packaging', 'intended_use', 'consumers'] as const;

// ── 계획 목록 ────────────────────────────────────────────────────────────────

export function HaccpPlans({ siteId, refs, onChanged }: { siteId: string; refs: PlantRefs; onChanged: () => void }) {
  const { tr } = useLanguage();
  const [plans, setPlans] = useState<HaccpPlan[]>([]);
  const [templates, setTemplates] = useState<HaccpTemplate[]>([]);
  const [planId, setPlanId] = useState('');
  const [name, setName] = useState('');
  const [templateId, setTemplateId] = useState('');
  const [err, setErr] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      const r = await Haccp.plans(siteId);
      setPlans(r.plans);
      setPlanId(cur => (r.plans.some(p => p.plan_id === cur) ? cur : r.plans[0]?.plan_id ?? ''));
    } catch (e) { setErr(errText(e)); }
  }, [siteId]);

  useEffect(() => {
    reload();
    Haccp.templates().then(r => setTemplates(r.templates)).catch(e => setErr(errText(e)));
  }, [reload]);

  const create = async () => {
    if (!name.trim()) { setErr(tr('plant.plan.nameRequired')); return; }
    try {
      const r = await Haccp.create({ site_id: siteId, name: name.trim(), template_id: templateId });
      setName(''); setErr(null);
      await reload();
      setPlanId(r.plan.plan_id);
    } catch (e) { setErr(errText(e)); }
  };

  const template = templates.find(t => t.id === templateId);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-2">
        {plans.map(p => (
          <button
            key={p.plan_id} type="button" onClick={() => setPlanId(p.plan_id)}
            className={'px-3 py-1.5 text-sm rounded border ' + (p.plan_id === planId
              ? 'bg-green-600/20 border-green-500/60 text-green-200'
              : 'border-gray-700 text-gray-400 hover:text-white')}
          >
            {p.name}
          </button>
        ))}
        <div className="flex items-end gap-2 ml-auto">
          <Field label={tr('plant.plan.newName')} value={name} onChange={setName} />
          <SelectField
            label={tr('plant.plan.template')} value={templateId} onChange={setTemplateId}
            options={[{ value: '', label: tr('plant.plan.templateNone') }, ...templates.map(t => ({ value: t.id, label: t.name }))]}
          />
          <Button size="sm" onClick={create}><Plus className="size-4" />{tr('plant.plan.create')}</Button>
        </div>
      </div>
      {template?.notice && <p className="text-xs text-amber-300/80">{template.notice}</p>}
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      {plans.length === 0 && <p className="text-sm text-gray-500">{tr('plant.plan.none')}</p>}
      {planId && <PlanVersions key={planId} planId={planId} refs={refs} onChanged={onChanged} />}
    </div>
  );
}

function PlanVersions({ planId, refs, onChanged }: { planId: string; refs: PlantRefs; onChanged: () => void }) {
  const { tr } = useLanguage();
  const [versions, setVersions] = useState<HaccpVersion[]>([]);
  const [version, setVersion] = useState<HaccpVersion | null>(null);
  const [reason, setReason] = useState('');
  const [err, setErr] = useState<string | null>(null);

  const open = useCallback(async (versionId: string) => {
    try { setVersion((await Haccp.version(planId, versionId)).version); } catch (e) { setErr(errText(e)); }
  }, [planId]);

  // 초안이 있으면 초안을, 없으면 가장 최근 버전을 연다.
  const reload = useCallback(async (openId?: string) => {
    try {
      const r = await Haccp.plan(planId);
      setVersions(r.versions);
      const pick = openId ?? (r.versions.find(v => v.status === 'draft') ?? r.versions[r.versions.length - 1])?.version_id;
      if (pick) await open(pick);
    } catch (e) { setErr(errText(e)); }
  }, [planId, open]);

  useEffect(() => { reload(); }, [reload]);

  const newDraft = async () => {
    try {
      const r = await Haccp.newDraft(planId, reason.trim());
      setReason(''); setErr(null);
      await reload(r.version.version_id);
    } catch (e) { setErr(errText(e)); }
  };

  const hasDraft = versions.some(v => v.status === 'draft');
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-2">
        {versions.map(v => (
          <button
            key={v.version_id} type="button" onClick={() => open(v.version_id)}
            className={'px-2.5 py-1 text-xs rounded border ' + (v.version_id === version?.version_id
              ? 'border-green-500/60 text-green-200' : 'border-gray-700 text-gray-400')}
          >
            v{v.version_no} · {tr(`plant.status.${v.status}`)}
          </button>
        ))}
        {!hasDraft && versions.length > 0 && (
          <div className="flex items-end gap-2 ml-auto">
            <Field label={tr('plant.plan.changeReason')} value={reason} onChange={setReason} />
            <Button size="sm" variant="outline" onClick={newDraft}>{tr('plant.plan.newDraft')}</Button>
          </div>
        )}
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      {version?.content && (
        <PlanEditor
          key={version.version_id} planId={planId} version={version} refs={refs}
          onSaved={setVersion}
          onPublished={() => { reload(version.version_id); onChanged(); }}
        />
      )}
    </div>
  );
}

// ── 편집기 ───────────────────────────────────────────────────────────────────

function PlanEditor({ planId, version, refs, onSaved, onPublished }: {
  planId: string; version: HaccpVersion; refs: PlantRefs;
  onSaved: (v: HaccpVersion) => void; onPublished: () => void;
}) {
  const { tr } = useLanguage();
  const [content, setContent] = useState<PlanContent>(() => normalize(version.content!));
  const [dirty, setDirty] = useState(false);
  const [issues, setIssues] = useState<PlanIssue[]>(version.publish_issues ?? []);
  const [err, setErr] = useState<string | null>(null);
  const [selected, setSelected] = useState<FlowSelection>(null);
  const [tab, setTab] = useState('flow');
  const readOnly = version.status !== 'draft';

  const edit = (fn: (c: PlanContent) => void) => {
    setContent(prev => { const next = structuredClone(prev); fn(next); return next; });
    setDirty(true);
  };

  const save = async (): Promise<boolean> => {
    try {
      const r = await Haccp.saveDraft(planId, version.version_id, content);
      setIssues(r.version.publish_issues ?? []);
      setDirty(false); setErr(null);
      onSaved(r.version);
      return true;
    } catch (e) {
      if (e instanceof PlanIssuesError) setIssues(e.issues);
      setErr(errText(e));
      return false;
    }
  };

  const steps = content.steps!, links = content.links!, hazards = content.hazards!, ccps = content.ccps!;
  const stepName = (id: string) => steps.find(s => s.step_id === id)?.name || id;
  const stepOptions = steps.map((s, i) => ({ value: s.step_id, label: `${i + 1}. ${s.name}` }));
  const linkLabel = (l: PlanLink) => `${stepName(l.from)} → ${stepName(l.to)}`;
  const linkOptions = links.map(l => ({ value: l.link_id, label: linkLabel(l) }));
  const none = { value: '', label: '—' };
  const enumOptions = (values: string[]) => values.map(v => ({ value: v, label: tr(`plant.enum.${v}`) }));

  const removeStep = (id: string) => edit(c => {
    const gone = new Set(c.links!.filter(l => l.from === id || l.to === id).map(l => l.link_id));
    c.steps = c.steps!.filter(s => s.step_id !== id);
    c.links = c.links!.filter(l => !gone.has(l.link_id));
    c.hazards = c.hazards!.filter(h => h.step_id !== id && !(h.link_id && gone.has(h.link_id)));
    for (const k of c.ccps!) {
      k.step_ids = (k.step_ids ?? []).filter(s => s !== id);
      k.link_ids = (k.link_ids ?? []).filter(l => !gone.has(l));
    }
  });
  const removeLink = (id: string) => edit(c => {
    c.links = c.links!.filter(l => l.link_id !== id);
    c.hazards = c.hazards!.filter(h => h.link_id !== id);
    for (const k of c.ccps!) k.link_ids = (k.link_ids ?? []).filter(l => l !== id);
  });
  // 새 단계는 마지막 단계 뒤에 잇는다.
  const addStep = () => edit(c => {
    const id = newPlanId('step');
    const last = c.steps![c.steps!.length - 1];
    c.steps!.push({ step_id: id, name: '', method: '', zone_id: '', equipment_id: '' });
    if (last) c.links!.push({ link_id: newPlanId('link'), from: last.step_id, to: id, flow_kind: 'product', handover: '' });
  });

  const rowClass = (on: boolean) => 'grid gap-2 items-end p-2 rounded border ' + (on ? 'border-green-500/50 bg-green-500/5' : 'border-gray-800');
  const del = (onClick: () => void) => (
    <Button size="icon" variant="ghost" onClick={onClick} title={tr('plant.delete')}><Trash2 className="size-4 text-gray-500" /></Button>
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3 text-xs">
        <span className={readOnly ? 'text-gray-400' : 'text-amber-300'}>
          v{version.version_no} · {tr(`plant.status.${version.status}`)}
        </span>
        {version.change_reason && <span className="text-gray-500">{tr('plant.plan.changeReason')}: {version.change_reason}</span>}
        {version.content_hash && <span className="text-gray-600 font-mono">{version.content_hash.slice(0, 16)}</span>}
        {version.ccp_changed && <span className="text-amber-300">{tr('plant.plan.ccpChanged')}</span>}
        {!readOnly && (
          <Button size="sm" className="ml-auto" disabled={!dirty} onClick={save}><Save className="size-4" />{tr('plant.save')}</Button>
        )}
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}

      <FlowDiagram content={content} zones={refs.zones} selected={selected} onSelect={s => { setSelected(s); setTab('flow'); }} />

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="flow">{tr('plant.tab.flow')} ({steps.length})</TabsTrigger>
          <TabsTrigger value="product">{tr('plant.tab.product')}</TabsTrigger>
          <TabsTrigger value="hazards">{tr('plant.tab.hazards')} ({hazards.length})</TabsTrigger>
          <TabsTrigger value="ccps">{tr('plant.tab.ccps')} ({ccps.length})</TabsTrigger>
          <TabsTrigger value="publish">
            {tr('plant.tab.publish')}{!readOnly && issues.length > 0 ? ` (${issues.length})` : ''}
          </TabsTrigger>
        </TabsList>

        <fieldset disabled={readOnly} className="min-w-0">
          {/* ── 공정: 단계와 연결 ── */}
          <TabsContent value="flow" className="pt-4 grid grid-cols-1 xl:grid-cols-2 gap-6">
            <div className="space-y-2">
              <h4 className="text-sm text-gray-300 font-medium">{tr('plant.steps')}</h4>
              {steps.map((s, i) => (
                <div key={s.step_id} className={rowClass(selected?.kind === 'step' && selected.id === s.step_id) + ' grid-cols-[1.5rem_2fr_1fr_1fr_auto]'}
                  onFocus={() => setSelected({ kind: 'step', id: s.step_id })}>
                  <span className="text-xs text-gray-500 font-mono pb-2">{i + 1}</span>
                  <Field label={tr('plant.step.name')} value={s.name} onChange={v => edit(c => { c.steps![i].name = v; })} />
                  <SelectField label={tr('plant.zone')} value={s.zone_id} onChange={v => edit(c => { c.steps![i].zone_id = v; })}
                    options={[none, ...refs.zones.map(z => ({ value: z.zone_id, label: z.name }))]} />
                  <SelectField label={tr('plant.equipment')} value={s.equipment_id} onChange={v => edit(c => { c.steps![i].equipment_id = v; })}
                    options={[none, ...refs.equipment.map(e => ({ value: e.equipment_id, label: e.name }))]} />
                  {del(() => removeStep(s.step_id))}
                  <div className="col-start-2 col-span-3">
                    <Field label={tr('plant.step.method')} value={s.method} onChange={v => edit(c => { c.steps![i].method = v; })} />
                  </div>
                </div>
              ))}
              <Button size="sm" variant="outline" onClick={addStep}><Plus className="size-4" />{tr('plant.step.add')}</Button>
            </div>

            <div className="space-y-2">
              <h4 className="text-sm text-gray-300 font-medium">{tr('plant.links')}</h4>
              <p className="text-xs text-gray-500">{tr('plant.links.hint')}</p>
              {links.map((l, i) => (
                <div key={l.link_id} className={rowClass(selected?.kind === 'link' && selected.id === l.link_id) + ' grid-cols-[1fr_1fr_auto]'}
                  onFocus={() => setSelected({ kind: 'link', id: l.link_id })}>
                  <SelectField label={tr('plant.link.from')} value={l.from} onChange={v => edit(c => { c.links![i].from = v; })} options={stepOptions} />
                  <SelectField label={tr('plant.link.to')} value={l.to} onChange={v => edit(c => { c.links![i].to = v; })} options={stepOptions} />
                  {del(() => removeLink(l.link_id))}
                  <div className="col-span-3 grid grid-cols-4 gap-2">
                    <SelectField label={tr('plant.link.kind')} value={l.flow_kind} onChange={v => edit(c => { c.links![i].flow_kind = v; })} options={enumOptions(FLOW_KINDS)} />
                    <Field label={tr('plant.link.maxWait')} type="number" value={numStr(l.max_wait_min)} onChange={v => edit(c => { c.links![i].max_wait_min = strNum(v); })} />
                    <Field label={tr('plant.link.envTemp')} type="number" value={numStr(l.env_temp_max_c)} onChange={v => edit(c => { c.links![i].env_temp_max_c = strNum(v); })} />
                    <Field label={tr('plant.link.handover')} value={l.handover} onChange={v => edit(c => { c.links![i].handover = v; })} />
                  </div>
                </div>
              ))}
              <Button size="sm" variant="outline" disabled={steps.length < 2}
                onClick={() => edit(c => { c.links!.push({ link_id: newPlanId('link'), from: steps[0].step_id, to: steps[1].step_id, flow_kind: 'product', handover: '' }); })}>
                <Plus className="size-4" />{tr('plant.link.add')}
              </Button>
            </div>
          </TabsContent>

          {/* ── 제품설명서 ── */}
          <TabsContent value="product" className="pt-4 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
            {PRODUCT_FIELDS.map(f => (
              <Field key={f} label={tr(`plant.product.${f}`)} value={content.product[f] ?? ''} onChange={v => edit(c => { c.product[f] = v; })} />
            ))}
          </TabsContent>

          {/* ── 위해요소 ── */}
          <TabsContent value="hazards" className="pt-4 space-y-2">
            {hazards.map((h, i) => {
              const set = (fn: (h: PlanHazard) => void) => edit(c => fn(c.hazards![i]));
              return (
                <div key={h.hazard_id} className={rowClass(false) + ' grid-cols-2 md:grid-cols-4 xl:grid-cols-8'}>
                  <div className="col-span-2">
                    <SelectField label={tr('plant.hazard.target')} value={h.step_id ? `s:${h.step_id}` : `l:${h.link_id ?? ''}`}
                      onChange={v => set(x => { x.step_id = v.startsWith('s:') ? v.slice(2) : undefined; x.link_id = v.startsWith('l:') ? v.slice(2) : undefined; })}
                      options={[...stepOptions.map(o => ({ value: `s:${o.value}`, label: o.label })),
                        ...linkOptions.map(o => ({ value: `l:${o.value}`, label: `${tr('plant.link')} ${o.label}` }))]} />
                  </div>
                  <SelectField label={tr('plant.hazard.category')} value={h.category} onChange={v => set(x => { x.category = v as PlanHazard['category']; })}
                    options={(['B', 'C', 'P'] as const).map(v => ({ value: v, label: tr(`plant.enum.hazard${v}`) }))} />
                  <div className="col-span-2"><Field label={tr('plant.hazard.name')} value={h.name} onChange={v => set(x => { x.name = v; })} /></div>
                  <Field label={tr('plant.hazard.severity')} type="number" value={String(h.severity)} onChange={v => set(x => { x.severity = strNum(v) ?? 0; })} />
                  <Field label={tr('plant.hazard.likelihood')} type="number" value={String(h.likelihood)} onChange={v => set(x => { x.likelihood = strNum(v) ?? 0; })} />
                  {del(() => edit(c => {
                    c.hazards!.splice(i, 1);
                    for (const k of c.ccps!) k.hazard_ids = k.hazard_ids.filter(id => id !== h.hazard_id);
                  }))}
                  <div className="col-span-2"><Field label={tr('plant.hazard.cause')} value={h.cause} onChange={v => set(x => { x.cause = v; })} /></div>
                  <div className="col-span-2"><Field label={tr('plant.hazard.control')} value={h.control_measure} onChange={v => set(x => { x.control_measure = v; })} /></div>
                  <div className="col-span-2"><Field label={tr('plant.hazard.source')} value={h.source_ref} onChange={v => set(x => { x.source_ref = v; })} /></div>
                  <Check label={tr('plant.hazard.significant')} checked={h.significant} onChange={v => set(x => { x.significant = v; })} />
                  <Check label={tr('plant.confirmed')} checked={h.confirmed} onChange={v => set(x => { x.confirmed = v; })} />
                </div>
              );
            })}
            <Button size="sm" variant="outline" disabled={steps.length === 0}
              onClick={() => edit(c => { c.hazards!.push({ hazard_id: newPlanId('hz'), step_id: steps[0].step_id, category: 'B', name: '', cause: '',
                severity: 1, likelihood: 1, significant: false, control_measure: '', source_ref: '', confirmed: false }); })}>
              <Plus className="size-4" />{tr('plant.hazard.add')}
            </Button>
          </TabsContent>

          {/* ── CCP 와 한계기준 ── */}
          <TabsContent value="ccps" className="pt-4 space-y-3">
            {ccps.map((k, i) => {
              const set = (fn: (k: PlanCCP) => void) => edit(c => fn(c.ccps![i]));
              const setLimit = (j: number, fn: (l: PlanLimit) => void) => set(x => fn(x.limits[j]));
              return (
                <div key={k.ccp_id} className="p-3 rounded-lg border border-red-500/30 space-y-3">
                  <div className="grid grid-cols-2 md:grid-cols-6 gap-2 items-end">
                    <Field label={tr('plant.ccp.code')} value={k.code} onChange={v => set(x => { x.code = v; })} />
                    <div className="col-span-2"><Field label={tr('plant.ccp.name')} value={k.name} onChange={v => set(x => { x.name = v; })} /></div>
                    <SelectField label={tr('plant.ccp.scope')} value={k.scope_kind} onChange={v => set(x => { x.scope_kind = v as PlanCCP['scope_kind']; })}
                      options={enumOptions(['step', 'link', 'span'])} />
                    <Field label={tr('plant.ccp.outage')} type="number" value={String(k.outage_after_sec || '')} onChange={v => set(x => { x.outage_after_sec = strNum(v) ?? 0; })} />
                    <div className="justify-self-end">{del(() => edit(c => { c.ccps!.splice(i, 1); }))}</div>
                  </div>
                  {k.scope_kind === 'link'
                    ? <Chips label={tr('plant.ccp.links')} options={linkOptions} values={k.link_ids ?? []} onChange={v => set(x => { x.link_ids = v; })} />
                    : <Chips label={tr('plant.ccp.steps')} options={stepOptions} values={k.step_ids ?? []} onChange={v => set(x => { x.step_ids = v; })} />}
                  <Chips label={tr('plant.ccp.hazards')} values={k.hazard_ids} onChange={v => set(x => { x.hazard_ids = v; })}
                    options={hazards.map(h => ({ value: h.hazard_id, label: `${h.category} · ${h.name} (${h.step_id ? stepName(h.step_id) : linkLabel(links.find(l => l.link_id === h.link_id) ?? { link_id: '', from: '', to: '', flow_kind: '', handover: '' })})` }))} />
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                    <SelectField label={tr('plant.ccp.monitor')} value={k.monitor_primary} onChange={v => set(x => { x.monitor_primary = v; })}
                      options={[none, ...refs.users.map(u => ({ value: u.user_id, label: u.name })),
                        ...(k.monitor_primary && !refs.users.some(u => u.user_id === k.monitor_primary) ? [{ value: k.monitor_primary, label: k.monitor_primary }] : [])]} />
                    <SelectField label={tr('plant.ccp.backup')} value={k.monitor_backup} onChange={v => set(x => { x.monitor_backup = v; })}
                      options={[none, ...refs.users.map(u => ({ value: u.user_id, label: u.name }))]} />
                    {k.scope_kind !== 'step' && (
                      <Field label={tr('plant.ccp.exportName')} value={k.export_step_name} onChange={v => set(x => { x.export_step_name = v; })} />
                    )}
                    <div className="md:col-span-3 grid grid-cols-1 md:grid-cols-2 gap-2">
                      <Field label={tr('plant.ccp.corrective')} value={k.corrective_action} onChange={v => set(x => { x.corrective_action = v; })} />
                      <Field label={tr('plant.ccp.verification')} value={k.verification} onChange={v => set(x => { x.verification = v; })} />
                    </div>
                  </div>

                  <h5 className="text-xs text-gray-400 font-medium">{tr('plant.limits')}</h5>
                  {k.limits.map((l, j) => (
                    <div key={l.limit_id} className={rowClass(false) + ' grid-cols-2 md:grid-cols-4 xl:grid-cols-8'}>
                      <Field label={tr('plant.limit.item')} value={l.item} onChange={v => setLimit(j, x => { x.item = v; })} />
                      <SelectField label={tr('plant.limit.rule')} value={l.rule_kind} onChange={v => setLimit(j, x => { x.rule_kind = v; })} options={enumOptions(RULE_KINDS)} />
                      <Field label={tr('plant.limit.min')} type="number" value={numStr(l.min)} onChange={v => setLimit(j, x => { x.min = strNum(v); })} />
                      <Field label={tr('plant.limit.max')} type="number" value={numStr(l.max)} onChange={v => setLimit(j, x => { x.max = strNum(v); })} />
                      <Field label={tr('plant.limit.unit')} value={l.unit} onChange={v => setLimit(j, x => { x.unit = v; })} />
                      <SelectField label={tr('plant.limit.source')} value={l.source_kind} onChange={v => setLimit(j, x => { x.source_kind = v; })} options={enumOptions(SOURCE_KINDS)} />
                      <SelectField label={tr('plant.limit.auto')} value={l.automation_level} onChange={v => setLimit(j, x => { x.automation_level = v; })} options={enumOptions(AUTO_LEVELS)} />
                      {del(() => set(x => { x.limits.splice(j, 1); }))}
                      {(RULE_PARAMS[l.rule_kind] ?? []).map(p => (
                        <Field key={p} label={tr(`plant.param.${p}`)} type="number" value={numStr(l.params?.[p])}
                          onChange={v => setLimit(j, x => {
                            const n = strNum(v);
                            x.params = { ...(x.params ?? {}) };
                            if (n == null) delete x.params[p]; else x.params[p] = n;
                          })} />
                      ))}
                      {l.source_kind !== 'operator' && l.item !== 'wait_time' && (
                        <div className="col-span-2">
                          <SelectField label={tr('plant.limit.sensor')} value={l.sensor_id} onChange={v => setLimit(j, x => { x.sensor_id = v; })}
                            options={[none, ...refs.sensors.map(s => ({ value: s.sensor_id, label: `${s.sensor_id} (${s.metric}${s.is_mock ? ` · ${tr('plant.sensor.mock')}` : ''})` }))]} />
                        </div>
                      )}
                      <Field label={tr('plant.limit.interval')} type="number" value={String(l.sample_interval_sec || '')} onChange={v => setLimit(j, x => { x.sample_interval_sec = strNum(v) ?? 0; })} />
                      <div className="col-span-2"><Field label={tr('plant.limit.basis')} value={l.basis_ref} onChange={v => setLimit(j, x => { x.basis_ref = v; })} /></div>
                      <Check label={tr('plant.confirmed')} checked={l.confirmed} onChange={v => setLimit(j, x => { x.confirmed = v; })} />
                    </div>
                  ))}
                  <Button size="sm" variant="outline"
                    onClick={() => set(x => { x.limits.push({ limit_id: newPlanId('lim'), item: '', rule_kind: 'instant', unit: '', source_kind: 'sensor',
                      sensor_id: '', sample_interval_sec: 60, automation_level: 'auto_record', basis_ref: '', confirmed: false }); })}>
                    <Plus className="size-4" />{tr('plant.limit.add')}
                  </Button>
                </div>
              );
            })}
            <Button size="sm" variant="outline"
              onClick={() => edit(c => { c.ccps!.push({ ccp_id: newPlanId('ccp'), code: `CCP-${c.ccps!.length + 1}`, name: '', scope_kind: 'step', step_ids: [],
                link_ids: [], hazard_ids: [], monitor_primary: '', monitor_backup: '', corrective_action: '', verification: '',
                outage_after_sec: 0, export_step_name: '', limits: [] }); })}>
              <Plus className="size-4" />{tr('plant.ccp.add')}
            </Button>
          </TabsContent>
        </fieldset>

        {/* ── 게시 ── */}
        <TabsContent value="publish" className="pt-4">
          {readOnly ? (
            <p className="text-sm text-gray-400">
              {tr('plant.publish.done', { at: version.published_at ?? '', by: refs.users.find(u => u.user_id === version.published_by)?.name ?? version.published_by })}
            </p>
          ) : (
            <PublishForm planId={planId} versionId={version.version_id} users={refs.users} issues={issues} dirty={dirty}
              save={save} onIssues={setIssues} onPublished={onPublished} />
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

// 서버는 비어 있는 목록을 null 로 준다. 편집하기 쉽게 빈 배열로 맞춘다.
function normalize(c: PlanContent): PlanContent {
  const n = structuredClone(c);
  n.team ??= []; n.steps ??= []; n.links ??= []; n.hazards ??= []; n.ccps ??= [];
  for (const k of n.ccps) { k.limits ??= []; k.hazard_ids ??= []; }
  return n;
}

function PublishForm({ planId, versionId, users, issues, dirty, save, onIssues, onPublished }: {
  planId: string; versionId: string; users: LocalUser[]; issues: PlanIssue[]; dirty: boolean;
  save: () => Promise<boolean>; onIssues: (i: PlanIssue[]) => void; onPublished: () => void;
}) {
  const { tr } = useLanguage();
  const [confirmer, setConfirmer] = useState('');
  const [confirmerPin, setConfirmerPin] = useState('');
  const [publisher, setPublisher] = useState('');
  const [publisherPin, setPublisherPin] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const active = users.filter(u => u.active);
  const opts = (roles: string[]) => [{ value: '', label: '—' },
    ...active.filter(u => roles.includes(u.role)).map(u => ({ value: u.user_id, label: `${u.name} (${tr(`plant.role.${u.role}`)})` }))];

  const publish = async () => {
    setBusy(true); setErr(null);
    try {
      if (dirty && !(await save())) return;
      await Haccp.publish(planId, versionId, {
        onsite_confirmed_by: confirmer, onsite_confirmed_pin: confirmerPin, published_by: publisher, published_pin: publisherPin,
      });
      onPublished();
    } catch (e) {
      if (e instanceof PlanIssuesError && e.issues.length) onIssues(e.issues);
      setErr(errText(e));
    } finally { setBusy(false); }
  };

  return (
    <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
      <div>
        <h4 className="text-sm text-gray-300 font-medium mb-2">{tr('plant.publish.issues', { n: issues.length })}</h4>
        {issues.length === 0
          ? <p className="text-sm text-green-400">{tr('plant.publish.ready')}</p>
          : (
            <ul className="space-y-1 max-h-96 overflow-y-auto">
              {issues.map((it, i) => (
                <li key={i} className="text-xs text-gray-300">
                  <span className="text-gray-500 font-mono mr-2">{it.path}</span>{it.message}
                </li>
              ))}
            </ul>
          )}
      </div>
      <div className="space-y-3">
        <p className="text-xs text-gray-500">{tr('plant.publish.hint')}</p>
        <div className="grid grid-cols-2 gap-2">
          <SelectField label={tr('plant.publish.confirmer')} value={confirmer} onChange={setConfirmer} options={opts(['admin', 'reviewer'])} />
          <Field label="PIN" type="password" value={confirmerPin} onChange={setConfirmerPin} />
          <SelectField label={tr('plant.publish.publisher')} value={publisher} onChange={setPublisher} options={opts(['admin'])} />
          <Field label="PIN" type="password" value={publisherPin} onChange={setPublisherPin} />
        </div>
        {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
        <Button disabled={busy || !confirmer || !publisher} onClick={publish}>{tr('plant.publish.submit')}</Button>
      </div>
    </div>
  );
}
