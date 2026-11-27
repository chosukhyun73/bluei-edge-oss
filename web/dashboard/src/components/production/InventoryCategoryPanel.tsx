import { useCallback, useEffect, useState } from 'react';
import { dateLocale } from '../../lib/format';
import { Plus, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react';
import { Inventory, ApiError } from '../../lib/api';
import type { InventoryItem, InventoryCategory, FeedComposition, PurchaseHistoryItem } from '../../lib/types';
import { Field, SelectField } from '../sections/TankSettingsSection';
import { Button } from '../ui/button';
import { useLanguage } from '../../lib/language-core';

// ─────────────────────────────────────────────────────────────────────────────
// InventoryCategoryPanel — 재고 현황 + 구매(입고) 폼 + 소모(자재만) 폼
// category: feed=사료, drug=약품, material=기타(현장용품·자재)
// ─────────────────────────────────────────────────────────────────────────────

function friendlyError(err: unknown, tr: (key: string) => string): string {
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return tr('inventoryCategory.unknownError');
}

// ── 카테고리별 accent 색 ──────────────────────────────────────────────────────

type Accent = { border: string; badge: string; text: string; bg: string; trigger: string };

function accentFor(cat: InventoryCategory): Accent {
  switch (cat) {
    case 'feed':
      return {
        border: 'border-blue-500/25',
        badge: 'bg-blue-500/20 text-blue-300 border border-blue-500/40',
        text: 'text-blue-300',
        bg: 'bg-blue-500/10 hover:bg-blue-500/15',
        trigger: 'text-blue-300',
      };
    case 'drug':
      return {
        border: 'border-orange-500/25',
        badge: 'bg-orange-500/20 text-orange-300 border border-orange-500/40',
        text: 'text-orange-300',
        bg: 'bg-orange-500/10 hover:bg-orange-500/15',
        trigger: 'text-orange-300',
      };
    case 'material':
      return {
        border: 'border-teal-500/25',
        badge: 'bg-teal-500/20 text-teal-300 border border-teal-500/40',
        text: 'text-teal-300',
        bg: 'bg-teal-500/10 hover:bg-teal-500/15',
        trigger: 'text-teal-300',
      };
  }
}

// ── 재고 카드 ─────────────────────────────────────────────────────────────────

function ItemCard({ item, accent, onChanged }: { item: InventoryItem; accent: Accent; onChanged?: () => void }) {
  const { tr } = useLanguage();
  return (
    <div className={`bg-gradient-to-br from-gray-900 to-black border ${accent.border} rounded-lg p-3 flex flex-col gap-1`}>
      <div className="flex items-start justify-between gap-1">
        <span className="text-xs font-semibold text-white leading-tight">{item.name}</span>
        {item.below_reorder && (
          <span className="shrink-0 text-[9px] font-bold px-1.5 py-0.5 rounded bg-red-500/20 text-red-400 border border-red-500/40">
            {tr('itemCard.reorderNeeded')}
          </span>
        )}
      </div>
      <div className={`text-2xl font-bold ${accent.text}`}>
        {item.on_hand_qty.toLocaleString(dateLocale())}
        <span className="text-sm font-normal text-gray-400 ml-1">{item.unit}</span>
      </div>
      {item.reorder_level != null && (
        <div className="text-[10px] text-gray-500">
          {tr('itemCard.reorderLevel')}: {item.reorder_level} {item.unit}
        </div>
      )}
      {item.supplier && (
        <div className="text-[10px] text-gray-500 truncate">{tr('itemCard.supplier')}: {item.supplier}</div>
      )}
      {item.category === 'feed' && <CompositionSection item={item} onChanged={onChanged} />}
    </div>
  );
}

// 사료 영양성분표 — 표시 + 편집. 라벨의 보증성분 8개 + 제품메타. label 은 i18n 키.
const COMPOSITION_FIELDS: { key: keyof FeedComposition; label: string }[] = [
  { key: 'crude_protein_pct', label: 'inventoryPanel.compCrudeProtein' },
  { key: 'crude_fat_pct', label: 'inventoryPanel.compCrudeFat' },
  { key: 'lysine_pct', label: 'inventoryPanel.compLysine' },
  { key: 'total_phosphorus_pct', label: 'inventoryPanel.compTotalPhosphorus' },
  { key: 'moisture_pct', label: 'inventoryPanel.compMoisture' },
  { key: 'crude_fiber_pct', label: 'inventoryPanel.compCrudeFiber' },
  { key: 'crude_ash_pct', label: 'inventoryPanel.compCrudeAsh' },
  { key: 'calcium_pct', label: 'inventoryPanel.compCalcium' },
];

function CompositionSection({ item, onChanged }: { item: InventoryItem; onChanged?: () => void }) {
  const { tr } = useLanguage();
  const [editing, setEditing] = useState(false);
  const comp = item.composition ?? null;
  const has = comp && Object.values(comp).some(v => v !== undefined && v !== null && v !== '');

  return (
    <div className="mt-1.5 pt-1.5 border-t border-gray-700/40">
      {has && !editing && (
        <div className="flex flex-col gap-0.5">
          <div className="flex flex-wrap gap-x-2 gap-y-0.5 text-[10px]">
            {comp?.crude_protein_pct != null && <span className="text-blue-300">{tr('inventoryPanel.compProteinShort')} {comp.crude_protein_pct}%</span>}
            {comp?.crude_fat_pct != null && <span className="text-amber-300">{tr('inventoryPanel.compFatShort')} {comp.crude_fat_pct}%</span>}
            {comp?.total_phosphorus_pct != null && <span className="text-cyan-400">{tr('inventoryPanel.compPhosphorusShort')} {comp.total_phosphorus_pct}%</span>}
            {comp?.calcium_pct != null && <span className="text-gray-400">Ca {comp.calcium_pct}%</span>}
          </div>
          {(comp?.product_model || comp?.pellet_size_mm != null) && (
            <div className="text-[9px] text-gray-600 truncate">
              {comp?.product_model}{comp?.pellet_size_mm != null ? ` · ${comp.pellet_size_mm}mm` : ''}
            </div>
          )}
        </div>
      )}
      {!editing && (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="text-[10px] text-blue-400 hover:text-blue-300 mt-0.5"
        >
          {has ? tr('inventoryPanel.btnEditComposition') : tr('inventoryPanel.btnAddComposition')}
        </button>
      )}
      {editing && (
        <CompositionEditor
          item={item}
          onDone={() => { setEditing(false); onChanged?.(); }}
          onCancel={() => setEditing(false)}
          tr={tr}
        />
      )}
    </div>
  );
}

function CompositionEditor({
  item, onDone, onCancel, tr,
}: {
  item: InventoryItem;
  onDone: () => void;
  onCancel: () => void;
  tr: (key: string) => string;
}) {
  const c = item.composition ?? {};
  const numInit = (v?: number) => (v == null ? '' : String(v));
  const [vals, setVals] = useState<Record<string, string>>(() => {
    const o: Record<string, string> = {};
    for (const f of COMPOSITION_FIELDS) o[f.key] = numInit(c[f.key] as number | undefined);
    return o;
  });
  const [model, setModel] = useState(c.product_model ?? '');
  const [pellet, setPellet] = useState(numInit(c.pellet_size_mm));
  const [species, setSpecies] = useState(c.target_species ?? '');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async () => {
    setBusy(true);
    setErr(null);
    try {
      const body: FeedComposition = {};
      for (const f of COMPOSITION_FIELDS) {
        const raw = vals[f.key];
        if (raw !== '' && !Number.isNaN(Number(raw))) (body[f.key] as number) = Number(raw);
      }
      if (model.trim()) body.product_model = model.trim();
      if (pellet !== '' && !Number.isNaN(Number(pellet))) body.pellet_size_mm = Number(pellet);
      if (species.trim()) body.target_species = species.trim();
      await Inventory.setComposition(item.item_id, body);
      onDone();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-1.5 flex flex-col gap-1.5">
      <div className="grid grid-cols-2 gap-1">
        {COMPOSITION_FIELDS.map(f => (
          <label key={f.key} className="text-[9px] text-gray-400 flex flex-col gap-0.5">
            {tr(f.label)}
            <input
              type="number"
              step="0.01"
              value={vals[f.key]}
              onChange={e => setVals(v => ({ ...v, [f.key]: e.target.value }))}
              className="px-1.5 py-0.5 text-[11px] bg-gray-800 border border-gray-600 rounded text-white w-full"
            />
          </label>
        ))}
      </div>
      <label className="text-[9px] text-gray-400 flex flex-col gap-0.5">
        {tr('inventoryPanel.fieldProductModel')}
        <input value={model} onChange={e => setModel(e.target.value)}
          placeholder={tr('inventoryPanel.phProductModel')}
          className="px-1.5 py-0.5 text-[11px] bg-gray-800 border border-gray-600 rounded text-white w-full" />
      </label>
      <div className="grid grid-cols-2 gap-1">
        <label className="text-[9px] text-gray-400 flex flex-col gap-0.5">
          {tr('inventoryPanel.fieldPelletSize')}
          <input type="number" step="0.1" value={pellet} onChange={e => setPellet(e.target.value)}
            className="px-1.5 py-0.5 text-[11px] bg-gray-800 border border-gray-600 rounded text-white w-full" />
        </label>
        <label className="text-[9px] text-gray-400 flex flex-col gap-0.5">
          {tr('inventoryPanel.fieldTargetSpecies')}
          <input value={species} onChange={e => setSpecies(e.target.value)} placeholder={tr('inventoryPanel.phTargetSpecies')}
            className="px-1.5 py-0.5 text-[11px] bg-gray-800 border border-gray-600 rounded text-white w-full" />
        </label>
      </div>
      {err && <div className="text-[10px] text-red-400 font-mono">{err}</div>}
      <div className="flex gap-1">
        <button type="button" onClick={() => void submit()} disabled={busy}
          className="px-2 py-0.5 text-[11px] rounded bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white">
          {busy ? tr('inventoryPanel.saving') : tr('inventoryPanel.btnSave')}
        </button>
        <button type="button" onClick={onCancel} className="px-2 py-0.5 text-[11px] rounded bg-gray-700 text-gray-200">{tr('inventoryPanel.btnCancel')}</button>
      </div>
    </div>
  );
}

// ── 구매(입고) 폼 ─────────────────────────────────────────────────────────────

const PURCHASE_DOC_TYPES = [
  { value: 'transaction_statement', labelKey: 'inventoryCategory.docTransactionStatement' },
  { value: 'tax_invoice', labelKey: 'inventoryCategory.docTaxInvoice' },
  { value: 'vehicle_doc', labelKey: 'inventoryCategory.docVehicleDoc' },
  { value: 'certification', labelKey: 'inventoryCategory.docCertification' },
  { value: 'other', labelKey: 'inventoryCategory.docOther' },
];

function PurchaseForm({
  category,
  items,
  accent,
  onPurchased,
}: {
  category: InventoryCategory;
  items: InventoryItem[];
  accent: Accent;
  onPurchased: () => void;
}) {
  const [open, setOpen] = useState(false);

  // 기존 품목 선택 or 신규
  const [itemId, setItemId] = useState('__new__');
  const [newName, setNewName] = useState('');
  const [newUnit, setNewUnit] = useState('');

  const [qty, setQty] = useState('');
  const [unitPrice, setUnitPrice] = useState('');
  const [supplier, setSupplier] = useState('');
  const [lot, setLot] = useState('');
  const [purchasedAt, setPurchasedAt] = useState('');

  // 서류 첨부 (입고 완료 후 표시)
  const [purchaseId, setPurchaseId] = useState<string | null>(null);
  const [docType, setDocType] = useState(PURCHASE_DOC_TYPES[0].value);
  const [docFile, setDocFile] = useState<File | null>(null);
  const [docBusy, setDocBusy] = useState(false);
  const [docOk, setDocOk] = useState<string | null>(null);
  const [docErr, setDocErr] = useState<string | null>(null);

  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const { tr } = useLanguage();

  const reset = () => {
    setItemId('__new__');
    setNewName('');
    setNewUnit('');
    setQty('');
    setUnitPrice('');
    setSupplier('');
    setLot('');
    setPurchasedAt('');
    setErr(null);
    setOk(null);
    setPurchaseId(null);
    setDocFile(null);
    setDocOk(null);
    setDocErr(null);
  };

  const submit = async () => {
    if (!qty || Number(qty) <= 0) { setErr(tr('purchaseForm.errQtyRequired')); return; }
    if (itemId === '__new__' && !newName.trim()) { setErr(tr('purchaseForm.errNameRequired')); return; }
    if (itemId === '__new__' && !newUnit.trim()) { setErr(tr('purchaseForm.errUnitRequired')); return; }
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      const body: Parameters<typeof Inventory.purchase>[0] = { qty: Number(qty) };
      if (itemId !== '__new__') {
        body.item_id = itemId;
      } else {
        body.category = category;
        body.name = newName.trim();
        body.unit = newUnit.trim();
      }
      if (unitPrice) body.unit_price = Number(unitPrice);
      if (supplier.trim()) body.supplier = supplier.trim();
      if (lot.trim()) body.lot = lot.trim();
      // 날짜 입력(YYYY-MM-DD)을 백엔드가 요구하는 RFC3339 타임스탬프로 변환.
      if (purchasedAt) body.purchased_at = new Date(purchasedAt).toISOString();
      const r = await Inventory.purchase(body);
      setOk(tr('inventoryCategory.purchaseOk', { name: r.name, qty: r.on_hand_qty, unit: r.unit }));
      setPurchaseId(r.purchase_id);
      onPurchased();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  const uploadDoc = async () => {
    if (!purchaseId || !docFile) return;
    setDocBusy(true);
    setDocErr(null);
    setDocOk(null);
    try {
      const form = new FormData();
      form.append('file', docFile);
      form.append('doc_type', docType);
      await Inventory.uploadPurchaseDoc(purchaseId, form);
      setDocOk(tr('purchaseForm.docUploadOk'));
      setDocFile(null);
    } catch (e) {
      setDocErr(friendlyError(e, tr));
    } finally {
      setDocBusy(false);
    }
  };

  return (
    <div className={`border ${accent.border} rounded-md overflow-hidden`}>
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className={`w-full flex items-center gap-2 px-3 py-2 ${accent.bg} text-xs ${accent.trigger} font-medium`}
      >
        <Plus className="w-3.5 h-3.5" />
        {tr('purchaseForm.trigger')}
        {open
          ? <ChevronDown className="w-3 h-3 ml-auto" />
          : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          {/* 품목 선택 */}
          <div>
            <label className="text-[11px] text-gray-400 font-medium block mb-0.5">{tr('purchaseForm.itemSelectLabel')}</label>
            <select
              value={itemId}
              onChange={e => setItemId(e.target.value)}
              className="w-full h-8 px-2 rounded border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
            >
              <option value="__new__">{tr('purchaseForm.newItemOption')}</option>
              {items.map(it => (
                <option key={it.item_id} value={it.item_id}>
                  {it.name} ({it.unit})
                </option>
              ))}
            </select>
          </div>

          {/* 신규 품목 입력 */}
          {itemId === '__new__' && (
            <div className="grid grid-cols-2 gap-3">
              <Field label={tr('purchaseForm.fieldName')} value={newName} onChange={setNewName} />
              <Field label={tr('purchaseForm.fieldUnit')} value={newUnit} onChange={setNewUnit} />
            </div>
          )}

          {/* 입고 정보 */}
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('purchaseForm.fieldQty')} type="number" value={qty} onChange={setQty} />
            <Field label={tr('purchaseForm.fieldUnitPrice')} type="number" value={unitPrice} onChange={setUnitPrice} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('purchaseForm.fieldSupplier')} value={supplier} onChange={setSupplier} />
            <Field label={tr('purchaseForm.fieldLot')} value={lot} onChange={setLot} />
          </div>
          <Field label={tr('purchaseForm.fieldPurchasedAt')} type="date" value={purchasedAt} onChange={setPurchasedAt} />

          {err && (
            <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>
          )}
          {ok && (
            <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">{ok}</div>
          )}

          {!purchaseId ? (
            <div className="flex items-center gap-2">
              <Button size="sm" onClick={() => void submit()} disabled={busy || !qty}>
                <Plus className="w-3.5 h-3.5 mr-1" />
                {busy ? tr('purchaseForm.registering') : tr('purchaseForm.btnRegister')}
              </Button>
              <button
                type="button"
                onClick={reset}
                className="text-[11px] text-gray-500 hover:text-gray-300"
              >
                {tr('purchaseForm.btnReset')}
              </button>
            </div>
          ) : (
            /* 거래명세서/계산서 첨부 */
            <div className="p-2.5 bg-gray-800/60 border border-gray-700/60 rounded space-y-2">
              <p className="text-[11px] text-gray-300 font-medium">{tr('purchaseForm.docSectionTitle')}</p>
              <div className="grid grid-cols-2 gap-2">
                <SelectField
                  label={tr('purchaseForm.docTypeLabel')}
                  value={docType}
                  onChange={setDocType}
                  options={PURCHASE_DOC_TYPES.map(d => ({ value: d.value, label: tr(d.labelKey) }))}
                />
                <div className="flex flex-col gap-0.5">
                  <label className="text-[10px] text-gray-400 font-medium">{tr('purchaseForm.docFileLabel')}</label>
                  <input
                    type="file"
                    accept=".pdf,.jpg,.jpeg,.png,.heic,.xlsx"
                    onChange={e => setDocFile(e.target.files?.[0] ?? null)}
                    className="text-[10px] text-gray-300 file:mr-2 file:py-0.5 file:px-2 file:rounded file:border-0 file:text-[10px] file:bg-gray-700 file:text-gray-200 hover:file:bg-gray-600"
                  />
                </div>
              </div>
              {docErr && (
                <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[10px] text-red-400 font-mono">{docErr}</div>
              )}
              {docOk && (
                <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[10px] text-green-400 font-mono">{docOk}</div>
              )}
              <div className="flex items-center gap-2">
                <Button size="sm" onClick={() => void uploadDoc()} disabled={docBusy || !docFile} className="text-[11px] h-6 px-2">
                  {docBusy ? tr('purchaseForm.uploading') : tr('purchaseForm.btnAttach')}
                </Button>
                <button
                  type="button"
                  onClick={reset}
                  className="text-[11px] text-gray-500 hover:text-gray-300"
                >
                  {tr('purchaseForm.btnDone')}
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── 소모(자재 사용) 폼 ────────────────────────────────────────────────────────

function ConsumeForm({
  items,
  accent,
  onConsumed,
}: {
  items: InventoryItem[];
  accent: Accent;
  onConsumed: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [itemId, setItemId] = useState('');
  const [qty, setQty] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const { tr } = useLanguage();

  useEffect(() => {
    if (items.length > 0 && !itemId) setItemId(items[0].item_id);
  }, [items, itemId]);

  const submit = async () => {
    if (!itemId) { setErr(tr('consumeForm.errItemRequired')); return; }
    if (!qty || Number(qty) <= 0) { setErr(tr('consumeForm.errQtyRequired')); return; }
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      const r = await Inventory.consume({
        item_id: itemId,
        qty: Number(qty),
        reason: reason.trim() || undefined,
      });
      const used = items.find(it => it.item_id === r.item_id);
      setOk(tr('inventoryCategory.consumeOk', { qty: r.on_hand_qty, unit: used?.unit ?? '' }));
      setQty('');
      setReason('');
      onConsumed();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={`border ${accent.border} rounded-md overflow-hidden`}>
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className={`w-full flex items-center gap-2 px-3 py-2 ${accent.bg} text-xs ${accent.trigger} font-medium`}
      >
        <RefreshCw className="w-3.5 h-3.5" />
        {tr('consumeForm.trigger')}
        {open
          ? <ChevronDown className="w-3 h-3 ml-auto" />
          : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <div>
            <label className="text-[11px] text-gray-400 font-medium block mb-0.5">{tr('consumeForm.itemLabel')}</label>
            <select
              value={itemId}
              onChange={e => setItemId(e.target.value)}
              className="w-full h-8 px-2 rounded border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
            >
              {items.map(it => (
                <option key={it.item_id} value={it.item_id}>
                  {tr('inventoryCategory.optionRemain', { name: it.name, qty: it.on_hand_qty, unit: it.unit })}
                </option>
              ))}
            </select>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('consumeForm.fieldQty')} type="number" value={qty} onChange={setQty} />
            <Field label={tr('consumeForm.fieldReason')} value={reason} onChange={setReason} />
          </div>
          {err && (
            <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>
          )}
          {ok && (
            <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">{ok}</div>
          )}
          <Button size="sm" onClick={() => void submit()} disabled={busy || !qty || !itemId}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {busy ? tr('consumeForm.registering') : tr('consumeForm.btnRegister')}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── 구매(입고) 이력 리스트 ────────────────────────────────────────────────────

function fmtDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString(dateLocale(), {
      year: '2-digit', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    });
  } catch {
    return iso;
  }
}

function PurchaseHistoryList({ category, reloadKey }: { category: InventoryCategory; reloadKey: number }) {
  const { tr } = useLanguage();
  const [rows, setRows] = useState<PurchaseHistoryItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    setErr(null);
    Inventory.listPurchases(category, 50)
      .then(r => { if (alive) setRows(r.items); })
      .catch(e => { if (alive) setErr(friendlyError(e, tr)); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [category, reloadKey]);

  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4 space-y-2">
      <h3 className="text-xs font-medium text-gray-400">{tr('inventoryPanel.purchaseHistoryTitle')}</h3>
      {loading && <div className="text-xs text-gray-500">{tr('inventoryPanel.loading')}</div>}
      {err && <div className="text-xs text-red-400 font-mono">{err}</div>}
      {!loading && !err && rows.length === 0 && (
        <div className="text-xs text-gray-500">{tr('inventoryPanel.purchaseHistoryEmpty')}</div>
      )}
      {rows.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-[11px]">
            <thead>
              <tr className="text-gray-500 border-b border-gray-700/40">
                <th className="text-left font-medium py-1 pr-2">{tr('inventoryPanel.colDate')}</th>
                <th className="text-left font-medium py-1 pr-2">{tr('inventoryPanel.colItem')}</th>
                <th className="text-right font-medium py-1 pr-2">{tr('inventoryPanel.colQty')}</th>
                <th className="text-left font-medium py-1 pr-2">{tr('inventoryPanel.colSupplier')}</th>
                <th className="text-left font-medium py-1 pr-2">{tr('inventoryPanel.colLot')}</th>
                <th className="text-right font-medium py-1">{tr('inventoryPanel.colAmount')}</th>
              </tr>
            </thead>
            <tbody className="font-mono">
              {rows.map(p => (
                <tr key={p.purchase_id} className="border-b border-gray-800/60 text-gray-300">
                  <td className="py-1 pr-2 whitespace-nowrap text-gray-400">{fmtDate(p.purchased_at)}</td>
                  <td className="py-1 pr-2">{p.name}</td>
                  <td className="py-1 pr-2 text-right tabular-nums">{p.qty.toLocaleString(dateLocale())} {p.unit}</td>
                  <td className="py-1 pr-2 text-gray-400 truncate max-w-[8rem]">{p.supplier || '—'}</td>
                  <td className="py-1 pr-2 text-gray-500">{p.lot || '—'}</td>
                  <td className="py-1 text-right tabular-nums text-gray-400">
                    {p.total_price ? tr('inventoryPanel.amountKrw', { amount: p.total_price.toLocaleString(dateLocale()) }) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ── InventoryCategoryPanel (exported) ────────────────────────────────────────

export function InventoryCategoryPanel({
  category,
  title,
}: {
  category: InventoryCategory;
  title: string;
}) {
  const [items, setItems] = useState<InventoryItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const accent = accentFor(category);
  const { tr } = useLanguage();

  const load = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      const r = await Inventory.list(category);
      setItems(r.items);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setLoading(false);
    }
  }, [category]);

  useEffect(() => { void load(); }, [load]);

  return (
    <div className="space-y-4">
      {/* 재고 현황 */}
      <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4 space-y-3">
        <div className="flex items-center justify-between">
          <h3 className={`text-sm font-semibold ${accent.text}`}>{title} {tr('inventoryPanel.stockStatus')}</h3>
          <button
            type="button"
            onClick={() => void load()}
            className="text-[11px] text-gray-500 hover:text-gray-300 flex items-center gap-1"
          >
            <RefreshCw className="w-3 h-3" />
            {tr('inventoryPanel.refresh')}
          </button>
        </div>
        {loading && <div className="text-xs text-gray-500">{tr('inventoryPanel.loading')}</div>}
        {err && <div className="text-xs text-red-400 font-mono">{err}</div>}
        {!loading && !err && items.length === 0 && (
          <div className="text-xs text-gray-500">{tr('inventoryPanel.emptyItems')}</div>
        )}
        {items.length > 0 && (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-2">
            {items.map(it => (
              <ItemCard key={it.item_id} item={it} accent={accent} onChanged={() => void load()} />
            ))}
          </div>
        )}
      </div>

      {/* 구매(입고) 폼 */}
      <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4 space-y-2">
        <h3 className="text-xs font-medium text-gray-400">{title} {tr('inventoryPanel.purchaseSection')}</h3>
        <PurchaseForm
          category={category}
          items={items}
          accent={accent}
          onPurchased={() => { void load(); setReloadKey(k => k + 1); }}
        />
      </div>

      {/* 구매·입고 이력 */}
      <PurchaseHistoryList category={category} reloadKey={reloadKey} />

      {/* 소모 폼 — material 전용 */}
      {category === 'material' && (
        <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4 space-y-2">
          <h3 className="text-xs font-medium text-gray-400">{tr('inventoryPanel.consumeSection')}</h3>
          <ConsumeForm
            items={items}
            accent={accent}
            onConsumed={() => void load()}
          />
        </div>
      )}
    </div>
  );
}
