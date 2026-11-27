// 가공공장(공장관리) — 타입, API, 공정도 배치 계산.
// 백엔드: internal/plantapi (docs/54).
import { api, request, ApiError } from './api';

// ── 공장 구성 ────────────────────────────────────────────────────────────────

export interface PlantProfile {
  site_id: string;
  business_no: string;
  license_name: string;
  license_no: string;
  license_authority: string;
  haccp_cert_no: string;
  human_welfare_policy: '' | 'none' | 'internal_policy' | 'third_party_audit';
  retention_years: number;
  missing_for_traceability: string[];
}

export interface PlantZone { zone_id: string; name: string; hygiene_class: string; temp_class: string }
export interface PlantEquipment { equipment_id: string; zone_id: string; kind: string; name: string }
export interface PlantSensor {
  sensor_id: string; zone_id: string; equipment_id: string; metric: string; unit: string; is_mock: boolean;
}
export interface LocalUser { user_id: string; name: string; role: 'admin' | 'reviewer' | 'monitor'; active: boolean }

export interface PlantCertificate {
  cert_id: string; scheme: string; cert_no: string; scope: string;
  holder_kind: 'own' | 'supplier'; holder_name: string;
  valid_from: string; valid_until: string; verified_at: string; expired: boolean;
}

export type ComplianceStatus = 'met' | 'unmet' | 'later' | 'manual';
export interface ComplianceItem {
  id: string; title: string; clause?: string; required: boolean; note?: string; status: ComplianceStatus;
}
export interface ComplianceProfileInfo { id: string; name: string; version: string; checked_at: string }
export interface ComplianceProfileView extends ComplianceProfileInfo {
  retention_note: string; items: ComplianceItem[]; required_unmet: number;
}
export interface ComplianceView {
  profile_ids: string[];
  profiles: ComplianceProfileView[];
  retention: { set_years: number; required_years: number; ok: boolean };
}

const plantPath = (siteId: string, res: string) => `/v1/plant/${encodeURIComponent(siteId)}/${res}`;
const send = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });

export interface PlantSite { site_id: string; name: string; address?: string }

export const Plant = {
  list: () => api<{ plants: PlantSite[] }>('/v1/plants'),
  register: (body: { name: string; address: string }) => api<{ plant: PlantSite }>('/v1/plants', send('POST', body)),
  createDemo: () => api<{ plant: PlantSite }>('/v1/plants/demo', send('POST', {})),
  readings: (siteId: string) => api<{ sensors: SensorReading[]; now: number }>(plantPath(siteId, 'readings')),
  setMock: (siteId: string, body: { sensor_id: string; mode: 'fixed' | 'outage' | 'normal'; value?: number; minutes?: number }) =>
    api<{ sensors: SensorReading[]; now: number }>(plantPath(siteId, 'readings'), send('POST', body)),
  profile: (siteId: string) => api<{ profile: PlantProfile }>(plantPath(siteId, 'profile')),
  saveProfile: (siteId: string, body: Partial<PlantProfile>) =>
    api<{ profile: PlantProfile }>(plantPath(siteId, 'profile'), send('PUT', body)),
  zones: (siteId: string) => api<{ zones: PlantZone[] }>(plantPath(siteId, 'zones')),
  saveZone: (siteId: string, body: Partial<PlantZone>) => api<unknown>(plantPath(siteId, 'zones'), send('POST', body)),
  equipment: (siteId: string) => api<{ equipment: PlantEquipment[] }>(plantPath(siteId, 'equipment')),
  saveEquipment: (siteId: string, body: Partial<PlantEquipment>) =>
    api<unknown>(plantPath(siteId, 'equipment'), send('POST', body)),
  sensors: (siteId: string) => api<{ sensors: PlantSensor[] }>(plantPath(siteId, 'sensors')),
  saveSensor: (siteId: string, body: Partial<PlantSensor>) =>
    api<unknown>(plantPath(siteId, 'sensors'), send('POST', body)),
  users: (siteId: string) => api<{ users: LocalUser[] }>(plantPath(siteId, 'users')),
  createUser: (siteId: string, body: { name: string; role: string; pin: string }) =>
    api<unknown>(plantPath(siteId, 'users'), send('POST', body)),
  certificates: (siteId: string) => api<{ certificates: PlantCertificate[] }>(plantPath(siteId, 'certificates')),
  saveCertificate: (siteId: string, body: Partial<PlantCertificate>) =>
    api<unknown>(plantPath(siteId, 'certificates'), send('POST', body)),
  compliance: (siteId: string) => api<ComplianceView>(plantPath(siteId, 'compliance')),
  setCompliance: (siteId: string, profileIds: string[]) =>
    api<ComplianceView>(plantPath(siteId, 'compliance'), send('PUT', { profile_ids: profileIds })),
  complianceProfiles: () => api<{ profiles: ComplianceProfileInfo[] }>('/v1/compliance/profiles'),
};

// ── HACCP 계획 ───────────────────────────────────────────────────────────────

export interface PlanProduct {
  name: string; food_type: string; form: string; composition: string; batch_unit: string; spec: string;
  storage: string; shelf_life: string; packaging: string; intended_use: string; consumers: string;
}
export interface PlanStep { step_id: string; name: string; method: string; zone_id: string; equipment_id: string }
export interface PlanLink {
  link_id: string; from: string; to: string; flow_kind: string;
  max_wait_min?: number; env_temp_max_c?: number; handover: string;
}
export interface PlanHazard {
  hazard_id: string; step_id?: string; link_id?: string; category: 'B' | 'C' | 'P'; name: string; cause: string;
  severity: number; likelihood: number; significant: boolean; control_measure: string; source_ref: string;
  confirmed: boolean;
}
export interface PlanLimit {
  limit_id: string; item: string; rule_kind: string; min?: number; max?: number; unit: string;
  params?: Record<string, number>; source_kind: string; sensor_id: string; sample_interval_sec: number;
  automation_level: string; basis_ref: string; confirmed: boolean;
}
export interface PlanCCP {
  ccp_id: string; code: string; name: string; scope_kind: 'step' | 'link' | 'span';
  step_ids?: string[]; link_ids?: string[]; hazard_ids: string[];
  monitor_primary: string; monitor_backup: string; corrective_action: string; verification: string;
  outage_after_sec: number; export_step_name: string; limits: PlanLimit[];
}
export interface PlanContent {
  product: PlanProduct; team: { name: string; position: string; role: string }[] | null;
  steps: PlanStep[] | null; links: PlanLink[] | null; hazards: PlanHazard[] | null; ccps: PlanCCP[] | null;
}
export interface PlanIssue { code: string; path: string; message: string }
export interface HaccpPlan { plan_id: string; site_id: string; product_group: string; name: string }
export interface HaccpVersion {
  version_id: string; plan_id: string; version_no: number; status: 'draft' | 'published' | 'retired';
  template_ref: string; content_hash: string; change_reason: string; ccp_changed: boolean;
  onsite_confirmed_by: string; published_by: string; published_at?: string;
  content?: PlanContent; publish_issues?: PlanIssue[];
}
export interface HaccpTemplate { id: string; name: string; product_group: string; notice: string }

// 계획 저장·게시는 거부될 때 응답에 issues 목록이 실린다. api() 는 error 만 넘기므로 따로 읽는다.
export class PlanIssuesError extends ApiError {
  constructor(code: string, message: string, status: number, public issues: PlanIssue[]) {
    super(code, message, status);
  }
}

async function planWrite(path: string, init: RequestInit): Promise<{ version: HaccpVersion }> {
  const res = await request(path, init);
  if (!res.ok) {
    const body = res.body as { error?: { code?: string; message?: string }; issues?: PlanIssue[] } | null;
    throw new PlanIssuesError(body?.error?.code ?? 'HTTP_ERROR', body?.error?.message ?? `HTTP ${res.status}`,
      res.status, body?.issues ?? []);
  }
  return res.body as { version: HaccpVersion };
}

const planPath = (planId: string) => `/v1/haccp/plans/${encodeURIComponent(planId)}`;

export const Haccp = {
  templates: () => api<{ templates: HaccpTemplate[] }>('/v1/haccp/templates'),
  plans: (siteId: string) => api<{ plans: HaccpPlan[] }>(`/v1/haccp/plans?site_id=${encodeURIComponent(siteId)}`),
  create: (body: { site_id: string; name: string; template_id: string }) =>
    api<{ plan: HaccpPlan; version: HaccpVersion }>('/v1/haccp/plans', send('POST', body)),
  plan: (planId: string) => api<{ plan: HaccpPlan; versions: HaccpVersion[] }>(planPath(planId)),
  version: (planId: string, versionId: string) =>
    api<{ version: HaccpVersion }>(`${planPath(planId)}/versions/${encodeURIComponent(versionId)}`),
  saveDraft: (planId: string, versionId: string, content: PlanContent) =>
    planWrite(`${planPath(planId)}/versions/${encodeURIComponent(versionId)}`, send('PUT', { content })),
  newDraft: (planId: string, changeReason: string) =>
    api<{ version: HaccpVersion }>(`${planPath(planId)}/versions`, send('POST', { change_reason: changeReason })),
  publish: (planId: string, versionId: string, body: {
    onsite_confirmed_by: string; onsite_confirmed_pin: string; published_by: string; published_pin: string;
  }) => planWrite(`${planPath(planId)}/versions/${encodeURIComponent(versionId)}/publish`, send('POST', body)),
};

// ── 생산 실행 ────────────────────────────────────────────────────────────────
// 시각은 UTC unix 밀리초.

export interface SensorReading extends PlantSensor {
  latest?: { value: number; at: number; source: string };
  mock?: { mode: 'fixed' | 'outage'; value: number; until_at: number };
}

export interface LotInput { supplier: string; supplier_lot: string; species: string; qty_kg: number }
export interface Lot {
  lot_id: string; site_id: string; plan_id: string; lot_no: string; status: 'running' | 'closed';
  inputs: LotInput[]; input_kg: number; output_kg: number; yield_pct?: number;
  started_by: string; started_at: number; closed_by: string; closed_at: number; reviewed_by: string; reviewed_at: number;
  deviations?: number; open_deviations?: number;
}
export type LimitStatus = 'pending' | 'ok' | 'in_progress' | 'needs_input' | 'deviation' | 'outage';
export interface LimitResult {
  status: LimitStatus; active: boolean; detail: string; count: number;
  last?: number; min?: number; max?: number; exposure_min: number;
}
export interface LotLimit {
  limit_id: string; item: string; item_label: string; text: string; unit: string; rule_kind: string; source_kind: string; sensor_id: string;
  result: LimitResult;
}
export interface LotCCP {
  ccp_id: string; code: string; name: string; scope_kind: string; status: LimitStatus; active: boolean;
  corrective_action: string; limits: LotLimit[];
}
export interface LotDeviation {
  dev_id: string; ccp_id: string; ccp_code: string; limit_id: string; kind: 'deviation' | 'outage';
  detected_at: number; detail: string; status: 'open' | 'closed';
  corrective_action: string; disposition: string; closed_by: string; closed_at: number;
}
export interface LotDetail {
  lot: Lot;
  plan: { plan_id: string; name: string; version_no: number; content_hash: string };
  content: PlanContent;
  position: { step_id: string; link_id: string; step_name: string; link_to_name: string };
  next: { link_id: string; to: string; to_name: string; tracked: boolean }[];
  timeline: { step_id: string; name: string; entered_at: number; exited_at?: number }[];
  ccps: LotCCP[]; deviations: LotDeviation[];
  checks: { limit_id: string; value: number; by: string; at: number }[];
  now: number;
}
export interface Signature { by: string; pin: string }

const lotPath = (lotId: string) => `/v1/process/lots/${encodeURIComponent(lotId)}`;

export const Process = {
  lots: (siteId: string) => api<{ lots: Lot[] }>(`/v1/process/lots?site_id=${encodeURIComponent(siteId)}`),
  start: (body: { site_id: string; plan_id: string; lot_no: string; inputs: LotInput[]; started_by: string; pin: string }) =>
    api<{ lot: Lot }>('/v1/process/lots', send('POST', body)),
  detail: (lotId: string) => api<LotDetail>(lotPath(lotId)),
  move: (lotId: string, action: 'exit' | 'enter' | 'advance', linkId?: string) =>
    api<LotDetail>(`${lotPath(lotId)}/move`, send('POST', { action, link_id: linkId ?? '' })),
  check: (lotId: string, limitId: string, value: number, sig: Signature) =>
    api<LotDetail>(`${lotPath(lotId)}/checks`, send('POST', { limit_id: limitId, value, ...sig })),
  closeDeviation: (lotId: string, devId: string, body: { corrective_action: string; disposition: string }, sig: Signature) =>
    api<LotDetail>(`${lotPath(lotId)}/deviations/${encodeURIComponent(devId)}/close`, send('POST', { ...body, ...sig })),
  close: (lotId: string, outputKg: number, sig: Signature) =>
    api<LotDetail>(`${lotPath(lotId)}/close`, send('POST', { output_kg: outputKg, ...sig })),
  review: (lotId: string, sig: Signature) => api<LotDetail>(`${lotPath(lotId)}/review`, send('POST', sig)),
};

// ── 문서 ─────────────────────────────────────────────────────────────────────

export interface IssuedDocument {
  doc_id: string; kind: 'plan' | 'lot'; ref_id: string; title: string; sha256: string; data_hash: string;
  issued_by_name: string; issued_at: number; intact: boolean;
}

export const Documents = {
  list: (siteId: string) => api<{ documents: IssuedDocument[] }>(`/v1/documents?site_id=${encodeURIComponent(siteId)}`),
  issue: (kind: 'plan' | 'lot', refId: string, sig: Signature) =>
    api<{ document: IssuedDocument; reissued?: boolean }>('/v1/documents', send('POST', { kind, ref_id: refId, ...sig })),
  fileUrl: (docId: string) => `/v1/documents/${encodeURIComponent(docId)}/file`,
  dataUrl: (docId: string) => `/v1/documents/${encodeURIComponent(docId)}/data`,
};

// 기록 양식 — 기본 양식 묶음에서 골라 받아 공장 판으로 쓴다.
export interface FormSummary {
  form_id: string; name: string; kind: 'checklist' | 'log' | 'sheet'; cycle: string; owner: string; covers: string[];
  revision: number; source: 'default' | 'import' | 'edit';
}
export interface FormPack { id: string; name: string; forms: { id: string; name: string; kind: FormSummary['kind']; cycle: string }[] }

export const Forms = {
  defaults: () => api<{ packs: FormPack[] }>('/v1/forms/defaults'),
  list: (siteId: string) => api<{ forms: FormSummary[] }>(`/v1/plant/${encodeURIComponent(siteId)}/forms`),
  adopt: (siteId: string, pack: string, formIds: string[]) =>
    api<{ adopted: string[]; skipped: string[] }>(`/v1/plant/${encodeURIComponent(siteId)}/forms`, send('POST', { pack, form_ids: formIds })),
  blankUrl: (siteId: string, formId: string) => `/v1/plant/${encodeURIComponent(siteId)}/forms/${encodeURIComponent(formId)}/blank.pdf`,
};

export function fmtTime(ms: number): string {
  if (!ms) return '';
  return new Date(ms).toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

export function newPlanId(prefix: string): string {
  return `${prefix}_${Math.random().toString(36).slice(2, 8)}`;
}

// ── 공정도 배치 ──────────────────────────────────────────────────────────────
// 단계를 흐름 순서(앞 단계에서 가장 먼 거리)대로 왼쪽에서 오른쪽으로 놓고, 폭이 모자라면 다음 줄로 넘긴다.
// 같은 순서에 놓이는 단계(갈라지는 흐름)는 위아래로 쌓는다.

export const FLOW_CARD_W = 172;
export const FLOW_CARD_H = 76;
const GAP_X = 64;
const GAP_Y = 14;
const ROW_GAP = 46;

export interface FlowNode { id: string; x: number; y: number; row: number; col: number }
export interface FlowEdge { id: string; d: string; labelX: number; labelY: number; rework: boolean }
export interface FlowLayout { nodes: FlowNode[]; edges: FlowEdge[]; width: number; height: number }

export function layoutFlow(steps: PlanStep[], links: PlanLink[], containerWidth: number): FlowLayout {
  const cols = Math.max(2, Math.floor((containerWidth + GAP_X) / (FLOW_CARD_W + GAP_X)));
  const index = new Map(steps.map((s, i) => [s.step_id, i]));
  const layer = steps.map(() => 0);
  // 재작업(되돌아가는 흐름)은 순서 계산에서 뺀다. 그래도 순환이 남을 수 있어 반복 횟수를 단계 수로 막는다.
  const forward = links.filter(l => l.flow_kind !== 'rework' && index.has(l.from) && index.has(l.to));
  for (let pass = 0; pass < steps.length; pass++) {
    let changed = false;
    for (const l of forward) {
      const a = index.get(l.from)!, b = index.get(l.to)!;
      if (layer[b] < layer[a] + 1 && layer[a] + 1 < steps.length) { layer[b] = layer[a] + 1; changed = true; }
    }
    if (!changed) break;
  }
  // 중간에 합류하는 흐름(포장재·부재료처럼 앞 단계가 없는 단계)은 합류하는 단계 바로 앞에 놓는다.
  const hasIncoming = new Set(forward.map(l => l.to));
  steps.forEach((s, i) => {
    if (i === 0 || hasIncoming.has(s.step_id)) return;
    const next = forward.filter(l => l.from === s.step_id).map(l => layer[index.get(l.to)!]);
    if (next.length) layer[i] = Math.max(0, Math.min(...next) - 1);
  });

  const stackOf = new Map<number, number>(); // layer → 지금까지 쌓인 수
  const placed = steps.map((s, i) => {
    const stack = stackOf.get(layer[i]) ?? 0;
    stackOf.set(layer[i], stack + 1);
    return { id: s.step_id, row: Math.floor(layer[i] / cols), col: layer[i] % cols, stack };
  });
  const rowCount = placed.reduce((m, p) => Math.max(m, p.row + 1), 0);
  const rowStacks = Array.from({ length: rowCount }, () => 1);
  for (const p of placed) rowStacks[p.row] = Math.max(rowStacks[p.row], p.stack + 1);
  const rowTop: number[] = [];
  let y = 0;
  for (let r = 0; r < rowCount; r++) {
    rowTop.push(y);
    y += rowStacks[r] * (FLOW_CARD_H + GAP_Y) - GAP_Y + ROW_GAP;
  }
  const rowBottom = (r: number) => rowTop[r] + rowStacks[r] * (FLOW_CARD_H + GAP_Y) - GAP_Y;

  const nodes: FlowNode[] = placed.map(p => ({
    id: p.id, row: p.row, col: p.col,
    x: p.col * (FLOW_CARD_W + GAP_X), y: rowTop[p.row] + p.stack * (FLOW_CARD_H + GAP_Y),
  }));
  const nodeOf = new Map(nodes.map(n => [n.id, n]));

  const edges: FlowEdge[] = [];
  for (const l of links) {
    const a = nodeOf.get(l.from), b = nodeOf.get(l.to);
    if (!a || !b) continue;
    const rework = l.flow_kind === 'rework';
    if (a.row === b.row && b.col === a.col + 1) {
      const x1 = a.x + FLOW_CARD_W, y1 = a.y + FLOW_CARD_H / 2;
      const x2 = b.x, y2 = b.y + FLOW_CARD_H / 2;
      const mx = (x1 + x2) / 2;
      edges.push({ id: l.link_id, rework, d: `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`,
        labelX: mx, labelY: (y1 + y2) / 2 });
    } else {
      // 줄이 바뀌거나 되돌아가는 연결: 아래 통로로 내려가 옆으로 간 뒤 대상에 붙는다.
      const x1 = a.x + FLOW_CARD_W / 2, y1 = a.y + FLOW_CARD_H;
      const lane = rowBottom(a.row) + ROW_GAP / 2;
      const x2 = b.x + FLOW_CARD_W / 2;
      const y2 = b.row > a.row ? b.y : b.y + FLOW_CARD_H;
      edges.push({ id: l.link_id, rework, d: `M${x1},${y1} L${x1},${lane} L${x2},${lane} L${x2},${y2}`,
        labelX: (x1 + x2) / 2, labelY: lane });
    }
  }
  return {
    nodes, edges,
    width: Math.max(...nodes.map(n => n.x + FLOW_CARD_W), 0),
    height: rowCount ? rowBottom(rowCount - 1) + ROW_GAP / 2 : 0,
  };
}
