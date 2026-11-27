import { useEffect, useState } from 'react';
import { useLanguage } from '../../lib/language-core';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../ui/tabs';
import { OperatingPolicy } from './OperatingPolicy';
import { InferenceMonitor } from './InferenceMonitor';
import { FeedCycleMonitor } from './FeedCycleMonitor';
import { HopperCard } from './HopperCard';
import { LearnedSafetyPanel } from './LearnedSafetyPanel';
import { ModelRegistry } from './ModelRegistry';
import { LearningDataCollector } from './LearningDataCollector';
import { AITrainingPage } from '../ai-training/AITrainingPage';
import { ControllerRegistry } from './ControllerRegistry';
import { AdminRegistry } from './AdminRegistry';

// sub-tab 순서 = 본선 시연 흐름:
//   1. 운영 정책        — BSF · 밀도 · GET (그룹 default + 수조 override)
//   2. 추론 모니터링    — LRCN 결과 (측면 카메라)
//   3. 사료공급        — 자동(AI)/수동 토글 + 자동 일정(수동) 흡수 + cycle + Safety Gate
//   4. 안전학습        — Learned Rules + Mining + Arbiter Log
//   5. 모델 관리       — Vision algorithms + tank application
//   6. 학습 데이터     — bootstrap + training + 5 영역 학습 카드 (현황판)
//   7. 현장 AI 학습    — 5/9 ai-training UI 의 React 이식 (4단계 마법사 + 박스 라벨링 + tank baseline/forecast)
//   8. 컨트롤러        — ESP32
//   9. 관리           — 자원 등록

type SubTab =
  | 'policy'
  | 'inference'
  | 'feed'
  | 'safety-learning'
  | 'models'
  | 'learning-data'
  | 'ai-training'
  | 'controllers'
  | 'admin';

const SUB_TABS: SubTab[] = [
  'policy', 'inference', 'feed', 'safety-learning', 'models',
  'learning-data', 'ai-training', 'controllers', 'admin',
];

export function MultiTankOperations() {
  const { tr } = useLanguage();
  const [subTab, setSubTab] = useState<SubTab>('policy');

  // 다른 sub-tab 에서의 이동 요청 (예: 운영 정책 → "사료공급 탭에서 변경" 링크).
  useEffect(() => {
    const onGoto = (ev: Event) => {
      const target = (ev as CustomEvent).detail;
      if (SUB_TABS.includes(target as SubTab)) setSubTab(target as SubTab);
    };
    window.addEventListener('bluei:goto-subtab', onGoto);
    return () => window.removeEventListener('bluei:goto-subtab', onGoto);
  }, []);

  return (
    <Tabs value={subTab} onValueChange={v => setSubTab(v as SubTab)}>
      <TabsList>
        <TabsTrigger value="policy">{tr('multiTankOps.tabPolicy')}</TabsTrigger>
        <TabsTrigger value="inference">{tr('multiTankOps.tabInference')}</TabsTrigger>
        <TabsTrigger value="feed">{tr('multiTankOps.tabFeed')}</TabsTrigger>
        <TabsTrigger value="safety-learning">{tr('multiTankOps.tabSafetyLearning')}</TabsTrigger>
        <TabsTrigger value="models">{tr('multiTankOps.tabModels')}</TabsTrigger>
        <TabsTrigger value="learning-data">{tr('multiTankOps.tabLearningData')}</TabsTrigger>
        <TabsTrigger value="ai-training">{tr('multiTankOps.tabAiTraining')}</TabsTrigger>
        <TabsTrigger value="controllers">{tr('multiTankOps.tabControllers')}</TabsTrigger>
        <TabsTrigger value="admin">{tr('multiTankOps.tabAdmin')}</TabsTrigger>
      </TabsList>

      <TabsContent value="policy" className="pt-4">
        <OperatingPolicy />
      </TabsContent>

      <TabsContent value="inference" className="pt-4">
        <InferenceMonitor />
      </TabsContent>

      <TabsContent value="feed" className="pt-4 space-y-4">
        <HopperCard />
        <FeedCycleMonitor />
      </TabsContent>

      <TabsContent value="safety-learning" className="pt-4">
        <LearnedSafetyPanel />
      </TabsContent>

      <TabsContent value="models" className="pt-4">
        <ModelRegistry />
      </TabsContent>

      <TabsContent value="learning-data" className="pt-4">
        <LearningDataCollector />
      </TabsContent>

      <TabsContent value="ai-training" className="pt-4">
        <AITrainingPage />
      </TabsContent>

      <TabsContent value="controllers" className="pt-4">
        <ControllerRegistry />
      </TabsContent>

      <TabsContent value="admin" className="pt-4">
        <AdminRegistry />
      </TabsContent>
    </Tabs>
  );
}
