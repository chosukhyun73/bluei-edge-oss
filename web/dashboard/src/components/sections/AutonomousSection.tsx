import { useState } from 'react';
import type { StateVector } from '../../lib/types';
import { relativeTime } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-core';
import { Tanks } from '../../lib/api';

type Props = {
  data?: StateVector['autonomous'];
  convergence?: StateVector['convergence'];
  tankId: string;
  onChanged?: () => void;
};

const MODES = ['off', 'observation', 'partial', 'full'] as const;
type Mode = typeof MODES[number];

// 자율 권장 게이트 임계 — 정확도(1-격차)가 이 미만이면 partial/full 비권장 경고.
const AUTONOMY_ACCURACY_GATE = 0.8;

function modeStyle(mode: string): string {
  switch (mode) {
    case 'full':        return 'bg-green-500/20 text-green-400 border-green-500/30';
    case 'partial':     return 'bg-orange-500/20 text-orange-400 border-orange-500/30';
    case 'observation': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    default:            return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
  }
}

function modeLabelKey(mode: string): string {
  switch (mode) {
    case 'full':        return 'autonomous.modeFull';
    case 'partial':     return 'autonomous.modePartial';
    case 'observation': return 'autonomous.modeObservation';
    default:            return 'autonomous.modeInactive';
  }
}

export function AutonomousSection({ data, convergence, tankId, onChanged }: Props) {
  const { tr } = useLanguage();
  const [pending, setPending] = useState<Mode | null>(null); // 전환 확정 대기 모드
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('autonomousSection.noData')}</p>;
  const notes = Array.isArray(data.notes) ? data.notes : [];
  const current = data.mode || 'off';

  // 정확도 게이트 — partial/full 은 정확도 충분해야 권장.
  const score = convergence?.convergence_score;
  const lowAccuracy = typeof score === 'number' && score < AUTONOMY_ACCURACY_GATE;

  const startChange = (m: Mode) => {
    if (m === current) return;
    setPending(m);
    setReason('');
    setError(null);
  };

  const confirm = async () => {
    if (!pending) return;
    setBusy(true);
    setError(null);
    try {
      await Tanks.setAutonomousMode(tankId, pending, reason.trim());
      setPending(null);
      setReason('');
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-3">
      {/* 현재 모드 */}
      <div className="flex items-center gap-3">
        <Badge className={`text-sm px-3 py-1 ${modeStyle(current)}`}>{tr(modeLabelKey(current))}</Badge>
        <span className="text-xs text-muted-foreground font-mono">{current}</span>
      </div>

      {/* 모드 토글 버튼 */}
      <div className="flex flex-wrap gap-2">
        {MODES.map(m => {
          const active = m === current;
          const isPending = m === pending;
          return (
            <button
              key={m}
              onClick={() => startChange(m)}
              disabled={busy}
              className={`text-xs px-3 py-1.5 rounded border transition-colors ${
                active ? modeStyle(m) + ' font-semibold'
                : isPending ? 'bg-primary/15 border-primary/40 text-primary'
                : 'bg-muted/30 border-border/50 text-muted-foreground hover:bg-muted/60'
              }`}
            >
              {tr(modeLabelKey(m))}
            </button>
          );
        })}
      </div>

      {/* 정확도 게이트 권장 */}
      {typeof score === 'number' && (pending === 'partial' || pending === 'full' || current === 'partial' || current === 'full') && (
        <div className={`text-xs rounded px-2 py-1.5 border ${lowAccuracy ? 'bg-orange-500/10 border-orange-500/30 text-orange-400' : 'bg-green-500/10 border-green-500/30 text-green-400'}`}>
          {lowAccuracy
            ? tr('autonomousSection.gateLow', { pct: Math.round(score * 100) })
            : tr('autonomousSection.gateOk', { pct: Math.round(score * 100) })}
        </div>
      )}

      {/* 전환 확정 (reason 입력) */}
      {pending && (
        <div className="space-y-2 rounded border border-border/50 bg-muted/20 p-2">
          <div className="text-xs text-muted-foreground">
            {tr('autonomousSection.changeTo', { mode: tr(modeLabelKey(pending)) })}
          </div>
          {(pending === 'partial' || pending === 'full') && (
            <textarea
              value={reason}
              onChange={e => setReason(e.target.value)}
              placeholder={tr('autonomousSection.reasonPlaceholder')}
              rows={2}
              className="w-full text-sm rounded border border-border/50 bg-background/50 px-2 py-1 resize-none"
            />
          )}
          {error && <div className="text-xs text-destructive">{error}</div>}
          <div className="flex gap-2">
            <button onClick={confirm} disabled={busy}
              className="text-xs px-3 py-1 rounded bg-primary text-primary-foreground disabled:opacity-50">
              {busy ? tr('autonomousSection.saving') : tr('autonomousSection.confirm')}
            </button>
            <button onClick={() => { setPending(null); setError(null); }} disabled={busy}
              className="text-xs px-3 py-1 rounded border border-border/50 text-muted-foreground">
              {tr('autonomousSection.cancel')}
            </button>
          </div>
        </div>
      )}

      {/* 현재 상태 메타 */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
        <div>
          <div className="text-xs text-muted-foreground mb-0.5">{tr('autonomousSection.reason')}</div>
          <div>{data.reason || '—'}</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground mb-0.5">{tr('autonomousSection.changedAt')}</div>
          <div className="font-mono">{data.changed_at ? relativeTime(data.changed_at) : '—'}</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground mb-0.5">{tr('autonomousSection.changedBy')}</div>
          <div className="font-mono">{data.changed_by || '—'}</div>
        </div>
      </div>

      {notes.length > 0 && (
        <ul className="space-y-0.5">
          {notes.map((n, i) => (
            <li key={i} className="text-xs text-muted-foreground italic">{n}</li>
          ))}
        </ul>
      )}

      {/* 현재 자율모드는 스케줄 우선순위 라벨만 바꾼다(급이 동작 무변). 실제 제어 연결은 후속. */}
      <p className="text-xs text-muted-foreground italic">{tr('autonomousSection.controlNote')}</p>
    </div>
  );
}
