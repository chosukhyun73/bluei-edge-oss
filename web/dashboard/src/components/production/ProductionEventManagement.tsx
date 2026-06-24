import { useState } from 'react';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../ui/tabs';
import { ProductionEventLog } from './ProductionEventLog';
import { InventoryCategoryPanel } from './InventoryCategoryPanel';
import { useLanguage } from '../../lib/language-context';

// ─────────────────────────────────────────────────────────────────────────────
// ProductionEventManagement — 생산·이벤트 기록 탭의 4 서브탭 래퍼
//   1. 생산이벤트기록 — 기존 ProductionEventLog (급이/투약/폐사/이동 + CTE)
//   2. 사료 — 구매(입고) + 재고 현황
//   3. 약품 — 구매(입고) + 재고 현황
//   4. 기타 — 구매(입고) + 재고 현황 + 소모 등록
// ─────────────────────────────────────────────────────────────────────────────

export function ProductionEventManagement() {
  const { tr } = useLanguage();
  const [sub, setSub] = useState<'events' | 'feed' | 'drug' | 'material'>('events');

  return (
    <Tabs
      value={sub}
      onValueChange={v => setSub(v as typeof sub)}
    >
      <TabsList>
        <TabsTrigger value="events">{tr('productionEventManagement.tabEvents')}</TabsTrigger>
        <TabsTrigger value="feed">{tr('productionEventManagement.tabFeed')}</TabsTrigger>
        <TabsTrigger value="drug">{tr('productionEventManagement.tabDrug')}</TabsTrigger>
        <TabsTrigger value="material">{tr('productionEventManagement.tabMaterial')}</TabsTrigger>
      </TabsList>

      <TabsContent value="events" className="pt-4">
        <ProductionEventLog />
      </TabsContent>

      <TabsContent value="feed" className="pt-4">
        <InventoryCategoryPanel category="feed" title={tr('productionEventManagement.titleFeed')} />
      </TabsContent>

      <TabsContent value="drug" className="pt-4">
        <InventoryCategoryPanel category="drug" title={tr('productionEventManagement.titleDrug')} />
      </TabsContent>

      <TabsContent value="material" className="pt-4">
        <InventoryCategoryPanel category="material" title={tr('productionEventManagement.titleMaterial')} />
      </TabsContent>
    </Tabs>
  );
}
