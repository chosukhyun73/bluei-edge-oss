import { useCallback, useEffect, useState } from 'react';
import { HardDrive } from 'lucide-react';
import { Vision } from '../../lib/api';
import type { CaptureDiskResponse } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { useLanguage } from '../../lib/language-context';

const POLL_INTERVAL_MS = 60_000; // 1분 — 디스크 변동 느림

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 ** 3) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  if (n < 1024 ** 4) return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`;
  return `${(n / 1024 / 1024 / 1024 / 1024).toFixed(2)} TB`;
}

/**
 * R16 — captures 디스크 사용량 위젯.
 * 강릉 D-18 외장 하드 운영 시 디스크 사용량 모니터링 + retention 정책 임계 확인용.
 */
export function DiskUsageCard() {
  const { tr } = useLanguage();
  const [data, setData] = useState<CaptureDiskResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await Vision.captureDisk();
      setData(res);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('diskUsageCard.loadFailed'));
    }
  }, [tr]);

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), POLL_INTERVAL_MS);
    return () => clearInterval(t);
  }, [load]);

  const usedPct = data?.disk.used_percent ?? 0;
  const barColor = usedPct < 50 ? 'bg-green-500' : usedPct < 80 ? 'bg-amber-500' : 'bg-red-500';
  const pctLabel = usedPct < 50 ? tr('diskUsageCard.statusOk') : usedPct < 80 ? tr('diskUsageCard.statusWarning') : tr('diskUsageCard.statusCritical');
  const pctColor = usedPct < 50 ? 'text-green-300' : usedPct < 80 ? 'text-amber-300' : 'text-red-300';

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm">
          <HardDrive className="w-4 h-4 text-gray-400" />
          {tr('diskUsageCard.title')}
          {error && (
            <span className="ml-auto text-xs text-red-400 font-mono">{error}</span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {!data && !error && (
          <div className="text-xs text-gray-500 italic">{tr('diskUsageCard.loading')}</div>
        )}
        {data && (
          <>
            {/* 디스크 % bar */}
            <div className="space-y-1.5">
              <div className="flex items-baseline justify-between text-xs">
                <span className="text-gray-400 font-mono">{data.captures_dir}</span>
                <span className={`font-mono font-semibold ${pctColor}`}>
                  {usedPct.toFixed(1)}% · {pctLabel}
                </span>
              </div>
              <div className="h-2 w-full bg-gray-700/50 rounded-full overflow-hidden">
                <div className={`h-full ${barColor} transition-all`} style={{ width: `${Math.min(100, usedPct)}%` }} />
              </div>
              <div className="flex justify-between text-xs text-gray-500 font-mono">
                <span>{formatBytes(data.disk.used_bytes)} {tr('diskUsageCard.used')}</span>
                <span>{formatBytes(data.disk.free_bytes)} {tr('diskUsageCard.free')} · {formatBytes(data.disk.total_bytes)} {tr('diskUsageCard.total')}</span>
              </div>
            </div>

            {/* captures 카운트 */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-2 border-t border-gray-700/30">
              <div className="space-y-0.5">
                <div className="text-xs text-gray-400">{tr('diskUsageCard.normalCaptures')}</div>
                <div className="flex items-baseline gap-2">
                  <span className="text-lg font-semibold text-gray-100 font-mono">
                    {data.captures.count.toLocaleString()}
                  </span>
                  <span className="text-xs text-gray-500">{tr('diskUsageCard.captureUnit')} · {formatBytes(data.captures.size_bytes)}</span>
                </div>
              </div>
              <div className="space-y-0.5">
                <div className="text-xs text-red-300">{tr('diskUsageCard.excludedCaptures')}</div>
                <div className="space-y-0.5 text-xs">
                  {Object.entries(data.excluded).map(([reason, info]) => (
                    <div key={reason} className="flex items-baseline justify-between">
                      <span className="text-gray-400 font-mono">{reason}</span>
                      <span className="text-gray-300 font-mono">
                        {info.count} · {formatBytes(info.size_bytes)}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            <div className="text-xs text-gray-500 italic">
              {tr('diskUsageCard.pollingNote')}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
