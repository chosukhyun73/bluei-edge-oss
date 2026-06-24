import { useState, useEffect, useRef } from 'react';
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts';
import { Tanks } from '../lib/api';
import { useLanguage } from '../lib/language-context';

type DataPoint = { time: string; value: number; ts: number };

interface Props {
  tankId: string;
  metricKey: string;
  metricLabel: string;
  unit: string;
  bufferMinutes?: number;
  pollIntervalSec?: number;
}

// HH:MM:SS 포맷 — 시계열 x축 레이블용
function hhmmss(d: Date): string {
  return d.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function RealtimeWaterChart({
  tankId,
  metricKey,
  metricLabel,
  unit,
  bufferMinutes = 30,
  pollIntervalSec = 5,
}: Props) {
  const { tr } = useLanguage();
  const [buffer, setBuffer] = useState<DataPoint[]>([]);
  const bufferRef = useRef<DataPoint[]>([]);

  const pushPoint = async () => {
    try {
      const sv = await Tanks.stateVector(tankId);
      const metric = sv.water.metrics[metricKey];
      if (!metric) return;
      const now = Date.now();
      const point: DataPoint = { time: hhmmss(new Date(now)), value: metric.value, ts: now };
      const cutoff = now - bufferMinutes * 60_000;
      const next = [...bufferRef.current.filter(p => p.ts >= cutoff), point];
      bufferRef.current = next;
      setBuffer([...next]);
    } catch {
      // 백엔드 일시 오류 무시 — 다음 폴링에서 재시도
    }
  };

  useEffect(() => {
    bufferRef.current = [];
    setBuffer([]);
    void pushPoint();
    const id = setInterval(() => void pushPoint(), pollIntervalSec * 1000);
    return () => clearInterval(id);
    // tankId, metricKey 가 바뀔 때 재마운트
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tankId, metricKey]);

  const latest = buffer[buffer.length - 1];

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-5">
      {/* 제목 행 */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2 text-white font-medium">
          <span>📊</span>
          <span>{metricLabel} {tr('realtimeWaterChart.realtimeTrend')}</span>
        </div>
        {latest != null ? (
          <span className="font-mono text-sm px-2 py-0.5 bg-green-500/10 border border-green-500/30 rounded text-green-400">
            {latest.value.toFixed(2)}{unit}
          </span>
        ) : (
          <span className="text-xs text-gray-500 font-mono">
            {tr('realtimeWaterChart.collecting')} ({pollIntervalSec}{tr('realtimeWaterChart.refreshInterval')})
          </span>
        )}
      </div>

      {/* 차트 */}
      {buffer.length === 0 ? (
        <div className="h-[260px] flex items-center justify-center text-gray-500 text-sm font-mono">
          {tr('realtimeWaterChart.collecting')} ({pollIntervalSec}{tr('realtimeWaterChart.refreshInterval')})
        </div>
      ) : (
        <div className="min-h-[260px] w-full min-w-0">
        <ResponsiveContainer width="100%" height={260} minWidth={120} minHeight={200} debounce={50}>
          <LineChart data={buffer}>
            <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
            <XAxis dataKey="time" stroke="#6b7280" fontSize={10} interval="preserveStartEnd" minTickGap={60} />
            <YAxis stroke="#6b7280" fontSize={10} width={45} />
            <Tooltip
              contentStyle={{ background: '#0a0a0a', border: '1px solid rgba(34,197,94,0.27)', borderRadius: 6, fontSize: 12 }}
              labelStyle={{ color: '#9ca3af' }}
            />
            <Line
              type="monotone"
              dataKey="value"
              stroke="#22c55e"
              strokeWidth={2}
              dot={false}
              animationDuration={300}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}
