import type {
  Group, Tank, StateVector, WeightHistoryResponse, Camera, AlertOpen,
  Farm, Site, WaterTreatmentGroup,
  Controller, ControllerStatus,
  Actuator, Sensor, SpeciesProfile,
  FeedCycle, NewCycleBody,
  FeedingSchedule, NewScheduleBody,
  OperatorIntent, NewIntentBody, LLMAnalysis,
  EnvironmentalSnapshot,
  Dispute, NewDisputeBody,
  LearnedRule,
  ArbiterDecision, ArbiterPriority,
  SafetyGatesStatus,
  NewFarmBody, NewSiteBody, NewWTGBody, NewTankBody,
  NewSensorBody, NewActuatorBody, NewSpeciesProfileBody, NewCameraBody,
  NewGroupBody,
  NewStockingBody, TankLifecycleResponse,
  NewHarvestBody,
  NewTreatmentBody, NewMortalityBody, NewTransferBody, TankTraceabilityResponse,
  NewFeedingBody,
  CameraModel, NewCameraModelBody,
  SensorModel, NewSensorModelBody,
  ActuatorModel, NewActuatorModelBody,
  VisionAlgorithmsResponse, VisionTankApplicationsResponse,
  VisionObservationsResponse, VisionTrainingStatus,
  VisionTrainingJobsResponse, VisionBootstrapLabelsResponse,
  VisionTrainingStartBody, VisionTrainingJob, VisionBootstrapSaveBody,
  VisionTrainingPoolResponse, CaptureDiskResponse,
  VisionAlgorithmActiveState, VisionAlgorithmApplyResponse,
  VisionObservationDisputeBody, VisionObservationDisputeResponse,
  TraceabilityDoc,
  InventoryListResponse, NewInventoryItemBody, PurchaseBody, ConsumeBody,
  Partner, NewPartnerBody, PartnersListResponse,
  SiteStocking, NewSiteStockingBody, SiteStockingsListResponse,
  SiteHarvest, NewSiteHarvestBody, SiteHarvestsListResponse,
  BroodstockCohort, NewBroodstockBody,
  SpawnBatch, NewSpawnBatchBody,
  LarvalBatch, NewLarvalBatchBody,
  LiveFeedCulture, NewLiveFeedBody,
  HatcheryTreatment, NewHatcheryTreatmentBody,
} from './types';

// 빈 문자열 default — Vite proxy 가 /v1/* 을 8080 으로 forward (vite.config.ts).
// Tauri 환경에서는 VITE_BLUEI_API 로 절대 URL 주입 가능.
const BASE = (import.meta.env['VITE_BLUEI_API'] as string | undefined) ?? '';

export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
    public details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { 'content-type': 'application/json' },
    ...init,
  });
  if (!res.ok) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let body: any = null;
    try { body = await res.json(); } catch { /* ignore parse error */ }
    const err = body?.error ?? {};
    const { code: _c, message: _m, ...rest } = err;
    void _c; void _m;
    throw new ApiError(
      err.code ?? 'HTTP_ERROR',
      err.message ?? res.statusText,
      res.status,
      Object.keys(rest).length > 0 ? (rest as Record<string, unknown>) : undefined,
    );
  }
  return res.json() as Promise<T>;
}

// 운영자 의도적 graceful 종료 (GX10 전원 끄기 직전용).
export const System = {
  shutdown: () => api<{ ok: boolean; message: string }>('/v1/system/shutdown', { method: 'POST' }),
};

// 운영자 가이드 채팅 — bluei-edge-assistant(26B). 무상태이므로 대화 전체(messages)를 매번 전송.
// 백엔드는 SSE 로 토큰을 스트리밍한다 (data: <json>\n\n).
export type AssistantTurn = { role: 'user' | 'assistant'; content: string };

export const Assistant = {
  // chatStream — 스트리밍 응답. onDelta 로 토큰 청크를 받는다.
  chatStream: async (
    messages: AssistantTurn[],
    handlers: { onDelta: (t: string) => void; onContext?: (ctx: string) => void; signal?: AbortSignal },
  ): Promise<void> => {
    const res = await fetch(BASE + '/v1/assistant/chat', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ messages }),
      signal: handlers.signal,
    });
    if (!res.ok || !res.body) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let body: any = null;
      try { body = await res.json(); } catch { /* ignore */ }
      const err = body?.error ?? {};
      throw new ApiError(err.code ?? 'HTTP_ERROR', err.message ?? res.statusText, res.status);
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let idx: number;
      while ((idx = buf.indexOf('\n\n')) >= 0) {
        const line = buf.slice(0, idx).trim();
        buf = buf.slice(idx + 2);
        if (!line.startsWith('data:')) continue;
        const json = line.slice(5).trim();
        if (!json) continue;
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        let evt: any;
        try { evt = JSON.parse(json); } catch { continue; }
        if (evt.error) throw new ApiError('LLM_ERROR', evt.error, 502);
        if (evt.context_ko !== undefined && handlers.onContext) handlers.onContext(evt.context_ko);
        if (evt.delta) handlers.onDelta(evt.delta);
        if (evt.done) return;
      }
    }
  },
};

export const Groups = {
  list: () => api<{ count: number; items: Group[] }>('/v1/groups'),
  tanks: (gid: string) =>
    api<{ count: number; group_id: string; items: Tank[] }>(`/v1/groups/${gid}/tanks`),
  create: (body: NewGroupBody) =>
    api<{ ok: boolean; item: Group }>('/v1/groups', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (gid: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/groups/${encodeURIComponent(gid)}`, {
      method: 'DELETE',
    }),
};

export const Tanks = {
  list: (filters?: { siteId?: string; wtgId?: string }) => {
    const params = new URLSearchParams();
    if (filters?.siteId) params.set('site_id', filters.siteId);
    if (filters?.wtgId) params.set('wtg_id', filters.wtgId);
    const qs = params.toString() ? `?${params.toString()}` : '';
    return api<{ items: Tank[] }>(`/v1/tanks${qs}`);
  },
  stateVector: (tid: string) =>
    api<StateVector>(`/v1/tanks/${encodeURIComponent(tid)}/state-vector`),
  weightHistory: (tid: string, days = 30) =>
    api<WeightHistoryResponse>(`/v1/tanks/${encodeURIComponent(tid)}/weight-history?days=${days}`),
  create: (body: NewTankBody) =>
    api<{ ok: boolean; item: Tank }>('/v1/tanks', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (tid: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/tanks/${encodeURIComponent(tid)}`, {
      method: 'DELETE',
    }),
  // C-9 — 입식 (stocking event 등록). POST /v1/tanks/{id}/stocking.
  // 응답: { ok, sequence, stocking_id, tank_id, species, growth_stage,
  //         initial_count, initial_avg_weight_g, stocked_at }
  stocking: (tid: string, body: NewStockingBody) =>
    api<{ ok: boolean; stocking_id: string; tank_id: string; stocked_at: string }>(
      `/v1/tanks/${encodeURIComponent(tid)}/stocking`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  // GET /v1/tanks/{id}/lifecycle — current + history.
  lifecycle: (tid: string) =>
    api<TankLifecycleResponse>(`/v1/tanks/${encodeURIComponent(tid)}/lifecycle`),
  // POST /v1/tanks/{id}/treatment — 투약 기록.
  treatment: (tid: string, body: NewTreatmentBody) =>
    api<{ ok: boolean; sequence: number; treatment_id: string; tank_id: string; lot_no: string; treatment_type: string; administered_at: string }>(
      `/v1/tanks/${encodeURIComponent(tid)}/treatment`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  // POST /v1/tanks/{id}/mortality — 폐사 기록.
  mortality: (tid: string, body: NewMortalityBody) =>
    api<{ ok: boolean; sequence: number; mortality_id: string; tank_id: string; lot_no: string; dead_count: number; observed_at: string }>(
      `/v1/tanks/${encodeURIComponent(tid)}/mortality`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  // POST /v1/tanks/{id}/transfer — 이동/선별. {id} 는 출발 수조.
  transfer: (tid: string, body: NewTransferBody) =>
    api<{ ok: boolean; sequence: number; transfer_id: string; transfer_type: string; from_tank_id: string; from_lot_no: string; to_tank_id: string; to_stocking_id: string; to_lot_no: string; transferred_at: string }>(
      `/v1/tanks/${encodeURIComponent(tid)}/transfer`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  // GET /v1/tanks/{id}/traceability — CTE 타임라인.
  traceability: (tid: string) =>
    api<TankTraceabilityResponse>(`/v1/tanks/${encodeURIComponent(tid)}/traceability`),
  // POST /v1/tanks/{id}/harvest — 출하 기록. 활성 lifecycle 필요 (없으면 409).
  harvest: (tid: string, body: NewHarvestBody) =>
    api<{ ok: boolean; harvest_id: string; stocking_id: string; tank_id: string; harvested_at: string }>(
      `/v1/tanks/${encodeURIComponent(tid)}/harvest`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
};

// ── 거래처 (파트너) 마스터 ────────────────────────────────────────────────────

export type PartnerDocUploadResponse = {
  ok: boolean;
  document_id: string;
  download_url: string;
};

export const Partners = {
  list: (type?: string, siteId?: string) => {
    const params = new URLSearchParams();
    if (type) params.set('type', type);
    if (siteId) params.set('site_id', siteId);
    const qs = params.toString() ? `?${params.toString()}` : '';
    return api<PartnersListResponse>(`/v1/partners${qs}`);
  },
  create: (body: NewPartnerBody & { site_id?: string }) =>
    api<{ ok: boolean; partner: Partner }>('/v1/partners', {
      method: 'POST', body: JSON.stringify(body),
    }),
  listDocs: (id: string) =>
    api<{ documents: Array<{ document_id: string; doc_type: string; filename: string; download_url: string; uploaded_at: string }>; count: number }>(
      `/v1/partners/${encodeURIComponent(id)}/documents`,
    ),
  uploadDoc: async (id: string, form: FormData): Promise<PartnerDocUploadResponse> => {
    const res = await fetch(BASE + `/v1/partners/${encodeURIComponent(id)}/documents`, {
      method: 'POST',
      body: form,
    });
    if (!res.ok) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let body: any = null;
      try { body = await res.json(); } catch { /* ignore */ }
      const err = body?.error ?? {};
      const { code: _c, message: _m, ...rest } = err;
      void _c; void _m;
      throw new ApiError(
        err.code ?? 'HTTP_ERROR',
        err.message ?? res.statusText,
        res.status,
        Object.keys(rest).length > 0 ? (rest as Record<string, unknown>) : undefined,
      );
    }
    return res.json() as Promise<PartnerDocUploadResponse>;
  },
};

// ── 사료 기록 (POST /v1/feedings) ────────────────────────────────────────────
export const Feedings = {
  record: (body: NewFeedingBody) =>
    api<{ ok: boolean; sequence: number; feeding: Record<string, unknown> }>('/v1/feedings', {
      method: 'POST', body: JSON.stringify(body),
    }),
};

// MJPEG long-lived stream — Vite dev proxy 가 forward 중 끊는 케이스가 있어 절대 URL 사용.
// MJPEG 도 나머지 /v1 호출과 동일한 base(기본=상대경로)를 쓴다. 절대 URL 로 백엔드에
// 직접 가면 dev 의 vite proxy(토큰 주입)·prod 동일출처 인증을 우회해 401 → offline 이 된다.
// <img> 는 Authorization 헤더를 못 보내므로 반드시 인증 경로(프록시/동일출처)를 타야 함.
// 특수 배포에서만 VITE_BLUEI_MJPEG_BASE 로 override.
const MJPEG_BASE =
  (import.meta.env['VITE_BLUEI_MJPEG_BASE'] as string | undefined) ?? BASE;

export interface DiscoveredCamera {
  ip: string;
  vendor?: string;
  model?: string;
  name?: string;
  mac?: string;
  rtsp_port?: number;
  http_port?: number;
  source: string; // 'onvif' | 'scan'
}

export const Cameras = {
  list: (filters?: { tankId?: string }) => {
    const qs = filters?.tankId ? `?tank_id=${encodeURIComponent(filters.tankId)}` : '';
    return api<{ count: number; items: Camera[] }>(`/v1/cameras${qs}`);
  },
  // GET /v1/cameras/discover — ONVIF + 포트스캔으로 LAN 카메라 탐지.
  discover: () => api<{ cameras: DiscoveredCamera[]; count: number }>('/v1/cameras/discover'),
  // POST /v1/cameras (upsert). password 는 별도 setSecret 호출로 저장 — 본 폼에서는 plain password 받지 않음.
  create: (body: NewCameraBody) =>
    api<{ ok: boolean; item: Camera }>('/v1/cameras', {
      method: 'POST', body: JSON.stringify(body),
    }),
  // POST /v1/cameras/{id}/secret — RTSP 연결용 password 를 KV store 에 저장.
  // 등록 직후 setSecret(id, password) 를 같은 흐름에서 호출해야 카메라가 실제로 동작.
  setSecret: (cameraId: string, password: string) =>
    api<{ ok: boolean; password_secret_ref: string }>(
      `/v1/cameras/${encodeURIComponent(cameraId)}/secret`,
      { method: 'POST', body: JSON.stringify({ password }) },
    ),
  // DELETE /v1/cameras/{id}
  delete: (cameraId: string) =>
    api<{ ok: boolean; deleted: string }>(
      `/v1/cameras/${encodeURIComponent(cameraId)}`,
      { method: 'DELETE' },
    ),
  // cacheBust — caller 가 mount 시점에 한 번 결정 (useMemo) 해서 re-render 마다 URL 안 바뀌게.
  mjpegURL: (cameraId: string, profile: 'main' | 'sub' = 'sub', cacheBust?: number) => {
    const t = cacheBust ?? Date.now();
    return `${MJPEG_BASE}/v1/cameras/${encodeURIComponent(cameraId)}/live.mjpeg?profile=${profile}&t=${t}`;
  },
  // 박스 라벨링 — 5/9 ai-training.js 의 loadSnapshot() 이 사용. 즉시 응답 단발 JPEG.
  snapshotURL: (cameraId: string, profile: 'main' | 'sub' = 'sub', cacheBust?: number) => {
    const t = cacheBust ?? Date.now();
    return `${BASE}/v1/cameras/${encodeURIComponent(cameraId)}/snapshot.jpg?profile=${profile}&t=${t}`;
  },
  // C-11 — 등록 전 연결 테스트. host/rtsp_port/username/password 로 snapshot.jpg blob 받음.
  testProfileSnapshot: async (body: {
    camera_id: string;
    tank_id: string;
    vendor?: string;
    host: string;
    rtsp_port?: number;
    http_port?: number;
    username?: string;
    password?: string;
    stream_profiles?: Record<string, unknown>;
  }): Promise<Blob> => {
    const res = await fetch(`${BASE}/v1/cameras/test-profile/snapshot.jpg`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      let msg = res.statusText;
      try {
        const j = await res.json() as { error?: { code?: string; message?: string } };
        msg = j?.error?.message ?? msg;
      } catch { /* ignore */ }
      throw new ApiError('CAMERA_SNAPSHOT_FAILED', msg, res.status);
    }
    return res.blob();
  },
  // C-11 — 등록 후 카메라 연결 테스트. TCP probe + snapshot 시도 결과 JSON.
  test: (cameraId: string) =>
    api<{ camera_id: string; snapshot_ok?: boolean; snapshot_error?: string; tcp?: Record<string, boolean> }>(
      `/v1/cameras/${encodeURIComponent(cameraId)}/test`,
      { method: 'POST', body: '{}' },
    ),
};

// C-11 — 카메라 모델 라이브러리.
// 단일 source of truth — TankSettings 의 InlineCameraForm 과 AdminRegistry CameraModelCard 모두 동일 list 사용.
export const CameraModels = {
  list: () => api<{ count: number; items: CameraModel[] }>('/v1/camera-models'),
  create: (body: NewCameraModelBody) =>
    api<{ ok: boolean; item: CameraModel }>('/v1/camera-models', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (modelId: string) =>
    api<{ ok: boolean; deleted: string }>(
      `/v1/camera-models/${encodeURIComponent(modelId)}`,
      { method: 'DELETE' },
    ),
};

export const Alerts = {
  listOpen: () => api<{ alerts: AlertOpen[]; count: number }>('/v1/alerts/open'),
  // 운영자 명시적 close. dashboard 의 알림 banner × 버튼이 호출.
  close: (alertID: string) =>
    api<{ alert_id: string; dismissed: boolean; reason?: string }>(
      `/v1/alerts/${alertID}/close`,
      { method: 'POST' }
    ),
};

// ── Multi-tank domain API ────────────────────────────────────────────────────

export const Farms = {
  list: () => api<{ items: Farm[] }>('/v1/farms'),
  create: (body: NewFarmBody) =>
    api<{ ok: boolean; item: Farm }>('/v1/farms', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/farms/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
};

export const Sites = {
  list: (farmId?: string) => {
    const qs = farmId ? `?farm_id=${encodeURIComponent(farmId)}` : '';
    return api<{ items: Site[] }>(`/v1/sites${qs}`);
  },
  create: (body: NewSiteBody) =>
    api<{ ok: boolean; item: Site }>('/v1/sites', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/sites/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
};

export const WTGs = {
  list: (siteId?: string) => {
    const qs = siteId ? `?site_id=${encodeURIComponent(siteId)}` : '';
    return api<{ items: WaterTreatmentGroup[] }>(`/v1/water-treatment-groups${qs}`);
  },
  create: (body: NewWTGBody) =>
    api<{ ok: boolean; item: WaterTreatmentGroup }>('/v1/water-treatment-groups', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/water-treatment-groups/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
};

export const Controllers = {
  list: (status?: ControllerStatus) => {
    const qs = status ? `?status=${encodeURIComponent(status)}` : '';
    return api<{ items: Controller[] }>(`/v1/controllers${qs}`);
  },
  register: (body: {
    mac_address: string;
    controller_id: string;
    firmware_version: string;
    tank_id?: string;
    site_id?: string;
  }) =>
    api<{ controller_id: string; status: ControllerStatus; registered_at: string }>(
      '/v1/controllers/register',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  activate: (id: string) =>
    api<{ controller_id: string; status: ControllerStatus }>(
      `/v1/controllers/${encodeURIComponent(id)}/activate`,
      { method: 'POST', body: JSON.stringify({}) },
    ),
  test: (id: string) =>
    api<{ controller_id: string; command_id: string; status: string; message: string }>(
      `/v1/controllers/${encodeURIComponent(id)}/test`,
      { method: 'POST', body: JSON.stringify({}) },
    ),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(
      `/v1/controllers/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
    ),
};

export interface ProvisionStartBody {
  tank_id: string;
  type?: string;
  controller_id?: string;
  port?: string;
  site_id?: string;
  reclaim?: boolean;
  activate?: boolean;
}

export interface ProvisionJob {
  job_id: string;
  status: string; // running | success | flashed_unconfirmed | failed
  exit_code: number;
  controller_id: string;
  tank_id: string;
  log: string;
  started_at: string;
  finished_at?: string;
}

export const Provision = {
  ports: () => api<{ ports: string[] }>('/v1/provision/ports'),
  start: (body: ProvisionStartBody) =>
    api<{ job_id: string; controller_id: string; tank_id: string; status: string }>(
      '/v1/provision',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  status: (jobId: string) =>
    api<ProvisionJob>(`/v1/provision/${encodeURIComponent(jobId)}`),
};

export const Actuators = {
  list: (filters?: { tankId?: string; siteId?: string; wtgId?: string }) => {
    const params = new URLSearchParams();
    if (filters?.tankId) params.set('tank_id', filters.tankId);
    if (filters?.siteId) params.set('site_id', filters.siteId);
    if (filters?.wtgId) params.set('wtg_id', filters.wtgId);
    const qs = params.toString() ? `?${params.toString()}` : '';
    return api<{ items: Actuator[] }>(`/v1/actuators${qs}`);
  },
  create: (body: NewActuatorBody) =>
    api<{ ok: boolean; item: Actuator }>('/v1/actuators', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/actuators/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
};

export const SensorDevices = {
  list: (filters?: { tankId?: string; siteId?: string; wtgId?: string }) => {
    const params = new URLSearchParams();
    if (filters?.tankId) params.set('tank_id', filters.tankId);
    if (filters?.siteId) params.set('site_id', filters.siteId);
    if (filters?.wtgId) params.set('wtg_id', filters.wtgId);
    const qs = params.toString() ? `?${params.toString()}` : '';
    return api<{ items: Sensor[] }>(`/v1/sensors${qs}`);
  },
  create: (body: NewSensorBody) =>
    api<{ ok: boolean; item: Sensor }>('/v1/sensors', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/sensors/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
};

export const SpeciesProfiles = {
  list: () => api<{ items: SpeciesProfile[] }>('/v1/species-profiles'),
  create: (body: NewSpeciesProfileBody) =>
    api<{ ok: boolean; species: string; item: SpeciesProfile }>('/v1/species-profiles', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (key: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/species-profiles/${encodeURIComponent(key)}`, {
      method: 'DELETE',
    }),
};

// ── Feed cycles ───────────────────────────────────────────────────────────────

export const Predictive = {
  forecast: (wtgId: string) =>
    api<{
      wtg_id: string;
      capacity_kg_per_h: number;
      headroom_kg_per_h: number;
      recent_load_kg_per_h: number;
      threshold: number;
      status: 'ok' | 'caution' | 'breach';
    }>(`/v1/predictive/forecast?wtg_id=${encodeURIComponent(wtgId)}`),
};

export const Schedules = {
  list: () => api<{ items: FeedingSchedule[] }>('/v1/feeding-schedules'),
  get: (id: string) =>
    api<{ schedule: FeedingSchedule }>(`/v1/feeding-schedules/${encodeURIComponent(id)}`),
  create: (body: NewScheduleBody) =>
    api<{ schedule: FeedingSchedule }>('/v1/feeding-schedules', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  update: (id: string, body: NewScheduleBody) =>
    api<{ schedule: FeedingSchedule }>(`/v1/feeding-schedules/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  enable: (id: string) =>
    api<{ schedule: FeedingSchedule }>(
      `/v1/feeding-schedules/${encodeURIComponent(id)}/enable`,
      { method: 'POST', body: '{}' },
    ),
  disable: (id: string) =>
    api<{ schedule: FeedingSchedule }>(
      `/v1/feeding-schedules/${encodeURIComponent(id)}/disable`,
      { method: 'POST', body: '{}' },
    ),
  delete: (id: string) =>
    fetch(BASE + `/v1/feeding-schedules/${encodeURIComponent(id)}`, { method: 'DELETE' }),
};

export const FeedCycles = {
  listActive: () =>
    api<{ items: FeedCycle[] }>('/v1/feed-cycles?status=active'),
  listForTank: (tankId: string, limit = 20) =>
    api<{ items: FeedCycle[] }>(
      `/v1/feed-cycles?tank_id=${encodeURIComponent(tankId)}&limit=${limit}`,
    ),
  get: (cycleId: string) =>
    api<FeedCycle>(`/v1/feed-cycles/${encodeURIComponent(cycleId)}`),
  start: (body: NewCycleBody) =>
    api<{ cycle_id: string; status: string; mode: string; tank_id: string }>(
      '/v1/feed-cycles',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  stop: (cycleId: string) =>
    api<{ cycle_id: string; status: string; termination_reason: string }>(
      `/v1/feed-cycles/${encodeURIComponent(cycleId)}/stop`,
      { method: 'POST', body: '{}' },
    ),
};

// ── Environmental safety (C-3w) ───────────────────────────────────────────────

export type EnvironmentalSnapshotBody = {
  site_id: string;
  wind_speed_ms?: number;
  wave_height_m?: number;
  tide_phase?: string;
  tide_minutes_to_low?: number;
  temperature_c?: number;
};

export const Environmental = {
  // GET /v1/environmental/current?site_id=...
  // 404 = 해당 site 에 환경 데이터 없음 (graceful: caller 가 null 처리)
  current: (siteId: string) =>
    api<EnvironmentalSnapshot>(
      `/v1/environmental/current?site_id=${encodeURIComponent(siteId)}`,
    ),
  // GET /v1/environmental/history?site_id=...&limit=N
  // backend 는 hours 가 아닌 limit (최근 N 건) — 24h ≒ 10초 폴링 시 약 8640개,
  // 실제로는 manual + 외부 source 주입 빈도 낮으므로 limit=200 (backend max) 충분.
  history: (siteId: string, limit = 200) =>
    api<{ items: EnvironmentalSnapshot[]; count: number }>(
      `/v1/environmental/history?site_id=${encodeURIComponent(siteId)}&limit=${limit}`,
    ),
  // POST /v1/environmental/snapshot — 본선 시연용 수동 입력
  snapshot: (body: EnvironmentalSnapshotBody) =>
    api<EnvironmentalSnapshot>('/v1/environmental/snapshot', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
};

// ── Operator intents (Phase 5) ────────────────────────────────────────────────

export const OperatorIntents = {
  create: (body: NewIntentBody) =>
    api<{ intent_id: string; llm_analysis?: LLMAnalysis }>('/v1/operator/intents', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  list: (tankId?: string, limit = 50) => {
    const params = new URLSearchParams();
    if (tankId) params.set('tank_id', tankId);
    params.set('limit', String(limit));
    return api<{ items: OperatorIntent[] }>(`/v1/operator/intents?${params.toString()}`);
  },
  apply: (intentId: string, opts?: { force?: boolean }) => {
    const qs = opts?.force ? '?force=true' : '';
    return api<{ intent_id: string; applied_at: string; schedule_refreshed: boolean; forced_by_operator?: boolean }>(
      `/v1/operator/intents/${encodeURIComponent(intentId)}/apply${qs}`,
      { method: 'POST' },
    );
  },
};

// ── Learned safety (Phase 4 C-3l) ─────────────────────────────────────────────
// 운영자 이의제기 → 7일 윈도우 mining → learned rule → 자동 차단 흐름.
// Backend: internal/learned_safety + internal/api/learned_safety.go.

export const Disputes = {
  // POST /v1/operator/disputes — cycle 상세 모달 제출에서 호출.
  create: (body: NewDisputeBody) =>
    api<Dispute>('/v1/operator/disputes', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  // GET /v1/operator/disputes?limit=N
  list: (limit = 50) =>
    api<{ items: Dispute[]; count: number }>(
      `/v1/operator/disputes?limit=${limit}`,
    ),
};

// ── Arbiter decisions (C-4) ──────────────────────────────────────────────────
// 5-G Progressive Autonomy 의 priority/decision 흐름 audit 로그.

export const ArbiterDecisions = {
  // GET /v1/arbiter/decisions?limit=&tank_id=&priority=&since=
  list: (opts?: {
    limit?: number;
    tankId?: string;
    priority?: ArbiterPriority;
    since?: string; // RFC3339
  }) => {
    const params = new URLSearchParams();
    if (opts?.limit) params.set('limit', String(opts.limit));
    if (opts?.tankId) params.set('tank_id', opts.tankId);
    if (opts?.priority) params.set('priority', opts.priority);
    if (opts?.since) params.set('since', opts.since);
    const qs = params.toString() ? `?${params.toString()}` : '';
    return api<{ items: ArbiterDecision[]; count: number }>(
      `/v1/arbiter/decisions${qs}`,
    );
  },
};

// ── Safety gates aggregated status (C-5) ──────────────────────────────────────
// 신규 사이클 시작 시점에 predictive/learned/environmental 3 게이트 상태 한 번에.

export const SafetyGates = {
  // GET /v1/safety-gates/status?tank_id=...
  status: (tankId: string) =>
    api<SafetyGatesStatus>(
      `/v1/safety-gates/status?tank_id=${encodeURIComponent(tankId)}`,
    ),
};

export const LearnedRules = {
  // GET /v1/learned-rules — 전체 (enabled + disabled).
  list: () =>
    api<{ items: LearnedRule[]; count: number }>('/v1/learned-rules'),
  // POST /v1/learned-rules/mine — 운영자 수동 mining 트리거.
  // rules_skipped: 같은 condition_json 의 기존 규칙(enabled+disabled)이 있어 중복 방지로 skip된 수.
  mine: () =>
    api<{
      disputes_checked: number;
      rules_mined: number;
      rules_inserted: number;
      rules_skipped?: number;
    }>(
      '/v1/learned-rules/mine',
      { method: 'POST', body: '{}' },
    ),
  // POST /v1/learned-rules/{id}/disable
  disable: (ruleId: string) =>
    api<{ rule_id: string; enabled: boolean }>(
      `/v1/learned-rules/${encodeURIComponent(ruleId)}/disable`,
      { method: 'POST', body: '{}' },
    ),
  // POST /v1/learned-rules/{id}/enable — C-3l 신규.
  enable: (ruleId: string) =>
    api<{ rule_id: string; enabled: boolean }>(
      `/v1/learned-rules/${encodeURIComponent(ruleId)}/enable`,
      { method: 'POST', body: '{}' },
    ),
};

// ── C-13a 센서 모델 라이브러리 ────────────────────────────────────────────────
// 단일 source of truth — TankSettings / WTGDetail / AdminRegistry 모두 동일 list 사용.
// 카메라 CameraModels 패턴과 평행 구조.

export const SensorModels = {
  list: () => api<{ count: number; items: SensorModel[] }>('/v1/sensor-models'),
  create: (body: NewSensorModelBody) =>
    api<{ ok: boolean; item: SensorModel }>('/v1/sensor-models', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (modelId: string) =>
    api<{ ok: boolean; deleted: string }>(
      `/v1/sensor-models/${encodeURIComponent(modelId)}`,
      { method: 'DELETE' },
    ),
};

// ── C-13b 액추에이터 모델 라이브러리 ──────────────────────────────────────────
// 단일 source of truth — TankSettings / WTGDetail / AdminRegistry 모두 동일 list 사용.
// 카메라 CameraModels / 센서 SensorModels 패턴과 평행 구조.
export const ActuatorModels = {
  list: () => api<{ count: number; items: ActuatorModel[] }>('/v1/actuator-models'),
  create: (body: NewActuatorModelBody) =>
    api<{ ok: boolean; item: ActuatorModel }>('/v1/actuator-models', {
      method: 'POST', body: JSON.stringify(body),
    }),
  delete: (modelId: string) =>
    api<{ ok: boolean; deleted: string }>(
      `/v1/actuator-models/${encodeURIComponent(modelId)}`,
      { method: 'DELETE' },
    ),
};

// ── GDST 서류 첨부 (POST /v1/tanks/{id}/documents) ───────────────────────────
// multipart/form-data — content-type 헤더를 직접 설정하지 않아야 browser 가 boundary 자동 포함.

export type DocumentUploadResponse = {
  ok: boolean;
  document_id: string;
  tank_id: string;
  cte_type: string;
  doc_type: string;
  filename: string;
  size_bytes: number;
  sha256: string;
  download_url: string;
};

// TraceabilityDoc 재노출 (소비자가 api.ts 에서 직접 import 할 수 있게).
export type { TraceabilityDoc };

export const Documents = {
  upload: async (tankId: string, form: FormData): Promise<DocumentUploadResponse> => {
    const res = await fetch(BASE + `/v1/tanks/${encodeURIComponent(tankId)}/documents`, {
      method: 'POST',
      body: form,
      // content-type 헤더 미설정 — browser 가 multipart boundary 자동 추가.
    });
    if (!res.ok) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let body: any = null;
      try { body = await res.json(); } catch { /* ignore */ }
      const err = body?.error ?? {};
      const { code: _c, message: _m, ...rest } = err;
      void _c; void _m;
      throw new ApiError(
        err.code ?? 'HTTP_ERROR',
        err.message ?? res.statusText,
        res.status,
        Object.keys(rest).length > 0 ? (rest as Record<string, unknown>) : undefined,
      );
    }
    return res.json() as Promise<DocumentUploadResponse>;
  },
};

// ── 재고 관리 (Inventory) ─────────────────────────────────────────────────────

export type PurchaseDocUploadResponse = {
  ok: boolean;
  document_id: string;
  download_url: string;
};

export const Inventory = {
  list: (category?: string) =>
    api<InventoryListResponse>(
      '/v1/inventory' + (category ? `?category=${encodeURIComponent(category)}` : ''),
    ),
  createItem: (body: NewInventoryItemBody) =>
    api<{ ok: boolean; item: { item_id: string; name: string; unit: string } }>(
      '/v1/inventory/items',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  purchase: (body: PurchaseBody) =>
    api<{ ok: boolean; purchase_id: string; item_id: string; name: string; on_hand_qty: number; unit: string }>(
      '/v1/inventory/purchase',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  consume: (body: ConsumeBody) =>
    api<{ ok: boolean; item_id: string; on_hand_qty: number }>(
      '/v1/inventory/consume',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  uploadPurchaseDoc: async (purchaseId: string, form: FormData): Promise<PurchaseDocUploadResponse> => {
    const res = await fetch(
      BASE + `/v1/inventory/purchases/${encodeURIComponent(purchaseId)}/documents`,
      { method: 'POST', body: form },
    );
    if (!res.ok) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let body: any = null;
      try { body = await res.json(); } catch { /* ignore */ }
      const err = body?.error ?? {};
      const { code: _c, message: _m, ...rest } = err;
      void _c; void _m;
      throw new ApiError(
        err.code ?? 'HTTP_ERROR',
        err.message ?? res.statusText,
        res.status,
        Object.keys(rest).length > 0 ? (rest as Record<string, unknown>) : undefined,
      );
    }
    return res.json() as Promise<PurchaseDocUploadResponse>;
  },
};

// ── 사이트 단위 입식·출하 거래 ──────────────────────────────────────────────────

export type SiteTradeDocUploadResponse = {
  ok: boolean;
  document_id: string;
  download_url: string;
};

async function siteTradeUpload(url: string, form: FormData): Promise<SiteTradeDocUploadResponse> {
  const res = await fetch(BASE + url, { method: 'POST', body: form });
  if (!res.ok) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let body: any = null;
    try { body = await res.json(); } catch { /* ignore */ }
    const err = body?.error ?? {};
    const { code: _c, message: _m, ...rest } = err;
    void _c; void _m;
    throw new ApiError(
      err.code ?? 'HTTP_ERROR',
      err.message ?? res.statusText,
      res.status,
      Object.keys(rest).length > 0 ? (rest as Record<string, unknown>) : undefined,
    );
  }
  return res.json() as Promise<SiteTradeDocUploadResponse>;
}

export const SiteTrade = {
  listStockings: (siteId: string) =>
    api<SiteStockingsListResponse>(`/v1/site-stockings?site_id=${encodeURIComponent(siteId)}`),
  createStocking: (body: NewSiteStockingBody) =>
    api<{ ok: boolean; site_stocking_id: string; total_count: number; allocations: SiteStocking['allocations'] }>(
      '/v1/site-stockings',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  uploadStockingDoc: (id: string, form: FormData) =>
    siteTradeUpload(`/v1/site-stockings/${encodeURIComponent(id)}/documents`, form),
  listStockingDocs: (id: string) =>
    api<{ documents: Array<{ document_id: string; doc_type: string; filename: string; download_url: string; uploaded_at: string }>; count: number }>(
      `/v1/site-stockings/${encodeURIComponent(id)}/documents`,
    ),
  listHarvests: (siteId: string) =>
    api<SiteHarvestsListResponse>(`/v1/site-harvests?site_id=${encodeURIComponent(siteId)}`),
  createHarvest: (body: NewSiteHarvestBody) =>
    api<{ ok: boolean; site_harvest_id: string; total_count: number; lines: SiteHarvest['lines'] }>(
      '/v1/site-harvests',
      { method: 'POST', body: JSON.stringify(body) },
    ),
  uploadHarvestDoc: (id: string, form: FormData) =>
    siteTradeUpload(`/v1/site-harvests/${encodeURIComponent(id)}/documents`, form),
  listHarvestDocs: (id: string) =>
    api<{ documents: Array<{ document_id: string; doc_type: string; filename: string; download_url: string; uploaded_at: string }>; count: number }>(
      `/v1/site-harvests/${encodeURIComponent(id)}/documents`,
    ),
};

// ── Vision (LRCN / 검출기) — 인공지능 관리 sub-tab 데이터 소스 ───────────────
export const Vision = {
  observations: (filters?: { tankId?: string; limit?: number }) => {
    const params = new URLSearchParams();
    if (filters?.tankId) params.set('tank_id', filters.tankId);
    if (filters?.limit) params.set('limit', String(filters.limit));
    const qs = params.toString() ? `?${params.toString()}` : '';
    return api<VisionObservationsResponse>(`/v1/vision/observations${qs}`);
  },
  algorithms: () => api<VisionAlgorithmsResponse>('/v1/vision/algorithms'),
  tankApplications: () =>
    api<VisionTankApplicationsResponse>('/v1/vision/tank-applications'),
  trainingStatus: () => api<VisionTrainingStatus>('/v1/vision/training/status'),
  trainingJobs: () => api<VisionTrainingJobsResponse>('/v1/vision/training/jobs'),
  bootstrapLabels: () =>
    api<VisionBootstrapLabelsResponse>('/v1/vision/bootstrap/labels'),
  algorithmState: (algorithmId: string) =>
    api<VisionAlgorithmActiveState>(
      `/v1/vision/algorithms/${encodeURIComponent(algorithmId)}/state`,
    ),
  promote: (
    algorithmId: string,
    body: { candidate_path?: string; job_id?: string; operator_id?: string },
  ) =>
    api<VisionAlgorithmApplyResponse>(
      `/v1/vision/algorithms/${encodeURIComponent(algorithmId)}/promote`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  rollback: (algorithmId: string, body: { operator_id?: string }) =>
    api<VisionAlgorithmApplyResponse>(
      `/v1/vision/algorithms/${encodeURIComponent(algorithmId)}/rollback`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  // G-4 cycle 종료 dispute. cycle 완료 직후 자동 modal 에서 호출.
  dispute: (
    observationId: string,
    body: VisionObservationDisputeBody,
  ) =>
    api<VisionObservationDisputeResponse>(
      `/v1/vision/observations/${encodeURIComponent(observationId)}/disputes`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  // [AI 가르치기 시작] — 5/9 ai-training.js 의 startTraining()
  trainingStart: (body: VisionTrainingStartBody) =>
    api<{ ok: boolean; job: VisionTrainingJob }>('/v1/vision/training/jobs', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  // [학습 취소]
  trainingCancel: (jobId: string) =>
    api<{ ok: boolean }>(
      `/v1/vision/training/jobs/${encodeURIComponent(jobId)}/cancel`,
      { method: 'POST', body: JSON.stringify({}) },
    ),
  // [💾 저장] 박스 라벨링 — 5/9 ai-training.js 의 saveBootstrapBoxes()
  saveBootstrapLabel: (body: VisionBootstrapSaveBody) =>
    api<{ ok: boolean; sequence: number }>('/v1/vision/bootstrap/labels', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  // R6.3 — LRCN 지도학습용 영상 풀 랜덤 추출.
  //   phase=feeding  → cycle 활성 중 캡처된 mp4 (사료 반응 평가용)
  //   phase=baseline → cycle 외 시간에 캡처된 mp4 (안정성 평가용)
  trainingPool: (phase: 'feeding' | 'baseline', limit = 10) =>
    api<VisionTrainingPoolResponse>(
      `/v1/vision/training-pool?phase=${phase}&limit=${limit}`,
    ),
  // R15/R16 — captures 디스크 사용량 + retention 정책 상태
  captureDisk: () => api<CaptureDiskResponse>('/v1/capture/disk'),
};
// ───── Hatchery 종묘장 (broodstock·spawn·larval·live-feed·treatment) ─────
export const Broodstock = {
  list: (groupId: string) =>
    api<{ items: BroodstockCohort[] }>(`/v1/broodstock?group_id=${encodeURIComponent(groupId)}`),
  create: (body: NewBroodstockBody) =>
    api<{ ok: boolean; item: BroodstockCohort }>('/v1/broodstock', { method: 'POST', body: JSON.stringify(body) }),
  update: (id: string, body: NewBroodstockBody) =>
    api<{ ok: boolean; item: BroodstockCohort }>(`/v1/broodstock/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/broodstock/${encodeURIComponent(id)}`, { method: 'DELETE' }),
};

export const SpawnBatches = {
  list: (groupId: string) =>
    api<{ items: SpawnBatch[] }>(`/v1/spawn-batches?group_id=${encodeURIComponent(groupId)}`),
  create: (body: NewSpawnBatchBody) =>
    api<{ ok: boolean; item: SpawnBatch }>('/v1/spawn-batches', { method: 'POST', body: JSON.stringify(body) }),
  update: (id: string, body: NewSpawnBatchBody) =>
    api<{ ok: boolean; item: SpawnBatch }>(`/v1/spawn-batches/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/spawn-batches/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  publish: (id: string) =>
    api<{ ok: boolean; item: SpawnBatch; projected: number; lot_code: string }>(`/v1/spawn-batches/${encodeURIComponent(id)}/publish`, { method: 'POST' }),
};

export const LarvalBatches = {
  list: (groupId: string) =>
    api<{ items: LarvalBatch[] }>(`/v1/larval-batches?group_id=${encodeURIComponent(groupId)}`),
  create: (body: NewLarvalBatchBody) =>
    api<{ ok: boolean; item: LarvalBatch }>('/v1/larval-batches', { method: 'POST', body: JSON.stringify(body) }),
  update: (id: string, body: NewLarvalBatchBody) =>
    api<{ ok: boolean; item: LarvalBatch }>(`/v1/larval-batches/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/larval-batches/${encodeURIComponent(id)}`, { method: 'DELETE' }),
};

export const LiveFeed = {
  list: (groupId: string) =>
    api<{ items: LiveFeedCulture[] }>(`/v1/live-feed?group_id=${encodeURIComponent(groupId)}`),
  create: (body: NewLiveFeedBody) =>
    api<{ ok: boolean; item: LiveFeedCulture }>('/v1/live-feed', { method: 'POST', body: JSON.stringify(body) }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/live-feed/${encodeURIComponent(id)}`, { method: 'DELETE' }),
};

export const HatcheryTreatments = {
  list: (groupId: string) =>
    api<{ items: HatcheryTreatment[] }>(`/v1/hatchery-treatments?group_id=${encodeURIComponent(groupId)}`),
  create: (body: NewHatcheryTreatmentBody) =>
    api<{ ok: boolean; item: HatcheryTreatment; lot_code: string }>('/v1/hatchery-treatments', { method: 'POST', body: JSON.stringify(body) }),
  delete: (id: string) =>
    api<{ ok: boolean; deleted: string }>(`/v1/hatchery-treatments/${encodeURIComponent(id)}`, { method: 'DELETE' }),
};
