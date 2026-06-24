import type { VisionTrainingJob } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { useLanguage } from '../../lib/language-context';

type Signal = 'green' | 'yellow' | 'red' | 'gray';

/**
 * 5/9 ai-training.js 의 signalFor() 동일 로직.
 */
function signalFor(
  value: number | undefined,
  target: number | undefined,
  higher = true,
): Signal {
  if (value == null || target == null) return 'gray';
  if (higher) {
    if (value >= target) return 'green';
    if (value >= target * 0.85) return 'yellow';
    return 'red';
  } else {
    if (value <= target) return 'green';
    if (value <= target * 1.15) return 'yellow';
    return 'red';
  }
}

const signalDot = (s: Signal) => ({
  green: 'text-green-400',
  yellow: 'text-amber-400',
  red: 'text-red-400',
  gray: 'text-gray-500',
}[s]);

function SignalRow({
  name, value, target, higher, fmt,
}: {
  name: string;
  value: number | undefined;
  target: number;
  higher: boolean;
  fmt: (v: number | undefined) => string;
}) {
  const { tr } = useLanguage();
  const sig = signalFor(value, target, higher);
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-gray-700/30 last:border-b-0">
      <div className="flex items-center gap-2">
        <span className={`text-lg leading-none ${signalDot(sig)}`}>●</span>
        <b className="text-sm text-gray-200">{name}</b>
      </div>
      <span className="text-sm text-gray-300 font-mono">
        {fmt(value)} <span className="text-gray-500">({tr('resultSignals.target')} {fmt(target)})</span>
      </span>
    </div>
  );
}

/**
 * 5/9 ai-training.js 의 renderResult() — 시험 결과 신호등 3개.
 */
export function ResultSignalsCard({
  job, gate,
}: {
  job: VisionTrainingJob;
  gate: Record<string, unknown>;
}) {
  const { tr } = useLanguage();
  const m = job.metrics ?? {};
  const targetCorr = Number(gate.target_label_correlation ?? 0.6);
  const maxLatency = Number(gate.max_inference_latency_ms ?? 200);
  const minVisibility = Number(gate.min_visibility_valid_rate ?? 0.8);

  const rows = [
    {
      name: tr('resultSignals.accuracy'),
      value: m.label_correlation,
      target: targetCorr,
      higher: true,
      fmt: (v: number | undefined) => v != null ? `${Math.round(v * 100)}%` : '-',
    },
    {
      name: tr('resultSignals.latency'),
      value: m.inference_latency_ms,
      target: maxLatency,
      higher: false,
      fmt: (v: number | undefined) => v != null ? `${Math.round(v)}ms` : '-',
    },
    {
      name: tr('resultSignals.visibility'),
      value: m.visibility_valid_rate,
      target: minVisibility,
      higher: true,
      fmt: (v: number | undefined) => v != null ? `${Math.round(v * 100)}%` : '-',
    },
  ];

  const signals = rows.map(r => signalFor(r.value, r.target, r.higher));
  const allGreen = signals.every(s => s === 'green');
  const anyRed = signals.some(s => s === 'red');
  const statusTag = allGreen ? tr('resultSignals.allPass') : anyRed ? tr('resultSignals.needsAttention') : tr('resultSignals.reviewRecommended');
  const statusColor = allGreen
    ? 'bg-green-500/20 text-green-300 border-green-500/40'
    : anyRed
      ? 'bg-red-500/20 text-red-300 border-red-500/40'
      : 'bg-amber-500/20 text-amber-300 border-amber-500/40';

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-baseline justify-between gap-3">
          <span>{tr('resultSignals.title')}</span>
          <span className={`px-2 py-0.5 rounded text-xs border ${statusColor}`}>{statusTag}</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          {rows.map(r => (
            <SignalRow key={r.name} {...r} />
          ))}
        </div>
        <div className="p-3 bg-blue-900/10 border border-blue-500/20 rounded space-y-1 text-xs">
          <b className="text-blue-200">🚦 {tr('resultSignals.legendTitle')}</b>
          <ul className="text-gray-300 space-y-0.5 pl-1">
            <li><span className="text-green-400">●</span> <b>{tr('resultSignals.legendPass')}</b> — {tr('resultSignals.legendPassDesc')}</li>
            <li><span className="text-amber-400">●</span> <b>{tr('resultSignals.legendOk')}</b> — {tr('resultSignals.legendOkDesc')}</li>
            <li><span className="text-red-400">●</span> <b>{tr('resultSignals.legendWarn')}</b> — {tr('resultSignals.legendWarnDesc')}</li>
          </ul>
        </div>
      </CardContent>
    </Card>
  );
}
