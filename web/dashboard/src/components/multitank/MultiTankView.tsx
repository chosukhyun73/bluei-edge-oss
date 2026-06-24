import { useState, useEffect, useRef } from 'react';
import type { Farm, Site, WaterTreatmentGroup, Actuator, Sensor, Tank, PredictiveForecast, OperatorIntent } from '../../lib/types';
import { Farms, Sites, WTGs, Actuators, SensorDevices, Tanks, Predictive, OperatorIntents } from '../../lib/api';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';
import { EnvironmentalCard } from './EnvironmentalCard';
import { InlineSensorForm, InlineActuatorForm, InlineWtgForm } from '../sections/TankSettingsSection';
import { useLanguage } from '../../lib/language-context';

// ── Helpers ──────────────────────────────────────────────────────────────────

function EmptyState() {
  const { tr } = useLanguage();
  return (
    <p className="text-sm text-gray-500 py-8 text-center">
      {tr('multiTankView.emptyState')}
    </p>
  );
}

function ErrorState({ message }: { message: string }) {
  return (
    <div className="px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive font-mono">
      {message}
    </div>
  );
}

// ── Forecast card ─────────────────────────────────────────────────────────────

function ForecastCard({ wtgId }: { wtgId: string }) {
  const { tr } = useLanguage();
  const [data, setData] = useState<PredictiveForecast | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    setLoading(true);
    setData(null);
    setError(null);

    const fetchForecast = () => {
      Predictive.forecast(wtgId)
        .then(d => { setData(d); setLoading(false); })
        .catch((err: unknown) => {
          setError(err instanceof Error ? err.message : tr('multiTankView.unknownError'));
          setLoading(false);
        });
    };

    fetchForecast();
    intervalRef.current = setInterval(fetchForecast, 10_000);
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, [wtgId]);

  const statusBadge = (s: PredictiveForecast['status']) => {
    if (s === 'ok') return 'bg-green-500/20 text-green-300 border-green-500/40';
    if (s === 'caution') return 'bg-amber-500/20 text-amber-300 border-amber-500/40';
    return 'bg-red-500/20 text-red-300 border-red-500/40';
  };

  const statusLabel = (s: PredictiveForecast['status']) => {
    if (s === 'ok') return tr('multiTankView.forecast.statusOk');
    if (s === 'caution') return tr('multiTankView.forecast.statusCaution');
    return tr('multiTankView.forecast.statusOver');
  };

  return (
    <div className="p-3 bg-gray-800/60 border border-gray-700/50 rounded-lg mb-4">
      <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">
        {tr('multiTankView.forecast.title')}
      </p>
      {loading && <Skeleton className="h-16 w-full" />}
      {error && (
        <p className="text-xs text-gray-500 font-mono">{tr('multiTankView.forecast.noData')}: {error}</p>
      )}
      {!loading && !error && data && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-400">{tr('multiTankView.forecast.statusLabel')}:</span>
            <span
              className={[
                'inline-flex items-center px-2 py-0.5 rounded border text-xs font-semibold',
                statusBadge(data.status),
              ].join(' ')}
            >
              {statusLabel(data.status)}
            </span>
          </div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
            <div className="flex justify-between">
              <span className="text-gray-500">Capacity</span>
              <span className="font-mono text-gray-300">{data.capacity_kg_per_h.toFixed(3)} kg/h</span>
            </div>
            <div className="flex justify-between">
              <span className="text-gray-500">Recent load</span>
              <span className="font-mono text-gray-300">{data.recent_load_kg_per_h.toFixed(3)} kg/h</span>
            </div>
            <div className="flex justify-between">
              <span className="text-gray-500">Headroom</span>
              <span className="font-mono text-gray-300">{data.headroom_kg_per_h.toFixed(3)} kg/h</span>
            </div>
            <div className="flex justify-between">
              <span className="text-gray-500">Caution thresh.</span>
              <span className="font-mono text-gray-300">{data.threshold.toFixed(3)} kg/h</span>
            </div>
          </div>
          {/* Load bar */}
          <div className="mt-1">
            <div className="h-2 rounded-full bg-gray-700 overflow-hidden">
              <div
                className={[
                  'h-full rounded-full transition-all duration-500',
                  data.status === 'ok'
                    ? 'bg-green-500'
                    : data.status === 'caution'
                    ? 'bg-amber-500'
                    : 'bg-red-500',
                ].join(' ')}
                style={{
                  width: `${Math.min(100, data.capacity_kg_per_h > 0
                    ? (data.recent_load_kg_per_h / data.capacity_kg_per_h) * 100
                    : 0)}%`,
                }}
              />
            </div>
            <p className="text-xs text-gray-600 mt-0.5 text-right">
              {data.capacity_kg_per_h > 0
                ? ((data.recent_load_kg_per_h / data.capacity_kg_per_h) * 100).toFixed(1)
                : '0.0'}% {tr('multiTankView.forecast.used')}
            </p>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Lifecycle chip ────────────────────────────────────────────────────────────

function LifecycleChip({ stage }: { stage: string }) {
  let cls = '';
  if (stage === 'fry') cls = 'bg-cyan-500/15 text-cyan-300 border-cyan-500/30';
  else if (stage === 'fingerling') cls = 'bg-blue-500/15 text-blue-300 border-blue-500/30';
  else if (stage === 'growout') cls = 'bg-green-500/15 text-green-300 border-green-500/30';
  else cls = 'bg-gray-500/15 text-gray-400 border-gray-500/30';
  return (
    <span className={['inline-flex items-center px-1.5 py-0.5 rounded border text-xs font-medium', cls].join(' ')}>
      {stage}
    </span>
  );
}

// ── WTG Detail panel ─────────────────────────────────────────────────────────

interface WTGDetailProps {
  wtg: WaterTreatmentGroup;
}

function WTGDetail({ wtg }: WTGDetailProps) {
  const { tr } = useLanguage();
  const [actuators, setActuators] = useState<Actuator[]>([]);
  const [sensors, setSensors] = useState<Sensor[]>([]);
  const [wtgTanks, setWtgTanks] = useState<Tank[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // 최근 운영자 의도 (WTG 내 첫 번째 Cage/Tank 기준으로 최대 5건)
  const [recentIntents, setRecentIntents] = useState<OperatorIntent[]>([]);
  // C-13a — WTGDetail 안에서 inline 센서 등록.
  const [showAddSensor, setShowAddSensor] = useState(false);
  const [showAddActuator, setShowAddActuator] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);

  // backend 가 빈 리스트를 null 로 직렬화하는 경우 + 운영자가 등록 시 비워 둔
  // 경우에 대비해 정규화 (yaml seed 와 POST 양쪽 모두 nil slice 가능).
  const tankIds: string[] = Array.isArray(wtg.tank_ids) ? wtg.tank_ids : [];
  const sharedEquipment: string[] = Array.isArray(wtg.shared_equipment) ? wtg.shared_equipment : [];

  useEffect(() => {
    setLoading(true);
    setError(null);
    Promise.all([
      Actuators.list({ wtgId: wtg.wtg_id }),
      SensorDevices.list({ wtgId: wtg.wtg_id }),
      Tanks.list({ wtgId: wtg.wtg_id }),
    ])
      .then(([aRes, sRes, tRes]) => {
        setActuators(aRes.items);
        setSensors(sRes.items);
        setWtgTanks(tRes.items);
        // WTG 내 첫 번째 Cage/Tank 의도 로드 (실패 시 무시)
        const firstTankId = tRes.items[0]?.tank_id ?? tankIds[0];
        if (firstTankId) {
          OperatorIntents.list(firstTankId, 5)
            .then(r => setRecentIntents(r.items))
            .catch(() => { /* non-fatal */ });
        }
      })
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : tr('multiTankView.unknownError');
        setError(msg);
      })
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wtg.wtg_id, reloadKey]);

  // Build a map for quick lookup of tank details by id
  const tankMap = new Map(wtgTanks.map(t => [t.tank_id, t]));

  return (
    <div className="space-y-4">
      {/* 수질 예측 forecast card */}
      <ForecastCard wtgId={wtg.wtg_id} />

      <div>
        <h3 className="text-base font-semibold text-white mb-1">{wtg.name}</h3>
        <p className="text-xs text-gray-400 font-mono">{wtg.wtg_id}</p>
      </div>

      {/* Cage/Tank 목록 */}
      <div>
        <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">Cage / Tank ({tankIds.length})</p>
        {tankIds.length === 0 ? (
          <p className="text-sm text-gray-500">{tr('multiTankView.wtgDetail.noCageTank')}</p>
        ) : (
          <div className="flex flex-col gap-2">
            {tankIds.map(tid => {
              const tank = tankMap.get(tid);
              return (
                <div
                  key={tid}
                  className="flex flex-wrap items-center gap-2 px-2 py-1.5 bg-green-500/5 border border-green-500/20 rounded"
                >
                  <span className="text-xs text-green-300 font-mono">{tid}</span>
                  {tank?.lifecycle_stage && (
                    <LifecycleChip stage={tank.lifecycle_stage} />
                  )}
                  {tank?.lot_no && (
                    <span className="text-xs text-gray-500 font-mono">
                      {tr('multiTankView.lot', { n: tank.lot_no })}
                    </span>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 공유 장비 */}
      {sharedEquipment.length > 0 && (
        <div>
          <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">{tr('multiTankView.wtgDetail.sharedEquipment')}</p>
          <div className="flex flex-wrap gap-2">
            {sharedEquipment.map(eq => (
              <span
                key={eq}
                className="px-2 py-1 text-xs bg-blue-500/10 border border-blue-500/20 rounded text-blue-300 font-mono"
              >
                {eq}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* 인입/배출 센서 */}
      <div className="grid grid-cols-2 gap-3">
        <div className="p-3 bg-gray-800/50 rounded-lg border border-gray-700/50">
          <p className="text-xs text-gray-500 mb-1">{tr('multiTankView.wtgDetail.intakeSensor')}</p>
          <p className="text-sm text-gray-300 font-mono truncate">
            {wtg.intake_sensor_id ?? '—'}
          </p>
        </div>
        <div className="p-3 bg-gray-800/50 rounded-lg border border-gray-700/50">
          <p className="text-xs text-gray-500 mb-1">{tr('multiTankView.wtgDetail.outletSensor')}</p>
          <p className="text-sm text-gray-300 font-mono truncate">
            {wtg.outlet_sensor_id ?? '—'}
          </p>
        </div>
      </div>

      {/* 용량 */}
      {wtg.capacity != null && (
        <div className="p-3 bg-gray-800/50 rounded-lg border border-gray-700/50">
          <p className="text-xs text-gray-500 mb-1">{tr('multiTankView.wtgDetail.capacity')}</p>
          <p className="text-sm text-gray-200 font-mono">{wtg.capacity.toLocaleString()} m³</p>
        </div>
      )}

      {loading && (
        <div className="space-y-2">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      )}

      {error && <ErrorState message={`${tr('multiTankView.wtgDetail.deviceLoadFail')}: ${error}`} />}

      {!loading && !error && (
        <>
          {/* 액추에이터 목록 + C-13b inline 등록 진입점 */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs text-gray-500 uppercase tracking-wide">
                {tr('multiTankView.wtgDetail.actuators')} ({actuators.length})
              </p>
              <button
                type="button"
                onClick={() => setShowAddActuator(v => !v)}
                className="text-xs px-2 py-0.5 rounded border border-green-500/40 text-green-400 hover:bg-green-500/10"
              >
                {showAddActuator ? tr('multiTankView.closeForm') : tr('multiTankView.wtgDetail.addActuator')}
              </button>
            </div>
            {actuators.length === 0 && !showAddActuator && (
              <p className="text-xs text-gray-500 italic">{tr('multiTankView.wtgDetail.noActuators')}</p>
            )}
            {actuators.length > 0 && (
              <div className="space-y-1">
                {actuators.map(a => (
                  <div
                    key={a.device_id}
                    className="flex items-center justify-between px-3 py-2 bg-gray-800/40 rounded border border-gray-700/30 text-xs"
                  >
                    <span className="text-gray-300 font-mono">{a.device_id}</span>
                    <span className="text-gray-500">{a.device_type}</span>
                  </div>
                ))}
              </div>
            )}
            {showAddActuator && (
              <div className="mt-2">
                <InlineActuatorForm
                  wtgId={wtg.wtg_id}
                  onSaved={() => {
                    setShowAddActuator(false);
                    setReloadKey(k => k + 1);
                  }}
                  onCancel={() => setShowAddActuator(false)}
                />
              </div>
            )}
          </div>


          {/* 센서 목록 + C-13a inline 등록 진입점 */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs text-gray-500 uppercase tracking-wide">
                {tr('multiTankView.wtgDetail.sensors')} ({sensors.length})
              </p>
              <button
                type="button"
                onClick={() => setShowAddSensor(v => !v)}
                className="text-xs px-2 py-0.5 rounded border border-green-500/40 text-green-400 hover:bg-green-500/10"
              >
                {showAddSensor ? tr('multiTankView.closeForm') : tr('multiTankView.wtgDetail.addSensor')}
              </button>
            </div>
            {sensors.length === 0 && !showAddSensor && (
              <p className="text-xs text-gray-500 italic">{tr('multiTankView.wtgDetail.noSensors')}</p>
            )}
            {sensors.length > 0 && (
              <div className="space-y-1">
                {sensors.map(s => (
                  <div
                    key={s.sensor_id}
                    className="flex items-center justify-between px-3 py-2 bg-gray-800/40 rounded border border-gray-700/30 text-xs"
                  >
                    <span className="text-gray-300 font-mono">{s.sensor_id}</span>
                    <span className="text-gray-500">{s.sensor_type}</span>
                  </div>
                ))}
              </div>
            )}
            {showAddSensor && (
              <div className="mt-2">
                <InlineSensorForm
                  wtgId={wtg.wtg_id}
                  onSaved={() => {
                    setShowAddSensor(false);
                    setReloadKey(k => k + 1);
                  }}
                  onCancel={() => setShowAddSensor(false)}
                />
              </div>
            )}
          </div>
        </>
      )}

      {/* 최근 운영자 의도 타임라인 (Phase 5) */}
      {recentIntents.length > 0 && (
        <div>
          <p className="text-xs text-gray-500 uppercase tracking-wide mb-2">{tr('multiTankView.wtgDetail.recentIntents')}</p>
          <div className="space-y-1.5">
            {recentIntents.map(intent => (
              <div
                key={intent.intent_id}
                className="px-3 py-2 bg-blue-500/5 border border-blue-500/15 rounded text-xs"
              >
                <div className="flex items-center gap-2 mb-0.5">
                  <span className="text-blue-400 font-mono text-[10px]">{intent.intent_type}</span>
                  <span className="text-gray-600 font-mono text-[10px]">
                    {new Date(intent.recorded_at).toLocaleString('ko-KR', {
                      month: '2-digit', day: '2-digit',
                      hour: '2-digit', minute: '2-digit',
                    })}
                  </span>
                </div>
                <p className="text-gray-300 leading-relaxed">{intent.reason}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ── WTG Card ─────────────────────────────────────────────────────────────────

interface WTGCardProps {
  wtg: WaterTreatmentGroup;
  selected: boolean;
  onClick: () => void;
}

function WTGCard({ wtg, selected, onClick }: WTGCardProps) {
  const { tr } = useLanguage();
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className={[
        'w-full text-left p-4 rounded-xl border transition-all',
        selected
          ? 'bg-green-500/10 border-green-500/50 shadow-lg shadow-green-500/10'
          : 'bg-gray-900/60 border-gray-700/50 hover:border-gray-600/70 hover:bg-gray-800/60',
      ].join(' ')}
    >
      <p className="text-sm font-semibold text-white truncate">{wtg.name}</p>
      <p className="text-xs text-gray-400 font-mono mt-0.5 truncate">{wtg.wtg_id}</p>
      <div className="flex items-center gap-3 mt-2 text-xs text-gray-500">
        <span>{tr('multiTankView.wtgCard.cageTankCount', { n: (Array.isArray(wtg.tank_ids) ? wtg.tank_ids : []).length })}</span>
        {typeof wtg.capacity === 'number' && <span>{tr('multiTankView.wtgCard.capacity')}: {wtg.capacity.toLocaleString()} m³</span>}
      </div>
    </button>
  );
}

// ── Main MultiTankView ────────────────────────────────────────────────────────

export function MultiTankView() {
  const { tr } = useLanguage();
  const [farms, setFarms] = useState<Farm[]>([]);
  const [sites, setSites] = useState<Site[]>([]);
  const [wtgs, setWtgs] = useState<WaterTreatmentGroup[]>([]);
  const [selectedFarmId, setSelectedFarmId] = useState<string>('');
  const [selectedSiteId, setSelectedSiteId] = useState<string>('');
  const [selectedWtgId, setSelectedWtgId] = useState<string | null>(null);

  const [farmsLoading, setFarmsLoading] = useState(true);
  const [sitesLoading, setSitesLoading] = useState(false);
  const [wtgsLoading, setWtgsLoading] = useState(false);
  const [farmsError, setFarmsError] = useState<string | null>(null);
  const [sitesError, setSitesError] = useState<string | null>(null);
  const [wtgsError, setWtgsError] = useState<string | null>(null);

  // 농장 목록 로드
  useEffect(() => {
    setFarmsLoading(true);
    Farms.list()
      .then(r => {
        setFarms(r.items);
        if (r.items.length > 0) setSelectedFarmId(r.items[0].farm_id);
      })
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : tr('multiTankView.unknownError');
        setFarmsError(msg);
      })
      .finally(() => setFarmsLoading(false));
  }, []);

  // 선택된 농장에 따른 사이트 로드
  useEffect(() => {
    if (!selectedFarmId) { setSites([]); setSelectedSiteId(''); return; }
    setSitesLoading(true);
    setSitesError(null);
    setSelectedSiteId('');
    Sites.list(selectedFarmId)
      .then(r => {
        setSites(r.items);
        if (r.items.length > 0) setSelectedSiteId(r.items[0].site_id);
      })
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : tr('multiTankView.unknownError');
        setSitesError(msg);
      })
      .finally(() => setSitesLoading(false));
  }, [selectedFarmId]);

  // 위임 2: Site 단계 inline WTG 등록 — 등록 시 wtgsReloadKey 증가로 list 재로드.
  const [showAddWtg, setShowAddWtg] = useState(false);
  const [wtgsReloadKey, setWtgsReloadKey] = useState(0);

  // 선택된 사이트가 바뀌면 WTG 선택 + inline form 상태도 초기화.
  useEffect(() => {
    setSelectedWtgId(null);
    setShowAddWtg(false);
  }, [selectedSiteId]);

  // 선택된 사이트에 따른 WTG 로드 (site 변경 + WTG 등록 후 reload 둘 다 트리거).
  useEffect(() => {
    if (!selectedSiteId) { setWtgs([]); return; }
    setWtgsLoading(true);
    setWtgsError(null);
    WTGs.list(selectedSiteId)
      .then(r => setWtgs(r.items))
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : tr('multiTankView.unknownError');
        setWtgsError(msg);
      })
      .finally(() => setWtgsLoading(false));
  }, [selectedSiteId, wtgsReloadKey]);

  const selectedWtg = wtgs.find(w => w.wtg_id === selectedWtgId) ?? null;
  const selectedSite = sites.find(s => s.site_id === selectedSiteId) ?? null;
  // 해상(marine) site 에만 환경 카드 표시 — 강릉 RAS (land) 에는 비표시.
  const showEnvironmental = selectedSite?.site_type === 'marine';

  return (
    <div className="space-y-5">
      {/* Farm / Site 선택 */}
      <Card className="bg-gray-900/60 border-gray-700/50">
        <CardContent className="pt-4 pb-4">
          <div className="flex flex-wrap gap-4 items-end">
            {/* Farm 드롭다운 */}
            <div className="flex flex-col gap-1.5">
              <label htmlFor="farm-select" className="text-xs text-gray-400 font-medium">{tr('multiTankView.main.farm')}</label>
              {farmsLoading ? (
                <Skeleton className="h-9 w-52" />
              ) : farmsError ? (
                <span className="text-xs text-destructive font-mono">{tr('multiTankView.main.loadFail')}: {farmsError}</span>
              ) : (
                <select
                  id="farm-select"
                  value={selectedFarmId}
                  onChange={e => setSelectedFarmId(e.target.value)}
                  className="h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50 min-w-[13rem]"
                  aria-label={tr('multiTankView.main.farmAriaLabel')}
                >
                  {farms.length === 0 && <option value="">{tr('multiTankView.main.noFarms')}</option>}
                  {farms.map(f => (
                    <option key={f.farm_id} value={f.farm_id}>
                      {f.operator} ({f.license_no})
                    </option>
                  ))}
                </select>
              )}
            </div>

            {/* Site 드롭다운 */}
            <div className="flex flex-col gap-1.5">
              <label htmlFor="site-select" className="text-xs text-gray-400 font-medium">{tr('multiTankView.main.site')}</label>
              {sitesLoading ? (
                <Skeleton className="h-9 w-52" />
              ) : sitesError ? (
                <span className="text-xs text-destructive font-mono">{tr('multiTankView.main.loadFail')}: {sitesError}</span>
              ) : (
                <select
                  id="site-select"
                  value={selectedSiteId}
                  onChange={e => setSelectedSiteId(e.target.value)}
                  disabled={sites.length === 0}
                  className="h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50 min-w-[13rem] disabled:opacity-50"
                  aria-label={tr('multiTankView.main.siteAriaLabel')}
                >
                  {sites.length === 0 && <option value="">{tr('multiTankView.main.noSites')}</option>}
                  {sites.map(s => (
                    <option key={s.site_id} value={s.site_id}>
                      {s.name} ({s.site_type === 'land' ? tr('multiTankView.main.land') : tr('multiTankView.main.marine')})
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 해상 환경 카드 (C-3w) — marine site 일 때만 */}
      {showEnvironmental && selectedSiteId && (
        <EnvironmentalCard siteId={selectedSiteId} />
      )}

      {/* WTG 목록 + 상세 */}
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-5">
        {/* 왼쪽: WTG 카드 목록 */}
        <div className="lg:col-span-2 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
              {tr('multiTankView.main.wtgGroupTitle')}
            </h2>
            {selectedSiteId && (
              <button
                type="button"
                onClick={() => setShowAddWtg(v => !v)}
                className="text-xs px-2 py-0.5 rounded border border-green-500/40 text-green-400 hover:bg-green-500/10"
              >
                {showAddWtg ? tr('multiTankView.closeForm') : tr('multiTankView.main.addWtg')}
              </button>
            )}
          </div>

          {/* 위임 2: Site 단계 inline WTG 등록 진입점 */}
          {selectedSiteId && showAddWtg && (
            <InlineWtgForm
              siteId={selectedSiteId}
              onSaved={() => {
                setShowAddWtg(false);
                setWtgsReloadKey(k => k + 1);
              }}
              onCancel={() => setShowAddWtg(false)}
            />
          )}

          {wtgsLoading && (
            <div className="space-y-3">
              <Skeleton className="h-20 w-full rounded-xl" />
              <Skeleton className="h-20 w-full rounded-xl" />
              <Skeleton className="h-20 w-full rounded-xl" />
            </div>
          )}

          {wtgsError && <ErrorState message={`${tr('multiTankView.main.wtgLoadFail')}: ${wtgsError}`} />}

          {!wtgsLoading && !wtgsError && wtgs.length === 0 && selectedSiteId && !showAddWtg && (
            <EmptyState />
          )}

          {!wtgsLoading && !wtgsError && !selectedSiteId && (
            <p className="text-sm text-gray-500 py-4 text-center">{tr('multiTankView.main.selectSite')}</p>
          )}

          {!wtgsLoading && !wtgsError && wtgs.map(wtg => (
            <WTGCard
              key={wtg.wtg_id}
              wtg={wtg}
              selected={wtg.wtg_id === selectedWtgId}
              onClick={() => setSelectedWtgId(
                wtg.wtg_id === selectedWtgId ? null : wtg.wtg_id
              )}
            />
          ))}
        </div>

        {/* 오른쪽: WTG 상세 */}
        <div className="lg:col-span-3">
          {selectedWtg ? (
            <Card className="bg-gray-900/60 border-gray-700/50">
              <CardHeader>
                <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
                  {tr('multiTankView.main.wtgDetail')}
                </CardTitle>
              </CardHeader>
              <CardContent className="pb-5">
                <WTGDetail wtg={selectedWtg} />
              </CardContent>
            </Card>
          ) : (
            <div className="h-full flex items-center justify-center min-h-[200px]">
              <p className="text-sm text-gray-600">
                {tr('multiTankView.main.selectWtg')}
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
