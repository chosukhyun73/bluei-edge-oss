import { useCallback, useEffect, useState } from 'react';
import { dateLocale } from '../../lib/format';
import { useLanguage } from '../../lib/language-core';
import { Plus, ChevronDown, ChevronRight, Syringe, Skull, ArrowRightLeft, ListTree, Utensils, Paperclip } from 'lucide-react';
import { Tanks, Feedings, Documents, Inventory, ApiError } from '../../lib/api';
import type {
  Tank, TraceabilityCTE, TankTraceabilityResponse,
  NewTreatmentBody, NewMortalityBody, NewTransferBody, NewFeedingBody,
  TreatmentType, TransferType, TraceabilityDoc,
  InventoryItem,
} from '../../lib/types';
import { Field, SelectField } from '../sections/TankSettingsSection';
import { Button } from '../ui/button';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../ui/tabs';

// ─────────────────────────────────────────────────────────────────────────────
// ProductionEventLog — 생산·이벤트 기록 탭
// 탱크 선택 → 기록 종류별 서브탭(사료/투약/폐사/이동) → 각 탭 = 입력 폼 + 그 종류 이력
//
// 종전에는 아코디언 4개 아래에 전 종류가 섞인 CTE 타임라인 하나였다. 사료가 하루 8회
// 쌓이다 보니 (tank_01 기준 105건 중 99건) 폐사 4건·투약 기록이 그 사이에 묻혀
// 사실상 조회가 불가능했다. 종류별로 화면을 갈라 이력을 바로 보게 한다 (2026-10-05).
// ─────────────────────────────────────────────────────────────────────────────

function friendlyError(err: unknown, tr: (key: string) => string): string {
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return tr('productionEventLog.unknownError');
}

// ── doc_type 분류 체계 ────────────────────────────────────────────────────────

type DocTypeEntry = { value: string; labelKey: string };

const DOC_TYPES_BY_CTE: Record<string, DocTypeEntry[]> = {
  stocking: [
    { value: 'broodstock_info',       labelKey: 'productionEventLog.docBroodstockInfo' },
    { value: 'producer_license',      labelKey: 'productionEventLog.docProducerLicense' },
    { value: 'transaction_statement', labelKey: 'productionEventLog.docTransactionStatement' },
    { value: 'tax_invoice',           labelKey: 'productionEventLog.docTaxInvoice' },
  ],
  feeding: [
    { value: 'feed_purchase_statement', labelKey: 'productionEventLog.docFeedPurchaseStatement' },
    { value: 'product_spec',            labelKey: 'productionEventLog.docProductSpec' },
    { value: 'certification',           labelKey: 'productionEventLog.docCertification' },
  ],
  treatment: [
    { value: 'prescription',            labelKey: 'productionEventLog.docPrescription' },
    { value: 'drug_purchase_statement', labelKey: 'productionEventLog.docDrugPurchaseStatement' },
  ],
  mortality: [
    { value: 'diagnosis_certificate', labelKey: 'productionEventLog.docDiagnosisCertificate' },
  ],
  transfer: [
    { value: 'transaction_statement', labelKey: 'productionEventLog.docTransactionStatement' },
    { value: 'tax_invoice',           labelKey: 'productionEventLog.docTaxInvoice' },
    { value: 'vehicle_doc',           labelKey: 'productionEventLog.docVehicleDoc' },
  ],
  sale: [
    { value: 'transaction_statement', labelKey: 'productionEventLog.docTransactionStatement' },
    { value: 'tax_invoice',           labelKey: 'productionEventLog.docTaxInvoice' },
    { value: 'vehicle_doc',           labelKey: 'productionEventLog.docVehicleDoc' },
  ],
  harvest: [
    { value: 'transaction_statement', labelKey: 'productionEventLog.docTransactionStatement' },
    { value: 'tax_invoice',           labelKey: 'productionEventLog.docTaxInvoice' },
  ],
};

const OTHER_DOC: DocTypeEntry = { value: 'other', labelKey: 'productionEventLog.docOther' };

function docTypesForCte(cteKey: string): DocTypeEntry[] {
  const base = DOC_TYPES_BY_CTE[cteKey] ?? [];
  return [...base, OTHER_DOC];
}

function docTypeLabelKey(docType: string): string | undefined {
  for (const list of Object.values(DOC_TYPES_BY_CTE)) {
    const found = list.find(d => d.value === docType);
    if (found) return found.labelKey;
  }
  if (docType === 'other') return 'productionEventLog.docOther';
  return undefined;
}

// ── CTE id 추출 헬퍼 ──────────────────────────────────────────────────────────

function cteEventId(item: TraceabilityCTE): string | undefined {
  const p = item.payload;
  const key = `${item.type}_id`;
  const val = p[key];
  if (typeof val === 'string') return val;
  // sampling_id fallback
  if (item.type === 'sampling') {
    const sid = p['sampling_id'];
    if (typeof sid === 'string') return sid;
  }
  return undefined;
}

// CTE 키 (transfer + sale 은 'sale' 로 분리해 doc_type 분류)
function cteDocKey(item: TraceabilityCTE): string {
  if (item.type === 'transfer' && item.payload['transfer_type'] === 'sale') return 'sale';
  return item.type;
}

// ── CTE 타임라인 helpers ──────────────────────────────────────────────────────

function cteTypeBadge(type: TraceabilityCTE['type']): string {
  switch (type) {
    case 'stocking':  return 'bg-green-500/20 text-green-400 border border-green-500/30';
    case 'feeding':   return 'bg-blue-500/20 text-blue-400 border border-blue-500/30';
    case 'sampling':  return 'bg-gray-500/20 text-gray-400 border border-gray-500/30';
    case 'treatment': return 'bg-orange-500/20 text-orange-400 border border-orange-500/30';
    case 'mortality': return 'bg-red-500/20 text-red-400 border border-red-500/30';
    case 'transfer':  return 'bg-purple-500/20 text-purple-400 border border-purple-500/30';
    case 'harvest':   return 'bg-teal-500/20 text-teal-400 border border-teal-500/30';
    default:          return 'bg-gray-700 text-gray-300';
  }
}

type Tr = (key: string, vars?: Record<string, string | number>) => string;

function cteTypeName(type: TraceabilityCTE['type'], tr: Tr): string {
  const keys: Record<string, string> = {
    stocking: 'productionEventLog.cteStocking',
    feeding: 'productionEventLog.cteFeeding',
    sampling: 'productionEventLog.cteSampling',
    treatment: 'productionEventLog.cteTreatment',
    mortality: 'productionEventLog.cteMortality',
    transfer: 'productionEventLog.cteTransfer',
    harvest: 'productionEventLog.cteHarvest',
  };
  const k = keys[type];
  return k ? tr(k) : type;
}

function cteSummary(item: TraceabilityCTE, tr: Tr): string {
  const p = item.payload;
  switch (item.type) {
    case 'stocking': {
      const lotPart = p['lot_no'] ? ` (lot ${p['lot_no']})` : '';
      return tr('productionEventLog.summaryStocked', { count: String(p['initial_count'] ?? '?') }) + lotPart;
    }
    case 'feeding': {
      const typePart = p['feed_type'] ? ` ${p['feed_type']}` : '';
      const lotPart = p['feed_lot'] ? ` lot ${p['feed_lot']}` : '';
      return tr('productionEventLog.summaryFeed', { amount: p['feed_amount_g'] != null ? Number(p['feed_amount_g']).toFixed(1) : '?' }) + typePart + lotPart;
    }
    case 'sampling':
      return tr('productionEventLog.summarySampling', { weight: String(p['avg_weight_g'] ?? '?') });
    case 'treatment': {
      const sub = p['substance'] ?? '';
      const dose = p['dose'] != null ? ` ${p['dose']}${p['dose_unit'] ?? ''}` : '';
      return tr('productionEventLog.summaryTreatment', { substance: String(sub) }) + dose;
    }
    case 'mortality': {
      // 운영자가 입력한 추정 원인·폐사중량을 같이 남긴다 — 마릿수만으로는 나중에
      // 왜 죽었는지 되짚을 수 없다 (2026-10-05 운영자 요청). 원인은 자유 입력이라
      // 가운뎃점으로 끊어 붙인다.
      const cause = p['estimated_cause'] ? ` · ${p['estimated_cause']}` : '';
      const weight = p['dead_weight_g'] != null ? ` · ${Number(p['dead_weight_g']).toFixed(0)}g` : '';
      return tr('productionEventLog.summaryMortality', { count: String(p['dead_count'] ?? '?') }) + cause + weight;
    }
    case 'transfer':
      if (p['transfer_type'] === 'sale') {
        return tr('productionEventLog.summarySale', { destination: String(p['destination_name'] ?? '?') });
      }
      return `${p['transfer_type'] ?? ''} → ${p['to_tank_id'] ?? '?'}`;
    case 'harvest':
      return tr('productionEventLog.summaryHarvest', { count: String(p['harvested_count'] ?? '?') });
    default:
      return '';
  }
}

// ── 서류 첨부 인라인 업로드 폼 ───────────────────────────────────────────────

function DocUploadForm({
  tankId,
  cteType,
  eventRef,
  operatorId,
  onUploaded,
  onCancel,
}: {
  tankId: string;
  cteType: string;       // doc_type 분류용 키 ('sale' 포함)
  eventRef?: string;
  operatorId: string;
  onUploaded: () => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const docTypes = docTypesForCte(cteType);
  const [docType, setDocType] = useState(docTypes[0]?.value ?? 'other');
  const [file, setFile] = useState<File | null>(null);
  const [notes, setNotes] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async () => {
    if (!file) { setErr(tr('docUploadForm.selectFile')); return; }
    setBusy(true);
    setErr(null);
    try {
      const form = new FormData();
      form.append('file', file);
      // traceability cte_type 은 transfer/sale 모두 'transfer' 로 전송 (backend 스키마)
      form.append('cte_type', cteType === 'sale' ? 'transfer' : cteType);
      form.append('doc_type', docType);
      if (eventRef) form.append('event_ref', eventRef);
      if (notes.trim()) form.append('notes', notes.trim());
      if (operatorId.trim()) form.append('operator_id', operatorId.trim());
      await Documents.upload(tankId, form);
      onUploaded();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-1.5 p-2.5 bg-gray-800/60 border border-gray-700/60 rounded space-y-2">
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('docUploadForm.docTypeLabel')}</label>
          <select
            value={docType}
            onChange={e => setDocType(e.target.value)}
            className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
          >
            {docTypes.map(d => (
              <option key={d.value} value={d.value}>{tr(d.labelKey)}</option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('docUploadForm.fileLabel')}</label>
          <input
            type="file"
            accept=".pdf,.jpg,.jpeg,.png,.heic,.xlsx"
            onChange={e => setFile(e.target.files?.[0] ?? null)}
            className="text-[10px] text-gray-300 file:mr-2 file:py-0.5 file:px-2 file:rounded file:border-0 file:text-[10px] file:bg-gray-700 file:text-gray-200 hover:file:bg-gray-600"
          />
        </div>
      </div>
      <div className="flex flex-col gap-0.5">
        <label className="text-[10px] text-gray-400 font-medium">{tr('docUploadForm.notesLabel')}</label>
        <input
          type="text"
          value={notes}
          onChange={e => setNotes(e.target.value)}
          className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
          placeholder={tr('docUploadForm.notesPlaceholder')}
        />
      </div>
      {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[10px] text-red-400 font-mono">{err}</div>}
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => void submit()} disabled={busy || !file} className="text-[11px] h-6 px-2">
          {busy ? tr('docUploadForm.submitting') : tr('docUploadForm.submit')}
        </Button>
        <button
          type="button"
          onClick={onCancel}
          className="text-[11px] text-gray-500 hover:text-gray-300"
        >
          {tr('docUploadForm.cancel')}
        </button>
      </div>
    </div>
  );
}

// ── 타임라인 행 서류 표시 ─────────────────────────────────────────────────────

function TimelineRowDocs({
  item,
  docs,
  tankId,
  operatorId,
  onUploaded,
}: {
  item: TraceabilityCTE;
  docs: TraceabilityDoc[];
  tankId: string;
  operatorId: string;
  onUploaded: () => void;
}) {
  const { tr } = useLanguage();
  const [uploadOpen, setUploadOpen] = useState(false);
  const eventRef = cteEventId(item);
  const docKey = cteDocKey(item);

  return (
    <div className="mt-1 ml-0 space-y-0.5">
      {/* 기존 첨부 서류 목록 */}
      {docs.map(doc => (
        <div key={doc.document_id} className="flex items-center gap-1 text-[10px]">
          <Paperclip className="w-2.5 h-2.5 text-gray-500 shrink-0" />
          <a
            href={doc.download_url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-teal-400 hover:text-teal-300 hover:underline truncate max-w-[220px]"
          >
            {(() => { const k = docTypeLabelKey(doc.doc_type); return k ? tr(k) : doc.doc_type; })()} · {doc.filename}
          </a>
        </div>
      ))}

      {/* 서류 카운트 + 첨부 버튼 */}
      <div className="flex items-center gap-2">
        {docs.length > 0 && (
          <span className="text-[10px] text-gray-500">📎 {tr('productionEventLog.docCount', { count: docs.length })}</span>
        )}
        {!uploadOpen && (
          <button
            type="button"
            onClick={() => setUploadOpen(true)}
            className="text-[10px] text-teal-500 hover:text-teal-400 border border-teal-700/40 hover:border-teal-500/60 rounded px-1.5 py-0.5 leading-none"
          >
            {tr('timelineRowDocs.attach')}
          </button>
        )}
      </div>

      {uploadOpen && (
        <DocUploadForm
          tankId={tankId}
          cteType={docKey}
          eventRef={eventRef}
          operatorId={operatorId}
          onUploaded={() => {
            setUploadOpen(false);
            onUploaded();
          }}
          onCancel={() => setUploadOpen(false)}
        />
      )}
    </div>
  );
}

// ── FeedingForm ───────────────────────────────────────────────────────────────

function FeedingForm({ tankId, onSaved }: { tankId: string; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(true); // 탭당 폼 1개 — 기본 펼침
  const [feedAmountG, setFeedAmountG] = useState('');
  const [feedType, setFeedType] = useState('');
  const [feedLot, setFeedLot] = useState('');
  const [feedSupplier, setFeedSupplier] = useState('');
  const [fedAt, setFedAt] = useState('');
  const [recordedBy, setRecordedBy] = useState(tr('feedingForm.defaultOperator'));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  // 재고 연동
  const [feedItems, setFeedItems] = useState<InventoryItem[]>([]);
  const [selectedItemId, setSelectedItemId] = useState('');

  useEffect(() => {
    if (open && feedItems.length === 0) {
      Inventory.list('feed')
        .then(r => setFeedItems(r.items))
        .catch(() => undefined); // 실패해도 폼은 동작
    }
  }, [open, feedItems.length]);

  const selectedItem = feedItems.find(it => it.item_id === selectedItemId) ?? null;

  // 재고 차감량 자동 계산
  function computeConsumedQty(): number | undefined {
    if (!selectedItem || !feedAmountG) return undefined;
    const g = Number(feedAmountG);
    if (!g) return undefined;
    if (selectedItem.unit === 'kg') return g / 1000;
    if (selectedItem.unit === 'g') return g;
    return undefined; // 단위 불명 → undefined (수동 입력 불필요, 그냥 skip)
  }

  const consumedQty = computeConsumedQty();
  const showManualConsumed = selectedItem && consumedQty === undefined;
  const [manualConsumed, setManualConsumed] = useState('');

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      const body: NewFeedingBody = {
        tank_id: tankId,
        feed_amount_g: Number(feedAmountG),
      };
      if (feedType) body.feed_type = feedType.trim();
      if (feedLot) body.feed_lot = feedLot.trim();
      if (feedSupplier) body.feed_supplier = feedSupplier.trim();
      if (fedAt) body.fed_at = new Date(fedAt).toISOString(); // 날짜→RFC3339
      if (recordedBy) body.recorded_by = recordedBy.trim();
      body.source = 'manual';
      if (selectedItemId) {
        body.item_id = selectedItemId;
        const cq = showManualConsumed ? Number(manualConsumed) : consumedQty;
        if (cq != null && cq > 0) body.consumed_qty = cq;
      }
      await Feedings.record(body);
      setOk(tr('feedingForm.savedOk'));
      setFeedAmountG('');
      setFeedType('');
      setFeedLot('');
      setFeedSupplier('');
      setFedAt('');
      setSelectedItemId('');
      setManualConsumed('');
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="border border-blue-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-blue-500/10 hover:bg-blue-500/15 text-xs text-blue-300 font-medium"
      >
        <Utensils className="w-3.5 h-3.5" />
        {tr('feedingForm.title')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('feedingForm.feedAmountLabel')} type="number" value={feedAmountG} onChange={setFeedAmountG} />
            <Field label={tr('feedingForm.feedTypeLabel')} value={feedType} onChange={setFeedType} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('feedingForm.feedLotLabel')} value={feedLot} onChange={setFeedLot} />
            <Field label={tr('feedingForm.feedSupplierLabel')} value={feedSupplier} onChange={setFeedSupplier} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('feedingForm.fedAtLabel')} type="date" value={fedAt} onChange={setFedAt} />
            <Field label={tr('feedingForm.recordedByLabel')} value={recordedBy} onChange={setRecordedBy} />
          </div>
          {/* 재고 연동 — 선택 사항 */}
          <div>
            <label className="text-[11px] text-gray-400 font-medium block mb-0.5">{tr('feedingForm.inventoryLabel')}</label>
            <select
              value={selectedItemId}
              onChange={e => setSelectedItemId(e.target.value)}
              className="w-full h-8 px-2 rounded border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500/50"
            >
              <option value="">{tr('feedingForm.inventoryNone')}</option>
              {feedItems.map(it => (
                <option key={it.item_id} value={it.item_id}>
                  {tr('productionEventLog.optionRemain', { name: it.name, qty: it.on_hand_qty, unit: it.unit })}
                </option>
              ))}
            </select>
            {selectedItem && consumedQty != null && (
              <p className="text-[10px] text-blue-400 mt-0.5">
                {tr('feedingForm.inventoryDeduct')}: {consumedQty.toFixed(3)} {selectedItem.unit}
              </p>
            )}
            {showManualConsumed && (
              <Field
                label={tr('productionEventLog.deductQtyLabel', { unit: selectedItem.unit })}
                type="number"
                value={manualConsumed}
                onChange={setManualConsumed}
              />
            )}
          </div>
          {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>}
          {ok && <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">{ok}</div>}
          <Button size="sm" onClick={() => void submit()} disabled={busy || !feedAmountG}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {busy ? tr('feedingForm.submitting') : tr('feedingForm.submitLabel')}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── TreatmentForm — 투약 기록 ────────────────────────────────────────────────

function TreatmentForm({ tankId, onSaved }: { tankId: string; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(true); // 탭당 폼 1개 — 기본 펼침
  const [treatmentType, setTreatmentType] = useState<TreatmentType>('antibiotic');
  const [substance, setSubstance] = useState('');
  const [dose, setDose] = useState('');
  const [doseUnit, setDoseUnit] = useState('');
  const [reason, setReason] = useState('');
  const [withdrawalUntil, setWithdrawalUntil] = useState('');
  const [operatorId, setOperatorId] = useState(tr('treatmentForm.defaultOperator'));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  // 재고 연동
  const [drugItems, setDrugItems] = useState<InventoryItem[]>([]);
  const [selectedItemId, setSelectedItemId] = useState('');
  const [consumedQty, setConsumedQty] = useState('');

  useEffect(() => {
    if (open && drugItems.length === 0) {
      Inventory.list('drug')
        .then(r => setDrugItems(r.items))
        .catch(() => undefined);
    }
  }, [open, drugItems.length]);

  const selectedDrugItem = drugItems.find(it => it.item_id === selectedItemId) ?? null;

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      const body: NewTreatmentBody = { treatment_type: treatmentType, substance: substance.trim() };
      if (dose) body.dose = Number(dose);
      if (doseUnit) body.dose_unit = doseUnit.trim();
      if (reason) body.reason = reason.trim();
      if (withdrawalUntil) body.withdrawal_until = withdrawalUntil;
      if (operatorId) body.operator_id = operatorId.trim();
      if (selectedItemId) {
        body.item_id = selectedItemId;
        if (consumedQty && Number(consumedQty) > 0) body.consumed_qty = Number(consumedQty);
      }
      const r = await Tanks.treatment(tankId, body);
      setOk(tr('productionEventLog.treatmentSavedOk', { id: r.treatment_id }));
      setSelectedItemId('');
      setConsumedQty('');
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="border border-orange-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-orange-500/10 hover:bg-orange-500/15 text-xs text-orange-300 font-medium"
      >
        <Syringe className="w-3.5 h-3.5" />
        {tr('treatmentForm.title')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <div className="grid grid-cols-2 gap-3">
            <SelectField label={tr('treatmentForm.treatmentTypeLabel')} value={treatmentType}
              onChange={v => setTreatmentType(v as TreatmentType)}
              options={[
                { value: 'antibiotic', label: tr('treatmentForm.typeAntibiotic') },
                { value: 'vaccine', label: tr('treatmentForm.typeVaccine') },
                { value: 'chemical', label: tr('treatmentForm.typeChemical') },
                { value: 'probiotic', label: tr('treatmentForm.typeProbiotic') },
                { value: 'anesthetic', label: tr('treatmentForm.typeAnesthetic') },
                { value: 'other', label: tr('treatmentForm.typeOther') },
              ]}
            />
            <Field label={tr('treatmentForm.substanceLabel')} value={substance} onChange={setSubstance} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('treatmentForm.doseLabel')} type="number" value={dose} onChange={setDose} />
            <Field label={tr('treatmentForm.doseUnitLabel')} value={doseUnit} onChange={setDoseUnit} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('treatmentForm.reasonLabel')} value={reason} onChange={setReason} />
            <Field label={tr('treatmentForm.withdrawalUntilLabel')} type="date" value={withdrawalUntil} onChange={setWithdrawalUntil} />
          </div>
          <Field label={tr('treatmentForm.operatorLabel')} value={operatorId} onChange={setOperatorId} />
          {/* 재고 연동 — 선택 사항 */}
          <div>
            <label className="text-[11px] text-gray-400 font-medium block mb-0.5">{tr('treatmentForm.drugInventoryLabel')}</label>
            <select
              value={selectedItemId}
              onChange={e => setSelectedItemId(e.target.value)}
              className="w-full h-8 px-2 rounded border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-1 focus:ring-orange-500/50"
            >
              <option value="">{tr('treatmentForm.inventoryNone')}</option>
              {drugItems.map(it => (
                <option key={it.item_id} value={it.item_id}>
                  {tr('productionEventLog.optionRemain', { name: it.name, qty: it.on_hand_qty, unit: it.unit })}
                </option>
              ))}
            </select>
          </div>
          {selectedDrugItem && (
            <Field
              label={tr('productionEventLog.deductQtyLabel', { unit: selectedDrugItem.unit })}
              type="number"
              value={consumedQty}
              onChange={setConsumedQty}
            />
          )}
          {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>}
          {ok && <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">{ok}</div>}
          <Button size="sm" onClick={() => void submit()} disabled={busy || !substance}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {busy ? tr('treatmentForm.submitting') : tr('treatmentForm.submitLabel')}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── MortalityForm — 폐사 기록 ────────────────────────────────────────────────

function MortalityForm({ tankId, onSaved }: { tankId: string; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(true); // 탭당 폼 1개 — 기본 펼침
  const [deadCount, setDeadCount] = useState('');
  const [estimatedCause, setEstimatedCause] = useState('');
  const [deadWeightG, setDeadWeightG] = useState('');
  const [operatorId, setOperatorId] = useState(tr('mortalityForm.defaultOperator'));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      const body: NewMortalityBody = { dead_count: Number(deadCount) };
      if (estimatedCause) body.estimated_cause = estimatedCause.trim();
      if (deadWeightG) body.dead_weight_g = Number(deadWeightG);
      if (operatorId) body.operator_id = operatorId.trim();
      const r = await Tanks.mortality(tankId, body);
      setOk(tr('productionEventLog.mortalitySavedOk', { id: r.mortality_id, count: r.dead_count }));
      setDeadCount('');
      setEstimatedCause('');
      setDeadWeightG('');
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="border border-red-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-red-500/10 hover:bg-red-500/15 text-xs text-red-300 font-medium"
      >
        <Skull className="w-3.5 h-3.5" />
        {tr('mortalityForm.title')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('mortalityForm.deadCountLabel')} type="number" value={deadCount} onChange={setDeadCount} />
            <Field label={tr('mortalityForm.estimatedCauseLabel')} value={estimatedCause} onChange={setEstimatedCause} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('mortalityForm.deadWeightLabel')} type="number" value={deadWeightG} onChange={setDeadWeightG} />
            <Field label={tr('mortalityForm.operatorLabel')} value={operatorId} onChange={setOperatorId} />
          </div>
          {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>}
          {ok && <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">{ok}</div>}
          <Button size="sm" onClick={() => void submit()} disabled={busy || !deadCount}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {busy ? tr('mortalityForm.submitting') : tr('mortalityForm.submitLabel')}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── TransferForm — 이동/선별/판매 ────────────────────────────────────────────

function TransferForm({ tankId, onSaved }: { tankId: string; onSaved: () => void }) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(true); // 탭당 폼 1개 — 기본 펼침
  const [transferType, setTransferType] = useState<TransferType>('move');
  const [toTankId, setToTankId] = useState('');
  const [toLotNo, setToLotNo] = useState('');
  const [movedCount, setMovedCount] = useState('');
  const [avgWeightG, setAvgWeightG] = useState('');
  const [operatorId, setOperatorId] = useState(tr('transferForm.defaultOperator'));
  // sale 전용
  const [destinationName, setDestinationName] = useState('');
  const [vehicleInfo, setVehicleInfo] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);

  const isSale = transferType === 'sale';

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setOk(null);
    try {
      if (isSale && !destinationName.trim()) {
        setErr(tr('transferForm.errNoDestination'));
        setBusy(false);
        return;
      }
      if (!isSale && !toTankId.trim()) {
        setErr(tr('transferForm.errNoToTank'));
        setBusy(false);
        return;
      }
      const body: NewTransferBody = {
        transfer_type: transferType,
        moved_count: Number(movedCount),
      };
      if (isSale) {
        body.destination_name = destinationName.trim();
        if (vehicleInfo.trim()) body.vehicle_info = vehicleInfo.trim();
      } else {
        body.to_tank_id = toTankId.trim();
        if (toLotNo) body.to_lot_no = toLotNo.trim();
      }
      if (avgWeightG) body.avg_weight_g = Number(avgWeightG);
      if (operatorId) body.operator_id = operatorId.trim();
      const r = await Tanks.transfer(tankId, body);
      setOk(tr('productionEventLog.transferSavedOk', { id: r.transfer_id }));
      onSaved();
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="border border-purple-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-purple-500/10 hover:bg-purple-500/15 text-xs text-purple-300 font-medium"
      >
        <ArrowRightLeft className="w-3.5 h-3.5" />
        {tr('transferForm.title')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <p className="text-[11px] text-gray-500">
            {isSale
              ? tr('transferForm.descSale')
              : tr('transferForm.descMove')}
          </p>
          <div className="grid grid-cols-2 gap-3">
            <SelectField label={tr('transferForm.transferTypeLabel')} value={transferType}
              onChange={v => setTransferType(v as TransferType)}
              options={[
                { value: 'move', label: tr('transferForm.typeMove') },
                { value: 'split', label: tr('transferForm.typeSplit') },
                { value: 'merge', label: tr('transferForm.typeMerge') },
                { value: 'sale', label: tr('transferForm.typeSale') },
              ]}
            />
            {isSale ? (
              <Field label={tr('transferForm.destinationLabel')} value={destinationName} onChange={setDestinationName} />
            ) : (
              <Field label={tr('transferForm.toTankIdLabel')} value={toTankId} onChange={setToTankId} />
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('transferForm.movedCountLabel')} type="number" value={movedCount} onChange={setMovedCount} />
            <Field label={tr('transferForm.avgWeightLabel')} type="number" value={avgWeightG} onChange={setAvgWeightG} />
          </div>
          {isSale ? (
            <Field label={tr('transferForm.vehicleInfoLabel')} value={vehicleInfo} onChange={setVehicleInfo} />
          ) : (
            <div className="grid grid-cols-2 gap-3">
              <Field label={tr('transferForm.toLotNoLabel')} value={toLotNo} onChange={setToLotNo} />
              <Field label={tr('transferForm.operatorLabel')} value={operatorId} onChange={setOperatorId} />
            </div>
          )}
          {isSale && (
            <Field label={tr('transferForm.operatorLabel')} value={operatorId} onChange={setOperatorId} />
          )}
          {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>}
          {ok && <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">{ok}</div>}
          <Button size="sm" onClick={() => void submit()} disabled={busy || !movedCount}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {busy ? tr('transferForm.submitting') : tr('transferForm.submitLabel')}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── EventTimeline ─────────────────────────────────────────────────────────────

function EventTimeline({
  tankId,
  data,
  loading,
  err,
  types,
  title,
  onDocUploaded,
}: {
  tankId: string;
  data: TankTraceabilityResponse | null;
  loading: boolean;
  err: string | null;
  types?: TraceabilityCTE['type'][]; // 미지정 = 전체 종류
  title: string;
  onDocUploaded: () => void;
}) {
  const { tr } = useLanguage();

  if (loading) return <div className="text-xs text-gray-500">{tr('eventTimeline.loading')}</div>;
  if (err) return <div className="text-xs text-red-400 font-mono">{err}</div>;
  if (!data) return null;

  const docs: TraceabilityDoc[] = data.documents ?? [];
  const rows = (types ? data.timeline.filter(i => types.includes(i.type)) : data.timeline)
    .slice()
    .reverse(); // 최신 우선

  // 각 타임라인 행에 붙은 서류 계산
  function docsForRow(item: TraceabilityCTE): TraceabilityDoc[] {
    const eventRef = cteEventId(item);
    const docKey = cteDocKey(item);
    return docs.filter(d => {
      if (eventRef && d.event_ref === eventRef) return true;
      // event_ref 없는 doc 은 cte_type 으로 매칭 ('sale' → 'transfer')
      if (!d.event_ref) {
        const docCteType = docKey === 'sale' ? 'transfer' : docKey;
        return d.cte_type === docCteType;
      }
      return false;
    });
  }

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2 text-xs text-teal-400 font-medium pb-1">
        <ListTree className="w-3.5 h-3.5" />
        {title}
        <span className="text-teal-500 font-mono">{tr('productionEventLog.eventCount', { count: rows.length })}</span>
      </div>
      {rows.length === 0 && (
        <div className="text-xs text-gray-500">{tr('eventTimeline.noEvents')}</div>
      )}
      <ol className="space-y-2">
        {rows.map(item => (
          <li key={item.sequence} className="py-1 border-b border-gray-800 last:border-0">
            <div className="flex items-start gap-2 text-xs">
              <span className={`shrink-0 px-1.5 py-0.5 rounded text-[10px] font-medium ${cteTypeBadge(item.type)}`}>
                {cteTypeName(item.type, tr)}
              </span>
              <span className="text-gray-400 font-mono shrink-0">
                {new Date(item.recorded_at).toLocaleString(dateLocale(), { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}
              </span>
              <span className="text-gray-300 truncate">{cteSummary(item, tr)}</span>
            </div>
            <TimelineRowDocs
              item={item}
              docs={docsForRow(item)}
              tankId={tankId}
              operatorId={tr('productionEventLog.defaultOperator')}
              onUploaded={onDocUploaded}
            />
          </li>
        ))}
      </ol>
    </div>
  );
}

// ── ProductionEventLog (exported) ─────────────────────────────────────────────

type SubTab = 'feeding' | 'treatment' | 'mortality' | 'transfer' | 'all';

export function ProductionEventLog() {
  const { tr } = useLanguage();
  const [tanks, setTanks] = useState<Tank[]>([]);
  const [selectedTankId, setSelectedTankId] = useState<string>('');
  const [tanksErr, setTanksErr] = useState<string | null>(null);
  const [timelineKey, setTimelineKey] = useState(0);
  const [sub, setSub] = useState<SubTab>('feeding');

  // traceability 를 이 레벨에서 한 번만 받는다 — 탭 라벨의 건수와 각 탭이 그리는
  // 이력이 같은 응답을 봐야 어긋나지 않는다 (탭마다 따로 조회하면 갈린다).
  const [trace, setTrace] = useState<TankTraceabilityResponse | null>(null);
  const [traceErr, setTraceErr] = useState<string | null>(null);
  const [traceLoading, setTraceLoading] = useState(false);

  const reload = useCallback(async () => {
    setTanksErr(null);
    try {
      const r = await Tanks.list();
      setTanks(r.items);
      if (r.items.length > 0 && !selectedTankId) {
        setSelectedTankId(r.items[0].tank_id);
      }
    } catch (e) {
      setTanksErr(friendlyError(e, tr));
    }
  }, [selectedTankId]);

  useEffect(() => { void reload(); }, [reload]);

  useEffect(() => {
    if (!selectedTankId) { setTrace(null); return; }
    let alive = true;
    setTraceLoading(true);
    setTraceErr(null);
    Tanks.traceability(selectedTankId)
      .then(d => { if (alive) setTrace(d); })
      .catch(e => { if (alive) setTraceErr(friendlyError(e, tr)); })
      .finally(() => { if (alive) setTraceLoading(false); });
    return () => { alive = false; };
  }, [selectedTankId, timelineKey]);

  const refreshTimeline = () => setTimelineKey(k => k + 1);

  const countOf = (types: TraceabilityCTE['type'][]) =>
    trace ? trace.timeline.filter(i => types.includes(i.type)).length : 0;

  // 이동 탭은 출하(harvest)까지 받는다 — 폼 하나(이동/선별/판매)가 둘 다 만든다.
  const TABS: { key: Exclude<SubTab, 'all'>; types: TraceabilityCTE['type'][]; label: string }[] = [
    { key: 'feeding',   types: ['feeding'],              label: tr('productionEventLog.cteFeeding') },
    { key: 'treatment', types: ['treatment'],            label: tr('productionEventLog.cteTreatment') },
    { key: 'mortality', types: ['mortality'],            label: tr('productionEventLog.cteMortality') },
    { key: 'transfer',  types: ['transfer', 'harvest'],  label: tr('productionEventLog.cteTransfer') },
  ];

  const formFor = (key: Exclude<SubTab, 'all'>) => {
    switch (key) {
      case 'feeding':   return <FeedingForm tankId={selectedTankId} onSaved={refreshTimeline} />;
      case 'treatment': return <TreatmentForm tankId={selectedTankId} onSaved={refreshTimeline} />;
      case 'mortality': return <MortalityForm tankId={selectedTankId} onSaved={refreshTimeline} />;
      case 'transfer':  return <TransferForm tankId={selectedTankId} onSaved={refreshTimeline} />;
    }
  };

  return (
    <div className="space-y-5">
      {/* 헤더 — 수조 선택 */}
      <div className="bg-gradient-to-br from-gray-900 to-black border border-teal-500/20 rounded-lg p-4 space-y-4">
        <h2 className="text-sm font-medium text-white flex items-center gap-2">
          <ListTree className="w-4 h-4 text-teal-400" />
          {tr('productionEventLog.title')}
        </h2>

        {tanksErr ? (
          <div className="text-xs text-red-400 font-mono">{tanksErr}</div>
        ) : (
          <div className="flex flex-col gap-1 max-w-xs">
            <label className="text-xs text-gray-400 font-medium">{tr('productionEventLog.tankSelectLabel')}</label>
            <select
              value={selectedTankId}
              onChange={e => setSelectedTankId(e.target.value)}
              className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-teal-500/50"
            >
              {tanks.length === 0 && <option value="">{tr('productionEventLog.noTanks')}</option>}
              {tanks.map(t => (
                <option key={t.tank_id} value={t.tank_id}>
                  {t.display_name} ({t.tank_id})
                </option>
              ))}
            </select>
          </div>
        )}
      </div>

      {/* 기록 종류별 탭 — 각 탭 = 입력 폼 + 그 종류의 과거 기록 */}
      {selectedTankId && (
        <Tabs value={sub} onValueChange={v => setSub(v as SubTab)}>
          <TabsList>
            {TABS.map(t => (
              <TabsTrigger key={t.key} value={t.key}>
                {t.label}
                <span className="ml-1.5 font-mono text-[10px] opacity-60">{countOf(t.types)}</span>
              </TabsTrigger>
            ))}
            <TabsTrigger value="all">
              {tr('productionEventLog.tabAll')}
              <span className="ml-1.5 font-mono text-[10px] opacity-60">{trace?.timeline.length ?? 0}</span>
            </TabsTrigger>
          </TabsList>

          {TABS.map(t => (
            <TabsContent key={t.key} value={t.key} className="pt-4 space-y-4">
              {formFor(t.key)}
              <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4">
                <EventTimeline
                  tankId={selectedTankId}
                  data={trace}
                  loading={traceLoading}
                  err={traceErr}
                  types={t.types}
                  title={t.label}
                  onDocUploaded={refreshTimeline}
                />
              </div>
            </TabsContent>
          ))}

          {/* 전체 — 종전 CTE 타임라인 그대로 (종류 혼합, 추적성 확인용) */}
          <TabsContent value="all" className="pt-4">
            <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4">
              <EventTimeline
                tankId={selectedTankId}
                data={trace}
                loading={traceLoading}
                err={traceErr}
                title={tr('eventTimeline.title')}
                onDocUploaded={refreshTimeline}
              />
            </div>
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}
