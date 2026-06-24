import { useEffect, type ReactNode } from 'react';
import { X, AlertTriangle } from 'lucide-react';
import { useLanguage } from '../../lib/language-context';

// C-8 — 단순 fixed-overlay 확인 다이얼로그.
// shadcn/ui dialog 를 추가하면 라이브러리 의존성이 늘어 본선 D-10 동선에서 위험.
// 기존 NewGroupModal 패턴 (GroupSelector.tsx) 과 동일한 inset overlay + 카드 패턴.

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  cancelLabel,
  destructive = true,
  busy = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const { tr } = useLanguage();
  const resolvedConfirmLabel = confirmLabel ?? tr('confirmDialog.delete');
  const resolvedCancelLabel = cancelLabel ?? tr('confirmDialog.cancel');
  // Esc 키로 닫기 — busy 중에는 무시 (요청 중 dispose 방지).
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onCancel();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, busy, onCancel]);

  if (!open) return null;

  const confirmBtnClass = destructive
    ? 'bg-red-600 hover:bg-red-500 disabled:bg-red-900 text-white'
    : 'bg-green-600 hover:bg-green-500 disabled:bg-green-900 text-white';

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 backdrop-blur-sm"
      onClick={busy ? undefined : onCancel}
      role="dialog"
      aria-modal="true"
    >
      <div
        className="w-full max-w-md mx-4 bg-gradient-to-br from-gray-900 to-black border border-red-500/30 rounded-lg p-5 shadow-2xl"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-start justify-between mb-3 pb-3 border-b border-red-500/20">
          <div className="flex items-start gap-2.5">
            {destructive && (
              <AlertTriangle className="w-5 h-5 text-red-400 flex-shrink-0 mt-0.5" />
            )}
            <h4 className="font-medium text-white">{title}</h4>
          </div>
          <button
            onClick={onCancel}
            className="text-gray-500 hover:text-white disabled:opacity-30"
            aria-label={tr('confirmDialog.close')}
            disabled={busy}
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="space-y-3 text-sm text-gray-200">
          <div>{message}</div>
          {destructive && (
            <p className="text-xs text-red-400 font-mono">
              {tr('confirmDialog.irreversible')}
            </p>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 pt-4 mt-3 border-t border-gray-700/30">
          <button
            onClick={onCancel}
            disabled={busy}
            className="px-3 py-1.5 text-sm text-gray-400 hover:text-white disabled:opacity-30"
          >
            {resolvedCancelLabel}
          </button>
          <button
            onClick={onConfirm}
            disabled={busy}
            className={`px-4 py-1.5 text-sm rounded font-medium disabled:cursor-not-allowed transition-colors ${confirmBtnClass}`}
          >
            {busy ? tr('confirmDialog.processing') : resolvedConfirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
