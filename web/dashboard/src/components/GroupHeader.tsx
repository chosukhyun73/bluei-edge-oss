import { Fish, TrendingUp, Boxes, BarChart3 } from 'lucide-react';
import type { Group } from '../lib/types';
import { useLanguage } from '../lib/language-context';

interface Stats {
  totalTanks: number;
  totalFish: number;
  avgWeight: number;
  totalBiomassKg: number;
}

interface Props {
  group: Group;
  stats: Stats;
}

function StatCard({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <div className="bg-gradient-to-br from-gray-800/50 to-black/50 border border-green-500/20 rounded-lg p-4">
      <div className="flex items-center gap-2 text-gray-400 mb-2">
        {icon}
        <span className="text-xs">{label}</span>
      </div>
      <div className="text-2xl font-bold text-white font-mono">{value}</div>
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
        <StatCard
          icon={<TrendingUp className="w-4 h-4" />}
          label={tr('groupHeader.avgWeight')}
          value={stats.avgWeight > 0 ? `${stats.avgWeight.toFixed(0)}g` : '—'}
        />
        <StatCard
          icon={<BarChart3 className="w-4 h-4" />}
          label={tr('groupHeader.totalBiomass')}
          value={stats.totalBiomassKg > 0 ? `${stats.totalBiomassKg.toFixed(1)}kg` : '—'}
        />
      </div>
    </div>
  );
}
