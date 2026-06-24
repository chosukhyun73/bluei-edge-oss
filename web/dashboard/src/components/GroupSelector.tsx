import { useState } from 'react';
import { Layers, Plus, X, Trash2 } from 'lucide-react';
import type { Group, NewGroupBody } from '../lib/types';
import { Groups, ApiError } from '../lib/api';
import { ConfirmDialog } from './ui/confirm-dialog';
import { useLanguage } from '../lib/language-context';

interface GroupSelectorProps {
  groups: Group[];
  selectedGroupId: string | null;
  onSelectGroup: (groupId: string | null) => void;
  // 신규 그룹 등록 성공 시 호출 — 상위 App 가 Groups.list() refetch + 자동 선택.
  onGroupCreated?: (group: Group) => void;
  // 그룹 삭제 성공 시 호출 — 상위 App 가 refetch.
  onGroupDeleted?: (groupId: string) => void;
}

// 기본 색상 — 운영자가 색을 별도 입력하지 않을 때 자동 부여
const DEFAULT_COLORS = ['#22c55e', '#3b82f6', '#a855f7', '#f59e0b', '#ef4444', '#06b6d4'];

export function GroupSelector({
  groups,
  selectedGroupId,
  onSelectGroup,
  onGroupCreated,
  onGroupDeleted,
}: GroupSelectorProps) {
  const { tr } = useLanguage();
  const [showModal, setShowModal] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState<Group | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDelete = async () => {
    if (!confirmTarget) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await Groups.delete(confirmTarget.group_id);
      onGroupDeleted?.(confirmTarget.group_id);
      if (selectedGroupId === confirmTarget.group_id) onSelectGroup(null);
      setConfirmTarget(null);
    } catch (err) {
      const msg =
        err instanceof ApiError ? `${err.code}: ${err.message}`
        : err instanceof Error ? err.message
        : 'unknown';
      setDeleteError(msg);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-4 sticky top-24">
      <div className="flex items-center justify-between mb-4 pb-3 border-b border-green-500/20">
        <h3 className="font-medium text-white">{tr('groupSelector.panelTitle')}</h3>
        <span className="text-xs text-gray-400 font-mono">{groups.length}{tr('groupSelector.groupCountSuffix')}</span>
      </div>

      <div className="space-y-2">
        {/* 전체 보기 */}
        <button
          onClick={() => onSelectGroup(null)}
          className={`w-full text-left p-3 rounded-lg transition-all ${
            selectedGroupId === null
              ? 'bg-gradient-to-r from-green-600 to-green-500 text-white shadow-lg shadow-green-500/50'
              : 'bg-gray-800/50 text-gray-300 hover:bg-gray-800 hover:border-green-500/30 border border-transparent'
          }`}
        >
          <div className="flex items-center gap-2">
            <Layers className="w-4 h-4" />
            <span className="font-medium">{tr('groupSelector.viewAll')}</span>
          </div>
        </button>

        {/* 개별 그룹 */}
        {groups.map(group => {
          const selected = selectedGroupId === group.group_id;
          return (
            <div key={group.group_id} className="group relative">
              <button
                onClick={() => onSelectGroup(group.group_id)}
                className={`w-full text-left p-3 pr-9 rounded-lg transition-all border ${
                  selected
                    ? 'bg-gradient-to-r from-green-600 to-green-500 text-white shadow-lg shadow-green-500/50 border-transparent'
                    : 'bg-gray-800/50 text-gray-300 hover:bg-gray-800 border-transparent hover:border-green-500/30'
                }`}
              >
                <div className="flex items-center gap-2 mb-1">
                  <div
                    className="w-3 h-3 rounded-full flex-shrink-0"
                    style={{ backgroundColor: group.color }}
                  />
                  <span className="font-medium truncate">{group.name}</span>
                </div>
                <div className="text-xs opacity-70 pl-5 font-mono truncate">{group.description}</div>
              </button>
              <button
                onClick={e => { e.stopPropagation(); setConfirmTarget(group); setDeleteError(null); }}
                className={`absolute top-2 right-2 p-1 rounded transition-opacity ${
                  selected
                    ? 'text-white/70 hover:text-white opacity-100'
                    : 'text-gray-500 hover:text-red-400 opacity-0 group-hover:opacity-100'
                }`}
                aria-label={`${tr('groupSelector.deleteGroupTitle')}: ${group.name}`}
                title={tr('groupSelector.deleteGroupTitle')}
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          );
        })}

        {/* 신규 그룹 등록 진입점 */}
        <button
          onClick={() => setShowModal(true)}
          className="w-full text-left p-3 rounded-lg border border-dashed border-gray-700/60 text-gray-400 hover:text-white hover:border-green-500/40 hover:bg-gray-800/40 transition-all"
        >
          <div className="flex items-center gap-2">
            <Plus className="w-4 h-4" />
            <span className="text-sm">{tr('groupSelector.newGroup')}</span>
          </div>
        </button>
      </div>

      {showModal && (
        <NewGroupModal
          existingIds={groups.map(g => g.group_id)}
          existingCount={groups.length}
          onClose={() => setShowModal(false)}
          onCreated={g => {
            setShowModal(false);
            onGroupCreated?.(g);
            onSelectGroup(g.group_id);
          }}
        />
      )}

      <ConfirmDialog
        open={confirmTarget !== null}
        title={`${tr('groupSelector.deleteGroupTitle')}: ${confirmTarget?.name ?? ''}`}
        message={
          <div className="space-y-2">
            <p>
              <span className="font-mono text-red-300">{confirmTarget?.group_id ?? ''}</span> {tr('groupSelector.deleteGroupConfirmBody')}
            </p>
            <p className="text-xs text-gray-400">
              {tr('groupSelector.deleteGroupWarning')}
            </p>
            {deleteError && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
                {deleteError}
              </div>
            )}
          </div>
        }
        busy={deleting}
        onConfirm={() => void handleDelete()}
        onCancel={() => { if (!deleting) { setConfirmTarget(null); setDeleteError(null); } }}
      />
    </div>
  );
}

interface NewGroupModalProps {
  existingIds: string[];
  existingCount: number;
  onClose: () => void;
  onCreated: (g: Group) => void;
}

function NewGroupModal({ existingIds, existingCount, onClose, onCreated }: NewGroupModalProps) {
  const { tr } = useLanguage();
  const [groupId, setGroupId] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [color, setColor] = useState(DEFAULT_COLORS[existingCount % DEFAULT_COLORS.length]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const idLooksOk = /^[a-z0-9_]{2,40}$/.test(groupId);
  const idConflict = existingIds.includes(groupId);
  const canSubmit = idLooksOk && !idConflict && name.trim().length > 0 && !submitting;

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const body: NewGroupBody = {
        group_id: groupId,
        name: name.trim(),
        description: description.trim(),
        color,
      };
      const res = await Groups.create(body);
      onCreated(res.item);
    } catch (err) {
      const msg =
        err instanceof ApiError ? `${err.code}: ${err.message}`
        : err instanceof Error ? err.message
        : 'unknown';
      setError(msg);
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md mx-4 bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-5 shadow-2xl"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4 pb-3 border-b border-green-500/20">
          <h4 className="font-medium text-white">{tr('groupSelector.newGroupModalTitle')}</h4>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-white"
            aria-label={tr('groupSelector.close')}
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="space-y-3">
          <label className="block">
            <span className="text-xs text-gray-400 font-mono">{tr('groupSelector.fieldId')}</span>
            <input
              type="text"
              value={groupId}
              onChange={e => setGroupId(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ''))}
              placeholder={tr('groupSelector.idPlaceholder')}
              className="w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-green-500/60"
            />
            {groupId && !idLooksOk && (
              <span className="text-xs text-red-400 font-mono">{tr('groupSelector.idFormatHint')}</span>
            )}
            {idConflict && (
              <span className="text-xs text-red-400 font-mono">{tr('groupSelector.idConflict')}</span>
            )}
          </label>

          <label className="block">
            <span className="text-xs text-gray-400 font-mono">{tr('groupSelector.fieldName')}</span>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder={tr('groupSelector.namePlaceholder')}
              className="w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-green-500/60"
            />
          </label>

          <label className="block">
            <span className="text-xs text-gray-400 font-mono">{tr('groupSelector.fieldDescription')}</span>
            <textarea
              value={description}
              onChange={e => setDescription(e.target.value)}
              rows={2}
              placeholder={tr('groupSelector.descriptionPlaceholder')}
              className="w-full mt-1 px-3 py-2 bg-black/50 border border-gray-700 rounded text-sm text-gray-200 focus:outline-none focus:border-green-500/60 resize-none"
            />
          </label>

          <div>
            <span className="text-xs text-gray-400 font-mono">{tr('groupSelector.fieldColor')}</span>
            <div className="flex flex-wrap gap-2 mt-1">
              {DEFAULT_COLORS.map(c => (
                <button
                  key={c}
                  onClick={() => setColor(c)}
                  className={`w-7 h-7 rounded-full transition-all ${
                    color === c ? 'ring-2 ring-white scale-110' : 'opacity-70 hover:opacity-100'
                  }`}
                  style={{ backgroundColor: c }}
                  aria-label={`${tr('groupSelector.colorAriaLabel')} ${c}`}
                />
              ))}
            </div>
          </div>

          {error && (
            <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
              {tr('groupSelector.createError')}: {error}
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              onClick={onClose}
              className="px-3 py-1.5 text-sm text-gray-400 hover:text-white"
              disabled={submitting}
            >
              {tr('groupSelector.cancel')}
            </button>
            <button
              onClick={() => void submit()}
              disabled={!canSubmit}
              className="px-4 py-1.5 text-sm rounded bg-gradient-to-r from-green-600 to-green-500 text-white disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {submitting ? tr('groupSelector.submitting') : tr('groupSelector.submit')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
