import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  Fish, Droplets, Wind, Thermometer, Filter as FilterIcon, Activity, Lightbulb,
  Camera as CameraIcon, Cpu, Waves, AlertTriangle, CheckCircle2, XCircle, HelpCircle,
  Snowflake, PowerOff,
} from 'lucide-react';
import type { StateVector, Tank } from '../lib/types';
import { relativeTime } from '../lib/format';
import { api, Chiller } from '../lib/api';
import { useLanguage } from '../lib/language-core';

// 냉각기 수동 ON/OFF (AI 자동제어와 별개). ⚠ 확인 후 실제 장비 작동.
function ChillerControls() {
  const { tr } = useLanguage();
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const send = async (on: boolean) => {
    const confirmMsg = on
      ? tr('equipmentGrid.chillerOnConfirm')
      : tr('equipmentGrid.chillerOffConfirm');
    if (!window.confirm(confirmMsg)) return;
    setBusy(true);
    setMsg(null);
    try {
      await Chiller.control(on);
      setMsg(on ? tr('equipmentGrid.onSent') : tr('equipmentGrid.offSent'));
    } catch (e) {
      setMsg(tr('equipmentGrid.sendFailed', { msg: e instanceof Error ? e.message : '' }));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col items-center gap-0.5 mt-1" onClick={e => e.stopPropagation()}>
      <div className="flex gap-1">
        <button disabled={busy} onClick={() => send(true)}
          className="px-2 py-0.5 text-[10px] rounded bg-green-700 hover:bg-green-600 disabled:opacity-50 text-white">ON</button>
        <button disabled={busy} onClick={() => send(false)}
          className="px-2 py-0.5 text-[10px] rounded bg-red-700 hover:bg-red-600 disabled:opacity-50 text-white">OFF</button>
      </div>
      <div className="text-[8px] text-gray-600">{tr('equipmentGrid.manualControl')}</div>
      {msg && <div className="text-[8px] text-gray-400">{msg}</div>}
    </div>
  );
}

// device_type → SCADA 아이콘/색/한글 라벨 매핑.
// status (online/offline/unknown/error) 와 별도로, type 별 형상은 고정.
// group: 사용자 요청 — 급이기만 따로 보고 나머지는 전부 '기타'.
// label/group 은 i18n 키. group 은 표시 라벨이자 그룹 분류 키이므로 안정적 식별자
// ('feeder'/'other') 로 두고, 화면에는 GROUP_LABEL_KEY 로 번역해 표시한다.
const DEVICE_VIS: Record<
  string,
  { icon: typeof Fish; label: string; group: string; activePulse?: boolean }
> = {
  feeder:    { icon: Fish,        label: 'equipmentGrid.deviceFeeder',     group: 'feeder', activePulse: true },
  pump:      { icon: Droplets,    label: 'equipmentGrid.devicePump',       group: 'other',  activePulse: true },
  aerator:   { icon: Wind,        label: 'equipmentGrid.deviceAerator',    group: 'other',  activePulse: true },
  heater:    { icon: Thermometer, label: 'equipmentGrid.deviceHeater',     group: 'other' },
  filter:    { icon: FilterIcon,  label: 'equipmentGrid.deviceFilter',     group: 'other' },
  probe:     { icon: Activity,    label: 'equipmentGrid.deviceProbe',       group: 'other' },
  sensor:    { icon: Activity,    label: 'equipmentGrid.deviceSensor',     group: 'other' },
  light:     { icon: Lightbulb,   label: 'equipmentGrid.deviceLight',       group: 'other' },
  camera:    { icon: CameraIcon,  label: 'equipmentGrid.deviceCamera',     group: 'other' },
  uv:        { icon: Cpu,         label: 'equipmentGrid.deviceUv',         group: 'other' },
  skimmer:   { icon: Waves,       label: 'equipmentGrid.deviceSkimmer',    group: 'other' },
  feeder_controller: { icon: Cpu, label: 'equipmentGrid.deviceController', group: 'other' },
  chiller:   { icon: Snowflake,   label: 'equipmentGrid.deviceChiller',    group: 'other', activePulse: true },
};

// 그룹 식별자 → 표시 라벨 i18n 키.
const GROUP_LABEL_KEY: Record<string, string> = {
  feeder: 'equipmentGrid.groupFeeder',
  other: 'equipmentGrid.groupOther',
};

// 급이기 카드 1Hz 폴링 — UDP push 주기와 일치. 더 빠르면 HW 평균화 시간(~400ms) 제약으로 의미 없음.
const FEEDER_POLL_MS = 1000;

// last_seen_at 기준 stale 임계 — polling 주기 3초 × 안전 마진 10 = 30초.
// 30초 이상 갱신 없으면 backend 에 status=active 라도 실제 통신 안 되는 상태.
const STALE_THRESHOLD_MS = 30 * 1000;

// 장비별 stale 임계(ms) — device details.stale_after_s 있으면 사용, 없으면 기본 30s.
function staleMsOf(dev: { details?: Record<string, any> }): number {
  const s = Number(dev.details?.stale_after_s);
  return s > 0 ? s * 1000 : STALE_THRESHOLD_MS;
}

// status + last_seen_at → 색 + 표시 텍스트 + 작동 중 판단.
// last_seen_at 이 STALE_THRESHOLD_MS 이상 지났으면 status 무시하고 'offline (stale)' 강등.
function statusVisual(status: string, lastSeenAt?: string, staleAfterMs: number = STALE_THRESHOLD_MS): {
  color: string; bgColor: string; ringColor: string; label: string; badge: typeof CheckCircle2;
  active: boolean;
} {
  // Stale check 우선. backend status 가 active 여도 통신 끊겼으면 offline.
  // staleAfterMs 는 장비별 임계 — 외부 폴링 장비(냉각기 등)는 갱신주기가 길어 더 크게 준다.
  if (lastSeenAt) {
    const ageMs = Date.now() - new Date(lastSeenAt).getTime();
    if (ageMs > staleAfterMs) {
      return { color: 'text-gray-500', bgColor: 'bg-gray-700/30', ringColor: 'ring-gray-600/40',
        label: 'equipmentGrid.statusOffline', badge: XCircle, active: false };
    }
  }
  const s = (status || '').toLowerCase();
  if (s === 'online' || s === 'active' || s === 'running' || s === 'verified' || s === 'ok') {
    return { color: 'text-green-400', bgColor: 'bg-green-500/10', ringColor: 'ring-green-500/40',
      label: 'equipmentGrid.statusOk', badge: CheckCircle2, active: true };
  }
  if (s === 'warning' || s === 'caution' || s === 'degraded') {
    return { color: 'text-yellow-400', bgColor: 'bg-yellow-500/10', ringColor: 'ring-yellow-500/40',
      label: 'equipmentGrid.statusWarning', badge: AlertTriangle, active: false };
  }
  if (s === 'error' || s === 'fault' || s === 'offline') {
    return { color: 'text-red-400', bgColor: 'bg-red-500/10', ringColor: 'ring-red-500/40',
      label: 'equipmentGrid.statusError', badge: XCircle, active: false };
  }
  return { color: 'text-gray-500', bgColor: 'bg-gray-700/20', ringColor: 'ring-gray-600/40',
    label: 'equipmentGrid.statusUnknown', badge: HelpCircle, active: false };
}

interface Props {
  tanks: Tank[];
  stateVectors: Map<string, StateVector>;
  // 제공 시: 요약바는 전체너비 위, 아래에 [디바이스 1/3 | sideContent 2/3] 로 배치.
  sideContent?: ReactNode;
  // 제공 시: 냉각기 카드 오른쪽 셀에 자동 수온제어 판단 패널 표시.
  tempControlPanel?: ReactNode;
}

type DeviceRow = {
  device_id: string;
  device_type: string;
  status: string;
  last_seen_at: string;
  tank_id: string;
  tank_display_name: string;
  details?: Record<string, any>;
};

export function EquipmentGrid({ tanks, stateVectors, sideContent, tempControlPanel }: Props) {
  const { tr } = useLanguage();
  const allDevices: DeviceRow[] = useMemo(() => {
    const list: DeviceRow[] = [];
    for (const tank of tanks) {
      const sv = stateVectors.get(tank.tank_id);
      // 방어적 정규화 — sv.equipment.devices 가 null/undefined 일 수 있음.
      const devices = Array.isArray(sv?.equipment?.devices) ? sv!.equipment.devices : [];
      for (const dev of devices) {
        list.push({
          device_id: dev.device_id,
          device_type: dev.device_type,
          status: dev.status,
          last_seen_at: dev.last_seen_at,
          details: (dev as any).details,
          tank_id: tank.tank_id,
          tank_display_name: tank.display_name,
        });
      }
    }
    return list;
  }, [tanks, stateVectors]);

  if (allDevices.length === 0) {
    return (
      <div className="py-4 text-sm text-gray-500 italic text-center">
        {tr('equipmentGrid.noData')}
      </div>
    );
  }

  // 상위 그룹 (먹이/수질, 환경, 센서, 관측, 제어, 기타) 으로 분류.
  const byGroup = new Map<string, DeviceRow[]>();
  for (const dev of allDevices) {
    const vis = DEVICE_VIS[dev.device_type];
    const g = vis?.group ?? 'other';
    if (!byGroup.has(g)) byGroup.set(g, []);
    byGroup.get(g)!.push(dev);
  }

  const groupOrder = ['feeder', 'other'];
  const sortedGroups = [...byGroup.entries()].sort(
    ([a], [b]) => groupOrder.indexOf(a) - groupOrder.indexOf(b),
  );

  // 그룹 헬스 요약 (상단)
  const totalDevices = allDevices.length;
  const okCount = allDevices.filter(d => statusVisual(d.status, d.last_seen_at, staleMsOf(d)).active).length;
  const issueCount = allDevices.filter(d => {
    const v = statusVisual(d.status, d.last_seen_at, staleMsOf(d));
    return !v.active && v.label !== 'equipmentGrid.statusUnknown';
  }).length;

  const deviceGridClass = sideContent
    ? 'grid grid-cols-1 sm:grid-cols-2 gap-2' // 좁은 1/3 컬럼 → 카드 크게
    : 'grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-2';

  const groupsEl = sortedGroups.map(([group, devices]) => (
    <div key={group}>
      <div className="text-xs text-gray-500 font-medium mb-2 flex items-center gap-2">
        <span className="text-gray-400">{tr(GROUP_LABEL_KEY[group] ?? 'equipmentGrid.groupOther')}</span>
        <span className="font-mono text-gray-600">· {devices.length}</span>
      </div>
      <div className={deviceGridClass}>
        {devices.map(dev => (
          <DeviceCard key={`${dev.tank_id}:${dev.device_id}`} dev={dev} isFeeder={group === 'feeder'} />
        ))}
        {tempControlPanel && devices.some(d => d.device_type === 'chiller') && tempControlPanel}
      </div>
    </div>
  ));

  return (
    <div className="space-y-4">
      {/* 상단 요약 바 — 전체너비. SCADA HMI 헬스 인디케이터. */}
      <div className="flex items-center gap-4 px-3 py-2 rounded bg-gray-900/40 border border-gray-700/30 text-xs font-mono">
        <span className="text-gray-400">{tr('equipmentGrid.totalDevices', { count: totalDevices })}</span>
        <span className="text-green-400">{tr('equipmentGrid.okCount', { count: okCount })}</span>
        {issueCount > 0 && (
          <span className="text-red-400">{tr('equipmentGrid.alarmCount', { count: issueCount })}</span>
        )}
        <span className="text-gray-600 ml-auto">{tr('equipmentGrid.legendHint')}</span>
      </div>

      {sideContent ? (
        // 요약바 아래: 디바이스(1/3) + sideContent(2/3)
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 items-start">
          <div className="lg:col-span-1 space-y-4">{groupsEl}</div>
          <div className="lg:col-span-2">{sideContent}</div>
        </div>
      ) : (
        <div className="space-y-4">{groupsEl}</div>
      )}
    </div>
  );
}

type LiveWeightResp = {
  grams: number;
  raw: number;
  mode: string;
  age_ms: number;
};

// 운영자 영점 버튼 — 빈 통 / 찌꺼기 상태에서 클릭하면 현재 안정값을 0 으로 정렬.
// piecewise cal 의 자연 drift + 사료 찌꺼기 누적 무게를 software offset 으로 보정.
function ZeroButton({ tankID }: { tankID: string }) {
  const { tr } = useLanguage();
  const [busy, setBusy] = useState(false);
  const handle = async () => {
    if (busy) return;
    if (!confirm(tr('zeroButton.confirmMsg'))) return;
    setBusy(true);
    try {
      await api(`/v1/tanks/${tankID}/zero`, { method: 'POST' });
    } catch (e) {
      console.warn('zero failed', e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <button
      type="button"
      onClick={handle}
      disabled={busy}
      title={tr('zeroButton.title')}
      className="text-[9px] px-1.5 py-0.5 rounded border border-gray-600/50 text-gray-400 hover:text-cyan-300 hover:border-cyan-500/60 disabled:opacity-40"
    >
      {busy ? '...' : tr('zeroButton.label')}
    </button>
  );
}

// 급이기 카드 — 1Hz polling 으로 UDP weight cache 읽음.
// age_ms > 3000 (3초 무수신) 이면 stale 로 표기.
function useLiveWeight(tankID: string, enabled: boolean): LiveWeightResp | null {
  const [w, setW] = useState<LiveWeightResp | null>(null);
  const stopRef = useRef(false);
  useEffect(() => {
    if (!enabled) return;
    stopRef.current = false;
    const tick = async () => {
      try {
        const j = await api<LiveWeightResp>(`/v1/tanks/${tankID}/live-weight`);
        if (!stopRef.current) setW(j);
      } catch {
        if (!stopRef.current) setW(null);
      }
    };
    tick();
    const tid = setInterval(tick, FEEDER_POLL_MS);
    return () => { stopRef.current = true; clearInterval(tid); };
  }, [tankID, enabled]);
  return w;
}

function DeviceCard({ dev, isFeeder }: { dev: DeviceRow; isFeeder?: boolean }) {
  const { tr } = useLanguage();
  const vis = DEVICE_VIS[dev.device_type] ?? {
    icon: HelpCircle, label: dev.device_type, group: 'other',
  };
  const Icon = vis.icon;
  const sv = statusVisual(dev.status, dev.last_seen_at, staleMsOf(dev));

  // 실측 전류(A) 기반 실제 가동 판정 — 통신 status/제어 모드(수동·자동)와 무관.
  // 전류 0 → 정지(흑백). 전류>0 & 정상 → 작동 중. 전류 데이터 없으면 status 그대로.
  const currentA = typeof dev.details?.current_a === 'number' ? (dev.details.current_a as number) : null;
  const stoppedByCurrent =
    currentA !== null && currentA < 0.1 && sv.label !== 'equipmentGrid.statusOffline';
  const ev = stoppedByCurrent
    ? { ...sv, color: 'text-gray-400', bgColor: 'bg-gray-700/20', ringColor: 'ring-gray-600/40',
        label: 'equipmentGrid.statusStopped', badge: PowerOff, active: false }
    : currentA !== null && sv.active
      ? { ...sv, label: 'equipmentGrid.statusRunning' }
      : sv;

  const Badge = ev.badge;
  const showPulse = ev.active && (vis.activePulse ?? false);
  const live = useLiveWeight(dev.tank_id, !!isFeeder);
  const fresh = live && live.age_ms < 3000;

  return (
    <div
      className={`relative flex flex-col items-center gap-1.5 p-2.5 rounded-lg border border-gray-700/30 ${ev.bgColor} hover:border-gray-600/60 transition-all`}
      title={`${dev.device_id} · ${tr(vis.label)} · ${tr(ev.label)}`}
    >
      {/* 작동 중 펄스 링 */}
      {showPulse && (
        <span
          className={`absolute inset-0 rounded-lg ring-2 ${ev.ringColor} animate-pulse pointer-events-none`}
          aria-hidden
        />
      )}

      {/* SCADA 아이콘 */}
      <div className={`relative w-10 h-10 rounded-full flex items-center justify-center ${ev.bgColor} border ${ev.ringColor.replace('ring-', 'border-')}`}>
        <Icon className={`w-5 h-5 ${ev.color}`} />
        {/* 상태 배지 (우상단) */}
        <Badge
          className={`absolute -top-1 -right-1 w-3.5 h-3.5 ${ev.color} bg-black/80 rounded-full`}
        />
      </div>

      {/* 타입 라벨 + device_id (truncate) */}
      <div className="text-center w-full">
        <div className="text-xs text-gray-300 font-medium leading-tight">{tr(vis.label)}</div>
        <div className="text-[10px] text-gray-600 font-mono truncate">{dev.device_id}</div>
      </div>

      {/* 급이기: 실시간 통 중량 (1.5Hz UDP push, backend EMA+dead band+영점 통과 후).
          단위 kg, 소수점 2자리, 1g 단위 절단 (floor) — 상용 저울 LCD 와 유사.
          음수는 cal bias / 영점 인공물이라 0 으로 clamp. */}
      {isFeeder && (
        <div className="flex items-center gap-1.5">
          <div className={`text-[11px] font-mono font-semibold tabular-nums ${fresh ? 'text-cyan-300' : 'text-gray-600'}`}>
            {live ? `${(Math.max(0, Math.floor(live.grams / 10)) / 100).toFixed(2)} kg` : '— kg'}
          </div>
          <ZeroButton tankID={dev.tank_id} />
        </div>
      )}

      {/* 냉각기: 파생 전력(kW) + 전류(A) + 가동(ON/OFF). 에너지=경진대회 점수지표.
          ⚠ 가동 판정은 backend running 플래그가 아니라 실측 전류 기준(플래그가 멈춤 후에도
          running=true 로 남는 불일치가 있음). 전류 데이터 없을 때만 running 플래그로 폴백. */}
      {dev.device_type === 'chiller' && dev.details && (() => {
        const isRunning = currentA !== null ? currentA >= 0.1 : !!dev.details.running;
        return (
        <div className="flex flex-col items-center gap-0.5">
          <div className={`text-[12px] font-mono font-semibold tabular-nums ${isRunning ? 'text-cyan-300' : 'text-gray-600'}`}>
            {typeof dev.details.power_kw === 'number' ? `${(dev.details.power_kw as number).toFixed(1)} kW` : '— kW'}
          </div>
          <div className="text-[9px] font-mono text-gray-500">
            {typeof dev.details.current_a === 'number' ? `${(dev.details.current_a as number).toFixed(1)}A · ` : ''}{isRunning ? 'ON' : 'OFF'}
          </div>
          <ChillerControls />
        </div>
        );
      })()}

      {/* tank_id (그룹 내 다중 tank 식별용) */}
      <div className="text-[9px] text-gray-700 font-mono truncate w-full text-center">
        {dev.tank_display_name}
      </div>

      {/* last_seen */}
      <div className="text-[9px] text-gray-600">
        {dev.last_seen_at ? relativeTime(dev.last_seen_at) : '—'}
      </div>
    </div>
  );
}
