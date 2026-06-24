import { t } from './i18n';

export function relativeTime(iso: string | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const diffSec = Math.round((Date.now() - d.getTime()) / 1000);
  if (Math.abs(diffSec) < 60) return t('format.secondsAgo', { n: diffSec });
  const min = Math.round(diffSec / 60);
  if (Math.abs(min) < 60) return t('format.minutesAgo', { n: min });
  const hr = Math.round(min / 60);
  if (Math.abs(hr) < 24) return t('format.hoursAgo', { n: hr });
  const day = Math.round(hr / 24);
  return t('format.daysAgo', { n: day });
}

export function formatNum(
  n: number | undefined | null,
  opts?: { digits?: number; suffix?: string },
): string {
  if (n === undefined || n === null) return '—';
  const d = opts?.digits ?? 1;
  return n.toFixed(d) + (opts?.suffix ?? '');
}

export function qualityColor(quality: string): string {
  switch (quality) {
    case 'ok': return 'bg-green-500';
    case 'stale':
    case 'stale_sampling': return 'bg-yellow-400';
    case 'low_data': return 'bg-orange-400';
    default: return 'bg-gray-500';
  }
}

// ── Learned safety (C-3l) ─────────────────────────────────────────────────────

// metric_id → 라벨 i18n 키 + 단위. backend mining 은 자유 metric 토큰을 받으므로
// 매핑되지 않으면 원본 토큰을 그대로 반환. label 은 키이며 사용 시 t 로 해석한다.
const METRIC_LABELS: Record<string, { label: string; unit: string }> = {
  water_temperature: { label: 'format.metricWaterTemp', unit: '°C' },
  temperature: { label: 'format.metricWaterTemp', unit: '°C' },
  dissolved_oxygen: { label: 'format.metricDissolvedOxygen', unit: 'mg/L' },
  do: { label: 'format.metricDissolvedOxygen', unit: 'mg/L' },
  ph: { label: 'pH', unit: '' },
  salinity: { label: 'format.metricSalinity', unit: 'ppt' },
  ammonia: { label: 'format.metricAmmonia', unit: 'mg/L' },
  nh3: { label: 'format.metricAmmonia', unit: 'mg/L' },
  turbidity: { label: 'format.metricTurbidity', unit: 'NTU' },
};

const OP_LABELS: Record<string, string> = {
  gt: '>',
  gte: '≥',
  lt: '<',
  lte: '≤',
  eq: '=',
};

// formatLearnedCondition — LearnedRule.condition_json 을 사람 친화 문구로 변환.
// 예: {"metric":"water_temperature","operator":"gt","threshold":28,"window_h":168}
//     → "수온 > 28°C (7일 윈도우)"
// 파싱 실패 시 원본 JSON 문자열 반환 (운영자가 raw 라도 볼 수 있게).
export function formatLearnedCondition(conditionJSON: string): string {
  try {
    const c = JSON.parse(conditionJSON) as {
      metric?: string;
      operator?: string;
      threshold?: number;
      window_h?: number;
    };
    const m = c.metric ? METRIC_LABELS[c.metric.toLowerCase()] : undefined;
    const metricLabel = m ? t(m.label) : (c.metric ?? '?');
    const unit = m?.unit ?? '';
    const opLabel = c.operator ? (OP_LABELS[c.operator] ?? c.operator) : '?';
    const thresh = typeof c.threshold === 'number' ? c.threshold : '?';
    let window = '';
    if (typeof c.window_h === 'number' && c.window_h > 0) {
      const days = Math.round(c.window_h / 24);
      window = days >= 1
        ? ' ' + t('format.windowDays', { days })
        : ' ' + t('format.windowHours', { hours: c.window_h });
    }
    return `${metricLabel} ${opLabel} ${thresh}${unit}${window}`;
  } catch {
    return conditionJSON;
  }
}

// severity → 한국어 + 색상 hint (Tailwind class fragment)
export function severityLabel(severity: string): { text: string; cls: string } {
  switch (severity) {
    case 'high':
      return { text: t('format.severityHigh'), cls: 'bg-red-500/15 text-red-300 border-red-500/30' };
    case 'medium':
      return { text: t('format.severityMedium'), cls: 'bg-amber-500/15 text-amber-300 border-amber-500/30' };
    case 'low':
      return { text: t('format.severityLow'), cls: 'bg-blue-500/15 text-blue-300 border-blue-500/30' };
    default:
      return { text: severity, cls: 'bg-gray-500/15 text-gray-300 border-gray-500/30' };
  }
}

// dispute_type → 라벨 i18n 키. label 은 키이며 렌더 시 t/tr 로 해석한다.
// (모듈 로드 시점 평가를 피하려 t 를 박지 않고 키만 보관 — 언어 전환 반영을 위해)
export const DISPUTE_TYPE_LABELS: { value: string; label: string }[] = [
  { value: 'wrong_condition', label: 'format.disputeWrongCondition' },
  { value: 'wrong_action', label: 'format.disputeWrongAction' },
  { value: 'wrong_timing', label: 'format.disputeWrongTiming' },
];

export function disputeTypeLabel(disputeType: string): string {
  const hit = DISPUTE_TYPE_LABELS.find(d => d.value === disputeType);
  return hit ? t(hit.label) : disputeType;
}

// Arbiter safety_gate rejection_reason → 운영자 안내 한국어 메시지.
// Backend internal/arbiter/arbiter.go 가 생성하는 reason 과 대응.
// 형식: "safety_gate:<category>" 또는 "safety_gate:<category>:<metric>".
export function safetyGateMessage(rejectionReason: string): string {
  const prefix = 'safety_gate:';
  if (!rejectionReason.startsWith(prefix)) return rejectionReason;
  const body = rejectionReason.slice(prefix.length);

  if (body === 'temp_critical_low') {
    return t('format.gateTempLow');
  }
  if (body === 'temp_critical_high') {
    return t('format.gateTempHigh');
  }
  if (body === 'oxygen_critical_low') {
    return t('format.gateOxygenLow');
  }
  if (body === 'sensor_observed_at_invalid') {
    return t('format.gateSensorInvalid');
  }
  if (body.startsWith('sensor_missing:')) {
    const metric = body.slice('sensor_missing:'.length);
    return t('format.gateSensorMissing', { metric });
  }
  if (body.startsWith('sensor_stale:')) {
    const metric = body.slice('sensor_stale:'.length);
    return t('format.gateSensorStale', { metric });
  }
  return t('format.gateRejected', { body });
}
