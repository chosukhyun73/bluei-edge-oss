import { useState, useEffect } from 'react';
import { Groups } from './lib/api';
import type { Group, Tank } from './lib/types';
import { Header } from './components/Header';
import { Footer } from './components/Footer';
import { GroupSelector } from './components/GroupSelector';
import { GroupDashboard } from './components/GroupDashboard';
import { ProductionOverview } from './components/ProductionOverview';
import { TankDetail } from './components/TankDetail';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './components/ui/tabs';
import { MultiTankOperations } from './components/multitank/MultiTankOperations';
import { FarmSiteHeader } from './components/FarmSiteHeader';
import { CycleCompletionDispute } from './components/multitank/CycleCompletionDispute';
import { useCycleCompletionDispute } from './components/multitank/useCycleCompletionDispute';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ProductionEventManagement } from './components/production/ProductionEventManagement';
import { SiteTradePanel } from './components/production/SiteTradePanel';
import { useLanguage } from './lib/language-context';

export default function App() {
  const { tr } = useLanguage();
  const [topTab, setTopTab] = useState<'legacy' | 'multitank' | 'events' | 'sitetrade'>('legacy');

  const [groups, setGroups] = useState<Group[]>([]);
  const [groupTanks, setGroupTanks] = useState<Tank[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [selectedTankId, setSelectedTankId] = useState<string | null>(null);
  const [groupsError, setGroupsError] = useState<string | null>(null);

  // 앱 시작 시 그룹 목록 로드
  useEffect(() => {
    Groups.list()
      .then(r => setGroups(r.items))
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : 'unknown error';
        setGroupsError(msg);
        console.error('[bluei-edge] groups fetch failed:', err);
      });
  }, []);

  // 그룹 선택 시 해당 Cage/Tank 목록 로드 + TankDetail 초기화
  useEffect(() => {
    if (!selectedGroupId) {
      setGroupTanks([]);
      setSelectedTankId(null);
      return;
    }
    setSelectedTankId(null);
    Groups.tanks(selectedGroupId)
      .then(r => setGroupTanks(r.items))
      .catch((err: unknown) => {
        console.error('[bluei-edge] tanks fetch failed:', err);
        setGroupTanks([]);
      });
  }, [selectedGroupId]);

  const selectedGroup = groups.find(g => g.group_id === selectedGroupId) ?? null;
  const selectedTank = groupTanks.find(t => t.tank_id === selectedTankId) ?? null;

  // G-4 — cycle 종료 dispute auto-modal. 어느 탭이든 cycle 종료 시 자동 표시.
  const { current: cycleCompletion, dismiss: dismissCycleCompletion } = useCycleCompletionDispute();

  return (
    <div className="min-h-screen bg-background">
      <Header />

      <main className="max-w-[1920px] mx-auto px-6 py-6">
        {/* 농장(Farm) → 사이트(Site) — 전역 식별 정보. 사용자가 직접 등록. */}
        <section className="mb-8">
          <FarmSiteHeader />
        </section>

        {/* 최상위 뷰 전환 탭 */}
        <Tabs value={topTab} onValueChange={v => setTopTab(v as typeof topTab)} className="mb-6">
          <TabsList>
            <TabsTrigger value="legacy">{tr('nav.siteTanks')}</TabsTrigger>
            <TabsTrigger value="multitank">{tr('nav.aiManagement')}</TabsTrigger>
            <TabsTrigger value="events">{tr('nav.production')}</TabsTrigger>
            <TabsTrigger value="sitetrade">{tr('nav.trade')}</TabsTrigger>
          </TabsList>

          {/* ── 레거시 그룹 보기 ── */}
          <TabsContent value="legacy" className="pt-5">
            {groupsError && (
              <div className="mb-4 px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive font-mono">
                {tr('app.backendError')}: {groupsError} — {tr('app.checkService')}
              </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
              {/* 사이드바 — Group 선택 */}
              <div className="lg:col-span-1">
                <GroupSelector
                  groups={groups}
                  selectedGroupId={selectedGroupId}
                  onSelectGroup={setSelectedGroupId}
                  onGroupCreated={() => {
                    // 신규 그룹 등록 후 전체 목록 refetch — 자동 선택은 GroupSelector 내부에서.
                    void Groups.list().then(r => setGroups(r.items)).catch(() => undefined);
                  }}
                  onGroupDeleted={() => {
                    // 삭제 후 목록 refetch. 선택 해제는 GroupSelector 가 직접 호출.
                    void Groups.list().then(r => setGroups(r.items)).catch(() => undefined);
                  }}
                />
              </div>

              {/* 메인 패널 */}
              <div className="lg:col-span-3">
                <ErrorBoundary fallbackTitle={tr('error.panel')}>
                {selectedTank ? (
                  <TankDetail
                    tankId={selectedTank.tank_id}
                    displayName={selectedTank.display_name}
                    tank={selectedTank}
                    onBack={() => setSelectedTankId(null)}
                  />
                ) : selectedGroup ? (
                  <GroupDashboard
                    group={selectedGroup}
                    tanks={groupTanks}
                    onSelectTank={setSelectedTankId}
                    onTanksChanged={() => {
                      // 그룹 내 수조 추가/삭제 후 재조회.
                      if (selectedGroupId) {
                        void Groups.tanks(selectedGroupId)
                          .then(r => setGroupTanks(r.items))
                          .catch(() => undefined);
                      }
                    }}
                  />
                ) : (
                  <ProductionOverview />
                )}
                </ErrorBoundary>
              </div>
            </div>
          </TabsContent>

          {/* ── 다중 수조 운영 ── */}
          <TabsContent value="multitank" className="pt-5">
            <MultiTankOperations />
          </TabsContent>

          {/* ── 생산·이벤트 기록 ── */}
          <TabsContent value="events" className="pt-5">
            <ErrorBoundary fallbackTitle={tr('error.productionPanel')}>
              <ProductionEventManagement />
            </ErrorBoundary>
          </TabsContent>

          {/* ── 입식·출하·거래처 (사이트 단위) ── */}
          <TabsContent value="sitetrade" className="pt-5">
            <ErrorBoundary fallbackTitle={tr('error.tradePanel')}>
              <SiteTradePanel />
            </ErrorBoundary>
          </TabsContent>
        </Tabs>
      </main>

      <Footer />

      {/* G-4: 전역 cycle 종료 dispute auto-modal — 어느 탭에 있든 cycle 종료 시 자동 표시 */}
      <CycleCompletionDispute
        open={cycleCompletion !== null}
        observation={cycleCompletion?.observation ?? null}
        cycleId={cycleCompletion?.cycle.cycle_id ?? ''}
        tankId={cycleCompletion?.cycle.tank_id ?? ''}
        onClose={dismissCycleCompletion}
      />
    </div>
  );
}
