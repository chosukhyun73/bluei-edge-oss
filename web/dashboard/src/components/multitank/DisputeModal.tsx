import { useEffect, useState } from 'react';
import { Disputes } from '../../lib/api';
import { DISPUTE_TYPE_LABELS } from '../../lib/format';
import { useLanguage } from '../../lib/language-context';
import { Button } from '../ui/button';

// C-3l: 운영자 이의제기 입력 모달.
// FeedCycleMonitor 의 사이클 상세 패널에서 "이의 제기" 버튼으로 열림.
// decision_id 는 cycle 응답의 echo (arbiter_decisions.resulting_cycle_id 역조회) 사용.

interface DisputeModalProps {
  decisionId: string;
  tankId: string;
  open: boolean;
  onClose: () => void;
  onSubmitted?: () => void;
}

export function DisputeModal({
  decisionId,
  tankId,
  open,
  onClose,
  onSubmitted,
}: DisputeModalProps) {
  const { tr } = useLanguage();
  const [disputeType, setDisputeType] = useState<string>(DISPUTE_TYPE_LABELS[0]?.value ?? 'wrong_condition');
  const [comment, setComment] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  // Close on Escape
  useEffect(() => {
    if (!open) return;
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !submitting) onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open, onClose, submitting]);

  // Reset state when modal opens
  useEffect(() => {
    if (open) {
      setDisputeType(DISPUTE_TYPE_LABELS[0]?.value ?? 'wrong_condition');
      setComment('');
      setError(null);
      setSuccess(false);
      setSubmitting(false);
    }
  }, [open]);

  if (!open) return null;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!decisionId) {
      setError(tr('disputeModal.errorNoDecisionId'));
      return;
    }
    if (!comment.trim()) {
      setError(tr('disputeModal.errorEmptyComment'));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await Disputes.create({
        decision_id: decisionId,
        tank_id: tankId,
        dispute_type: disputeType,
        comment: comment.trim(),
      });
      setSuccess(true);
      onSubmitted?.();
      // 잠시 success 표시 후 자동 닫기 (운영자 확인용)
      setTimeout(() => {
        onClose();
      }, 900);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('disputeModal.errorUnknown');
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={tr('disputeModal.ariaDialog')}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
    >
      <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl p-6 w-full max-w-md space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-white">{tr('disputeModal.title')}</h2>
            <p className="text-xs text-gray-500 mt-0.5">
              Cage/Tank: <span className="font-mono text-gray-400">{tankId}</span>
            </p>
            <p className="text-xs text-gray-500 mt-0.5 font-mono break-all">
              decision_id: {decisionId || tr('disputeModal.none')}
            </p>
          </div>
          <button
            type="button"
            aria-label={tr('disputeModal.ariaClose')}
            disabled={submitting}
            onClick={onClose}
            className="text-gray-500 hover:text-white transition-colors text-lg leading-none disabled:opacity-50"
          >
            ✕
          </button>
        </div>

        {success ? (
          <div className="px-3 py-4 bg-green-500/10 border border-green-500/30 rounded-md text-center">
            <p className="text-sm text-green-300 font-medium">{tr('disputeModal.successTitle')}</p>
            <p className="text-xs text-green-200/70 mt-1">
              {tr('disputeModal.successHint')}
            </p>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4" aria-label={tr('disputeModal.ariaForm')}>
            {error && (
              <p className="text-xs text-red-400 font-mono px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md">
                {error}
              </p>
            )}

            <div className="flex flex-col gap-1">
              <label htmlFor="dispute-type" className="text-xs text-gray-400 font-medium">
                {tr('disputeModal.labelType')}
              </label>
              <select
                id="dispute-type"
                value={disputeType}
                onChange={e => setDisputeType(e.target.value)}
                disabled={submitting}
                className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/50 disabled:opacity-50"
              >
                {DISPUTE_TYPE_LABELS.map(d => (
                  <option key={d.value} value={d.value}>
                    {tr(d.label)}
                  </option>
                ))}
              </select>
            </div>

            <div className="flex flex-col gap-1">
              <label htmlFor="dispute-comment" className="text-xs text-gray-400 font-medium">
                {tr('disputeModal.labelComment')}
              </label>
              <textarea
                id="dispute-comment"
                rows={4}
                value={comment}
                onChange={e => setComment(e.target.value)}
                disabled={submitting}
                placeholder={tr('disputeModal.commentPlaceholder')}
                className="px-3 py-2 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500/50 resize-none disabled:opacity-50"
              />
              <p className="text-xs text-gray-600 leading-relaxed">
                {tr('disputeModal.commentHint')}
              </p>
            </div>

            <div className="flex gap-2 pt-1">
              <Button
                type="submit"
                size="sm"
                disabled={submitting || !decisionId}
                aria-label={tr('disputeModal.ariaSubmit')}
              >
                {submitting ? tr('disputeModal.submitting') : tr('disputeModal.submit')}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onClose}
                disabled={submitting}
                aria-label={tr('disputeModal.ariaCancel')}
              >
                {tr('disputeModal.cancel')}
              </Button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
