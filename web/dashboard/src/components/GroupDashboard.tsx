import { useState, useEffect, useCallback } from 'react';
import type { Group, Tank, StateVector } from '../lib/types';
import { Tanks } from '../lib/api';
import { useLanguage } from '../lib/language-context';
import { GroupHeader } from './GroupHeader';
import { GroupOverview } from './GroupOverview';
import { GroupTankComparison } from './GroupTankComparison';
import { BroodstockPanel } from './BroodstockPanel';
import { SpawnPanel } from './SpawnPanel';
import { LarvalPanel, LiveFeedPanel } from './LarvalPanel';
import { TreatmentPanel } from './TreatmentPanel';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './ui/tabs';

interface Props {
  group: Group;
  tanks: Tank[];
  onSelectTank: (tankId: string) => void;
  // 그룹 내 수조 등록/삭제 후 상위 App 에 refetch 트리거.
  onTanksChanged?: () => void;
}

export function GroupDashboard({ group, tanks, onSelectTank, onTanksChanged }: Props) {
  const { tr } = useLanguage();
  const [view, setView] = useState<string>('overview');
  // 종묘장 단계(stage_role) — 그룹별 hatchery 탭 노출 분기.
  const stageRole = (group.metadata as { stage_role?: string } | undefined)?.stage_role;
  const isHatchery = stageRole === 'broodstock' || stageRole === 'spawning'
    || stageRole === 'larval' || stageRole === 'juvenile';

  // Cage/Tank 탭에서 비교 카드가 stateVectors 를 필요로 하므로 GroupDashboard 레벨에서 호이스팅
  // GroupOverview 도 동일한 Map 을 공유하면 이중 폴링이 사라지지만,
  // 탭 전환 시 언마운트되는 GroupOverview 의 useEffect 와 충돌 방지를 위해 각자 독립 폴링 유지
  // → Cage/Tank 탭 전용으로 별도 stateVectors 폴링
  const [comparisonSVs, setComparisonSVs] = useState<Map<string, StateVector>>(new Map());

  const tankIds = tanks.map(t => t.tank_id);

  // Phase A.1 — 4 tank 동시 호출 시 backend SQLite read 경합으로 timeout 발생.
  // Promise.all 대신 250ms 간격 직렬 호출 (개별 실패는 skip, 나머지 진행).
  const fetchForComparison = useCallback(async () => {
    if (tankIds.length === 0) return;
    try {
      const m = new Map<string, StateVector>();
      for (let i = 0; i < tanks.length; i++) {
        if (i > 0) await new Promise(r => setTimeout(r, 250));
        try {
          const sv = await Tanks.stateVector(tanks[i].tank_id);
          m.set(tanks[i].tank_id, sv);
        } catch {
          // 개별 tank 실패는 skip — 나머지 진행
        }
      }
      setComparisonSVs(m);
    } catch {
      // 오류 무시
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(tankIds)]);

  // Cage/Tank 탭 활성 시에만 폴링 — MJPEG 연결 수 절약과 같은 이유
  useEffect(() => {
    if (view !== 'tank') return;
    void fetchForComparison();
    const id = setInterval(() => void fetchForComparison(), 10_000);
    return () => clearInterval(id);
  }, [view, fetchForComparison]);

  const totalTanks = tanks.length;
  const totalFish = tanks.reduce((sum, t) => sum + (t.fish_count ?? 0), 0);
  const avgWeight =
    totalFish > 0
      ? tanks.reduce((sum, t) => sum + (t.fish_count ?? 0) * (t.avg_weight_g ?? 0), 0) / totalFish
      : 0;
  const totalBiomassKg = tanks.reduce(
    (sum, t) => sum + ((t.fish_count ?? 0) * (t.avg_weight_g ?? 0)) / 1000,
    0,
  );

  return (
    <div className="space-y-5">
      <GroupHeader
        group={group}
        stats={{ totalTanks, totalFish, avgWeight, totalBiomassKg }}
      />

      <Tabs
        value={view}
        onValueChange={v => setView(v)}
      >
        <TabsList>
          <TabsTrigger value="overview">{tr('groupDashboard.overview')}</TabsTrigger>
          <TabsTrigger value="tank">Cage/Tank ({totalTanks})</TabsTrigger>
          {stageRole === 'broodstock' && <TabsTrigger value="broodstock">{tr('groupDashboard.stageBroodstock')}</TabsTrigger>}
          {stageRole === 'spawning' && <TabsTrigger value="spawn">{tr('groupDashboard.stageSpawning')}</TabsTrigger>}
          {stageRole === 'spawning' && <TabsTrigger value="hatch">{tr('groupDashboard.stageHatching')}</TabsTrigger>}
          {stageRole === 'larval' && <TabsTrigger value="larval">{tr('groupDashboard.stageLarval')}</TabsTrigger>}
          {stageRole === 'larval' && <TabsTrigger value="livefeed">{tr('groupDashboard.stageLiveFeed')}</TabsTrigger>}
          {isHatchery && <TabsTrigger value="treatment">{tr('groupDashboard.stageTreatment')}</TabsTrigger>}
        </TabsList>

        <TabsContent value="overview">
          {/* 개요 탭 — 탱크별 카메라 LIVE + 센서 매트릭스 + 장비 상태 */}
          <GroupOverview tanks={tanks} onSelectTank={onSelectTank} />
        </TabsContent>

        <TabsContent value="tank">
          {/* Cage/Tank 탭 — 탱크별 비교 카드 + 수조 추가/삭제 */}
          <GroupTankComparison
            tanks={tanks}
            stateVectors={comparisonSVs}
            onSelectTank={onSelectTank}
            groupId={group.group_id}
            onTanksChanged={onTanksChanged}
          />
        </TabsContent>

        {stageRole === 'broodstock' && (
          <TabsContent value="broodstock">
            <BroodstockPanel groupId={group.group_id} tanks={tanks} />
          </TabsContent>
        )}
        {stageRole === 'spawning' && (
          <>
            <TabsContent value="spawn">
              <SpawnPanel groupId={group.group_id} tanks={tanks} mode="spawning" />
            </TabsContent>
            <TabsContent value="hatch">
              <SpawnPanel groupId={group.group_id} tanks={tanks} mode="hatching" />
            </TabsContent>
          </>
        )}
        {stageRole === 'larval' && (
          <>
            <TabsContent value="larval">
              <LarvalPanel groupId={group.group_id} tanks={tanks} />
            </TabsContent>
            <TabsContent value="livefeed">
              <LiveFeedPanel groupId={group.group_id} tanks={tanks} />
            </TabsContent>
          </>
        )}
        {isHatchery && (
          <TabsContent value="treatment">
            <TreatmentPanel groupId={group.group_id} tanks={tanks} />
          </TabsContent>
        )}

      </Tabs>
    </div>
  );
}
