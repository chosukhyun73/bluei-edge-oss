import { useEffect, useState, type ReactNode } from 'react';
import { Plus, X } from 'lucide-react';
import { Farms, Sites } from '../lib/api';
import type { Farm, Site, NewFarmBody, NewSiteBody } from '../lib/types';
import { Card, CardContent } from './ui/card';
import { Skeleton } from './ui/skeleton';
import { useLanguage } from '../lib/language-context';

type Tr = (key: string, vars?: Record<string, string | number>) => string;

function genId(prefix: string): string {
  const rand = Math.random().toString(36).slice(2, 8);
  return `${prefix}_${Date.now().toString(36)}${rand}`;
}

function friendlyError(err: unknown, tr: Tr): string {
  if (err instanceof Error) return err.message;
  return tr('farmSite.unknownError');
}

type Status = { kind: 'idle' } | { kind: 'busy' } | { kind: 'err'; message: string };

// ── 모달 셸 ────────────────────────────────────────────────────────────────

function Modal({
  open, title, busy = false, onClose, children,
}: {
  open: boolean;
  title: string;
  busy?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const { tr } = useLanguage();
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, busy, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 backdrop-blur-sm"
      onClick={busy ? undefined : onClose}
      role="dialog"
      aria-modal="true"
    >
      <div
        className="w-full max-w-lg mx-4 bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-5 shadow-2xl"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-start justify-between mb-4 pb-3 border-b border-green-500/20">
          <h4 className="font-medium text-white">{title}</h4>
          <button
            onClick={onClose}
            disabled={busy}
            className="text-gray-500 hover:text-white disabled:opacity-30"
            aria-label={tr('farmSite.close')}
          >
            <X className="w-4 h-4" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

// ── 농장 등록 모달 ─────────────────────────────────────────────────────────

function NewFarmModal({
  open, onClose, onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (farm: Farm) => void;
}) {
  const { tr } = useLanguage();
  const [operator, setOperator] = useState('');
  const [licenseNo, setLicenseNo] = useState('');
  const [certsStr, setCertsStr] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  function reset() {
    setOperator(''); setLicenseNo(''); setCertsStr(''); setStatus({ kind: 'idle' });
  }

  async function submit() {
    const op = operator.trim();
    const lic = licenseNo.trim();
    if (!op || !lic) {
      setStatus({ kind: 'err', message: tr('farmSite.farm.requiredError') });
      return;
    }
    setStatus({ kind: 'busy' });
    try {
      const body: NewFarmBody = {
        farm_id: genId('farm'),
        operator: op,
        license_no: lic,
      };
      const certs = certsStr.split(',').map(s => s.trim()).filter(Boolean);
      if (certs.length > 0) body.certifications = certs;
      const res = await Farms.create(body);
      onCreated(res.item);
      reset();
      onClose();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <Modal
      open={open}
      title={tr('farmSite.farm.modalTitle')}
      busy={status.kind === 'busy'}
      onClose={() => { reset(); onClose(); }}
    >
      <div className="space-y-3 text-sm">
        <p className="text-xs text-gray-400">
          {tr('farmSite.farm.intro')}
        </p>

        <div className="space-y-1.5">
          <label htmlFor="nf_op" className="block text-xs font-medium text-gray-300">
            {tr('farmSite.farm.operatorLabel')} <span className="text-red-400">*</span>
          </label>
          <input
            id="nf_op"
            value={operator}
            onChange={e => setOperator(e.target.value)}
            placeholder={tr('farmSite.farm.operatorPlaceholder')}
            className="w-full h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-white text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
            disabled={status.kind === 'busy'}
          />
        </div>

        <div className="space-y-1.5">
          <label htmlFor="nf_lic" className="block text-xs font-medium text-gray-300">
            {tr('farmSite.farm.licenseLabel')} <span className="text-red-400">*</span>
          </label>
          <input
            id="nf_lic"
            value={licenseNo}
            onChange={e => setLicenseNo(e.target.value)}
            placeholder={tr('farmSite.farm.licensePlaceholder')}
            className="w-full h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-white text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
            disabled={status.kind === 'busy'}
          />
        </div>

        <div className="space-y-1.5">
          <label htmlFor="nf_cert" className="block text-xs font-medium text-gray-300">
            {tr('farmSite.farm.certLabel')}
          </label>
          <input
            id="nf_cert"
            value={certsStr}
            onChange={e => setCertsStr(e.target.value)}
            placeholder={tr('farmSite.farm.certPlaceholder')}
            className="w-full h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-white text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
            disabled={status.kind === 'busy'}
          />
        </div>

        {status.kind === 'err' && (
          <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
            {status.message}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-3 mt-3 border-t border-gray-700/30">
          <button
            onClick={() => { reset(); onClose(); }}
            disabled={status.kind === 'busy'}
            className="px-3 py-1.5 text-sm text-gray-400 hover:text-white disabled:opacity-30"
          >
            {tr('farmSite.cancel')}
          </button>
          <button
            onClick={submit}
            disabled={status.kind === 'busy'}
            className="px-4 py-1.5 text-sm rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-green-900 text-white"
          >
            {status.kind === 'busy' ? tr('farmSite.submitting') : tr('farmSite.submit')}
          </button>
        </div>
      </div>
    </Modal>
  );
}

// ── 사이트 등록 모달 ───────────────────────────────────────────────────────

function NewSiteModal({
  open, farmId, farmLabel, onClose, onCreated,
}: {
  open: boolean;
  farmId: string;
  farmLabel: string;
  onClose: () => void;
  onCreated: (site: Site) => void;
}) {
  const { tr } = useLanguage();
  const [name, setName] = useState('');
  const [fisheryLicenseNo, setFisheryLicenseNo] = useState('');
  const [siteType, setSiteType] = useState<'land' | 'marine'>('land');
  const [address, setAddress] = useState('');
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  function reset() {
    setName(''); setFisheryLicenseNo(''); setSiteType('land'); setAddress('');
    setStatus({ kind: 'idle' });
  }

  async function submit() {
    const n = name.trim();
    const lic = fisheryLicenseNo.trim();
    if (!n || !lic) {
      setStatus({ kind: 'err', message: tr('farmSite.site.requiredError') });
      return;
    }
    setStatus({ kind: 'busy' });
    try {
      const body: NewSiteBody = {
        site_id: genId('site'),
        farm_id: farmId,
        site_type: siteType,
        name: n,
        timezone: 'Asia/Seoul',
        metadata: { fishery_license_no: lic },
      };
      if (address.trim()) body.address = address.trim();
      const res = await Sites.create(body);
      onCreated(res.item);
      reset();
      onClose();
    } catch (err) {
      setStatus({ kind: 'err', message: friendlyError(err, tr) });
    }
  }

  return (
    <Modal
      open={open}
      title={tr('farmSite.site.modalTitle')}
      busy={status.kind === 'busy'}
      onClose={() => { reset(); onClose(); }}
    >
      <div className="space-y-3 text-sm">
        <p className="text-xs text-gray-400">
          {tr('farmSite.site.introPrefix')}
          {' '}<span className="text-gray-200 font-medium">{farmLabel}</span>.
          {' '}{tr('farmSite.site.introSuffix')}
        </p>

        <div className="space-y-1.5">
          <label htmlFor="ns_name" className="block text-xs font-medium text-gray-300">
            {tr('farmSite.site.nameLabel')} <span className="text-red-400">*</span>
          </label>
          <input
            id="ns_name"
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder={tr('farmSite.site.namePlaceholder')}
            className="w-full h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-white text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
            disabled={status.kind === 'busy'}
          />
        </div>

        <div className="space-y-1.5">
          <label htmlFor="ns_lic" className="block text-xs font-medium text-gray-300">
            {tr('farmSite.site.licenseLabel')} <span className="text-red-400">*</span>
          </label>
          <input
            id="ns_lic"
            value={fisheryLicenseNo}
            onChange={e => setFisheryLicenseNo(e.target.value)}
            placeholder={tr('farmSite.site.licensePlaceholder')}
            className="w-full h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-white text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
            disabled={status.kind === 'busy'}
          />
        </div>

        <div className="space-y-1.5">
          <label className="block text-xs font-medium text-gray-300">
            {tr('farmSite.site.typeLabel')} <span className="text-red-400">*</span>
          </label>
          <div className="flex gap-2">
            {(['land', 'marine'] as const).map(t => (
              <button
                key={t}
                type="button"
                onClick={() => setSiteType(t)}
                disabled={status.kind === 'busy'}
                className={
                  siteType === t
                    ? 'flex-1 px-3 py-1.5 text-sm rounded border bg-green-600/20 border-green-500/60 text-green-200'
                    : 'flex-1 px-3 py-1.5 text-sm rounded border border-gray-700 text-gray-400 hover:text-white hover:border-gray-600'
                }
              >
                {t === 'land' ? tr('farmSite.site.typeLand') : tr('farmSite.site.typeMarine')}
              </button>
            ))}
          </div>
        </div>

        <div className="space-y-1.5">
          <label htmlFor="ns_addr" className="block text-xs font-medium text-gray-300">
            {tr('farmSite.site.addressLabel')}
          </label>
          <input
            id="ns_addr"
            value={address}
            onChange={e => setAddress(e.target.value)}
            placeholder={tr('farmSite.site.addressPlaceholder')}
            className="w-full h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-white text-sm focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50"
            disabled={status.kind === 'busy'}
          />
        </div>

        {status.kind === 'err' && (
          <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 font-mono">
            {status.message}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-3 mt-3 border-t border-gray-700/30">
          <button
            onClick={() => { reset(); onClose(); }}
            disabled={status.kind === 'busy'}
            className="px-3 py-1.5 text-sm text-gray-400 hover:text-white disabled:opacity-30"
          >
            {tr('farmSite.cancel')}
          </button>
          <button
            onClick={submit}
            disabled={status.kind === 'busy'}
            className="px-4 py-1.5 text-sm rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-green-900 text-white"
          >
            {status.kind === 'busy' ? tr('farmSite.submitting') : tr('farmSite.submit')}
          </button>
        </div>
      </div>
    </Modal>
  );
}

// ── 메인 헤더 ──────────────────────────────────────────────────────────────

export function FarmSiteHeader() {
  const { tr } = useLanguage();
  const [farms, setFarms] = useState<Farm[]>([]);
  const [sites, setSites] = useState<Site[]>([]);
  const [selectedFarmId, setSelectedFarmId] = useState<string>('');
  const [selectedSiteId, setSelectedSiteId] = useState<string>('');
  const [farmsLoading, setFarmsLoading] = useState(true);
  const [sitesLoading, setSitesLoading] = useState(false);
  const [farmsError, setFarmsError] = useState<string | null>(null);
  const [sitesError, setSitesError] = useState<string | null>(null);
  const [farmModalOpen, setFarmModalOpen] = useState(false);
  const [siteModalOpen, setSiteModalOpen] = useState(false);

  useEffect(() => {
    setFarmsLoading(true);
    Farms.list()
      .then(r => {
        setFarms(r.items);
        if (r.items.length > 0 && !selectedFarmId) {
          setSelectedFarmId(r.items[0].farm_id);
        }
      })
      .catch(err => setFarmsError(friendlyError(err, tr)))
      .finally(() => setFarmsLoading(false));
    // 최초 1회만 — 등록 후 갱신은 onCreated 콜백에서 직접 처리.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!selectedFarmId) { setSites([]); setSelectedSiteId(''); return; }
    setSitesLoading(true);
    setSitesError(null);
    Sites.list(selectedFarmId)
      .then(r => {
        setSites(r.items);
        if (r.items.length > 0) setSelectedSiteId(r.items[0].site_id);
        else setSelectedSiteId('');
      })
      .catch(err => setSitesError(friendlyError(err, tr)))
      .finally(() => setSitesLoading(false));
  }, [selectedFarmId]);

  const selectedFarm = farms.find(f => f.farm_id === selectedFarmId) ?? null;
  const farmLabel = selectedFarm
    ? `${selectedFarm.operator}${selectedFarm.license_no ? ` (${selectedFarm.license_no})` : ''}`
    : '';

  async function handleFarmCreated(farm: Farm) {
    // 등록 후 list 재조회 — POST 응답이 일부 필드를 누락할 수 있어 안전 동작.
    try {
      const r = await Farms.list();
      setFarms(r.items);
    } catch {
      setFarms(prev => [...prev, farm]);
    }
    setSelectedFarmId(farm.farm_id);
  }

  async function handleSiteCreated(site: Site) {
    // POST 응답에 site_type 등이 빠질 수 있어 list 재조회.
    try {
      const r = await Sites.list(selectedFarmId);
      setSites(r.items);
    } catch {
      setSites(prev => [...prev, site]);
    }
    setSelectedSiteId(site.site_id);
  }

  return (
    <Card>
      <CardContent className="pt-4 pb-4">
        <div className="flex flex-wrap items-end gap-x-6 gap-y-3">
          {/* 농장 */}
          <div className="flex flex-col gap-1.5">
            <label htmlFor="farm-select" className="text-xs text-gray-400 font-medium">
              {tr('farmSite.farmLabel')}
            </label>
            <div className="flex items-center gap-2">
              {farmsLoading ? (
                <Skeleton className="h-9 w-72" />
              ) : farmsError ? (
                <span className="text-xs text-destructive font-mono">{tr('farmSite.loadFailed', { error: farmsError })}</span>
              ) : (
                <select
                  id="farm-select"
                  value={selectedFarmId}
                  onChange={e => setSelectedFarmId(e.target.value)}
                  className="h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50 min-w-[18rem]"
                  aria-label={tr('farmSite.farmSelectAria')}
                >
                  {farms.length === 0 && <option value="">{tr('farmSite.noFarms')}</option>}
                  {farms.map(f => (
                    <option key={f.farm_id} value={f.farm_id}>
                      {f.operator}{f.license_no ? ` (${f.license_no})` : ''}
                    </option>
                  ))}
                </select>
              )}
              <button
                type="button"
                onClick={() => setFarmModalOpen(true)}
                className="h-9 px-2.5 inline-flex items-center gap-1 rounded-md border border-gray-700 bg-gray-800 text-gray-300 hover:text-white hover:border-green-500/50 hover:bg-green-600/10 text-xs"
                title={tr('farmSite.farm.newButtonTitle')}
              >
                <Plus className="w-3.5 h-3.5" />
                {tr('farmSite.new')}
              </button>
            </div>
          </div>

          {/* 사이트 */}
          <div className="flex flex-col gap-1.5">
            <label htmlFor="site-select" className="text-xs text-gray-400 font-medium">
              {tr('farmSite.siteLabel')}
            </label>
            <div className="flex items-center gap-2">
              {sitesLoading ? (
                <Skeleton className="h-9 w-72" />
              ) : sitesError ? (
                <span className="text-xs text-destructive font-mono">{tr('farmSite.loadFailed', { error: sitesError })}</span>
              ) : (
                <select
                  id="site-select"
                  value={selectedSiteId}
                  onChange={e => setSelectedSiteId(e.target.value)}
                  disabled={sites.length === 0}
                  className="h-9 px-3 rounded-md border border-gray-700 bg-gray-800 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50 focus:border-green-500/50 min-w-[18rem] disabled:opacity-50"
                  aria-label={tr('farmSite.siteSelectAria')}
                >
                  {sites.length === 0 && (
                    <option value="">
                      {selectedFarmId ? tr('farmSite.noSites') : tr('farmSite.selectFarmFirst')}
                    </option>
                  )}
                  {sites.map(s => (
                    <option key={s.site_id} value={s.site_id}>
                      {s.name} ({s.site_type === 'land' ? tr('farmSite.site.typeLandShort') : tr('farmSite.site.typeMarineShort')})
                    </option>
                  ))}
                </select>
              )}
              <button
                type="button"
                onClick={() => setSiteModalOpen(true)}
                disabled={!selectedFarmId}
                className="h-9 px-2.5 inline-flex items-center gap-1 rounded-md border border-gray-700 bg-gray-800 text-gray-300 hover:text-white hover:border-green-500/50 hover:bg-green-600/10 text-xs disabled:opacity-40 disabled:cursor-not-allowed"
                title={selectedFarmId ? tr('farmSite.site.newButtonTitle') : tr('farmSite.site.newButtonDisabledTitle')}
              >
                <Plus className="w-3.5 h-3.5" />
                {tr('farmSite.new')}
              </button>
            </div>
          </div>
        </div>

        <p className="mt-3 text-xs text-gray-600">
          {tr('farmSite.footerNote')}
        </p>
      </CardContent>

      <NewFarmModal
        open={farmModalOpen}
        onClose={() => setFarmModalOpen(false)}
        onCreated={handleFarmCreated}
      />
      <NewSiteModal
        open={siteModalOpen}
        farmId={selectedFarmId}
        farmLabel={farmLabel}
        onClose={() => setSiteModalOpen(false)}
        onCreated={handleSiteCreated}
      />
    </Card>
  );
}
