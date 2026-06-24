import { useCallback, useEffect, useState } from 'react';
import { useLanguage } from '../../lib/language-context';
import { Vision } from '../../lib/api';
import type { VisionTrainingStatus, VisionAlgorithm } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';
import { StatusSection } from './StatusSection';
import { BootstrapTab } from './BootstrapTab';
import { DisputeTab } from './DisputeTab';
import { TrainingControlCard } from './TrainingControlCard';
import { ResultSignalsCard } from './ResultSignalsCard';
import { PromoteCard } from './PromoteCard';
import { TankBaselineCard } from './TankBaselineCard';
import { TankForecastCard } from './TankForecastCard';
import { SafetyInfoCard } from './SafetyInfoCard';
import { DiskUsageCard } from './DiskUsageCard';

const POLL_INTERVAL_MS = 4000; // 5/9 원본과 동일

/**
 * 5/9 web/local-ui/ai-training.html + ai-training.js 의 React + Tauri 이식.
 *
 * UI 패턴은 현재 dashboard (Tailwind + shadcn 류) 를 빌리고 내용/흐름/문구는 원본 그대로.
 * 향후 변경 지점 (시점별 dispute 분리, 5 영역 누적 학습 wiring 등) 시각화용 baseline.
 */
export function AITrainingPage() {
  const { tr } = useLanguage();
  const [status, setStatus] = useState<VisionTrainingStatus | null>(null);
  const [algorithm, setAlgorithm] = useState<VisionAlgorithm | null>(null);
  const [activeTab, setActiveTab] = useState<'bootstrap' | 'dispute'>('bootstrap');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refreshAIState = useCallback(async () => {
    try {
      const [s, algs] = await Promise.all([
        Vision.trainingStatus(),
        Vision.algorithms(),
      ]);
      setStatus(s);
      setAlgorithm(algs.items.find(a => a.vision_algorithm_id === s.algorithm_id) ?? null);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('aiTrainingPage.errorApiCheck'));
    } finally {
      setLoading(false);
    }
  }, [tr]);

  useEffect(() => {
    void refreshAIState();
    const t = setInterval(() => void refreshAIState(), POLL_INTERVAL_MS);
    return () => clearInterval(t);
  }, [refreshAIState]);

  if (loading && !status) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{tr('aiTrainingPage.title')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-32 w-full" />
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      {/* 헤더 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-baseline justify-between gap-3">
            <span>{tr('aiTrainingPage.title')}</span>
            <span className="text-xs text-gray-500 font-normal">
              {tr('aiTrainingPage.subtitle')}
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          {error && (
            <div className="mb-3 px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
              {error}
            </div>
          )}
          <StatusSection status={status} cameraOK />
        </CardContent>
      </Card>

      {/* 탭 ① 처음 알려주기 / ② 정답으로 가르치기 */}
      <Card>
        <CardContent className="space-y-3 pt-5">
          <div className="flex items-center gap-2 border-b border-gray-700/50">
            <button
              onClick={() => setActiveTab('bootstrap')}
              className={
                'px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ' +
                (activeTab === 'bootstrap'
                  ? 'border-green-400 text-green-300'
                  : 'border-transparent text-gray-400 hover:text-gray-200')
              }
            >
              {tr('aiTrainingPage.tabBootstrap')}
            </button>
            <button
              onClick={() => setActiveTab('dispute')}
              className={
                'px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ' +
                (activeTab === 'dispute'
                  ? 'border-green-400 text-green-300'
                  : 'border-transparent text-gray-400 hover:text-gray-200')
              }
            >
              {tr('aiTrainingPage.tabDispute')}
            </button>
          </div>
          {activeTab === 'bootstrap' && (
            <BootstrapTab status={status} onSaved={refreshAIState} />
          )}
          {activeTab === 'dispute' && (
            <DisputeTab status={status} onSubmitted={refreshAIState} />
          )}
        </CardContent>
      </Card>

      {/* 가르치기 카드 */}
      <TrainingControlCard status={status} onChanged={refreshAIState} />

      {/* 시험 결과 (학습 완료 후만 노출) */}
      {status?.current_job?.status === 'completed' && (
        <ResultSignalsCard
          job={status.current_job}
          gate={algorithm?.validation ?? {}}
        />
      )}

      {/* 현장에 적용 (학습 완료 후만 노출) */}
      {status?.current_job?.status === 'completed' && (
        <PromoteCard status={status} onChanged={refreshAIState} />
      )}

      {/* Cage/Tank baseline 학습 */}
      <TankBaselineCard status={status} onChanged={refreshAIState} />

      {/* Cage/Tank 단기 수질 예측 학습 */}
      <TankForecastCard status={status} onChanged={refreshAIState} />

      {/* R16 — 디스크 사용량 위젯 */}
      <DiskUsageCard />

      {/* 안전 안내 */}
      <SafetyInfoCard />
    </div>
  );
}
