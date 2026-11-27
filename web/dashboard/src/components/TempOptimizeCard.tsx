import { useEffect, useState, useCallback } from 'react';
import { Snowflake, TrendingUp, Minus } from 'lucide-react';
import type { Tank } from '../lib/types';
import { Settings } from 'lucide-react';
import { Optimize, type TempRecommendation, type TempParams } from '../lib/api';
import { Card, CardHeader, CardTitle, CardContent } from './ui/card';
import { useLanguage } from '../lib/language-core';
import { tempRationale } from '../lib/format';

// 최적환경조성 알고리즘 — 수온 (SHADOW). 권고만 표시, 냉각기 자동제어 안 함.
const POLL_MS = 30_000;

// label 은 i18n 키 — 렌더 시 tr().
const ACTION_META: Record<string, { label: string; color: string; icon: typeof Snowflake }> = {
  allow_rise: { label: 'tempOptimize.actionAllowRise', color: 'text-cyan-300', icon: TrendingUp },
  cool: { label: 'tempOptimize.actionCool', color: 'text-blue-300', icon: Snowflake },
  hold: { label: 'tempOptimize.actionHold', color: 'text-gray-300', icon: Minus },
};

export function TempOptimizeCard({ tanks }: { tanks: Tank[] }) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<TempRecommendation[]>([]);
  const [loading, setLoading] = useState(true);
  const [params, setParams] = useState<TempParams | null>(null);
  const [showSet, setShowSet] = useState(false);
  const [saving, setSaving] = useState(false);
  const [mode, setMode] = useState<string>('shadow'); // 'active'(실제 제어) | 'shadow'(권고만)
  const tankSet = new Set(tanks.map(t => t.tank_id));
  const nameOf = (id: string) => tanks.find(t => t.tank_id === id)?.display_name ?? id;

  useEffect(() => {
    void Optimize.getParams().then(setParams).catch(() => undefined);
  }, []);

  const saveParams = async () => {
    if (!params) return;
    setSaving(true);
    try {
      await Optimize.setParams(params);
      await load();
      setShowSet(false);
    } catch {
      /* ignore */
    } finally {
      setSaving(false);
    }
  };
  const setP = (k: keyof TempParams, v: number) => setParams(p => (p ? { ...p, [k]: v } : p));

  const load = useCallback(async () => {
    try {
      const res = await Optimize.temp();
      setItems((res.items ?? []).filter(i => tankSet.has(i.tank_id)));
      if (res.mode) setMode(res.mode);
    } catch {
      /* keep */
    }
    setLoading(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tanks.map(t => t.tank_id).join(',')]);

  useEffect(() => {
    void load();
    const id = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(id);
  }, [load]);

  return (
    <Card>
      <CardHeader className="border-b border-gray-700/30">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-white">
          <span>🎯</span>
          <span>{tr('tempOptimize.title')}</span>
          <button
            onClick={async () => {
              try {
                const r = await Optimize.setMode(mode !== 'active');
                setMode(r.mode);
              } catch { /* ignore */ }
            }}
            title={tr('tempOptimize.toggleTooltip')}
            className={`text-[10px] px-1.5 py-0.5 rounded font-mono border cursor-pointer hover:brightness-125 ${mode === 'active' ? 'bg-green-500/20 text-green-300 border-green-500/40' : 'bg-gray-600/30 text-gray-300 border-gray-500/40'}`}>
            {mode === 'active' ? tr('tempOptimize.modeActiveLabel') : tr('tempOptimize.modeShadowLabel')}
          </button>
          <span className="text-xs text-gray-500 font-normal ml-auto mr-2">{tr('tempOptimize.subtitle')}</span>
          <button onClick={() => setShowSet(v => !v)} className="text-gray-400 hover:text-white" title={tr('tempOptimize.settingsTooltip')}>
            <Settings className="w-4 h-4" />
          </button>
        </CardTitle>
      </CardHeader>
      <CardContent className="pt-4">
        {showSet && params && (
          <div className="mb-4 p-3 rounded-lg bg-gray-900/60 border border-gray-700/40">
            <div className="text-xs text-gray-300 font-medium mb-2">{tr('tempOptimize.settingsHeader')}</div>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
              {([
                ['optimal_growth_c', 'tempOptimize.fieldOptimalGrowth'],
                ['temp_max_c', 'tempOptimize.fieldTempMax'],
                ['temp_min_c', 'tempOptimize.fieldTempMin'],
                ['do_floor', 'tempOptimize.fieldDoFloor'],
                ['max_ramp_c_per_hour', 'tempOptimize.fieldMaxRamp'],
              ] as [keyof TempParams, string][]).map(([k, label]) => (
                <label key={k} className="text-[11px] text-gray-400 flex flex-col gap-0.5">
                  {tr(label)}
                  <input
                    type="number"
                    step="0.1"
                    value={params[k]}
                    onChange={e => setP(k, parseFloat(e.target.value))}
                    className="px-2 py-1 text-sm bg-gray-800 border border-gray-600 rounded text-white w-full"
                  />
                </label>
              ))}
            </div>
            <div className="flex gap-2 mt-2">
              <button onClick={saveParams} disabled={saving}
                className="px-3 py-1 text-xs rounded bg-green-600 hover:bg-green-500 disabled:opacity-50 text-white">
                {saving ? tr('tempOptimize.saving') : tr('tempOptimize.btnSave')}
              </button>
              <button onClick={() => setShowSet(false)} className="px-3 py-1 text-xs rounded bg-gray-700 text-gray-200">{tr('tempOptimize.btnCancel')}</button>
            </div>
          </div>
        )}
        {loading ? (
          <div className="py-6 text-center text-sm text-gray-500 font-mono">{tr('tempOptimize.loading')}</div>
        ) : items.length === 0 ? (
          <div className="py-6 text-center text-sm text-gray-500 font-mono">{tr('tempOptimize.noData')}</div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {items.map(r => {
              const meta = ACTION_META[r.action] ?? ACTION_META.hold;
              const Icon = meta.icon;
              return (
                <div key={r.tank_id} className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3">
                  <div className="flex items-center justify-between mb-1">
                    <div className="text-sm font-medium text-gray-200">{nameOf(r.tank_id)}</div>
                    <div className={`flex items-center gap-1 text-xs font-medium ${meta.color}`}>
                      <Icon className="w-3.5 h-3.5" /> {tr(meta.label)}
                    </div>
                  </div>
                  <div className="text-xs font-mono text-gray-400">
                    {tr('tempOptimize.currentLabel')} {r.current_temp_c.toFixed(1)}℃
                    {typeof r.current_do === 'number' && <> · DO {r.current_do.toFixed(1)}</>}
                    {' → '}
                    <span className="text-green-400">{tr('tempOptimize.targetLabel')} {r.recommended_setpoint_c.toFixed(1)}℃</span>
                  </div>
                  <div className="text-[11px] text-gray-500 mt-1">
                    {tempRationale(r.action, r.current_temp_c, r.recommended_setpoint_c, r.rationale,
                      r.constraint_binding === 'do', r.current_do, params?.do_floor)}
                  </div>
                  {r.energy_note && <div className="text-[11px] text-cyan-400 mt-0.5">⚡ {tr('tempOptimize.energyNote')}</div>}
                  {r.constraint_binding === 'do' && (
                    <div className="text-[11px] text-amber-400 mt-0.5">{tr('tempOptimize.doConstraint')}</div>
                  )}
                </div>
              );
            })}
          </div>
        )}
        <div className="text-[10px] text-gray-600 mt-2">
          {mode === 'active'
            ? tr('tempOptimize.footerActive')
            : tr('tempOptimize.footerShadow')}
        </div>
      </CardContent>
    </Card>
  );
}
