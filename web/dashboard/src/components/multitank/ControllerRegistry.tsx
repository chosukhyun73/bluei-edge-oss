import { useState, useEffect, useCallback } from 'react';
import { Trash2 } from 'lucide-react';
import type { Controller, ControllerStatus, ControllerTestResult, Tank } from '../../lib/types';
import { Controllers, Tanks, Provision } from '../../lib/api';
import type { ProvisionJob } from '../../lib/api';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Button } from '../ui/button';
import { Skeleton } from '../ui/skeleton';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { useLanguage } from '../../lib/language-context';

// ── Status badge ──────────────────────────────────────────────────────────────

const STATUS_LABEL_KEYS: Record<ControllerStatus, string> = {
  pending: 'controllerRegistry.statusPending',
  active: 'controllerRegistry.statusActive',
  disabled: 'controllerRegistry.statusDisabled',
  fault: 'controllerRegistry.statusFault',
};

function StatusBadge({ status }: { status: ControllerStatus }) {
  const { tr } = useLanguage();
  const variantMap: Record<ControllerStatus, string> = {
    pending: 'bg-yellow-500/15 text-yellow-300 border-yellow-500/30',
    active: 'bg-green-500/15 text-green-300 border-green-500/30',
    disabled: 'bg-gray-500/15 text-gray-400 border-gray-500/30',
    fault: 'bg-red-500/15 text-red-300 border-red-500/30',
  };
  return (
    <span
      className={[
        'inline-flex items-center px-2 py-0.5 rounded-md border text-xs font-medium',
        variantMap[status],
      ].join(' ')}
    >
      {tr(STATUS_LABEL_KEYS[status])}
    </span>
  );
}

// ── Test result modal (inline overlay) ───────────────────────────────────────

interface TestModalProps {
  result: ControllerTestResult;
  onClose: () => void;
}

function TestModal({ result, onClose }: TestModalProps) {
  const { tr } = useLanguage();
  // Close on Escape
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onClose]);

  const { dac_ok, stop_ok, latency_ms, motor_ok, has_weight, weight_g, tared } = result.results;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={tr('testModal.ariaLabel')}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
    >
      <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl p-6 w-full max-w-sm space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold text-white">{tr('testModal.title')}</h2>
          <button
            type="button"
            aria-label={tr('testModal.closeAriaLabel')}
            onClick={onClose}
            className="text-gray-500 hover:text-white transition-colors text-lg leading-none"
          >
            ✕
          </button>
        </div>

        <p className="text-xs text-gray-400 font-mono">{result.controller_id}</p>

        <div className="space-y-3">
          <ResultRow label={tr('testModal.dacOutput')} ok={dac_ok} />
          <ResultRow label={tr('testModal.stopSignal')} ok={stop_ok} />
          <ResultRow label={tr('testModal.motorSequence')} ok={Boolean(motor_ok)} />
          {has_weight ? (
            <>
              <div className="flex items-center justify-between py-2 border-b border-gray-800">
                <span className="text-sm text-gray-400">{tr('testModal.weightMeasure')}</span>
                <span className="text-sm font-mono text-white">
                  {weight_g != null ? `${weight_g.toFixed(1)} g` : '—'}
                </span>
              </div>
              <ResultRow label={tr('testModal.weightTare')} ok={Boolean(tared)} />
            </>
          ) : (
            <div className="flex items-center justify-between py-2 border-b border-gray-800">
              <span className="text-sm text-gray-400">{tr('testModal.weightSensor')}</span>
              <span className="text-sm text-gray-500">{tr('testModal.notConnected')}</span>
            </div>
          )}
          <div className="flex items-center justify-between py-2 border-b border-gray-800">
            <span className="text-sm text-gray-400">{tr('testModal.latency')}</span>
            <span className="text-sm font-mono text-white">{latency_ms} ms</span>
          </div>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={onClose}
          className="w-full"
          aria-label={tr('testModal.closeResultAriaLabel')}
        >
          {tr('testModal.close')}
        </Button>
      </div>
    </div>
  );
}

function ResultRow({ label, ok }: { label: string; ok: boolean }) {
  const { tr } = useLanguage();
  return (
    <div className="flex items-center justify-between py-2 border-b border-gray-800">
      <span className="text-sm text-gray-400">{label}</span>
      <span className={ok ? 'text-green-400 font-semibold' : 'text-red-400 font-semibold'}>
        {ok ? tr('testModal.resultOk') : tr('testModal.resultFail')}
      </span>
    </div>
  );
}

// ── Registration form (inline panel) ─────────────────────────────────────────

interface RegisterFormProps {
  onSuccess: () => void;
  onCancel: () => void;
}

function RegisterForm({ onSuccess, onCancel }: RegisterFormProps) {
  const { tr } = useLanguage();
  const [macAddress, setMacAddress] = useState('');
  const [controllerId, setControllerId] = useState('');
  const [firmwareVersion, setFirmwareVersion] = useState('');
  const [tankId, setTankId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!macAddress.trim() || !controllerId.trim() || !firmwareVersion.trim()) {
      setError(tr('registerForm.requiredFieldsError'));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await Controllers.register({
        mac_address: macAddress.trim(),
        controller_id: controllerId.trim(),
        firmware_version: firmwareVersion.trim(),
        tank_id: tankId.trim() || undefined,
      });
      onSuccess();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('registerForm.unknownError');
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Card className="bg-gray-800/60 border-green-500/20">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm text-green-400">{tr('registerForm.title')}</CardTitle>
      </CardHeader>
      <CardContent className="pb-5">
        <form onSubmit={handleSubmit} className="space-y-3" aria-label={tr('registerForm.formAriaLabel')}>
          {error && (
            <p className="text-xs text-red-400 font-mono px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md">
              {error}
            </p>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <FormField
              id="reg-mac"
              label={tr('registerForm.labelMac')}
              value={macAddress}
              onChange={setMacAddress}
              placeholder="AA:BB:CC:DD:EE:FF"
            />
            <FormField
              id="reg-ctrl-id"
              label={tr('registerForm.labelControllerId')}
              value={controllerId}
              onChange={setControllerId}
              placeholder="ctrl-001"
            />
            <FormField
              id="reg-fw"
              label={tr('registerForm.labelFirmware')}
              value={firmwareVersion}
              onChange={setFirmwareVersion}
              placeholder="1.0.0"
            />
            <FormField
              id="reg-tank"
              label={tr('registerForm.labelTankOptional')}
              value={tankId}
              onChange={setTankId}
              placeholder="tank-001"
            />
          </div>

          <div className="flex gap-2 pt-1">
            <Button
              type="submit"
              size="sm"
              disabled={submitting}
              aria-label={tr('registerForm.submitAriaLabel')}
            >
              {submitting ? tr('registerForm.submitting') : tr('registerForm.submit')}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onCancel}
              aria-label={tr('registerForm.cancelAriaLabel')}
            >
              {tr('registerForm.cancel')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function FormField({
  id, label, value, onChange, placeholder,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs text-gray-400 font-medium">{label}</label>
      <input
        id={id}
        type="text"
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
      />
    </div>
  );
}

// ── USB 자동 provisioning 패널 ─────────────────────────────────────────────────

const CONTROLLER_TYPES: { value: string; labelKey: string; enabled: boolean }[] = [
  { value: 'feeder', labelKey: 'controllerRegistry.typeFeeder', enabled: true },
  { value: 'circulation_pump', labelKey: 'controllerRegistry.typeCirculationPump', enabled: false },
  { value: 'air_pump', labelKey: 'controllerRegistry.typeAirPump', enabled: false },
  { value: 'heat_pump', labelKey: 'controllerRegistry.typeHeatPump', enabled: false },
  { value: 'uv_sterilizer', labelKey: 'controllerRegistry.typeUvSterilizer', enabled: false },
  { value: 'other', labelKey: 'controllerRegistry.typeOther', enabled: false },
];

const PROVISION_STATUS: Record<string, { labelKey: string; cls: string }> = {
  running: { labelKey: 'controllerRegistry.provRunning', cls: 'text-cyan-300' },
  success: { labelKey: 'controllerRegistry.provSuccess', cls: 'text-green-400' },
  flashed_unconfirmed: { labelKey: 'controllerRegistry.provFlashedUnconfirmed', cls: 'text-yellow-300' },
  failed: { labelKey: 'controllerRegistry.provFailed', cls: 'text-red-400' },
};

function ProvisionPanel({
  tanks, onSuccess, onCancel,
}: {
  tanks: Tank[];
  onSuccess: () => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const [type, setType] = useState('feeder');
  const [ports, setPorts] = useState<string[]>([]);
  const [port, setPort] = useState('');
  const [tankId, setTankId] = useState('');
  const [controllerId, setControllerId] = useState('');
  const [cidEdited, setCidEdited] = useState(false);
  const [reclaim, setReclaim] = useState(false);
  const [activate, setActivate] = useState(false);
  const [job, setJob] = useState<ProvisionJob | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // controller_id 자동 생성 (사용자가 직접 편집하기 전까지)
  useEffect(() => {
    if (!cidEdited) setControllerId(tankId ? `feeder_${tankId}` : '');
  }, [tankId, cidEdited]);

  const refreshPorts = useCallback(async () => {
    try {
      const r = await Provision.ports();
      setPorts(r.ports);
      setPort(prev => (prev && r.ports.includes(prev) ? prev : (r.ports[0] ?? '')));
    } catch { setPorts([]); }
  }, []);
  useEffect(() => { void refreshPorts(); }, [refreshPorts]);

  // job 폴링
  useEffect(() => {
    if (!job || job.status !== 'running') return;
    const timer = setInterval(async () => {
      try {
        const s = await Provision.status(job.job_id);
        setJob(s);
        if (s.status !== 'running') {
          clearInterval(timer);
          setBusy(false);
          if (s.status === 'success') onSuccess();
        }
      } catch { /* 일시 오류는 다음 tick 에 재시도 */ }
    }, 1500);
    return () => clearInterval(timer);
  }, [job, onSuccess]);

  async function handleFlash() {
    if (!tankId) { setError(tr('provisionPanel.selectTankError')); return; }
    setError(null);
    setBusy(true);
    try {
      const r = await Provision.start({
        tank_id: tankId,
        type,
        controller_id: controllerId || undefined,
        port: port || undefined,
        reclaim,
        activate,
      });
      setJob({
        job_id: r.job_id, status: 'running', exit_code: 0,
        controller_id: r.controller_id, tank_id: r.tank_id, log: '', started_at: '',
      });
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : tr('provisionPanel.startError'));
      setBusy(false);
    }
  }

  const inputCls = 'h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50';

  return (
    <Card className="bg-gray-800/60 border-cyan-500/20">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm text-cyan-300">{tr('provisionPanel.title')}</CardTitle>
      </CardHeader>
      <CardContent className="pb-5 space-y-3">
        <p className="text-xs text-gray-500">
          {tr('provisionPanel.description')}
        </p>
        {error && (
          <p className="text-xs text-red-400 font-mono px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md">{error}</p>
        )}

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {/* 타입 */}
          <div className="flex flex-col gap-1">
            <label htmlFor="prov-type" className="text-xs text-gray-400 font-medium">{tr('provisionPanel.labelType')}</label>
            <select id="prov-type" value={type} onChange={e => setType(e.target.value)} className={inputCls} disabled={busy}>
              {CONTROLLER_TYPES.map(t => (
                <option key={t.value} value={t.value} disabled={!t.enabled}>
                  {tr(t.labelKey)}{t.enabled ? '' : ` (${tr('provisionPanel.firmwarePending')})`}
                </option>
              ))}
            </select>
          </div>
          {/* 탱크 */}
          <div className="flex flex-col gap-1">
            <label htmlFor="prov-tank" className="text-xs text-gray-400 font-medium">{tr('provisionPanel.labelTank')}</label>
            <select id="prov-tank" value={tankId} onChange={e => setTankId(e.target.value)} className={inputCls} disabled={busy}>
              <option value="">{tr('provisionPanel.tankSelectPlaceholder')}</option>
              {tanks.map(t => (
                <option key={t.tank_id} value={t.tank_id}>{t.tank_id} · {t.display_name}</option>
              ))}
            </select>
          </div>
          {/* 포트 */}
          <div className="flex flex-col gap-1">
            <label htmlFor="prov-port" className="text-xs text-gray-400 font-medium">{tr('provisionPanel.labelUsbPort')}</label>
            <div className="flex gap-2">
              <select id="prov-port" value={port} onChange={e => setPort(e.target.value)} className={`${inputCls} flex-1`} disabled={busy}>
                {ports.length === 0 && <option value="">{tr('provisionPanel.noPortDetected')}</option>}
                {ports.map(p => <option key={p} value={p}>{p}</option>)}
              </select>
              <Button type="button" variant="outline" size="sm" onClick={() => void refreshPorts()} disabled={busy} aria-label={tr('provisionPanel.refreshPortAriaLabel')}>↻</Button>
            </div>
          </div>
          {/* controller_id */}
          <div className="flex flex-col gap-1">
            <label htmlFor="prov-cid" className="text-xs text-gray-400 font-medium">controller_id</label>
            <input id="prov-cid" type="text" value={controllerId}
              onChange={e => { setCidEdited(true); setControllerId(e.target.value); }}
              placeholder="feeder_<tank>" className={inputCls} disabled={busy} />
          </div>
        </div>

        <div className="flex flex-wrap gap-4 text-xs text-gray-400">
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" checked={reclaim} onChange={e => setReclaim(e.target.checked)} disabled={busy} />
            {tr('provisionPanel.reclaimLabel')}
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" checked={activate} onChange={e => setActivate(e.target.checked)} disabled={busy} />
            {tr('provisionPanel.activateLabel')}
          </label>
        </div>

        <div className="flex gap-2 pt-1">
          <Button type="button" size="sm" onClick={() => void handleFlash()} disabled={busy || !tankId} aria-label={tr('provisionPanel.flashAriaLabel')}>
            {busy ? tr('provisionPanel.flashing') : tr('provisionPanel.flashButton')}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={busy} aria-label={tr('provisionPanel.closeAriaLabel')}>{tr('provisionPanel.close')}</Button>
        </div>

        {job && (
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <span className="text-gray-400">{tr('provisionPanel.statusLabel')}</span>
              <span className={PROVISION_STATUS[job.status]?.cls ?? 'text-gray-300'}>
                {PROVISION_STATUS[job.status] ? tr(PROVISION_STATUS[job.status].labelKey) : job.status}
              </span>
              <span className="text-gray-500 font-mono text-xs">{job.controller_id}</span>
            </div>
            {job.log && (
              <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words text-[11px] leading-snug font-mono text-gray-300 bg-black/40 border border-gray-800 rounded-md p-3">
                {job.log}
              </pre>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Filter chips ──────────────────────────────────────────────────────────────

const FILTER_OPTIONS: { labelKey: string; value: ControllerStatus | 'all' }[] = [
  { labelKey: 'controllerRegistry.filterAll', value: 'all' },
  { labelKey: 'controllerRegistry.statusPending', value: 'pending' },
  { labelKey: 'controllerRegistry.statusActive', value: 'active' },
  { labelKey: 'controllerRegistry.statusDisabled', value: 'disabled' },
  { labelKey: 'controllerRegistry.statusFault', value: 'fault' },
];

// ── Main ControllerRegistry ───────────────────────────────────────────────────

export function ControllerRegistry() {
  const { tr } = useLanguage();
  const [controllers, setControllers] = useState<Controller[]>([]);
  const [filter, setFilter] = useState<ControllerStatus | 'all'>('all');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showRegForm, setShowRegForm] = useState(false);
  const [showProvision, setShowProvision] = useState(false);
  const [testResult, setTestResult] = useState<ControllerTestResult | null>(null);
  const [testError, setTestError] = useState<string | null>(null);
  const [actionBusy, setActionBusy] = useState<Record<string, boolean>>({});
  const [deleteTarget, setDeleteTarget] = useState<Controller | null>(null);
  const [deleting, setDeleting] = useState(false);
  // 등록된 tank 목록 — controller.tank_id 가 실제 tank 인지 판별 (미연결 표시).
  const [tanks, setTanks] = useState<Tank[]>([]);
  useEffect(() => {
    void (async () => {
      try { const r = await Tanks.list(); setTanks(r.items); }
      catch { setTanks([]); }
    })();
  }, []);

  const fetchControllers = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await Controllers.list(filter === 'all' ? undefined : filter);
      setControllers(res.items);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('controllerRegistry.unknownError');
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [filter, tr]);

  useEffect(() => { void fetchControllers(); }, [fetchControllers]);

  async function handleActivate(id: string) {
    setActionBusy(prev => ({ ...prev, [id]: true }));
    try {
      await Controllers.activate(id);
      await fetchControllers();
    } catch (err: unknown) {
      console.error('[ControllerRegistry] activate failed:', err);
    } finally {
      setActionBusy(prev => ({ ...prev, [id]: false }));
    }
  }

  async function handleTest(id: string) {
    setActionBusy(prev => ({ ...prev, [id]: true }));
    setTestError(null);
    const before = controllers.find(c => c.controller_id === id);
    const beforeTestedAt = (before?.commissioning as Record<string, unknown> | undefined)?.['tested_at'] as string | undefined;
    try {
      await Controllers.test(id); // 실제 self-test 명령 enqueue (ESP32 폴링 후 결과 보고)
      const deadline = Date.now() + 60_000;
      let shown = false;
      while (Date.now() < deadline) {
        await new Promise(r => setTimeout(r, 2000));
        const res = await Controllers.list(filter === 'all' ? undefined : filter);
        setControllers(res.items);
        const c = res.items.find(x => x.controller_id === id);
        const comm = c?.commissioning as Record<string, unknown> | undefined;
        const testedAt = comm?.['tested_at'] as string | undefined;
        if (testedAt && testedAt !== beforeTestedAt) {
          setTestResult({
            controller_id: id,
            results: {
              dac_ok: Boolean(comm?.['dac_ok']),
              stop_ok: Boolean(comm?.['stop_ok']),
              latency_ms: Number(comm?.['latency_ms'] ?? 0),
              motor_ok: Boolean(comm?.['motor_ok']),
              has_weight: Boolean(comm?.['has_weight']),
              weight_g: comm?.['weight_g'] != null ? Number(comm['weight_g']) : undefined,
              tared: Boolean(comm?.['tared']),
            },
          });
          shown = true;
          break;
        }
      }
      if (!shown) {
        setTestError(`${id}: ${tr('controllerRegistry.testTimeout')}`);
      }
    } catch (err: unknown) {
      console.error('[ControllerRegistry] test failed:', err);
      setTestError(err instanceof Error ? err.message : tr('controllerRegistry.testStartError'));
    } finally {
      setActionBusy(prev => ({ ...prev, [id]: false }));
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await Controllers.delete(deleteTarget.controller_id);
      setDeleteTarget(null);
      await fetchControllers();
    } catch (err: unknown) {
      console.error('[ControllerRegistry] delete failed:', err);
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div className="space-y-4">
      {/* 헤더 행 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        {/* 필터 칩 */}
        <div className="flex flex-wrap gap-2" role="group" aria-label={tr('controllerRegistry.filterAriaLabel')}>
          {FILTER_OPTIONS.map(opt => (
            <button
              key={opt.value}
              type="button"
              aria-pressed={filter === opt.value}
              onClick={() => setFilter(opt.value)}
              className={[
                'px-3 py-1.5 rounded-full text-xs font-medium border transition-all',
                filter === opt.value
                  ? 'bg-green-500/20 border-green-500/50 text-green-300'
                  : 'bg-gray-800/60 border-gray-700/50 text-gray-400 hover:border-gray-600 hover:text-gray-300',
              ].join(' ')}
            >
              {tr(opt.labelKey)}
            </button>
          ))}
        </div>

        {/* 신규 등록 버튼 */}
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => { setShowProvision(v => !v); setShowRegForm(false); }}
            aria-label={tr('controllerRegistry.usbRegisterAriaLabel')}
          >
            {tr('controllerRegistry.usbRegisterButton')}
          </Button>
          <Button
            size="sm"
            onClick={() => { setShowRegForm(v => !v); setShowProvision(false); }}
            aria-label={tr('controllerRegistry.manualRegisterAriaLabel')}
          >
            {tr('controllerRegistry.manualRegisterButton')}
          </Button>
        </div>
      </div>

      {/* USB 자동 등록 패널 */}
      {showProvision && (
        <ProvisionPanel
          tanks={tanks}
          onSuccess={() => { void fetchControllers(); }}
          onCancel={() => setShowProvision(false)}
        />
      )}

      {/* 등록 폼 */}
      {showRegForm && (
        <RegisterForm
          onSuccess={() => {
            setShowRegForm(false);
            void fetchControllers();
          }}
          onCancel={() => setShowRegForm(false)}
        />
      )}

      {/* 오류 */}
      {error && (
        <div className="px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive font-mono">
          {tr('controllerRegistry.backendError')}: {error} — {tr('controllerRegistry.backendErrorHint')}
        </div>
      )}

      {/* 테이블 */}
      <Card className="bg-gray-900/60 border-gray-700/50 overflow-hidden">
        <CardHeader className="pb-0 border-b border-gray-800">
          <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide py-1">
            {tr('controllerRegistry.listTitle')}
            {!loading && <span className="ml-2 text-gray-500 font-normal">({controllers.length})</span>}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="p-5 space-y-3">
              {[1, 2, 3].map(i => <Skeleton key={i} className="h-10 w-full" />)}
            </div>
          ) : controllers.length === 0 ? (
            <div className="py-10 text-center text-sm text-gray-500">
              {tr('controllerRegistry.emptyMessage')}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" aria-label={tr('controllerRegistry.tableAriaLabel')}>
                <thead>
                  <tr className="border-b border-gray-800 text-xs text-gray-500 uppercase tracking-wide">
                    <th className="text-left px-4 py-3 font-medium">{tr('controllerRegistry.colControllerId')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('controllerRegistry.colMac')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('controllerRegistry.colCageTank')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('controllerRegistry.colStatus')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('controllerRegistry.colFirmware')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('controllerRegistry.colLastSeen')}</th>
                    <th className="text-right px-4 py-3 font-medium">{tr('controllerRegistry.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {controllers.map(ctrl => (
                    <tr
                      key={ctrl.controller_id}
                      className="border-b border-gray-800/50 hover:bg-gray-800/30 transition-colors"
                    >
                      <td className="px-4 py-3 font-mono text-xs text-gray-300">
                        {ctrl.controller_id}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-400">
                        {ctrl.mac_address}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs">
                        {(() => {
                          const t = tanks.find(t => t.tank_id === ctrl.tank_id);
                          if (t) return <span className="text-gray-300">{t.display_name}</span>;
                          return <span className="text-orange-400">{tr('controllerRegistry.notConnected')}</span>;
                        })()}
                      </td>
                      <td className="px-4 py-3">
                        <StatusBadge status={ctrl.status} />
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-400">
                        {ctrl.firmware_version ?? '—'}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-500">
                        {ctrl.last_seen_at
                          ? new Date(ctrl.last_seen_at).toLocaleString('ko-KR')
                          : '—'}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-end gap-2">
                          {ctrl.status === 'pending' && (
                            <Button
                              size="sm"
                              variant="default"
                              disabled={actionBusy[ctrl.controller_id]}
                              onClick={() => handleActivate(ctrl.controller_id)}
                              aria-label={`${ctrl.controller_id} ${tr('controllerRegistry.activateAriaLabelSuffix')}`}
                            >
                              {actionBusy[ctrl.controller_id] ? tr('controllerRegistry.processing') : tr('controllerRegistry.activate')}
                            </Button>
                          )}
                          {(ctrl.status === 'pending' || ctrl.status === 'active') && (
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={actionBusy[ctrl.controller_id]}
                              onClick={() => handleTest(ctrl.controller_id)}
                              aria-label={`${ctrl.controller_id} ${tr('controllerRegistry.testAriaLabelSuffix')}`}
                            >
                              {actionBusy[ctrl.controller_id] ? tr('controllerRegistry.testing') : tr('controllerRegistry.test')}
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={actionBusy[ctrl.controller_id]}
                            onClick={() => setDeleteTarget(ctrl)}
                            aria-label={`${ctrl.controller_id} ${tr('controllerRegistry.deleteAriaLabelSuffix')}`}
                            className="text-red-400 hover:text-red-300 border-red-500/30 hover:border-red-500/50"
                          >
                            <Trash2 className="w-4 h-4" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 테스트 결과 모달 */}
      {testResult && (
        <TestModal result={testResult} onClose={() => setTestResult(null)} />
      )}

      {/* 테스트 오류/타임아웃 */}
      {testError && (
        <div
          role="alert"
          className="px-4 py-3 bg-yellow-500/10 border border-yellow-500/30 rounded-lg text-sm text-yellow-300 flex items-center justify-between gap-3"
        >
          <span>{testError}</span>
          <button type="button" onClick={() => setTestError(null)} aria-label={tr('controllerRegistry.closeAlertAriaLabel')} className="text-yellow-400 hover:text-yellow-200">✕</button>
        </div>
      )}

      {/* 삭제 확인 다이얼로그 */}
      <ConfirmDialog
        open={deleteTarget !== null}
        title={tr('controllerRegistry.deleteDialogTitle')}
        message={
          <>
            <span className="font-mono text-white">{deleteTarget?.controller_id}</span> {tr('controllerRegistry.deleteDialogBody')}
            <br />
            {tr('controllerRegistry.deleteDialogHint')}
          </>
        }
        busy={deleting}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}
