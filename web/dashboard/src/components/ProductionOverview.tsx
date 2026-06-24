import { useEffect, useState } from 'react';
import type { Group, Tank, TankLifecycleResponse, WeightHistoryResponse } from '../lib/types';
import { Groups, Tanks } from '../lib/api';
import { useLanguage } from '../lib/language-context';
import { Card, CardHeader, CardTitle, CardContent } from './ui/card';
import { Skeleton } from './ui/skeleton';
import { FeedQuickControl } from './multitank/FeedQuickControl';

type TankSnapshot = {
  tank: Tank;
  lifecycle: TankLifecycleResponse['current'];
  latest: WeightHistoryResponse['snapshots'][number] | null;
};

type GroupBlock = {
  group: Group;
  tanks: TankSnapshot[];
};

function daysSince(iso: string): number {
  const ms = Date.now() - new Date(iso).getTime();
  return Math.max(0, Math.floor(ms / 86_400_000));
}

function daysUntil(iso: string): number {
  const ms = new Date(iso).getTime() - Date.now();
  return Math.ceil(ms / 86_400_000);
}

function fmtNumber(n: number, digits = 0): string {
  return n.toLocaleString('ko-KR', { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

function progressBarColor(pct: number): string {
  if (pct >= 100) return 'bg-amber-400';
  if (pct >= 90) return 'bg-blue-400';
  if (pct >= 50) return 'bg-green-500';
  return 'bg-gray-500';
}

function TankCard({ snap }: { snap: TankSnapshot }) {
  const { tr } = useLanguage();
  const { tank, lifecycle, latest } = snap;
  const displayName = tank.display_name || tank.tank_id;

  // backend state-vector 와 동일한 fallback: lifecycle 없으면 tank_profile 사용.
  // 실 운영자 입식 등록 후엔 lifecycle 우선 (자동 전환).
  if (!lifecycle) {
    const hasProfile =
      (tank.fish_count != null && tank.fish_count > 0) ||
      (tank.avg_weight_g != null && tank.avg_weight_g > 0) ||
      (tank.biomass_kg != null && tank.biomass_kg > 0);

    if (!hasProfile) {
      return (
        <div className="p-3 bg-gray-900/40 border border-dashed border-gray-700/60 rounded-lg space-y-2">
          <div className="flex items-baseline justify-between">
            <span className="text-sm font-medium text-gray-400">{displayName}</span>
            <span className="text-xs text-gray-600">{tr('tankCard.vacant')}</span>
          </div>
          <p className="text-xs text-gray-600 italic">{tr('tankCard.waitingForStocking')}</p>
          <p className="text-xs text-gray-700">{tr('tankCard.registerStockingInSettings')}</p>
          <FeedQuickControl tank={tank} variant="compact" />
        </div>
      );
    }

    return (
      <div className="p-3 bg-gray-800/60 border border-gray-700/50 rounded-lg space-y-2">
        <div className="flex items-baseline justify-between gap-2">
          <span className="text-sm font-medium text-white">
            {displayName}
            {tank.species && (
              <span className="text-xs text-gray-500 ml-1.5">({tank.species})</span>
            )}
          </span>
          <span
            className="text-xs font-mono px-1.5 py-0.5 rounded border bg-amber-500/10 border-amber-500/30 text-amber-300"
            title={tr('tankCard.tankProfileTooltip')}
          >
            TankProfile
          </span>
        </div>

        <div className="text-xs text-gray-500 font-mono">
          {tank.lot_no ?? '—'}
          {tank.lifecycle_stage && <span className="ml-1">· {tank.lifecycle_stage}</span>}
        </div>

        <div className="text-xs text-gray-400">
          {tr('tankCard.fishCount')}{' '}
          <span className="text-gray-200 font-mono">
            {tank.fish_count != null ? fmtNumber(tank.fish_count) : '—'}
          </span>
        </div>

        <div className="flex items-baseline gap-3 text-xs">
          {tank.avg_weight_g != null && (
            <span className="text-gray-400">
              {tr('tankCard.avgWeight')} <span className="text-gray-200 font-mono">{fmtNumber(tank.avg_weight_g, 0)}g</span>
            </span>
          )}
          {tank.biomass_kg != null && (
            <span className="text-gray-400">
              {tr('tankCard.biomass')} <span className="text-gray-200 font-mono">{fmtNumber(tank.biomass_kg, 1)}kg</span>
            </span>
          )}
        </div>

        <p className="text-xs text-gray-600 italic">
          {tr('tankCard.lifecycleAutoSwitchHint')}
        </p>

        <FeedQuickControl tank={tank} variant="compact" />
      </div>
    );
  }

  const initialCount = lifecycle.initial_count;
  const initialWeight = lifecycle.initial_avg_weight_g;
  const targetWeight = lifecycle.target_harvest_weight_g;
  const targetDate = lifecycle.target_harvest_date;
  const currentWeight = latest?.estimated_avg_weight_g ?? initialWeight;
  const ageDays = daysSince(lifecycle.stocked_at);
  const dgr = ageDays > 0 ? (currentWeight - initialWeight) / ageDays : 0;
  const fcr = latest?.expected_fcr;
  const cumFeedKg = latest ? latest.cumulative_feed_g / 1000 : null;

  const progressPct = targetWeight && targetWeight > 0
    ? Math.min(999, (currentWeight / targetWeight) * 100)
    : null;

  let dHarvest: number | null = null;
  if (targetDate) dHarvest = daysUntil(targetDate);
  const harvestImminent = dHarvest !== null && dHarvest <= 7 && dHarvest >= 0;
  const harvestOverdue = dHarvest !== null && dHarvest < 0;

  return (
    <div className="p-3 bg-gray-800/60 border border-gray-700/50 rounded-lg space-y-2">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-sm font-medium text-white">
          {displayName}
          <span className="text-xs text-gray-500 ml-1.5">({lifecycle.species})</span>
        </span>
        {dHarvest !== null && (
          <span
            className={
              harvestImminent
                ? 'text-xs font-mono text-red-400 font-semibold'
                : harvestOverdue
                  ? 'text-xs font-mono text-red-500 font-semibold'
                  : 'text-xs font-mono text-gray-400'
            }
          >
            {harvestImminent && '⚠ '}
            {harvestOverdue ? `D+${Math.abs(dHarvest)} ${tr('tankCard.harvestOverdue')}` : `D-${dHarvest}`}
          </span>
        )}
      </div>

      <div className="text-xs text-gray-500 font-mono">
        {tank.lot_no || lifecycle.active_stocking_id.slice(0, 12)} · D+{ageDays}
      </div>

      <div className="text-xs text-gray-400">
        {tr('tankCard.fishCount')} <span className="text-gray-200 font-mono">{fmtNumber(initialCount)}</span>
      </div>

      <div>
        <div className="flex items-baseline justify-between text-xs">
          <span className="text-gray-400">
            <span className="text-gray-200 font-mono">{fmtNumber(currentWeight, 0)}g</span>
            {targetWeight && (
              <>
                <span className="text-gray-600 mx-1">→</span>
                <span className="text-gray-500 font-mono">{fmtNumber(targetWeight, 0)}g</span>
              </>
            )}
          </span>
          {progressPct !== null && (
            <span className="text-gray-300 font-mono">{progressPct.toFixed(0)}%</span>
          )}
        </div>
        {progressPct !== null && (
          <div className="mt-1 h-1.5 w-full bg-gray-700/60 rounded-full overflow-hidden">
            <div
              className={`h-full ${progressBarColor(progressPct)} transition-all`}
              style={{ width: `${Math.min(100, progressPct)}%` }}
            />
          </div>
        )}
      </div>

      <div className="flex items-center gap-3 text-xs text-gray-400 pt-0.5">
        {fcr !== undefined && (
          <span>
            FCR <span className="text-gray-200 font-mono">{fcr.toFixed(2)}</span>
          </span>
        )}
        <span>
          DGR <span className="text-gray-200 font-mono">{dgr.toFixed(1)}</span>g/d
        </span>
        {cumFeedKg !== null && cumFeedKg > 0 && (
          <span className="text-gray-500">
            {tr('tankCard.feed')} <span className="font-mono">{fmtNumber(cumFeedKg, 1)}</span>kg
          </span>
        )}
      </div>

      {/* 사료공급 quick 컨트롤 — 활성 사이클 시 멈춤, 없으면 시작 */}
      <FeedQuickControl tank={tank} variant="compact" />
    </div>
  );
}

function GroupSection({ block }: { block: GroupBlock }) {
  const { tr } = useLanguage();
  const { group, tanks } = block;
  // backend state-vector 와 동일한 fallback — lifecycle 또는 tank profile 데이터 있으면 active.
  const activeCount = tanks.filter(t => {
    if (t.lifecycle) return true;
    const tk = t.tank;
    return (tk.fish_count != null && tk.fish_count > 0)
      || (tk.avg_weight_g != null && tk.avg_weight_g > 0)
      || (tk.biomass_kg != null && tk.biomass_kg > 0);
  }).length;

  return (
    <div className="space-y-2">
      <div className="flex items-baseline gap-2 px-1">
        <span
          className="inline-block w-2 h-2 rounded-full"
          style={{ backgroundColor: group.color || '#888' }}
        />
        <h3 className="text-sm font-semibold text-gray-200">{group.name}</h3>
        {group.description && (
          <span className="text-xs text-gray-500">— {group.description}</span>
        )}
        <span className="text-xs text-gray-600 ml-auto font-mono">
          {activeCount}/{tanks.length} {tr('groupSection.active')}
        </span>
      </div>

      {tanks.length === 0 ? (
        <p className="text-xs text-gray-600 italic px-2 py-3">{tr('groupSection.noTanks')}</p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {tanks.map(snap => (
            <TankCard key={snap.tank.tank_id} snap={snap} />
          ))}
        </div>
      )}
    </div>
  );
}

export function ProductionOverview() {
  const { tr } = useLanguage();
  const [blocks, setBlocks] = useState<GroupBlock[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const groupRes = await Groups.list();
        const groups = groupRes.items;

        const groupTanks = await Promise.all(
          groups.map(async g => ({
            group: g,
            tanks: (await Groups.tanks(g.group_id)).items,
          })),
        );

        const allTanks = groupTanks.flatMap(gt => gt.tanks);
        const snapshots = await Promise.all(
          allTanks.map(async tank => {
            let lifecycle: TankLifecycleResponse['current'] = null;
            let latest: WeightHistoryResponse['snapshots'][number] | null = null;
            try {
              const lc = await Tanks.lifecycle(tank.tank_id);
              lifecycle = lc.current;
            } catch {
              // tank without lifecycle = treated as empty
            }
            if (lifecycle) {
              try {
                const wh = await Tanks.weightHistory(tank.tank_id, 30);
                latest = wh.snapshots.length > 0
                  ? wh.snapshots[wh.snapshots.length - 1]
                  : null;
              } catch {
                // weight history optional
              }
            }
            return { tank, lifecycle, latest } as TankSnapshot;
          }),
        );

        const snapByTank = new Map(snapshots.map(s => [s.tank.tank_id, s]));
        const next: GroupBlock[] = groupTanks.map(gt => ({
          group: gt.group,
          tanks: gt.tanks.map(t => snapByTank.get(t.tank_id)!).filter(Boolean),
        }));

        if (!cancelled) setBlocks(next);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : tr('productionOverview.loadFailed'));
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void load();
    return () => { cancelled = true; };
  }, []);

  // backend state-vector 와 동일한 fallback — lifecycle 또는 tank_profile 둘 중 하나라도
  // 데이터 있으면 "활성/운영 중" 으로 카운트. yaml seed 만 있는 mock 상태에서도 자연 표시.
  function hasTankProfileData(t: Tank): boolean {
    return (t.fish_count != null && t.fish_count > 0)
      || (t.avg_weight_g != null && t.avg_weight_g > 0)
      || (t.biomass_kg != null && t.biomass_kg > 0);
  }

  const summary = (() => {
    if (!blocks) return null;
    const allSnaps = blocks.flatMap(b => b.tanks);
    const operating = allSnaps.filter(s => s.lifecycle || hasTankProfileData(s.tank));
    const empty = allSnaps.length - operating.length;
    const activeGroups = blocks.filter(
      b => b.tanks.some(t => t.lifecycle || hasTankProfileData(t.tank)),
    ).length;
    const totalFish = operating.reduce(
      (sum, s) => sum + (s.lifecycle?.initial_count ?? s.tank.fish_count ?? 0),
      0,
    );
    const fcrs = operating
      .map(s => s.latest?.expected_fcr)
      .filter((v): v is number => typeof v === 'number' && v > 0);
    const avgFcr = fcrs.length > 0 ? fcrs.reduce((a, b) => a + b, 0) / fcrs.length : null;
    const imminent = operating.filter(s => {
      if (!s.lifecycle?.target_harvest_date) return false;
      const d = daysUntil(s.lifecycle.target_harvest_date);
      return d <= 7;
    }).length;
    return {
      activeGroups, totalFish, avgFcr, imminent, empty,
      totalTanks: allSnaps.length,
      operating: operating.length,
    };
  })();

  return (
    <Card>
      <CardHeader>
        <CardTitle>{tr('productionOverview.title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {loading && (
          <div className="space-y-3">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        )}

        {error && (
          <div className="px-3 py-2 bg-destructive/10 border border-destructive/30 rounded text-sm text-destructive font-mono">
            {error}
          </div>
        )}

        {!loading && !error && blocks && summary && (
          <>
            <div className="grid grid-cols-2 md:grid-cols-5 gap-3 p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
              <div>
                <div className="text-xs text-gray-500 uppercase tracking-wide">{tr('productionOverview.activeGroups')}</div>
                <div className="text-lg font-mono font-semibold text-white mt-0.5">
                  {summary.activeGroups}
                  <span className="text-xs text-gray-500 ml-1">/ {blocks.length}</span>
                </div>
              </div>
              <div>
                <div className="text-xs text-gray-500 uppercase tracking-wide">{tr('productionOverview.totalFish')}</div>
                <div className="text-lg font-mono font-semibold text-white mt-0.5">
                  {fmtNumber(summary.totalFish)}
                </div>
              </div>
              <div>
                <div className="text-xs text-gray-500 uppercase tracking-wide">{tr('productionOverview.avgFcr')}</div>
                <div className="text-lg font-mono font-semibold text-white mt-0.5">
                  {summary.avgFcr !== null ? summary.avgFcr.toFixed(2) : '—'}
                </div>
              </div>
              <div>
                <div className="text-xs text-gray-500 uppercase tracking-wide">{tr('productionOverview.harvestImminent')}</div>
                <div
                  className={
                    summary.imminent > 0
                      ? 'text-lg font-mono font-semibold text-red-400 mt-0.5'
                      : 'text-lg font-mono font-semibold text-white mt-0.5'
                  }
                >
                  {summary.imminent}
                  {summary.imminent > 0 && <span className="text-xs ml-1">⚠</span>}
                </div>
              </div>
              <div>
                <div className="text-xs text-gray-500 uppercase tracking-wide">{tr('productionOverview.vacant')}</div>
                <div className="text-lg font-mono font-semibold text-gray-400 mt-0.5">
                  {summary.empty}
                  <span className="text-xs text-gray-500 ml-1">/ {summary.totalTanks}</span>
                </div>
              </div>
            </div>

            {blocks.length === 0 ? (
              <p className="text-sm text-gray-500 py-6 text-center">
                {tr('productionOverview.noGroupsHint')}
              </p>
            ) : (
              <div className="space-y-5">
                {blocks.map(b => (
                  <GroupSection key={b.group.group_id} block={b} />
                ))}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
