import { Fish, TrendingUp, Boxes, BarChart3 } from 'lucide-react';
import type { Group } from '../lib/types';
import { useLanguage } from '../lib/language-core';

interface Stats {
  totalTanks: number;
  totalFish: number;
  /** 마지막 실측 샘플링(앵커) 기준 */
  avgWeight: number;
  totalBiomassKg: number;
  /** 급이량 기반 성장 추정 기준 (2026-09-07) */
  estAvgWeight: number;
  estTotalBiomassKg: number;
  /** 마지막 실측 이후 경과일 (수조 중 최대) */
  daysSinceAnchor: number;
  hasEstimate: boolean;
}

interface Props {
  group: Group;
  stats: Stats;
}

function StatCard({
  icon,
  label,
  value,
  sub,
  subTitle,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  /** 보조 표시 — 실측/추정처럼 같은 항목의 다른 근거를 나란히 둘 때 쓴다. */
  sub?: string;
  subTitle?: string;
}) {
  return (
    <div className="bg-gradient-to-br from-gray-800/50 to-black/50 border border-green-500/20 rounded-lg p-4">
      <div className="flex items-center gap-2 text-gray-400 mb-2">
        {icon}
        <span className="text-xs">{label}</span>
      </div>
      <div className="text-2xl font-bold text-white font-mono">{value}</div>
      {sub && (
        <div className="text-xs text-gray-500 font-mono mt-1" title={subTitle}>
          {sub}
        </div>
      )}
    </div>
  );
}

export function GroupHeader({ group, stats }: Props) {
  const { tr } = useLanguage();
  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-6">
      <div className="flex items-start justify-between mb-4">
        <div>
          <div className="flex items-center gap-3 mb-2">
            <div
              className="w-4 h-4 rounded-full flex-shrink-0"
              style={{ backgroundColor: group.color, boxShadow: `0 0 15px ${group.color}` }}
            />
            <h2 className="text-3xl font-bold text-white">{group.name}</h2>
          </div>
          <p className="text-gray-400 font-mono text-sm">{group.description}</p>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard
          icon={<Boxes className="w-4 h-4" />}
          label={tr('groupHeader.totalTanks')}
          value={String(stats.totalTanks)}
        />
        <StatCard
          icon={<Fish className="w-4 h-4" />}
          label={tr('groupHeader.totalFish')}
          value={stats.totalFish.toLocaleString()}
        />
        {/* 추정치를 큰 값으로, 실측(앵커)을 그 아래에 둔다 (2026-09-07 운영자 결정).
            현재 상태를 대표하는 값은 추정치이고, 실측은 그 근거가 언제 것인지를 보여준다.
            둘이 갈라지는 폭이 곧 "샘플링이 얼마나 밀렸나"다.
            추정치가 없으면 실측을 큰 값으로 되돌린다. */}
        <StatCard
          icon={<TrendingUp className="w-4 h-4" />}
          label={tr('groupHeader.avgWeight')}
          value={
            stats.hasEstimate && stats.estAvgWeight > 0
              ? `${stats.estAvgWeight.toFixed(0)}g`
              : stats.avgWeight > 0
                ? `${stats.avgWeight.toFixed(0)}g`
                : '—'
          }
          sub={
            stats.hasEstimate && stats.avgWeight > 0
              ? tr('groupHeader.avgWeightMeasured', {
                  value: stats.avgWeight.toFixed(0),
                  days: String(stats.daysSinceAnchor),
                })
              : undefined
          }
          subTitle={tr('groupHeader.weightSourceHint')}
        />
        <StatCard
          icon={<BarChart3 className="w-4 h-4" />}
          label={tr('groupHeader.totalBiomass')}
          value={
            stats.hasEstimate && stats.estTotalBiomassKg > 0
              ? `${stats.estTotalBiomassKg.toFixed(1)}kg`
              : stats.totalBiomassKg > 0
                ? `${stats.totalBiomassKg.toFixed(1)}kg`
                : '—'
          }
          sub={
            stats.hasEstimate && stats.totalBiomassKg > 0
              ? tr('groupHeader.totalBiomassMeasured', {
                  value: stats.totalBiomassKg.toFixed(1),
                })
              : undefined
          }
          subTitle={tr('groupHeader.weightSourceHint')}
        />
      </div>
    </div>
  );
}
