import { useState, useEffect, useCallback } from 'react';
import {
  ArrowLeft, RefreshCw,
  Fish, Activity, Cpu, Weight,
} from 'lucide-react';
import { Tanks } from '../lib/api';
import type { StateVector, WeightHistorySnapshot, Tank } from '../lib/types';
import { relativeTime } from '../lib/format';
import { Skeleton } from './ui/skeleton';
import { Button } from './ui/button';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './ui/tabs';
import { FishSection } from './sections/FishSection';
import { WaterSection } from './sections/WaterSection';
import { EquipmentSection } from './sections/EquipmentSection';
import { FeedingSection } from './sections/FeedingSection';
import { BiologicalContextSection } from './sections/BiologicalContextSection';
import { ConfidenceSection } from './sections/ConfidenceSection';
import { AnomalySection } from './sections/AnomalySection';
import { AdaptationSection } from './sections/AdaptationSection';
import { AutonomousSection } from './sections/AutonomousSection';
import { DecisionsSection } from './sections/DecisionsSection';
import { TankSettingsSection } from './sections/TankSettingsSection';
import { FeedQuickControl } from './multitank/FeedQuickControl';
import { useLanguage } from '../lib/language-context';

interface Props {
  tankId: string;
  displayName: string;
  tank?: Tank;       // 부모가 갖고 있으면 전달, 없으면 quick control 미표시
  onBack: () => void;
}

// 섹션 카드 — 탭 내부 개별 영역용
function SectionCard({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) {
  return (
    <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/20 rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-3 border-b border-green-500/10 bg-gradient-to-r from-green-900/10 to-transparent">
        {icon}
        <span className="text-sm font-medium text-white">{title}</span>
      </div>
      <div className="p-5">{children}</div>
    </div>
  );
}

// 히어로 헤더의 작은 지표 카드
function StatCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="bg-gradient-to-br from-gray-800/50 to-black/50 border border-green-500/20 rounded-lg p-4">
      <div className="flex items-center gap-2 text-gray-400 mb-2">
        {icon}
        <span className="text-xs">{label}</span>
      </div>
      <div className="text-xl font-bold text-white font-mono">{value}</div>
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="space-y-4">
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="space-y-2 p-6 bg-card border border-border rounded-xl">
          <Skeleton className="h-4 w-32" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-4/5" />
        </div>
      ))}
    </div>
  );
}

export function TankDetail({ tankId, displayName, tank, onBack }: Props) {
  const { tr } = useLanguage();
  const [state, setState] = useState<StateVector | null>(null);
  const [weightSnapshots, setWeightSnapshots] = useState<WeightHistorySnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [tab, setTab] = useState<'status' | 'biological' | 'autonomous' | 'settings'>('status');

  const load = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true);
    else setLoading(true);
    setError(null);
    try {
      const [sv, wh] = await Promise.all([
        Tanks.stateVector(tankId),
        Tanks.weightHistory(tankId, 30),
      ]);
      setState(sv);
      setWeightSnapshots(wh.snapshots);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [tankId]);

  useEffect(() => { void load(); }, [load]);

  const bio = state?.biological_context;
  const avgWeightG = bio?.estimated_avg_weight_g ?? bio?.avg_weight_g;

  return (
    <div className="space-y-5">
      {/* ── 히어로 헤더 ── */}
      <div className="bg-gradient-to-br from-gray-900 to-black border border-green-500/30 rounded-lg p-6">
        <div className="flex items-start justify-between mb-6">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <Button variant="ghost" size="sm" onClick={onBack} className="px-2 gap-1 text-gray-400 hover:text-white">
                <ArrowLeft className="w-4 h-4" />
              </Button>
              <h2 className="text-3xl font-bold text-white">{displayName}</h2>
            </div>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-sm text-gray-400 font-mono ml-1">
              <span>· {tankId}</span>
              {bio && (
                <>
                  {bio.species && <span>· {bio.species}</span>}
                  {bio.system_type && <span>· {bio.system_type}</span>}
                  {bio.fish_count != null && <span>· {tr('tankDetail.fishCountValue', { count: bio.fish_count.toLocaleString() })}</span>}
                </>
              )}
            </div>
          </div>

          {/* 평균 체중 글로우 */}
          <div className="text-right flex-shrink-0">
            <div className="text-sm text-gray-400 mb-1">{tr('tankDetail.avgWeight')}</div>
            <div
              className="text-4xl font-bold text-green-400"
              style={{ textShadow: '0 0 20px rgba(34, 197, 94, 0.5)' }}
            >
              {avgWeightG != null ? `${avgWeightG.toFixed(0)}g` : '—'}
            </div>
            {bio?.fcr_source && (
              <div className="text-xs text-gray-500 mt-1 font-mono">
                FCR: {bio.expected_fcr?.toFixed(2)} ({bio.fcr_source})
              </div>
            )}
          </div>
        </div>

        {/* 4 stat cards */}
        {state ? (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <StatCard
              icon={<Fish className="w-4 h-4" />}
              label={tr('tankDetail.fishCount')}
              value={bio?.fish_count != null ? bio.fish_count.toLocaleString() : '—'}
            />
            <StatCard
              icon={<Weight className="w-4 h-4" />}
              label={tr('tankDetail.totalBiomass')}
              value={bio?.biomass_kg != null ? `${bio.biomass_kg.toFixed(1)}kg` : '—'}
            />
            <StatCard
              icon={<Activity className="w-4 h-4" />}
              label={tr('tankDetail.activity')}
              value={state.fish?.activity_score != null ? `${(state.fish.activity_score * 100).toFixed(0)}%` : '—'}
            />
            <StatCard
              icon={<Cpu className="w-4 h-4" />}
              label={tr('tankDetail.autonomousMode')}
              value={state.autonomous?.mode ?? '—'}
            />
          </div>
        ) : (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-20 rounded-lg" />
            ))}
          </div>
        )}
      </div>

      {/* 새로고침 / 마지막 업데이트 */}
      <div className="flex items-center justify-end gap-3">
        {state && (
          <span className="text-xs text-muted-foreground font-mono">{relativeTime(state.timestamp)}</span>
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() => void load(true)}
          disabled={refreshing}
          className="gap-1"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />
          {tr('tankDetail.refresh')}
        </Button>
      </div>

      {/* 에러 */}
      {error && (
        <div className="px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive font-mono">
          {error}
        </div>
      )}

      {/* ── 3-탭 콘텐츠 ── */}
      {loading ? (
        <LoadingSkeleton />
      ) : state ? (
        <Tabs value={tab} onValueChange={v => setTab(v as typeof tab)}>
          <TabsList>
            <TabsTrigger value="status">{tr('tankDetail.tabStatus')}</TabsTrigger>
            <TabsTrigger value="biological">{tr('tankDetail.tabBiological')}</TabsTrigger>
            <TabsTrigger value="autonomous">{tr('tankDetail.tabAutonomous')}</TabsTrigger>
            <TabsTrigger value="settings">{tr('tankDetail.tabSettings')}</TabsTrigger>
          </TabsList>

          {/* 상태 탭 */}
          <TabsContent value="status" className="space-y-4 pt-1">
            <SectionCard icon={<Fish className="w-4 h-4 text-green-500" />} title={tr('tankDetail.sectionFishStatus')}>
              <FishSection data={state.fish} />
            </SectionCard>
            <SectionCard icon={<span className="text-green-500 text-sm">💧</span>} title={tr('tankDetail.sectionWater')}>
              <WaterSection data={state.water} />
            </SectionCard>
            <SectionCard icon={<span className="text-green-500 text-sm">🔧</span>} title={tr('tankDetail.sectionEquipment')}>
              <EquipmentSection data={state.equipment} />
            </SectionCard>
            <SectionCard icon={<span className="text-green-500 text-sm">🍽</span>} title={tr('tankDetail.sectionFeeding')}>
              {tank && (
                <div className="mb-3">
                  <FeedQuickControl tank={tank} variant="full" />
                </div>
              )}
              <FeedingSection data={state.feeding} />
            </SectionCard>
          </TabsContent>

          {/* 생물 & FCR 탭 */}
          <TabsContent value="biological" className="space-y-4 pt-1">
            <SectionCard icon={<span className="text-green-500 text-sm">🌱</span>} title={tr('tankDetail.sectionBiologicalContext')}>
              <BiologicalContextSection
                data={state.biological_context}
                weightSnapshots={weightSnapshots}
              />
            </SectionCard>
          </TabsContent>

          {/* 설정 탭 — 기본 정보 / 자원 매핑 / 입식 이벤트 */}
          <TabsContent value="settings" className="space-y-4 pt-1">
            <TankSettingsSection tankId={tankId} />
          </TabsContent>

          {/* 자율 & 결정 탭 */}
          <TabsContent value="autonomous" className="space-y-4 pt-1">
            <SectionCard icon={<span className="text-green-500 text-sm">📊</span>} title={tr('tankDetail.sectionConfidence')}>
              <ConfidenceSection data={state.confidence} />
            </SectionCard>
            <SectionCard icon={<span className="text-orange-400 text-sm">⚠️</span>} title={tr('tankDetail.sectionAnomaly')}>
              <AnomalySection data={state.anomaly} />
            </SectionCard>
            <SectionCard icon={<span className="text-green-500 text-sm">📈</span>} title={tr('tankDetail.sectionAdaptation')}>
              <AdaptationSection data={state.adaptation} />
            </SectionCard>
            <SectionCard icon={<Cpu className="w-4 h-4 text-green-500" />} title={tr('tankDetail.sectionAutonomousMode')}>
              <AutonomousSection data={state.autonomous} />
            </SectionCard>
            <SectionCard icon={<span className="text-green-500 text-sm">🔀</span>} title={tr('tankDetail.sectionDecisions')}>
              <DecisionsSection data={state.decisions} />
            </SectionCard>
          </TabsContent>
        </Tabs>
      ) : null}
    </div>
  );
}
