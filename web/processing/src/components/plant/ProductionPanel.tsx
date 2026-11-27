import { useCallback, useEffect, useState } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import { ApiError } from '../../lib/api';
import { Process, Documents, Plant, fmtTime } from '../../lib/plant';
import type {
  HaccpPlan, Lot, LotDetail, LotCCP, LotDeviation, LotInput, LimitStatus, LocalUser, PlantZone, SensorReading, Signature,
} from '../../lib/plant';
import { Field, SelectField } from '../fields';
import { Button } from '../ui/button';
import { useLanguage } from '../../lib/language-core';
import { FlowDiagram } from './FlowDiagram';

// 생산 — lot 을 공정도 위에서 움직이고, 그 사이의 측정값으로 CCP 를 판정한다.
// 판정은 서버의 규칙 엔진이 한다. 화면은 3초마다 다시 읽어 보여 주기만 한다.

function errText(e: unknown): string {
  if (e instanceof ApiError) return `${e.code}: ${e.message}`;
  return e instanceof Error ? e.message : String(e);
}

const STATUS_CLASS: Record<LimitStatus, string> = {
  pending: 'bg-gray-700/60 text-gray-300', ok: 'bg-green-500/20 text-green-300',
  in_progress: 'bg-amber-500/20 text-amber-200', needs_input: 'bg-amber-500/30 text-amber-200',
  deviation: 'bg-red-600 text-white', outage: 'bg-red-600 text-white',
};

// 서명자 — 사람이 하는 기록(시작·입력·개선조치·마감·검토·발행)에는 계정과 PIN 이 붙는다.
export function SignerBar({ users, value, onChange }: { users: LocalUser[]; value: Signature; onChange: (s: Signature) => void }) {
  const { tr } = useLanguage();
  return (
    <div className="flex items-end gap-2">
      <SelectField label={tr('proc.signer')} value={value.by} onChange={by => onChange({ ...value, by })}
        options={[{ value: '', label: '—' }, ...users.filter(u => u.active).map(u => ({ value: u.user_id, label: `${u.name} (${tr(`plant.role.${u.role}`)})` }))]} />
      <div className="w-24"><Field label="PIN" type="password" value={value.pin} onChange={pin => onChange({ ...value, pin })} /></div>
    </div>
  );
}

export function ProductionPanel({ siteId, plans, users, zones }: {
  siteId: string; plans: HaccpPlan[]; users: LocalUser[]; zones: PlantZone[];
}) {
  const { tr } = useLanguage();
  const [lots, setLots] = useState<Lot[]>([]);
  const [lotId, setLotId] = useState('');
  const [sig, setSig] = useState<Signature>({ by: '', pin: '' });
  const [starting, setStarting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      const r = await Process.lots(siteId);
      setLots(r.lots);
      setLotId(cur => cur || r.lots.find(l => l.status === 'running')?.lot_id || r.lots[0]?.lot_id || '');
    } catch (e) { setErr(errText(e)); }
  }, [siteId]);
  useEffect(() => { reload(); }, [reload]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-2">
        {lots.map(l => (
          <button key={l.lot_id} type="button" onClick={() => { setLotId(l.lot_id); setStarting(false); }}
            className={'px-3 py-1.5 text-sm rounded border ' + (l.lot_id === lotId && !starting
              ? 'bg-green-600/20 border-green-500/60 text-green-200' : 'border-gray-700 text-gray-400 hover:text-white')}>
            {l.lot_no}
            <span className="ml-2 text-xs text-gray-500">{tr(`proc.lotStatus.${l.status}`)}</span>
            {!!l.open_deviations && <span className="ml-2 px-1 rounded bg-red-600 text-white text-xs">{l.open_deviations}</span>}
          </button>
        ))}
        <Button size="sm" variant="outline" onClick={() => setStarting(true)}><Plus className="size-4" />{tr('proc.newLot')}</Button>
        <div className="ml-auto"><SignerBar users={users} value={sig} onChange={setSig} /></div>
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      {lots.length === 0 && !starting && <p className="text-sm text-gray-500">{tr('proc.noLots')}</p>}

      {starting
        ? <StartLotForm siteId={siteId} plans={plans} sig={sig} onStarted={id => { setStarting(false); setLotId(id); reload(); }} />
        : lotId && <LotView key={lotId} siteId={siteId} lotId={lotId} zones={zones} users={users} sig={sig} onChanged={reload} />}
    </div>
  );
}

function StartLotForm({ siteId, plans, sig, onStarted }: {
  siteId: string; plans: HaccpPlan[]; sig: Signature; onStarted: (lotId: string) => void;
}) {
  const { tr } = useLanguage();
  const [planId, setPlanId] = useState(plans[0]?.plan_id ?? '');
  const [lotNo, setLotNo] = useState('');
  const [inputs, setInputs] = useState<LotInput[]>([{ supplier: '', supplier_lot: '', species: '', qty_kg: 0 }]);
  const [err, setErr] = useState<string | null>(null);
  const setInput = (i: number, patch: Partial<LotInput>) => setInputs(prev => prev.map((x, j) => (j === i ? { ...x, ...patch } : x)));

  const start = async () => {
    try {
      const r = await Process.start({ site_id: siteId, plan_id: planId, lot_no: lotNo.trim(), inputs, started_by: sig.by, pin: sig.pin });
      onStarted(r.lot.lot_id);
    } catch (e) { setErr(errText(e)); }
  };

  return (
    <div className="p-4 rounded-lg border border-gray-800 space-y-3 max-w-4xl">
      <h3 className="text-sm text-white font-medium">{tr('proc.newLot')}</h3>
      <p className="text-xs text-gray-500">{tr('proc.newLot.hint')}</p>
      <div className="grid grid-cols-2 gap-2">
        <SelectField label={tr('proc.plan')} value={planId} onChange={setPlanId} options={plans.map(p => ({ value: p.plan_id, label: p.name }))} />
        <Field label={tr('proc.lotNo')} value={lotNo} onChange={setLotNo} />
      </div>
      <h4 className="text-xs text-gray-400 font-medium">{tr('proc.inputs')}</h4>
      {inputs.map((x, i) => (
        <div key={i} className="grid grid-cols-[1fr_1fr_1fr_6rem_auto] gap-2 items-end">
          <Field label={tr('proc.input.supplier')} value={x.supplier} onChange={v => setInput(i, { supplier: v })} />
          <Field label={tr('proc.input.supplierLot')} value={x.supplier_lot} onChange={v => setInput(i, { supplier_lot: v })} />
          <Field label={tr('proc.input.species')} value={x.species} onChange={v => setInput(i, { species: v })} />
          <Field label="kg" type="number" value={x.qty_kg ? String(x.qty_kg) : ''} onChange={v => setInput(i, { qty_kg: Number(v) || 0 })} />
          <Button size="icon" variant="ghost" disabled={inputs.length === 1} onClick={() => setInputs(prev => prev.filter((_, j) => j !== i))}>
            <Trash2 className="size-4 text-gray-500" />
          </Button>
        </div>
      ))}
      <Button size="sm" variant="outline" onClick={() => setInputs(prev => [...prev, { supplier: '', supplier_lot: '', species: '', qty_kg: 0 }])}>
        <Plus className="size-4" />{tr('proc.input.add')}
      </Button>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}
      <div><Button disabled={!planId || !sig.by} onClick={start}>{tr('proc.start')}</Button></div>
      {!sig.by && <p className="text-xs text-amber-300">{tr('proc.signerNeeded')}</p>}
    </div>
  );
}

function LotView({ siteId, lotId, zones, users, sig, onChanged }: {
  siteId: string; lotId: string; zones: PlantZone[]; users: LocalUser[]; sig: Signature; onChanged: () => void;
}) {
  const { tr } = useLanguage();
  const [d, setD] = useState<LotDetail | null>(null);
  const [sensors, setSensors] = useState<SensorReading[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [outputKg, setOutputKg] = useState('');
  const [issued, setIssued] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [detail, readings] = await Promise.all([Process.detail(lotId), Plant.readings(siteId)]);
      setD(detail); setSensors(readings.sensors);
    } catch (e) { setErr(errText(e)); }
  }, [lotId, siteId]);
  useEffect(() => {
    load();
    const id = window.setInterval(load, 3000);
    return () => window.clearInterval(id);
  }, [load]);

  // 사람이 하는 동작: 결과로 받은 상세를 바로 반영한다.
  const act = async (fn: () => Promise<LotDetail>) => {
    try { setD(await fn()); setErr(null); onChanged(); } catch (e) { setErr(errText(e)); }
  };

  if (!d) return <p className="text-sm text-gray-500">{err ?? tr('plant.loading')}</p>;
  const running = d.lot.status === 'running';
  const userName = (id: string) => users.find(u => u.user_id === id)?.name ?? id;
  const ccpStatus = Object.fromEntries(d.ccps.map(k => [k.ccp_id, k.status]));
  const openDevs = d.deviations.filter(x => x.status === 'open');
  const inTransit = d.position.link_id !== '';

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-sm">
        <span className="text-white font-semibold">lot {d.lot.lot_no}</span>
        <span className={running ? 'text-sky-300' : 'text-gray-400'}>{tr(`proc.lotStatus.${d.lot.status}`)}</span>
        <span className="text-gray-400">{d.plan.name} v{d.plan.version_no}</span>
        <span className="text-gray-400">{tr('proc.inputKg')} {d.lot.input_kg} kg</span>
        {!running && <span className="text-gray-400">{tr('proc.outputKg')} {d.lot.output_kg} kg · {tr('proc.yield')} {d.lot.yield_pct?.toFixed(1)}%</span>}
        <span className="text-gray-500 text-xs">{tr('proc.startedAt')} {fmtTime(d.lot.started_at)} · {userName(d.lot.started_by)}</span>
      </div>
      {err && <p className="text-xs text-red-400 font-mono">{err}</p>}

      <FlowDiagram content={d.content} zones={zones} selected={null} onSelect={() => {}}
        current={{ stepId: d.position.step_id, linkId: d.position.link_id }} ccpStatus={ccpStatus} />

      {/* ── 이동 ── */}
      {running && (
        <div className="flex flex-wrap items-center gap-2 p-3 rounded-lg border border-sky-500/30 bg-sky-500/5">
          <span className="text-sm text-sky-200">
            {inTransit ? tr('proc.movingTo', { to: d.position.link_to_name }) : tr('proc.atStep', { step: d.position.step_name })}
          </span>
          {inTransit && <Button size="sm" onClick={() => act(() => Process.move(lotId, 'enter'))}>{tr('proc.arrive', { to: d.position.link_to_name })}</Button>}
          {!inTransit && d.next.map(n => (
            <Button key={n.link_id} size="sm" onClick={() => act(() => Process.move(lotId, n.tracked ? 'exit' : 'advance', n.link_id))}>
              {n.tracked ? tr('proc.depart', { to: n.to_name }) : tr('proc.next', { to: n.to_name })}
            </Button>
          ))}
          {!inTransit && d.next.length === 0 && (
            <>
              <div className="w-32"><Field label={tr('proc.outputKg')} type="number" value={outputKg} onChange={setOutputKg} /></div>
              <Button size="sm" className="self-end" disabled={!sig.by} onClick={() => act(() => Process.close(lotId, Number(outputKg) || 0, sig))}>
                {tr('proc.close')}
              </Button>
            </>
          )}
          <span className="ml-auto text-xs text-gray-500">{tr('proc.move.hint')}</span>
        </div>
      )}

      {/* ── 마감 뒤: 검토와 문서 ── */}
      {!running && (
        <div className="flex flex-wrap items-center gap-3 p-3 rounded-lg border border-gray-800">
          <span className="text-sm text-gray-300">{tr('proc.closedBy', { at: fmtTime(d.lot.closed_at), by: userName(d.lot.closed_by) })}</span>
          {d.lot.reviewed_by
            ? <span className="text-sm text-green-300">{tr('proc.reviewedBy', { at: fmtTime(d.lot.reviewed_at), by: userName(d.lot.reviewed_by) })}</span>
            : <Button size="sm" variant="outline" disabled={!sig.by} onClick={() => act(() => Process.review(lotId, sig))}>{tr('proc.review')}</Button>}
          <Button size="sm" disabled={!sig.by} onClick={async () => {
            try { const r = await Documents.issue('lot', lotId, sig); setIssued(r.document.doc_id); setErr(null); } catch (e) { setErr(errText(e)); }
          }}>{tr('proc.issueRecord')}</Button>
          {issued && <a className="text-sm text-sky-300 underline" href={Documents.fileUrl(issued)} target="_blank" rel="noreferrer">{tr('doc.openPdf')}</a>}
        </div>
      )}

      {/* ── 이탈 ── */}
      {d.deviations.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-sm text-gray-300 font-medium">
            {tr('proc.deviations')} ({d.deviations.length}){openDevs.length > 0 && <span className="ml-2 text-red-300">{tr('proc.deviations.open', { n: openDevs.length })}</span>}
          </h4>
          {d.deviations.map(dev => (
            <DeviationRow key={dev.dev_id} dev={dev} planned={d.ccps.find(k => k.ccp_id === dev.ccp_id)?.corrective_action ?? ''}
              userName={userName} canSign={!!sig.by}
              onClose={body => act(() => Process.closeDeviation(lotId, dev.dev_id, body, sig))} />
          ))}
        </div>
      )}

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-4">
        {/* ── CCP 판정 ── */}
        <div className="xl:col-span-2 grid grid-cols-1 md:grid-cols-2 gap-3 content-start">
          {d.ccps.map(k => <CCPCard key={k.ccp_id} ccp={k} running={running} canSign={!!sig.by}
            onCheck={(limitId, value) => act(() => Process.check(lotId, limitId, value, sig))} />)}
        </div>

        <div className="space-y-4">
          {/* ── 센서 ── */}
          <MockSensors siteId={siteId} sensors={sensors} now={d.now} onChanged={load} />
          {/* ── 단계 이동 기록 ── */}
          <div>
            <h4 className="text-sm text-gray-300 font-medium mb-1">{tr('proc.timeline')}</h4>
            <ul className="text-xs text-gray-400 space-y-0.5">
              {d.timeline.map((t, i) => (
                <li key={i}>
                  <span className="text-gray-200">{t.name}</span> · {fmtTime(t.entered_at)}
                  {t.exited_at ? ` → ${fmtTime(t.exited_at)} (${((t.exited_at - t.entered_at) / 60000).toFixed(1)}${tr('proc.min')})` : ` · ${tr('proc.flow.here')}`}
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </div>
  );
}

function CCPCard({ ccp, running, canSign, onCheck }: {
  ccp: LotCCP; running: boolean; canSign: boolean; onCheck: (limitId: string, value: number) => void;
}) {
  const { tr } = useLanguage();
  const [values, setValues] = useState<Record<string, string>>({});
  const bad = ccp.status === 'deviation' || ccp.status === 'outage';
  return (
    <div className={'p-3 rounded-lg border ' + (bad ? 'border-red-500/70 bg-red-500/5' : ccp.active ? 'border-sky-500/50' : 'border-gray-800')}>
      <div className="flex items-center gap-2 mb-2">
        <span className="text-sm text-white font-semibold">{ccp.code}</span>
        <span className="text-sm text-gray-300 truncate">{ccp.name}</span>
        <span className={'ml-auto shrink-0 px-1.5 py-0.5 rounded text-xs ' + STATUS_CLASS[ccp.status]}>{tr(`proc.status.${ccp.status}`)}</span>
      </div>
      {ccp.limits.map(l => {
        const r = l.result;
        const manual = l.source_kind === 'operator' || l.rule_kind === 'periodic_check';
        return (
          <div key={l.limit_id} className="py-1.5 border-t border-gray-800 first:border-t-0">
            <div className="flex items-baseline gap-2">
              <span className="text-xs text-gray-300">{l.item_label}</span>
              <span className="text-xs text-gray-500">{l.text}</span>
              <span className="ml-auto text-lg text-white font-mono tabular-nums">
                {l.rule_kind === 'cumulative_exposure' ? `${r.exposure_min.toFixed(1)} ${tr('proc.min')}` : r.last != null ? `${r.last} ${l.unit}` : '—'}
              </span>
            </div>
            <div className="flex flex-wrap gap-x-3 text-[11px] text-gray-500">
              <span>{tr(`plant.enum.${l.source_kind}`)}{l.sensor_id ? ` · ${l.sensor_id}` : ''}</span>
              <span>{tr('proc.count', { n: r.count })}</span>
              {r.min != null && <span>{tr('proc.minMax', { min: r.min, max: r.max ?? '' })}</span>}
            </div>
            {r.detail && <p className={'text-xs ' + (r.status === 'deviation' || r.status === 'outage' ? 'text-red-300' : 'text-amber-200')}>{r.detail}</p>}
            {manual && running && r.active && (
              <div className="flex items-end gap-2 mt-1">
                <div className="w-28">
                  <Field label={l.rule_kind === 'periodic_check' ? tr('proc.checkValue') : `${l.item_label} (${l.unit})`} type="number"
                    value={values[l.limit_id] ?? ''} onChange={v => setValues(prev => ({ ...prev, [l.limit_id]: v }))} />
                </div>
                <Button size="sm" variant="outline" disabled={!canSign || (values[l.limit_id] ?? '') === ''}
                  onClick={() => { onCheck(l.limit_id, Number(values[l.limit_id])); setValues(prev => ({ ...prev, [l.limit_id]: '' })); }}>
                  {tr('proc.record')}
                </Button>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function DeviationRow({ dev, planned, userName, canSign, onClose }: {
  dev: LotDeviation; planned: string; userName: (id: string) => string; canSign: boolean;
  onClose: (body: { corrective_action: string; disposition: string }) => void;
}) {
  const { tr } = useLanguage();
  const [action, setAction] = useState('');
  const [disposition, setDisposition] = useState('');
  const open = dev.status === 'open';
  return (
    <div className={'p-3 rounded-lg border ' + (open ? 'border-red-500/60 bg-red-500/5' : 'border-gray-800')}>
      <div className="flex flex-wrap items-center gap-x-3 text-sm">
        <span className="px-1.5 py-0.5 rounded bg-red-600 text-white text-xs">{dev.ccp_code}</span>
        <span className="text-gray-200">{tr(`proc.devKind.${dev.kind}`)}</span>
        <span className="text-gray-400">{dev.detail}</span>
        <span className="text-xs text-gray-500">{fmtTime(dev.detected_at)}</span>
        <span className={'ml-auto text-xs ' + (open ? 'text-red-300' : 'text-green-300')}>{tr(`proc.devStatus.${dev.status}`)}</span>
      </div>
      {open ? (
        <div className="mt-2 space-y-2">
          {planned && <p className="text-xs text-gray-500">{tr('proc.dev.planned')}: {planned}</p>}
          <div className="grid grid-cols-1 md:grid-cols-[1fr_1fr_auto] gap-2 items-end">
            <Field label={tr('proc.dev.action')} value={action} onChange={setAction} />
            <Field label={tr('proc.dev.disposition')} value={disposition} onChange={setDisposition} />
            <Button size="sm" disabled={!canSign} onClick={() => onClose({ corrective_action: action, disposition })}>{tr('proc.dev.close')}</Button>
          </div>
        </div>
      ) : (
        <p className="mt-1 text-xs text-gray-400">
          {tr('proc.dev.action')}: {dev.corrective_action} · {tr('proc.dev.disposition')}: {dev.disposition} · {userName(dev.closed_by)} {fmtTime(dev.closed_at)}
        </p>
      )}
    </div>
  );
}

// 센서 — 최근 값. 모의 센서는 값을 지정하거나 끊어서 이탈·모니터링 불능을 시험할 수 있다.
export function MockSensors({ siteId, sensors, now, onChanged }: {
  siteId: string; sensors: SensorReading[]; now: number; onChanged: () => void;
}) {
  const { tr } = useLanguage();
  const [values, setValues] = useState<Record<string, string>>({});
  const [err, setErr] = useState<string | null>(null);
  const set = async (sensorId: string, mode: 'fixed' | 'outage' | 'normal') => {
    try {
      await Plant.setMock(siteId, { sensor_id: sensorId, mode, value: Number(values[sensorId]) || 0, minutes: 10 });
      setErr(null); onChanged();
    } catch (e) { setErr(errText(e)); }
  };
  return (
    <div>
      <h4 className="text-sm text-gray-300 font-medium mb-1">{tr('proc.sensors')}</h4>
      <p className="text-xs text-gray-500 mb-2">{tr('proc.sensors.hint')}</p>
      <div className="space-y-2">
        {sensors.map(s => {
          const age = s.latest ? Math.round((now - s.latest.at) / 1000) : null;
          return (
            <div key={s.sensor_id} className="p-2 rounded border border-gray-800">
              <div className="flex items-baseline gap-2">
                <span className="text-xs text-gray-300 font-mono truncate">{s.sensor_id}</span>
                {s.is_mock && <span className="text-[10px] px-1 rounded bg-gray-700 text-gray-300">{tr('plant.sensor.mock')}</span>}
                {s.mock && <span className="text-[10px] px-1 rounded bg-amber-500/30 text-amber-200">{tr(`proc.mock.${s.mock.mode}`)}</span>}
                <span className="ml-auto text-base text-white font-mono tabular-nums">{s.latest ? `${s.latest.value} ${s.unit}` : '—'}</span>
              </div>
              <div className="flex items-center gap-1.5 mt-1">
                <span className={'text-[11px] ' + (age != null && age > 30 ? 'text-red-300' : 'text-gray-500')}>
                  {age != null ? tr('proc.sensor.age', { s: age }) : tr('proc.sensor.none')}
                </span>
                {s.is_mock && (
                  <div className="ml-auto flex items-center gap-1">
                    <input type="number" value={values[s.sensor_id] ?? ''} placeholder={s.unit}
                      onChange={e => setValues(prev => ({ ...prev, [s.sensor_id]: e.target.value }))}
                      className="h-7 w-16 px-2 rounded border border-gray-700 bg-gray-900 text-xs text-white" />
                    <Button size="sm" variant="outline" disabled={(values[s.sensor_id] ?? '') === ''} onClick={() => set(s.sensor_id, 'fixed')}>{tr('proc.mock.setValue')}</Button>
                    <Button size="sm" variant="outline" onClick={() => set(s.sensor_id, 'outage')}>{tr('proc.mock.cut')}</Button>
                    <Button size="sm" variant="ghost" onClick={() => set(s.sensor_id, 'normal')}>{tr('proc.mock.normal')}</Button>
                  </div>
                )}
              </div>
            </div>
          );
        })}
        {sensors.length === 0 && <p className="text-xs text-gray-500">{tr('proc.sensor.noSensors')}</p>}
      </div>
      {err && <p className="text-xs text-red-400 font-mono mt-1">{err}</p>}
    </div>
  );
}
