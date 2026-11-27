import { useState, useEffect, useCallback } from 'react';
import type { StateVector, WeightHistorySnapshot, SamplingItem } from '../../lib/types';
import { formatNum } from '../../lib/format';
import { Badge } from '../ui/badge';
import { WeightHistoryChart } from '../WeightHistoryChart';
import { useLanguage } from '../../lib/language-core';
import { Tanks as tanksApi } from '../../lib/api';

type Props = {
  tankId?: string;
  data?: StateVector['biological_context'];
  weightSnapshots: WeightHistorySnapshot[];
};

export function BiologicalContextSection({ tankId, data, weightSnapshots }: Props) {
  const { tr } = useLanguage();
  const [editing, setEditing] = useState(false);
  const [fcrInput, setFcrInput] = useState('');
  const [saving, setSaving] = useState(false);
  const [feedback, setFeedback] = useState<{ ok: boolean; msg: string } | null>(null);

  // 실측중량 샘플링
  const [samplings, setSamplings] = useState<SamplingItem[]>([]);
  const [measuring, setMeasuring] = useState(false);
  const [mWeight, setMWeight] = useState('');
  const [mCount, setMCount] = useState('');
  const [mDate, setMDate] = useState('');

  const loadSamplings = useCallback(async () => {
    if (!tankId) return;
    try {
      const res = await tanksApi.sampling(tankId);
      setSamplings(res.history ?? []);
    } catch {
      setSamplings([]);
    }
  }, [tankId]);

  useEffect(() => { loadSamplings(); }, [loadSamplings]);

  async function saveFCR() {
    if (!tankId) return;
    const val = parseFloat(fcrInput);
    if (!Number.isFinite(val) || val < 0.5 || val > 3.0) {
      setFeedback({ ok: false, msg: tr('biologicalContext.fcrError') });
      return;
    }
    setSaving(true);
    setFeedback(null);
    try {
      await tanksApi.setFCR(tankId, val);
      setFeedback({ ok: true, msg: tr('biologicalContext.fcrSaved') });
      setEditing(false);
    } catch {
      setFeedback({ ok: false, msg: tr('biologicalContext.fcrError') });
    } finally {
      setSaving(false);
    }
  }

  async function saveSampling() {
    if (!tankId) return;
    const w = parseFloat(mWeight);
    const c = parseInt(mCount, 10);
    if (!Number.isFinite(w) || w <= 0 || !Number.isFinite(c) || c <= 0) {
      setFeedback({ ok: false, msg: tr('biologicalContext.measureError') });
      return;
    }
    setSaving(true);
    setFeedback(null);
    try {
      // mDate(YYYY-MM-DD) → RFC3339 (정오 기준, 날짜만 입력받으므로)
      const sampledAt = mDate ? `${mDate}T12:00:00Z` : undefined;
      await tanksApi.recordSampling(tankId, { avg_weight_g: w, sampled_count: c, sampled_at: sampledAt });
      setFeedback({ ok: true, msg: tr('biologicalContext.measureSaved') });
      setMeasuring(false);
      setMWeight(''); setMCount(''); setMDate('');
      await loadSamplings();
    } catch {
      setFeedback({ ok: false, msg: tr('biologicalContext.measureError') });
    } finally {
      setSaving(false);
    }
  }

  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('biologicalContext.noData')}</p>;
  const fishCount = typeof data.fish_count === 'number' ? data.fish_count.toLocaleString() : '—';
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.species')}</div>
          <div className="text-sm font-medium">{data.species || '—'}</div>
        </div>
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.fishCount')}</div>
          <div className="text-sm font-mono font-medium">{fishCount}</div>
        </div>
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.avgWeight')}</div>
          <div className="text-sm font-mono font-medium">{formatNum(data.avg_weight_g, { digits: 1, suffix: ' g' })}</div>
        </div>
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.biomass')}</div>
          <div className="text-sm font-mono font-medium">{formatNum(data.biomass_kg, { digits: 1, suffix: ' kg' })}</div>
        </div>
      </div>

      {data.estimated_avg_weight_g !== undefined && (
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{tr('biologicalContext.estimatedAvgWeight')}</span>
          <span className="font-mono font-medium">{formatNum(data.estimated_avg_weight_g, { digits: 1, suffix: 'g' })}</span>
          {data.fcr_source && (
            <Badge
              className={`text-xs ${data.fcr_source === 'calibrated' ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-blue-500/20 text-blue-400 border-blue-500/30'}`}
            >
              {data.fcr_source === 'calibrated' ? tr('biologicalContext.fcrCalibrated') : tr('biologicalContext.fcrDefault')}
            </Badge>
          )}
          {data.expected_fcr !== undefined && (
            <span className="text-xs text-muted-foreground">FCR {formatNum(data.expected_fcr, { digits: 2 })}</span>
          )}
          {tankId && !editing && (
            <button
              type="button"
              onClick={() => { setEditing(true); setFcrInput(data.expected_fcr ? String(data.expected_fcr) : ''); setFeedback(null); }}
              className="text-xs px-2 py-0.5 rounded border border-border text-muted-foreground hover:bg-muted/60"
            >
              {tr('biologicalContext.fcrEdit')}
            </button>
          )}
        </div>
      )}

      {editing && (
        <div className="bg-muted/40 rounded-lg p-3 space-y-2">
          <div className="text-xs font-medium">{tr('biologicalContext.fcrInputTitle')}</div>
          <div className="text-xs text-muted-foreground">{tr('biologicalContext.fcrInputHint')}</div>
          <div className="flex items-center gap-2">
            <input
              type="number" step="0.01" min="0.5" max="3.0"
              value={fcrInput}
              onChange={(e) => setFcrInput(e.target.value)}
              className="w-24 px-2 py-1 text-sm font-mono rounded border border-border bg-background"
              autoFocus
            />
            <button
              type="button" disabled={saving} onClick={saveFCR}
              className="text-xs px-3 py-1 rounded bg-green-500/20 text-green-400 border border-green-500/30 hover:bg-green-500/30 disabled:opacity-50"
            >
              {tr('biologicalContext.fcrSave')}
            </button>
            <button
              type="button" disabled={saving} onClick={() => { setEditing(false); setFeedback(null); }}
              className="text-xs px-3 py-1 rounded border border-border text-muted-foreground hover:bg-muted/60"
            >
              {tr('biologicalContext.fcrCancel')}
            </button>
          </div>
        </div>
      )}

      {feedback && (
        <div className={`text-xs ${feedback.ok ? 'text-green-400' : 'text-red-400'}`}>{feedback.msg}</div>
      )}

      {/* Weight history chart (추정 곡선 + 실측 오버레이) */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <div className="text-xs text-muted-foreground">{tr('biologicalContext.weightTrend30d')}</div>
          {tankId && !measuring && (
            <button
              type="button"
              onClick={() => {
                setMeasuring(true);
                setMCount(typeof data.fish_count === 'number' ? String(data.fish_count) : '');
                setMWeight(''); setMDate(''); setFeedback(null);
              }}
              className="text-xs px-2 py-0.5 rounded border border-amber-500/40 text-amber-500 hover:bg-amber-500/10"
            >
              {tr('biologicalContext.measureAdd')}
            </button>
          )}
        </div>
        <WeightHistoryChart snapshots={weightSnapshots} samplings={samplings} />
      </div>

      {measuring && (
        <div className="bg-muted/40 rounded-lg p-3 space-y-2">
          <div className="text-xs font-medium">{tr('biologicalContext.measureTitle')}</div>
          <div className="text-xs text-muted-foreground">{tr('biologicalContext.measureHint')}</div>
          <div className="flex flex-wrap items-end gap-2">
            <label className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{tr('biologicalContext.measureWeight')}</span>
              <input
                type="number" step="0.1" min="0" value={mWeight}
                onChange={(e) => setMWeight(e.target.value)}
                className="w-28 px-2 py-1 text-sm font-mono rounded border border-border bg-background"
                autoFocus
              />
            </label>
            <label className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{tr('biologicalContext.measureCount')}</span>
              <input
                type="number" step="1" min="1" value={mCount}
                onChange={(e) => setMCount(e.target.value)}
                className="w-24 px-2 py-1 text-sm font-mono rounded border border-border bg-background"
              />
            </label>
            <label className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{tr('biologicalContext.measureDate')}</span>
              <input
                type="date" value={mDate}
                onChange={(e) => setMDate(e.target.value)}
                className="px-2 py-1 text-sm font-mono rounded border border-border bg-background"
              />
            </label>
            <button
              type="button" disabled={saving} onClick={saveSampling}
              className="text-xs px-3 py-1.5 rounded bg-amber-500/20 text-amber-500 border border-amber-500/30 hover:bg-amber-500/30 disabled:opacity-50"
            >
              {tr('biologicalContext.measureSave')}
            </button>
            <button
              type="button" disabled={saving} onClick={() => { setMeasuring(false); setFeedback(null); }}
              className="text-xs px-3 py-1.5 rounded border border-border text-muted-foreground hover:bg-muted/60"
            >
              {tr('biologicalContext.fcrCancel')}
            </button>
          </div>
        </div>
      )}

      <div className="text-xs text-muted-foreground">
        {tr('biologicalContext.dataSource')}: <span className="font-mono">{data.source}</span> · {data.system_type}
      </div>

      {notes.length > 0 && (
        <ul className="space-y-0.5">
          {notes.map((n, i) => (
            <li key={i} className="text-xs text-muted-foreground italic">{n}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
