import { useEffect, useState } from 'react';
import type { LLMAnalysis } from '../../lib/types';
import { useLanguage } from '../../lib/language-context';
import { Button } from '../ui/button';

// F.2 — LLM 종합 판단 결과 modal.
// 운영자가 분석 결과를 확인 후 [이번 스케줄에 적용] 을 명시적으로 눌러야 schedule 반영.
// can_apply=false 이면 [확인] 버튼만 노출.

interface LLMAnalysisModalProps {
  intentId: string;
  // null 이면 loading 상태 (POST 응답 대기 중).
  analysis: LLMAnalysis | null;
  // force=false: LLM 권고 적용. force=true: 운영자 책임으로 강제 적용 (can_apply=false 시).
  onApply: (force: boolean) => Promise<void>;
  onClose: () => void;
}

const BSF_MODE_LABEL_KEYS: Record<string, string> = {
  aggressive: 'llmAnalysis.bsfModeAggressive',
  standard: 'llmAnalysis.bsfModeStandard',
  conservative: 'llmAnalysis.bsfModeConservative',
};

function confidenceColor(c: number): string {
  if (c >= 0.7) return 'text-green-400';
  if (c >= 0.4) return 'text-yellow-400';
  return 'text-red-400';
}

function confidenceBg(c: number): string {
  if (c >= 0.7) return 'bg-green-500/10 border-green-500/30';
  if (c >= 0.4) return 'bg-yellow-500/10 border-yellow-500/30';
  return 'bg-red-500/10 border-red-500/30';
}

export function LLMAnalysisModal({ intentId, analysis, onApply, onClose }: LLMAnalysisModalProps) {
  const { tr } = useLanguage();
  const [applying, setApplying] = useState(false);
  const [success, setSuccess] = useState(false);
  const [forced, setForced] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmForce, setConfirmForce] = useState(false);

  // Loading 단계 표시용 elapsed 초 (LLM 호출 30초 가량).
  const loading = analysis == null;
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!loading) return;
    setElapsed(0);
    const t = setInterval(() => setElapsed(e => e + 1), 1000);
    return () => clearInterval(t);
  }, [loading]);

  // Close on Escape — loading/applying 중에는 닫기 금지.
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !applying && !loading) onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [applying, loading, onClose]);

  const adj = analysis?.adjustment ?? {};
  const hasAdjustment = Object.keys(adj).length > 0;

  // 단계 메시지 (실제 backend phase 비공개라 elapsed 기반 추정).
  function loadingPhase(sec: number): string {
    if (sec < 5) return tr('llmAnalysis.loadingPhase1');
    if (sec < 20) return tr('llmAnalysis.loadingPhase2');
    if (sec < 40) return tr('llmAnalysis.loadingPhase3');
    return tr('llmAnalysis.loadingPhase4');
  }

  async function handleApply(force: boolean) {
    setApplying(true);
    setError(null);
    try {
      await onApply(force);
      setForced(force);
      setSuccess(true);
      setTimeout(() => {
        onClose();
      }, 1200);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : tr('llmAnalysis.applyFailed'));
    } finally {
      setApplying(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={tr('llmAnalysis.dialogAriaLabel')}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
    >
      <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl p-6 w-full max-w-lg space-y-4 max-h-[90vh] overflow-y-auto">
        {/* ── 헤더 ── */}
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold text-white">
              {loading ? tr('llmAnalysis.titleLoading') : tr('llmAnalysis.titleResult')}
            </h2>
            {analysis && (
              <p className="text-xs text-gray-500 mt-0.5 font-mono">
                {analysis.model_used}
                {analysis.fallback && (
                  <span className="ml-2 px-1.5 py-0.5 bg-amber-500/20 border border-amber-500/30 rounded text-amber-300">
                    fallback
                  </span>
                )}
              </p>
            )}
            <p className="text-xs text-gray-600 font-mono mt-0.5 break-all">
              intent: {intentId}
            </p>
          </div>
          <button
            type="button"
            aria-label={tr('llmAnalysis.closeAriaLabel')}
            disabled={applying || loading}
            onClick={onClose}
            className="text-gray-500 hover:text-white transition-colors text-lg leading-none disabled:opacity-50 flex-shrink-0"
          >
            ✕
          </button>
        </div>

        {loading ? (
          <div className="space-y-3 py-2">
            {/* spinner */}
            <div className="flex items-center justify-center py-4">
              <div className="w-10 h-10 border-4 border-gray-700 border-t-blue-400 rounded-full animate-spin" />
            </div>
            <p className="text-sm text-gray-200 text-center">
              {loadingPhase(elapsed)}
            </p>
            <div className="text-center">
              <span className="text-xs text-gray-500 font-mono">{tr('llmAnalysis.elapsed')} {elapsed}{tr('llmAnalysis.elapsedUnit')} · {tr('llmAnalysis.elapsedEstimate')}</span>
            </div>
            <p className="text-xs text-gray-500 text-center leading-relaxed pt-1">
              {tr('llmAnalysis.loadingDesc1')}<br />
              {tr('llmAnalysis.loadingDesc2')}
            </p>
          </div>
        ) : success ? (
          <div className={`px-3 py-4 rounded-md text-center border ${
            forced
              ? 'bg-red-500/10 border-red-500/30'
              : 'bg-green-500/10 border-green-500/30'
          }`}>
            <p className={`text-sm font-medium ${forced ? 'text-red-300' : 'text-green-300'}`}>
              {forced ? tr('llmAnalysis.forceApplyDone') : tr('llmAnalysis.applyDone')}
            </p>
            <p className={`text-xs mt-1 ${forced ? 'text-red-200/70' : 'text-green-200/70'}`}>
              {forced
                ? tr('llmAnalysis.forceApplyDoneDesc')
                : tr('llmAnalysis.applyDoneDesc')}
            </p>
          </div>
        ) : analysis ? (
          <>
            {/* ── 신뢰도 + 적용 가능 배지 ── */}
            <div className="flex flex-wrap items-center gap-3">
              <div className={`px-3 py-1.5 border rounded-md flex items-center gap-2 ${confidenceBg(analysis.confidence)}`}>
                <span className="text-xs text-gray-400">{tr('llmAnalysis.confidence')}</span>
                <span className={`text-sm font-bold ${confidenceColor(analysis.confidence)}`}>
                  {Math.round(analysis.confidence * 100)}%
                </span>
              </div>
              <div className={`px-3 py-1.5 border rounded-md ${analysis.can_apply
                ? 'bg-green-500/10 border-green-500/30'
                : 'bg-red-500/10 border-red-500/30'}`}>
                <span className={`text-sm font-semibold ${analysis.can_apply ? 'text-green-300' : 'text-red-300'}`}>
                  {analysis.can_apply ? tr('llmAnalysis.canApply') : tr('llmAnalysis.cannotApply')}
                </span>
              </div>
            </div>

            {/* ── 차단 사유 ── */}
            {analysis.blocked_by && analysis.blocked_by.length > 0 && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md space-y-1">
                <p className="text-xs font-semibold text-red-300">{tr('llmAnalysis.blockedBy')}</p>
                <ul className="list-disc list-inside space-y-0.5">
                  {analysis.blocked_by.map((b, i) => (
                    <li key={i} className="text-xs text-red-200 font-mono">{b}</li>
                  ))}
                </ul>
              </div>
            )}

            {/* ── 판단 사유 + 범위 ── */}
            <div className="space-y-2">
              {analysis.reason && (
                <div>
                  <p className="text-xs text-gray-500 font-medium mb-0.5">{tr('llmAnalysis.reason')}</p>
                  <p className="text-xs text-gray-300">{analysis.reason}</p>
                </div>
              )}
              {analysis.scope && (
                <div>
                  <p className="text-xs text-gray-500 font-medium mb-0.5">{tr('llmAnalysis.scope')}</p>
                  <p className="text-xs text-gray-300">{analysis.scope}</p>
                </div>
              )}
              {analysis.explanation_ko && (
                <div>
                  <p className="text-xs text-gray-500 font-medium mb-0.5">{tr('llmAnalysis.explanation')}</p>
                  <p className="text-xs text-gray-200 leading-relaxed">{analysis.explanation_ko}</p>
                </div>
              )}
            </div>

            {/* ── adjustment 표 ── */}
            {hasAdjustment && (
              <div>
                <p className="text-xs text-gray-500 font-medium mb-1.5">{tr('llmAnalysis.adjustments')}</p>
                <table className="w-full text-xs">
                  <tbody className="divide-y divide-gray-800">
                    {adj.max_daily_cycles_override != null && (
                      <tr>
                        <td className="py-1 pr-4 text-gray-400">{tr('llmAnalysis.adjMaxDailyCycles')}</td>
                        <td className="py-1 font-mono text-white">{tr('llmAnalysis.cyclesUnit', { count: adj.max_daily_cycles_override })}</td>
                      </tr>
                    )}
                    {adj.bsf_mode_override != null && (
                      <tr>
                        <td className="py-1 pr-4 text-gray-400">{tr('llmAnalysis.adjBsfMode')}</td>
                        <td className="py-1 font-mono text-white">
                          {BSF_MODE_LABEL_KEYS[adj.bsf_mode_override] ? tr(BSF_MODE_LABEL_KEYS[adj.bsf_mode_override]) : adj.bsf_mode_override}
                        </td>
                      </tr>
                    )}
                    {adj.get_factor != null && (
                      <tr>
                        <td className="py-1 pr-4 text-gray-400">{tr('llmAnalysis.adjGetFactor')}</td>
                        <td className="py-1 font-mono text-white">×{adj.get_factor}</td>
                      </tr>
                    )}
                    {adj.min_interval_min != null && (
                      <tr>
                        <td className="py-1 pr-4 text-gray-400">{tr('llmAnalysis.adjMinInterval')}</td>
                        <td className="py-1 font-mono text-white">{tr('llmAnalysis.minutesUnit', { count: adj.min_interval_min })}</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            )}

            {/* ── 에러 ── */}
            {error && (
              <p className="text-xs text-red-400 font-mono px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md">
                {error}
              </p>
            )}

            {/* ── 액션 버튼 ── */}
            <div className="flex flex-wrap gap-2 pt-1">
              {analysis.can_apply ? (
                <>
                  <Button
                    type="button"
                    size="sm"
                    onClick={() => void handleApply(false)}
                    disabled={applying}
                    aria-label={tr('llmAnalysis.applyThisSchedule')}
                  >
                    {applying ? tr('llmAnalysis.applying') : tr('llmAnalysis.applyThisSchedule')}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={onClose}
                    disabled={applying}
                    aria-label={tr('llmAnalysis.reject')}
                  >
                    {tr('llmAnalysis.reject')}
                  </Button>
                </>
              ) : (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={onClose}
                    disabled={applying}
                    aria-label={tr('llmAnalysis.confirm')}
                  >
                    {tr('llmAnalysis.confirm')}
                  </Button>
                  {!confirmForce ? (
                    <Button
                      type="button"
                      size="sm"
                      onClick={() => setConfirmForce(true)}
                      disabled={applying || !hasAdjustment}
                      aria-label={tr('llmAnalysis.forceApply')}
                      className="bg-red-600 hover:bg-red-500 text-white border-red-600"
                      title={!hasAdjustment ? tr('llmAnalysis.noAdjustmentItems') : tr('llmAnalysis.forceApplyTitle')}
                    >
                      {tr('llmAnalysis.forceApply')}
                    </Button>
                  ) : (
                    <Button
                      type="button"
                      size="sm"
                      onClick={() => void handleApply(true)}
                      disabled={applying}
                      aria-label={tr('llmAnalysis.forceApplyConfirm')}
                      className="bg-red-700 hover:bg-red-600 text-white border-red-700"
                    >
                      {applying ? tr('llmAnalysis.forceApplying') : tr('llmAnalysis.forceApplyConfirm')}
                    </Button>
                  )}
                </>
              )}
            </div>
            {!analysis.can_apply && confirmForce && !applying && !success && (
              <p className="text-xs text-red-300/80 pt-1">
                {tr('llmAnalysis.forceApplyWarning')}
              </p>
            )}
          </>
        ) : null}
      </div>
    </div>
  );
}
