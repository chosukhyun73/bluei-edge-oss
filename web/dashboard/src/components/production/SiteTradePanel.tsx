import { useCallback, useEffect, useState } from 'react';
import {
  Plus, ChevronDown, ChevronRight, Fish, PackageCheck, Users, Paperclip,
} from 'lucide-react';
import { Sites, Tanks, Partners, SiteTrade, ApiError } from '../../lib/api';
import type {
  Site, Tank, Partner, PartnerType,
  SiteStocking, SiteHarvest,
  TankGrowthStage,
} from '../../lib/types';
import { Field, SelectField } from '../sections/TankSettingsSection';
import { Button } from '../ui/button';
import { useLanguage } from '../../lib/language-context';

// ─────────────────────────────────────────────────────────────────────────────
// SiteTradePanel — 사이트(사업자) 단위 입식·출하·거래처 관리
// 입식 = 공급처 → 여러 수조 분배 (site-stocking 배치)
// 출하 = 여러 수조 → 구매처 출하 (site-harvest 건)
// ─────────────────────────────────────────────────────────────────────────────

function friendlyError(err: unknown, tr: (key: string) => string): string {
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return tr('siteTrade.unknownError');
}

const PARTNER_TYPE_LABEL_KEYS: Record<PartnerType, string> = {
  hatchery: 'siteTrade.partnerTypeHatchery',
  feed_supplier: 'siteTrade.partnerTypeFeedSupplier',
  drug_supplier: 'siteTrade.partnerTypeDrugSupplier',
  buyer: 'siteTrade.partnerTypeBuyer',
  other: 'siteTrade.partnerTypeOther',
};

const PARTNER_DOC_TYPES = [
  { value: 'producer_license', labelKey: 'siteTrade.docProducerLicense' },
  { value: 'certification', labelKey: 'siteTrade.docCertification' },
  { value: 'transaction_statement', labelKey: 'siteTrade.docTransactionStatement' },
  { value: 'other', labelKey: 'siteTrade.docOther' },
];

const STOCKING_DOC_TYPES = [
  { value: 'broodstock_info', labelKey: 'siteTrade.docBroodstockInfo' },
  { value: 'producer_license', labelKey: 'siteTrade.docProducerLicense' },
  { value: 'transaction_statement', labelKey: 'siteTrade.docTransactionStatement' },
  { value: 'tax_invoice', labelKey: 'siteTrade.docTaxInvoice' },
  { value: 'other', labelKey: 'siteTrade.docOther' },
];

const HARVEST_DOC_TYPES = [
  { value: 'transaction_statement', labelKey: 'siteTrade.docTransactionStatement' },
  { value: 'tax_invoice', labelKey: 'siteTrade.docTaxInvoice' },
  { value: 'vehicle_doc', labelKey: 'siteTrade.docVehicleDoc' },
  { value: 'other', labelKey: 'siteTrade.docOther' },
];

// ── 서류 첨부 인라인 폼 ──────────────────────────────────────────────────────

function DocAttachForm({
  docTypes,
  onUpload,
  onCancel,
}: {
  docTypes: Array<{ value: string; labelKey: string }>;
  onUpload: (docType: string, file: File) => Promise<void>;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const [docType, setDocType] = useState(docTypes[0]?.value ?? 'other');
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async () => {
    if (!file) { setErr(tr('siteTrade.selectFile')); return; }
    setBusy(true);
    setErr(null);
    try {
      await onUpload(docType, file);
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
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.docType')}</label>
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
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.fileLabel')}</label>
          <input
            type="file"
            accept=".pdf,.jpg,.jpeg,.png,.heic,.xlsx"
            onChange={e => setFile(e.target.files?.[0] ?? null)}
            className="text-[10px] text-gray-300 file:mr-2 file:py-0.5 file:px-2 file:rounded file:border-0 file:text-[10px] file:bg-gray-700 file:text-gray-200 hover:file:bg-gray-600"
          />
        </div>
      </div>
      {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[10px] text-red-400 font-mono">{err}</div>}
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => void submit()} disabled={busy || !file} className="text-[11px] h-6 px-2">
          {busy ? tr('siteTrade.registering') : tr('siteTrade.register')}
        </Button>
        <button type="button" onClick={onCancel} className="text-[11px] text-gray-500 hover:text-gray-300">{tr('siteTrade.cancel')}</button>
      </div>
    </div>
  );
}

// ── 거래처 신규 등록 인라인 폼 ───────────────────────────────────────────────

function NewPartnerInline({
  siteId,
  defaultType,
  onCreated,
  onCancel,
}: {
  siteId: string;
  defaultType: PartnerType;
  onCreated: (partner: Partner) => void;
  onCancel: () => void;
}) {
  const { tr } = useLanguage();
  const [pType, setPType] = useState<PartnerType>(defaultType);
  const [name, setName] = useState('');
  const [businessNo, setBusinessNo] = useState('');
  const [licenseNo, setLicenseNo] = useState('');
  const [contact, setContact] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const submit = async () => {
    if (!name.trim()) { setErr(tr('siteTrade.enterPartnerName')); return; }
    setBusy(true);
    setErr(null);
    try {
      const r = await Partners.create({
        partner_type: pType,
        name: name.trim(),
        business_no: businessNo.trim() || undefined,
        license_no: licenseNo.trim() || undefined,
        contact: contact.trim() || undefined,
        site_id: siteId,
      });
      onCreated(r.partner);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-2.5 border border-gray-700/60 rounded bg-gray-800/40 space-y-2">
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.partnerTypeLabel')}</label>
          <select value={pType} onChange={e => setPType(e.target.value as PartnerType)}
            className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-yellow-500/50">
            {(Object.keys(PARTNER_TYPE_LABEL_KEYS) as PartnerType[]).map(k => (
              <option key={k} value={k}>{tr(PARTNER_TYPE_LABEL_KEYS[k])}</option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.partnerNameLabel')}</label>
          <input type="text" value={name} onChange={e => setName(e.target.value)}
            className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-yellow-500/50"
            placeholder={tr('siteTrade.partnerNamePlaceholder')} />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-2">
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.businessNo')}</label>
          <input type="text" value={businessNo} onChange={e => setBusinessNo(e.target.value)}
            className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-yellow-500/50" />
        </div>
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.licenseNo')}</label>
          <input type="text" value={licenseNo} onChange={e => setLicenseNo(e.target.value)}
            className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-yellow-500/50" />
        </div>
        <div className="flex flex-col gap-0.5">
          <label className="text-[10px] text-gray-400 font-medium">{tr('siteTrade.contact')}</label>
          <input type="text" value={contact} onChange={e => setContact(e.target.value)}
            className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-yellow-500/50" />
        </div>
      </div>
      {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[10px] text-red-400 font-mono">{err}</div>}
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={() => void submit()} disabled={busy || !name.trim()} className="text-[11px] h-6 px-2">
          {busy ? tr('siteTrade.registering') : tr('siteTrade.register')}
        </Button>
        <button type="button" onClick={onCancel} className="text-[11px] text-gray-500 hover:text-gray-300">{tr('siteTrade.cancel')}</button>
      </div>
    </div>
  );
}

// ── 거래처 관리 섹션 ─────────────────────────────────────────────────────────

function PartnerSection({ siteId }: { siteId: string }) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(false);
  const [partners, setPartners] = useState<Partner[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [showNew, setShowNew] = useState(false);
  const [docOpenFor, setDocOpenFor] = useState<string | null>(null);
  const [docsMap, setDocsMap] = useState<Record<string, Array<{ document_id: string; doc_type: string; filename: string; download_url: string }>>>({});

  const reload = useCallback(async () => {
    if (!siteId) return;
    setLoading(true);
    setErr(null);
    try {
      const r = await Partners.list(undefined, siteId);
      setPartners(r.partners);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setLoading(false);
    }
  }, [siteId]);

  useEffect(() => {
    if (open) void reload();
  }, [open, reload]);

  const loadDocs = async (partnerId: string) => {
    try {
      const r = await Partners.listDocs(partnerId);
      setDocsMap(prev => ({ ...prev, [partnerId]: r.documents }));
    } catch { /* 계속 */ }
  };

  const handleUploadDoc = async (partnerId: string, docType: string, file: File) => {
    const form = new FormData();
    form.append('file', file);
    form.append('doc_type', docType);
    await Partners.uploadDoc(partnerId, form);
    setDocOpenFor(null);
    await loadDocs(partnerId);
  };

  return (
    <div className="border border-yellow-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-yellow-500/10 hover:bg-yellow-500/15 text-xs text-yellow-300 font-medium"
      >
        <Users className="w-3.5 h-3.5" />
        {tr('siteTrade.partnerManagement')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          {err && <div className="text-xs text-red-400 font-mono">{err}</div>}
          {loading && <div className="text-xs text-gray-500">{tr('siteTrade.loading')}</div>}

          {partners.map(p => (
            <div key={p.partner_id} className="p-2 border border-gray-700/60 rounded bg-gray-800/40 text-xs space-y-1">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-white">{p.name}</span>
                <span className="px-1.5 py-0.5 rounded text-[10px] bg-yellow-500/20 text-yellow-300 border border-yellow-500/30">
                  {PARTNER_TYPE_LABEL_KEYS[p.partner_type as PartnerType] ? tr(PARTNER_TYPE_LABEL_KEYS[p.partner_type as PartnerType]) : p.partner_type}
                </span>
                {p.license_no && <span className="text-gray-400">{tr('siteTrade.licensePrefix')}{p.license_no}</span>}
                {p.business_no && <span className="text-gray-400">{tr('siteTrade.businessNoPrefix')}{p.business_no}</span>}
              </div>
              {p.contact && <div className="text-gray-500">{tr('siteTrade.contactPrefix')}{p.contact}</div>}
              {(docsMap[p.partner_id] ?? []).map(doc => (
                <div key={doc.document_id} className="flex items-center gap-1">
                  <Paperclip className="w-2.5 h-2.5 text-gray-500 shrink-0" />
                  <a href={doc.download_url} target="_blank" rel="noopener noreferrer"
                    className="text-teal-400 hover:text-teal-300 hover:underline truncate max-w-[240px]">
                    {doc.doc_type} · {doc.filename}
                  </a>
                </div>
              ))}
              {docOpenFor === p.partner_id ? (
                <DocAttachForm
                  docTypes={PARTNER_DOC_TYPES}
                  onUpload={(dt, f) => handleUploadDoc(p.partner_id, dt, f)}
                  onCancel={() => setDocOpenFor(null)}
                />
              ) : (
                <button type="button"
                  onClick={() => { setDocOpenFor(p.partner_id); void loadDocs(p.partner_id); }}
                  className="text-[10px] text-teal-500 hover:text-teal-400 border border-teal-700/40 hover:border-teal-500/60 rounded px-1.5 py-0.5 leading-none">
                  {tr('siteTrade.attachDoc')}
                </button>
              )}
            </div>
          ))}
          {!loading && partners.length === 0 && (
            <div className="text-xs text-gray-500">{tr('siteTrade.noPartnersForSite')}</div>
          )}

          {showNew ? (
            <NewPartnerInline
              siteId={siteId}
              defaultType="hatchery"
              onCreated={() => { setShowNew(false); void reload(); }}
              onCancel={() => setShowNew(false)}
            />
          ) : (
            <button type="button" onClick={() => setShowNew(true)}
              className="flex items-center gap-1 text-xs text-yellow-400 hover:text-yellow-300 border border-yellow-500/30 hover:border-yellow-500/60 rounded px-2 py-1">
              <Plus className="w-3 h-3" />
              {tr('siteTrade.registerPartner')}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ── 분배 행 타입 ─────────────────────────────────────────────────────────────

type AllocRow = { tank_id: string; count: string; avg_weight_g: string; lot_no: string };
type HarvestRow = { tank_id: string; lot_no: string; count: string; avg_weight_g: string; full_close: boolean };

// ── 입식 배치 폼 ─────────────────────────────────────────────────────────────

function StockingBatchForm({
  siteId,
  tanks,
  partners,
  onRefresh,
}: {
  siteId: string;
  tanks: Tank[];
  partners: Partner[];
  onRefresh: () => void;
}) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(false);
  const [supplierId, setSupplierId] = useState('');
  const [showNewSupplier, setShowNewSupplier] = useState(false);
  const [species, setSpecies] = useState('');
  const [growthStage, setGrowthStage] = useState<TankGrowthStage>('growout');
  const [batchLotNo, setBatchLotNo] = useState('');
  const [stockedAt, setStockedAt] = useState('');
  const [rows, setRows] = useState<AllocRow[]>([{ tank_id: '', count: '', avg_weight_g: '', lot_no: '' }]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [createdId, setCreatedId] = useState<string | null>(null);
  const [showDocForm, setShowDocForm] = useState(false);

  const hatcheries = partners.filter(p => p.partner_type === 'hatchery');
  const totalCount = rows.reduce((s, r) => s + (Number(r.count) || 0), 0);

  const addRow = () => setRows(prev => [...prev, { tank_id: '', count: '', avg_weight_g: '', lot_no: '' }]);
  const removeRow = (i: number) => setRows(prev => prev.filter((_, idx) => idx !== i));
  const updateRow = (i: number, field: keyof AllocRow, val: string) =>
    setRows(prev => prev.map((r, idx) => idx === i ? { ...r, [field]: val } : r));

  const submit = async () => {
    if (!species.trim()) { setErr(tr('siteTrade.enterSpecies')); return; }
    const allocations = rows
      .filter(r => r.tank_id && r.count && r.avg_weight_g)
      .map(r => ({
        tank_id: r.tank_id,
        count: Number(r.count),
        avg_weight_g: Number(r.avg_weight_g),
        lot_no: r.lot_no.trim() || undefined,
      }));
    if (allocations.length === 0) { setErr(tr('siteTrade.enterAtLeastOneAllocation')); return; }
    setBusy(true);
    setErr(null);
    try {
      const r = await SiteTrade.createStocking({
        site_id: siteId,
        supplier_id: supplierId || undefined,
        species: species.trim(),
        growth_stage: growthStage,
        batch_lot_no: batchLotNo.trim() || undefined,
        stocked_at: stockedAt || undefined,
        allocations,
      });
      setCreatedId(r.site_stocking_id);
      setShowDocForm(true);
      onRefresh();
      // 폼 초기화
      setSupplierId(''); setSpecies(''); setBatchLotNo(''); setStockedAt('');
      setRows([{ tank_id: '', count: '', avg_weight_g: '', lot_no: '' }]);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  const handleDocUpload = async (docType: string, file: File) => {
    if (!createdId) return;
    const form = new FormData();
    form.append('file', file);
    form.append('doc_type', docType);
    await SiteTrade.uploadStockingDoc(createdId, form);
    setShowDocForm(false);
  };

  return (
    <div className="border border-green-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-green-500/10 hover:bg-green-500/15 text-xs text-green-300 font-medium"
      >
        <Fish className="w-3.5 h-3.5" />
        {tr('siteTrade.stockingFormTitle')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <p className="text-[11px] text-gray-500">{tr('siteTrade.stockingFormDesc')}</p>

          {/* 공급처 */}
          <div className="space-y-1">
            <label className="text-[11px] text-gray-400 font-medium block">{tr('siteTrade.supplierLabel')}</label>
            <div className="flex items-center gap-2">
              <select value={supplierId} onChange={e => setSupplierId(e.target.value)}
                className="flex-1 h-8 px-2 rounded border border-gray-700 bg-gray-900 text-xs text-white focus:outline-none focus:ring-1 focus:ring-green-500/50">
                <option value="">{tr('siteTrade.noSelection')}</option>
                {hatcheries.map(p => (
                  <option key={p.partner_id} value={p.partner_id}>{p.name}{p.license_no ? ` (${p.license_no})` : ''}</option>
                ))}
              </select>
              {!showNewSupplier && (
                <button type="button" onClick={() => setShowNewSupplier(true)}
                  className="text-xs text-green-400 hover:text-green-300 border border-green-500/30 rounded px-2 py-1 whitespace-nowrap">
                  {tr('siteTrade.newEntry')}
                </button>
              )}
            </div>
            {showNewSupplier && (
              <NewPartnerInline
                siteId={siteId}
                defaultType="hatchery"
                onCreated={p => { setSupplierId(p.partner_id); setShowNewSupplier(false); onRefresh(); }}
                onCancel={() => setShowNewSupplier(false)}
              />
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('siteTrade.speciesLabel')} value={species} onChange={setSpecies} />
            <SelectField label={tr('siteTrade.growthStageLabel')} value={growthStage}
              onChange={v => setGrowthStage(v as TankGrowthStage)}
              options={[
                { value: 'fry', label: tr('siteTrade.stageFry') },
                { value: 'juvenile', label: tr('siteTrade.stageJuvenile') },
                { value: 'growout', label: tr('siteTrade.stageGrowout') },
                { value: 'broodstock', label: tr('siteTrade.stageBroodstock') },
              ]}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('siteTrade.batchLotNo')} value={batchLotNo} onChange={setBatchLotNo} />
            <Field label={tr('siteTrade.stockedAtLabel')} type="date" value={stockedAt} onChange={setStockedAt} />
          </div>

          {/* 분배 행 */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-[11px] text-gray-400 font-medium">{tr('siteTrade.tankAllocation')}</label>
              <span className="text-[10px] text-gray-500">{tr('siteTrade.totalCount', { count: totalCount.toLocaleString() })}</span>
            </div>
            {rows.map((row, i) => (
              <div key={i} className="grid grid-cols-[1fr_80px_80px_80px_auto] gap-1.5 items-end">
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colTank')}</label>}
                  <select value={row.tank_id} onChange={e => updateRow(i, 'tank_id', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-green-500/50">
                    <option value="">{tr('siteTrade.selectOption')}</option>
                    {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name}</option>)}
                  </select>
                </div>
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colCount')}</label>}
                  <input type="number" value={row.count} onChange={e => updateRow(i, 'count', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-green-500/50"
                    placeholder="0" />
                </div>
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colAvgWeight')}</label>}
                  <input type="number" value={row.avg_weight_g} onChange={e => updateRow(i, 'avg_weight_g', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-green-500/50"
                    placeholder="0" />
                </div>
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colLotOptional')}</label>}
                  <input type="text" value={row.lot_no} onChange={e => updateRow(i, 'lot_no', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-green-500/50"
                    placeholder="lot" />
                </div>
                <button type="button" onClick={() => removeRow(i)}
                  className={`text-[11px] text-gray-500 hover:text-red-400 ${i === 0 ? 'mt-3.5' : ''}`}>
                  ×
                </button>
              </div>
            ))}
            <button type="button" onClick={addRow}
              className="flex items-center gap-1 text-xs text-green-400 hover:text-green-300 border border-green-500/30 rounded px-2 py-1">
              <Plus className="w-3 h-3" />
              {tr('siteTrade.addAllocation')}
            </button>
          </div>

          {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>}
          {createdId && !showDocForm && (
            <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">
              {tr('siteTrade.stockingSavedOk', { id: createdId })}
            </div>
          )}
          {!createdId && (
            <Button size="sm" onClick={() => void submit()} disabled={busy || !species.trim()}>
              <Plus className="w-3.5 h-3.5 mr-1" />
              {busy ? tr('siteTrade.registering') : tr('siteTrade.registerStocking')}
            </Button>
          )}

          {showDocForm && createdId && (
            <div className="space-y-1">
              <div className="text-[11px] text-gray-400">{tr('siteTrade.attachStockingDocHint', { id: createdId })}</div>
              <DocAttachForm
                docTypes={STOCKING_DOC_TYPES}
                onUpload={handleDocUpload}
                onCancel={() => setShowDocForm(false)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── 출하 거래 폼 ─────────────────────────────────────────────────────────────

function HarvestBatchForm({
  siteId,
  tanks,
  partners,
  onRefresh,
}: {
  siteId: string;
  tanks: Tank[];
  partners: Partner[];
  onRefresh: () => void;
}) {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(false);
  const [buyerId, setBuyerId] = useState('');
  const [showNewBuyer, setShowNewBuyer] = useState(false);
  const [vehicleInfo, setVehicleInfo] = useState('');
  const [harvestedAt, setHarvestedAt] = useState('');
  const [rows, setRows] = useState<HarvestRow[]>([{ tank_id: '', lot_no: '', count: '', avg_weight_g: '', full_close: false }]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [createdId, setCreatedId] = useState<string | null>(null);
  const [showDocForm, setShowDocForm] = useState(false);

  const buyers = partners.filter(p => p.partner_type === 'buyer');
  const totalCount = rows.reduce((s, r) => s + (Number(r.count) || 0), 0);

  const addRow = () => setRows(prev => [...prev, { tank_id: '', lot_no: '', count: '', avg_weight_g: '', full_close: false }]);
  const removeRow = (i: number) => setRows(prev => prev.filter((_, idx) => idx !== i));
  const updateRow = <K extends keyof HarvestRow>(i: number, field: K, val: HarvestRow[K]) =>
    setRows(prev => prev.map((r, idx) => idx === i ? { ...r, [field]: val } : r));

  const submit = async () => {
    const lines = rows
      .filter(r => r.tank_id && r.count)
      .map(r => ({
        tank_id: r.tank_id,
        lot_no: r.lot_no.trim() || undefined,
        count: Number(r.count),
        avg_weight_g: r.avg_weight_g ? Number(r.avg_weight_g) : undefined,
        full_close: r.full_close,
      }));
    if (lines.length === 0) { setErr(tr('siteTrade.enterAtLeastOneHarvestLine')); return; }
    setBusy(true);
    setErr(null);
    try {
      const r = await SiteTrade.createHarvest({
        site_id: siteId,
        buyer_id: buyerId || undefined,
        vehicle_info: vehicleInfo.trim() || undefined,
        harvested_at: harvestedAt || undefined,
        lines,
      });
      setCreatedId(r.site_harvest_id);
      setShowDocForm(true);
      onRefresh();
      setBuyerId(''); setVehicleInfo(''); setHarvestedAt('');
      setRows([{ tank_id: '', lot_no: '', count: '', avg_weight_g: '', full_close: false }]);
    } catch (e) {
      setErr(friendlyError(e, tr));
    } finally {
      setBusy(false);
    }
  };

  const handleDocUpload = async (docType: string, file: File) => {
    if (!createdId) return;
    const form = new FormData();
    form.append('file', file);
    form.append('doc_type', docType);
    await SiteTrade.uploadHarvestDoc(createdId, form);
    setShowDocForm(false);
  };

  return (
    <div className="border border-teal-500/20 rounded-md overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-teal-500/10 hover:bg-teal-500/15 text-xs text-teal-300 font-medium"
      >
        <PackageCheck className="w-3.5 h-3.5" />
        {tr('siteTrade.harvestFormTitle')}
        {open ? <ChevronDown className="w-3 h-3 ml-auto" /> : <ChevronRight className="w-3 h-3 ml-auto" />}
      </button>
      {open && (
        <div className="p-3 space-y-3 bg-gray-900/40">
          <p className="text-[11px] text-gray-500">{tr('siteTrade.harvestFormDesc')}</p>

          {/* 구매처 */}
          <div className="space-y-1">
            <label className="text-[11px] text-gray-400 font-medium block">{tr('siteTrade.buyerLabel')}</label>
            <div className="flex items-center gap-2">
              <select value={buyerId} onChange={e => setBuyerId(e.target.value)}
                className="flex-1 h-8 px-2 rounded border border-gray-700 bg-gray-900 text-xs text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50">
                <option value="">{tr('siteTrade.noSelection')}</option>
                {buyers.map(p => <option key={p.partner_id} value={p.partner_id}>{p.name}</option>)}
              </select>
              {!showNewBuyer && (
                <button type="button" onClick={() => setShowNewBuyer(true)}
                  className="text-xs text-teal-400 hover:text-teal-300 border border-teal-500/30 rounded px-2 py-1 whitespace-nowrap">
                  {tr('siteTrade.newEntry')}
                </button>
              )}
            </div>
            {showNewBuyer && (
              <NewPartnerInline
                siteId={siteId}
                defaultType="buyer"
                onCreated={p => { setBuyerId(p.partner_id); setShowNewBuyer(false); onRefresh(); }}
                onCancel={() => setShowNewBuyer(false)}
              />
            )}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label={tr('siteTrade.vehicleLabel')} value={vehicleInfo} onChange={setVehicleInfo} />
            <Field label={tr('siteTrade.harvestedAtLabel')} type="date" value={harvestedAt} onChange={setHarvestedAt} />
          </div>

          {/* 출하 행 */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-[11px] text-gray-400 font-medium">{tr('siteTrade.harvestLines')}</label>
              <span className="text-[10px] text-gray-500">{tr('siteTrade.totalCount', { count: totalCount.toLocaleString() })}</span>
            </div>
            {rows.map((row, i) => (
              <div key={i} className="grid grid-cols-[1fr_70px_80px_80px_auto_auto] gap-1.5 items-end">
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colTank')}</label>}
                  <select value={row.tank_id} onChange={e => updateRow(i, 'tank_id', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50">
                    <option value="">{tr('siteTrade.selectOption')}</option>
                    {tanks.map(t => <option key={t.tank_id} value={t.tank_id}>{t.display_name}</option>)}
                  </select>
                </div>
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">Lot</label>}
                  <input type="text" value={row.lot_no} onChange={e => updateRow(i, 'lot_no', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
                    placeholder="lot" />
                </div>
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colCount')}</label>}
                  <input type="number" value={row.count} onChange={e => updateRow(i, 'count', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
                    placeholder="0" />
                </div>
                <div className="flex flex-col gap-0.5">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.colAvgWeight')}</label>}
                  <input type="number" value={row.avg_weight_g} onChange={e => updateRow(i, 'avg_weight_g', e.target.value)}
                    className="h-7 px-2 rounded border border-gray-700 bg-gray-900 text-[11px] text-white focus:outline-none focus:ring-1 focus:ring-teal-500/50"
                    placeholder={tr('siteTrade.optional')} />
                </div>
                <div className="flex flex-col gap-0.5 items-center">
                  {i === 0 && <label className="text-[10px] text-gray-400">{tr('siteTrade.fullClose')}</label>}
                  <input type="checkbox" checked={row.full_close}
                    onChange={e => updateRow(i, 'full_close', e.target.checked)}
                    className="w-4 h-4 accent-teal-500" />
                </div>
                <button type="button" onClick={() => removeRow(i)}
                  className={`text-[11px] text-gray-500 hover:text-red-400 ${i === 0 ? 'mt-3.5' : ''}`}>
                  ×
                </button>
              </div>
            ))}
            <div className="text-[10px] text-gray-500">{tr('siteTrade.fullCloseHint')}</div>
            <button type="button" onClick={addRow}
              className="flex items-center gap-1 text-xs text-teal-400 hover:text-teal-300 border border-teal-500/30 rounded px-2 py-1">
              <Plus className="w-3 h-3" />
              {tr('siteTrade.addHarvestLine')}
            </button>
          </div>

          {err && <div className="px-2 py-1 bg-red-500/10 border border-red-500/30 rounded text-[11px] text-red-400 font-mono">{err}</div>}
          {createdId && !showDocForm && (
            <div className="px-2 py-1 bg-green-500/10 border border-green-500/30 rounded text-[11px] text-green-400 font-mono">
              {tr('siteTrade.harvestSavedOk', { id: createdId })}
            </div>
          )}
          {!createdId && (
            <Button size="sm" onClick={() => void submit()} disabled={busy}>
              <Plus className="w-3.5 h-3.5 mr-1" />
              {busy ? tr('siteTrade.registering') : tr('siteTrade.registerHarvest')}
            </Button>
          )}

          {showDocForm && createdId && (
            <div className="space-y-1">
              <div className="text-[11px] text-gray-400">{tr('siteTrade.attachHarvestDocHint', { id: createdId })}</div>
              <DocAttachForm
                docTypes={HARVEST_DOC_TYPES}
                onUpload={handleDocUpload}
                onCancel={() => setShowDocForm(false)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── 입식/출하 내역 리스트 ────────────────────────────────────────────────────

function TradeHistory({
  siteId,
  refreshKey,
}: {
  siteId: string;
  refreshKey: number;
}) {
  const { tr } = useLanguage();
  const [stockings, setStockings] = useState<SiteStocking[]>([]);
  const [harvests, setHarvests] = useState<SiteHarvest[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [docOpenFor, setDocOpenFor] = useState<{ kind: 'stocking' | 'harvest'; id: string } | null>(null);

  useEffect(() => {
    if (!siteId) return;
    setLoading(true);
    setErr(null);
    Promise.all([
      SiteTrade.listStockings(siteId),
      SiteTrade.listHarvests(siteId),
    ])
      .then(([sr, hr]) => { setStockings(sr.stockings); setHarvests(hr.harvests); })
      .catch(e => setErr(friendlyError(e, tr)))
      .finally(() => setLoading(false));
  }, [siteId, refreshKey]);

  const handleDocUpload = async (kind: 'stocking' | 'harvest', id: string, docType: string, file: File) => {
    const form = new FormData();
    form.append('file', file);
    form.append('doc_type', docType);
    if (kind === 'stocking') await SiteTrade.uploadStockingDoc(id, form);
    else await SiteTrade.uploadHarvestDoc(id, form);
    setDocOpenFor(null);
  };

  if (loading) return <div className="text-xs text-gray-500">{tr('siteTrade.loading')}</div>;
  if (err) return <div className="text-xs text-red-400 font-mono">{err}</div>;
  if (stockings.length === 0 && harvests.length === 0) {
    return <div className="text-xs text-gray-500">{tr('siteTrade.noTradeHistory')}</div>;
  }

  return (
    <div className="space-y-4">
      {/* 입식 내역 */}
      {stockings.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs text-green-300 font-medium">{tr('siteTrade.stockingHistoryTitle', { count: stockings.length })}</div>
          {stockings.map(s => (
            <div key={s.site_stocking_id} className="p-2.5 border border-green-500/20 rounded bg-gray-800/30 text-xs space-y-1">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-white">{s.species}</span>
                <span className="px-1 py-0.5 rounded text-[10px] bg-green-500/20 text-green-300 border border-green-500/30">{s.growth_stage}</span>
                <span className="text-gray-400">{s.supplier_name ?? tr('siteTrade.noSupplier')}</span>
                <span className="text-gray-500 ml-auto">{new Date(s.stocked_at).toLocaleDateString('ko-KR')}</span>
              </div>
              <div className="text-gray-400">{tr('siteTrade.totalCount', { count: s.total_count.toLocaleString() })}
                {s.batch_lot_no && ` · lot ${s.batch_lot_no}`}
              </div>
              <div className="flex flex-wrap gap-1">
                {s.allocations.map((a, i) => (
                  <span key={i} className="text-[10px] bg-gray-700/60 rounded px-1.5 py-0.5">
                    {tr('siteTrade.allocationLine', { tank: a.tank_id, count: a.count.toLocaleString() })}
                  </span>
                ))}
              </div>
              {docOpenFor?.kind === 'stocking' && docOpenFor.id === s.site_stocking_id ? (
                <DocAttachForm
                  docTypes={STOCKING_DOC_TYPES}
                  onUpload={(dt, f) => handleDocUpload('stocking', s.site_stocking_id, dt, f)}
                  onCancel={() => setDocOpenFor(null)}
                />
              ) : (
                <button type="button"
                  onClick={() => setDocOpenFor({ kind: 'stocking', id: s.site_stocking_id })}
                  className="text-[10px] text-teal-500 hover:text-teal-400 border border-teal-700/40 hover:border-teal-500/60 rounded px-1.5 py-0.5 leading-none">
                  {tr('siteTrade.attachDoc')}
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {/* 출하 내역 */}
      {harvests.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs text-teal-300 font-medium">{tr('siteTrade.harvestHistoryTitle', { count: harvests.length })}</div>
          {harvests.map(h => (
            <div key={h.site_harvest_id} className="p-2.5 border border-teal-500/20 rounded bg-gray-800/30 text-xs space-y-1">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-white">{h.buyer_name ?? tr('siteTrade.noBuyer')}</span>
                <span className="text-gray-500 ml-auto">{new Date(h.harvested_at).toLocaleDateString('ko-KR')}</span>
              </div>
              <div className="text-gray-400">{tr('siteTrade.totalCount', { count: h.total_count.toLocaleString() })}
                {h.vehicle_info && ` · ${tr('siteTrade.vehiclePrefix')}${h.vehicle_info}`}
              </div>
              <div className="flex flex-wrap gap-1">
                {h.lines.map((l, i) => (
                  <span key={i} className="text-[10px] bg-gray-700/60 rounded px-1.5 py-0.5">
                    {tr('siteTrade.allocationLine', { tank: l.tank_id, count: l.count.toLocaleString() })}{l.full_close ? ` (${tr('siteTrade.fullCloseSuffix')})` : ''}
                  </span>
                ))}
              </div>
              {docOpenFor?.kind === 'harvest' && docOpenFor.id === h.site_harvest_id ? (
                <DocAttachForm
                  docTypes={HARVEST_DOC_TYPES}
                  onUpload={(dt, f) => handleDocUpload('harvest', h.site_harvest_id, dt, f)}
                  onCancel={() => setDocOpenFor(null)}
                />
              ) : (
                <button type="button"
                  onClick={() => setDocOpenFor({ kind: 'harvest', id: h.site_harvest_id })}
                  className="text-[10px] text-teal-500 hover:text-teal-400 border border-teal-700/40 hover:border-teal-500/60 rounded px-1.5 py-0.5 leading-none">
                  {tr('siteTrade.attachDoc')}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── SiteTradePanel (exported) ─────────────────────────────────────────────────

export function SiteTradePanel() {
  const { tr } = useLanguage();
  const [sites, setSites] = useState<Site[]>([]);
  const [selectedSiteId, setSelectedSiteId] = useState('');
  const [tanks, setTanks] = useState<Tank[]>([]);
  const [partners, setPartners] = useState<Partner[]>([]);
  const [sitesErr, setSitesErr] = useState<string | null>(null);
  const [historyKey, setHistoryKey] = useState(0);

  // 사이트 목록 로드
  useEffect(() => {
    Sites.list()
      .then(r => {
        setSites(r.items);
        if (r.items.length > 0) setSelectedSiteId(r.items[0].site_id);
      })
      .catch(e => setSitesErr(friendlyError(e, tr)));
  }, []);

  // 사이트 변경 시 수조·거래처 로드
  useEffect(() => {
    if (!selectedSiteId) return;
    Promise.all([
      Tanks.list({ siteId: selectedSiteId }),
      Partners.list(undefined, selectedSiteId),
    ])
      .then(([tr, pr]) => { setTanks(tr.items); setPartners(pr.partners); })
      .catch(() => undefined);
  }, [selectedSiteId]);

  const refreshAll = useCallback(() => {
    if (!selectedSiteId) return;
    Partners.list(undefined, selectedSiteId)
      .then(r => setPartners(r.partners))
      .catch(() => undefined);
    setHistoryKey(k => k + 1);
  }, [selectedSiteId]);

  return (
    <div className="space-y-5">
      <div className="bg-gradient-to-br from-gray-900 to-black border border-yellow-500/20 rounded-lg p-4 space-y-4">
        <h2 className="text-sm font-medium text-white flex items-center gap-2">
          <Fish className="w-4 h-4 text-yellow-400" />
          {tr('siteTrade.panelTitle')}
          <span className="text-xs text-gray-500 font-normal">{tr('siteTrade.panelSubtitle')}</span>
        </h2>

        {sitesErr ? (
          <div className="text-xs text-red-400 font-mono">{sitesErr}</div>
        ) : (
          <div className="flex flex-col gap-1 max-w-xs">
            <label className="text-xs text-gray-400 font-medium">{tr('siteTrade.siteSelect')}</label>
            <select
              value={selectedSiteId}
              onChange={e => setSelectedSiteId(e.target.value)}
              className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-yellow-500/50"
            >
              {sites.length === 0 && <option value="">{tr('siteTrade.noSites')}</option>}
              {sites.map(s => (
                <option key={s.site_id} value={s.site_id}>{s.name} ({s.site_id})</option>
              ))}
            </select>
          </div>
        )}

        {selectedSiteId && (
          <div className="space-y-2">
            <PartnerSection siteId={selectedSiteId} />
            <StockingBatchForm
              siteId={selectedSiteId}
              tanks={tanks}
              partners={partners}
              onRefresh={refreshAll}
            />
            <HarvestBatchForm
              siteId={selectedSiteId}
              tanks={tanks}
              partners={partners}
              onRefresh={refreshAll}
            />
          </div>
        )}
      </div>

      {/* 입식/출하 내역 */}
      {selectedSiteId && (
        <div className="bg-gradient-to-br from-gray-900 to-black border border-gray-700/40 rounded-lg p-4 space-y-3">
          <h3 className="text-xs font-medium text-gray-300">{tr('siteTrade.historyTitle')}</h3>
          <TradeHistory siteId={selectedSiteId} refreshKey={historyKey} />
        </div>
      )}
    </div>
  );
}
