import { Fish } from 'lucide-react';
import type { Tank } from '../lib/types';
import { Card, CardContent } from './ui/card';
import { useLanguage } from '../lib/language-context';

interface Props {
  tank: Tank;
  onSelect: (tankId: string) => void;
}

export function TankCard({ tank, onSelect }: Props) {
  const { tr } = useLanguage();
  return (
    <Card
      className="cursor-pointer transition-all hover:border-green-500/60 hover:shadow-[0_0_12px_rgba(34,197,94,0.15)] active:scale-[0.99]"
      onClick={() => onSelect(tank.tank_id)}
    >
      <CardContent className="pt-4 pb-4">
        {/* Header row */}
        <div className="flex items-start gap-2 mb-3">
          <Fish className="w-4 h-4 text-green-500 flex-shrink-0 mt-0.5" />
          <div className="min-w-0 flex-1">
            <div className="font-medium truncate">{tank.display_name}</div>
            <div className="flex items-center gap-1.5 mt-0.5 flex-wrap">
              <span className="text-xs bg-muted text-muted-foreground px-1.5 py-0.5 rounded font-mono">
                {tank.species}
              </span>
              <span className="text-xs bg-muted text-muted-foreground px-1.5 py-0.5 rounded font-mono">
                {tank.system_type}
              </span>
            </div>
          </div>
        </div>

        {/* Stats grid */}
        <div className="grid grid-cols-3 gap-2 text-center">
          <div className="bg-muted/40 rounded-lg p-2">
            <div className="text-xs text-muted-foreground mb-0.5">{tr('tankCard.fishCount')}</div>
            <div className="text-sm font-mono font-medium">
              {tank.fish_count != null ? tank.fish_count.toLocaleString() : '—'}
            </div>
          </div>
          <div className="bg-muted/40 rounded-lg p-2">
            <div className="text-xs text-muted-foreground mb-0.5">{tr('tankCard.avgWeight')}</div>
            <div className="text-sm font-mono font-medium">
              {tank.avg_weight_g != null ? `${tank.avg_weight_g.toFixed(0)}g` : '—'}
            </div>
          </div>
          <div className="bg-muted/40 rounded-lg p-2">
            <div className="text-xs text-muted-foreground mb-0.5">{tr('tankCard.volume')}</div>
            <div className="text-sm font-mono font-medium">
              {tank.volume_m3 != null ? `${tank.volume_m3}m³` : '—'}
            </div>
          </div>
        </div>

        {/* Footer: tank_id */}
        <div className="mt-3 text-xs text-muted-foreground font-mono truncate">
          {tank.tank_id}
        </div>
      </CardContent>
    </Card>
  );
}
