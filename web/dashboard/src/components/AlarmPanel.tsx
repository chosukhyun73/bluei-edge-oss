import { useState, useEffect, useCallback, useRef } from 'react';
import { AlertTriangle, BellOff, FileText, X, Check } from 'lucide-react';
import { Alerts } from '../lib/api';
import type { AlertOpen, StateVector, Tank } from '../lib/types';
import { useLanguage } from '../lib/language-core';
import { t } from '../lib/i18n';

// ── 알람 패널 (하단) ──────────────────────────────────────────────────────────
// (a) 실제 열린 알림(/v1/alerts/open) + (b) 장비 이슈(offline/stale/error) 를
// 내용까지 한 곳에 표시. "확인" 시 사라짐(알림=close API, 장비이슈=로컬 ack).
// 본 대시보드에서 관측된 모든 알람은 localStorage 이력에 누적 저장 → 전체 기록 열람/다운로드.

const OK_STATUS = new Set(['online', 'active', 'running', 'ok', 'verified']);
const LOG_KEY = 'bluei-alarm-log';
const ACK_KEY = 'bluei-alarm-device-ack';
const LOG_MAX = 2000;

type AlarmItem = {
  key: string;              // dedup/ack 용 안정 키
  source: 'alert' | 'device';
  severity: 'info' | 'warning' | 'critical';
  title: string;
  message: string;
  at: string;               // ISO
  alertId?: string;         // source=alert 일 때 close 용
};

type LogEntry = { at: string; source: string; severity: string; title: string; message: string };

function loadLog(): LogEntry[] {
  try { return JSON.parse(localStorage.getItem(LOG_KEY) || '[]'); } catch { return []; }
}
function loadAcks(): Record<string, string> {
  try { return JSON.parse(localStorage.getItem(ACK_KEY) || '{}'); } catch { return {}; }
}

// stateVectors 에서 장비 이슈(비정상/stale) 도출.
function deriveDeviceIssues(stateVectors: Map<string, StateVector>, tanks: Tank[]): AlarmItem[] {
  const nameOf = (tid: string) => tanks.find(t => t.tank_id === tid)?.display_name ?? tid;
  const now = Date.now();
  const out: AlarmItem[] = [];
  for (const [tid, sv] of stateVectors) {
    // equipment 구조는 백엔드 state-vector 그대로 — 느슨하게 접근.
    const eq = (sv as unknown as { equipment?: { devices?: Array<Record<string, unknown>> } }).equipment;
    for (const d of eq?.devices ?? []) {
      const status = String(d.status ?? '').toLowerCase();
      const deviceId = String(d.device_id ?? '');
      const deviceType = String(d.device_type ?? '');
      const details = (d.details ?? {}) as Record<string, unknown>;
      const staleAfterS = Number(details.stale_after_s) || 150;
      const lastSeen = d.last_seen_at ? Date.parse(String(d.last_seen_at)) : 0;
      const stale = lastSeen > 0 && (now - lastSeen) > staleAfterS * 1000;
      const bad = !OK_STATUS.has(status);
      if (!stale && !bad) continue;
      const reason = stale
        ? t('alarmPanel.staleReason', { age: Math.round((now - lastSeen) / 1000), threshold: staleAfterS })
        : t('alarmPanel.statusReason', { status: status || 'unknown' });
      const severity: AlarmItem['severity'] = status === 'error' || status === 'fault' ? 'critical' : 'warning';
      out.push({
        key: `device:${deviceId}:${stale ? 'stale' : status}`,
        source: 'device',
        severity,
        title: `${deviceType || t('alarmPanel.deviceFallback')} · ${deviceId}`,
        message: `${nameOf(tid)} — ${reason}`,
        at: d.last_seen_at ? String(d.last_seen_at) : new Date(now).toISOString(),
      });
    }
  }
  return out;
}

function alertToItem(a: AlertOpen): AlarmItem {
  const sev = (a.severity?.toLowerCase() as AlarmItem['severity']) || 'warning';
  return {
    key: `alert:${a.alert_id}`,
    source: 'alert',
    severity: sev === 'critical' || sev === 'info' ? sev : 'warning',
    title: `${a.alert_type} · ${a.subject?.kind ?? ''}/${a.subject?.id ?? ''}`,
    message: a.message,
    at: a.raised_at || a.updated_at || new Date().toISOString(),
    alertId: a.alert_id,
  };
}

export function AlarmPanel({
  stateVectors,
  tanks,
}: {
  stateVectors: Map<string, StateVector>;
  tanks: Tank[];
}) {
  const { tr } = useLanguage();
  const [openAlerts, setOpenAlerts] = useState<AlertOpen[]>([]);
  const [deviceAcks, setDeviceAcks] = useState<Record<string, string>>(() => loadAcks());
  const [closing, setClosing] = useState<Set<string>>(new Set());
  const [showLog, setShowLog] = useState(false);
  const loggedKeys = useRef<Set<string>>(new Set());

  // 열린 알림 5초 폴링.
  useEffect(() => {
    let cancelled = false;
    const fetchAlerts = async () => {
      try {
        const res = await Alerts.listOpen();
        if (!cancelled) setOpenAlerts(res.alerts);
      } catch { /* keep prev */ }
    };
    void fetchAlerts();
    const id = setInterval(() => void fetchAlerts(), 5000);
    return () => { cancelled = true; clearInterval(id); };
  }, []);

  // 통합 알람 목록 = 열린 알림 + 장비 이슈(로컬 ack 제외).
  const deviceIssues = deriveDeviceIssues(stateVectors, tanks);
  const items: AlarmItem[] = [
    ...openAlerts.map(alertToItem),
    ...deviceIssues.filter(i => !deviceAcks[i.key]),
  ];

  // 관측된 모든 알람을 이력에 누적(중복 제외).
  useEffect(() => {
    const all = [...openAlerts.map(alertToItem), ...deviceIssues];
    const fresh = all.filter(i => !loggedKeys.current.has(i.key));
    if (fresh.length === 0) return;
    fresh.forEach(i => loggedKeys.current.add(i.key));
    const log = loadLog();
    const now = new Date().toISOString();
    for (const i of fresh) {
      log.push({ at: now, source: i.source, severity: i.severity, title: i.title, message: i.message });
    }
    localStorage.setItem(LOG_KEY, JSON.stringify(log.slice(-LOG_MAX)));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openAlerts, JSON.stringify(deviceIssues.map(i => i.key))]);

  const ack = useCallback(async (item: AlarmItem) => {
    if (item.source === 'alert' && item.alertId) {
      setClosing(prev => new Set(prev).add(item.alertId!));
      try {
        await Alerts.close(item.alertId);
        setOpenAlerts(prev => prev.filter(a => a.alert_id !== item.alertId));
      } catch { /* 재시도 가능 */ } finally {
        setClosing(prev => { const n = new Set(prev); n.delete(item.alertId!); return n; });
      }
    } else {
      // 장비 이슈 — 로컬 ack (상태 바뀌면 key 변해 재등장).
      const next = { ...deviceAcks, [item.key]: new Date().toISOString() };
      setDeviceAcks(next);
      localStorage.setItem(ACK_KEY, JSON.stringify(next));
    }
  }, [deviceAcks]);

  const sevStyle = (s: AlarmItem['severity']) =>
    s === 'critical' ? 'bg-red-500/10 border-red-500/40 text-red-200'
      : s === 'warning' ? 'bg-orange-500/10 border-orange-500/40 text-orange-200'
        : 'bg-sky-500/10 border-sky-500/40 text-sky-200';

  const downloadLog = () => {
    const log = loadLog();
    const lines = log.map(e => `${e.at}\t[${e.severity}]\t${e.source}\t${e.title}\t${e.message}`);
    const blob = new Blob([lines.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `bluei-alarm-log-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="bg-gray-900/40 border border-gray-800 rounded-lg p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <AlertTriangle className={`w-4 h-4 ${items.length ? 'text-orange-400' : 'text-gray-600'}`} />
          <span className="text-sm font-medium text-gray-200">{tr('alarmPanel.title')}</span>
          {items.length > 0 && (
            <span className="text-xs font-mono px-1.5 py-0.5 rounded bg-orange-500/20 text-orange-300">{items.length}</span>
          )}
        </div>
        <button
          onClick={() => setShowLog(true)}
          className="flex items-center gap-1 text-xs text-gray-400 hover:text-white"
        >
          <FileText className="w-3.5 h-3.5" /> {tr('alarmPanel.history')}
        </button>
      </div>

      {items.length === 0 ? (
        <div className="flex items-center gap-2 text-sm text-gray-500 py-2">
          <BellOff className="w-4 h-4" /> {tr('alarmPanel.empty')}
        </div>
      ) : (
        <div className="space-y-2">
          {items.map(item => (
            <div key={item.key} className={`flex items-start gap-3 px-3 py-2 rounded border ${sevStyle(item.severity)}`}>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono uppercase opacity-70">{item.severity}</span>
                  <span className="text-sm font-medium truncate">{item.title}</span>
                </div>
                <div className="text-sm opacity-90 mt-0.5">{item.message}</div>
                <div className="text-[10px] opacity-50 font-mono mt-0.5">{item.at}</div>
              </div>
              <button
                onClick={() => void ack(item)}
                disabled={item.alertId ? closing.has(item.alertId) : false}
                className="flex items-center gap-1 text-xs px-2 py-1 rounded bg-black/30 hover:bg-black/50 border border-white/10 shrink-0"
              >
                <Check className="w-3.5 h-3.5" /> {tr('alarmPanel.ack')}
              </button>
            </div>
          ))}
        </div>
      )}

      {showLog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4" onClick={() => setShowLog(false)}>
          <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-3xl max-h-[85vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between p-3 border-b border-gray-800">
              <span className="text-sm font-medium text-gray-200">{tr('alarmPanel.historyTitle')}</span>
              <div className="flex items-center gap-2">
                <button onClick={downloadLog} className="flex items-center gap-1 text-xs px-2 py-1 rounded bg-green-600/80 hover:bg-green-600 text-white">
                  <FileText className="w-3.5 h-3.5" /> {tr('alarmPanel.download')}
                </button>
                <button onClick={() => setShowLog(false)} className="p-1 text-gray-400 hover:text-white"><X className="w-4 h-4" /></button>
              </div>
            </div>
            <div className="overflow-auto p-3 font-mono text-xs text-gray-300 space-y-1">
              {loadLog().slice().reverse().map((e, i) => (
                <div key={i} className="flex gap-2 border-b border-gray-800/50 py-0.5">
                  <span className="text-gray-500 shrink-0">{e.at.slice(0, 19).replace('T', ' ')}</span>
                  <span className={`shrink-0 ${e.severity === 'critical' ? 'text-red-400' : e.severity === 'warning' ? 'text-orange-400' : 'text-sky-400'}`}>[{e.severity}]</span>
                  <span className="text-gray-400 shrink-0">{e.title}</span>
                  <span className="truncate">{e.message}</span>
                </div>
              ))}
              {loadLog().length === 0 && <div className="text-gray-500">{tr('alarmPanel.historyEmpty')}</div>}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
