import { useState, useEffect, useCallback } from 'react';
import { Tanks } from '../lib/api';
import type { Tank, StateVector } from '../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from './ui/card';
import { CameraGrid, type CameraGridColumns } from './CameraGrid';
import { SensorMatrix } from './SensorMatrix';
import { EquipmentGrid } from './EquipmentGrid';
import { useLanguage } from '../lib/language-context';

interface Props {
  tanks: Tank[];
  onSelectTank: (tankId: string) => void;
}

// 센서 + 장비 폴링을 이 컴포넌트에서 호이스팅 — SensorMatrix, EquipmentGrid 모두 동일한 Map 공유.
// 알림 표시는 상단 헤더의 GlobalAlerts (단일 source) 가 담당 — 그룹 컨텍스트 폴링 X.
const CAMERA_COLUMNS_KEY = 'bluei.cameraColumns';

function CameraColumnsToggle({
  value, onChange,
}: { value: CameraGridColumns; onChange: (v: CameraGridColumns) => void }) {
  const { tr } = useLanguage();
  const options: CameraGridColumns[] = [1, 2, 4];
  return (
    <div className="flex items-center gap-1 text-xs">
      <span className="text-gray-500 mr-1">{tr('groupOverview.cameraColumnsPrefix')}</span>
      {options.map(n => (
        <button
          key={n}
          onClick={() => onChange(n)}
          className={
            'px-2 py-0.5 rounded font-mono border transition-colors ' +
            (value === n
              ? 'bg-green-600 border-green-500 text-white'
              : 'bg-gray-800 border-gray-700 text-gray-400 hover:text-white hover:border-gray-500')
          }
          title={tr('groupOverview.cameraColumnsTitle', { n })}
        >
          {n}
        </button>
      ))}
      <span className="text-gray-500 ml-1">{tr('groupOverview.cameraColumnsSuffix')}</span>
    </div>
  );
}

export function GroupOverview({ tanks }: Props) {
  const { tr } = useLanguage();
  const [stateVectors, setStateVectors] = useState<Map<string, StateVector>>(new Map());
  const [svLoading, setSvLoading] = useState(true);
  const [lastFetchedAt, setLastFetchedAt] = useState<Date | null>(null);
  // 카메라 컬럼 수 — localStorage persist (default 2).
  const [cameraColumns, setCameraColumnsRaw] = useState<CameraGridColumns>(() => {
    try {
      const v = parseInt(localStorage.getItem(CAMERA_COLUMNS_KEY) ?? '2', 10);
      if (v === 1 || v === 2 || v === 4) return v;
    } catch { /* ignore */ }
    return 2;
  });
  const setCameraColumns = useCallback((v: CameraGridColumns) => {
    setCameraColumnsRaw(v);
    try { localStorage.setItem(CAMERA_COLUMNS_KEY, String(v)); } catch { /* ignore */ }
  }, []);

  const tankIds = tanks.map(t => t.tank_id);

  // 상태벡터 일괄 폴링
  // Phase A.1 — 4 tank 동시 호출 시 backend SQLite read 경합으로 timeout 발생.
  // Promise.all 대신 250ms 간격 직렬 호출 (개별 실패는 skip, 나머지 진행).
  const fetchStateVectors = useCallback(async () => {
    if (tankIds.length === 0) {
      setSvLoading(false);
      return;
    }
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
      setStateVectors(m);
      setLastFetchedAt(new Date());
    } catch {
      // 네트워크 오류 무시 — 이전 데이터 유지
    } finally {
      setSvLoading(false);
    }
  // tanks 배열 자체 변경 감지를 위해 JSON 직렬화 의존
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(tankIds)]);

  // 5초 센서 폴링
  useEffect(() => {
    void fetchStateVectors();
    const id = setInterval(() => void fetchStateVectors(), 5_000);
    return () => clearInterval(id);
  }, [fetchStateVectors]);

  return (
    <div className="space-y-5 pt-1">
      {/* 카메라 LIVE 섹션 */}
      <Card>
        <CardHeader className="border-b border-gray-700/30">
          <CardTitle className="flex items-center justify-between gap-2 text-sm font-medium text-white">
            <div className="flex items-center gap-2">
              <span>📹</span>
              <span>{tr('groupOverview.cameraTitle')}</span>
              <span className="text-xs text-gray-500 font-normal">LIVE</span>
            </div>
            {/* 컬럼 수 토글 (1/2/4). localStorage 에 persist. */}
            <CameraColumnsToggle value={cameraColumns} onChange={setCameraColumns} />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-4">
          <CameraGrid tanks={tanks} columns={cameraColumns} />
        </CardContent>
      </Card>

      {/* 센서 매트릭스 */}
      <Card>
        <CardHeader className="border-b border-gray-700/30">
          <CardTitle className="flex items-center gap-2 text-sm font-medium text-white">
            <span>💧</span>
            <span>{tr('groupOverview.sensorMatrixTitle')}</span>
            <span className="text-xs text-gray-500 font-normal">{tr('groupOverview.sensorMatrixSubtitle')}</span>
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-4">
          <SensorMatrix
            tanks={tanks}
            stateVectors={stateVectors}
            lastFetchedAt={lastFetchedAt}
            loading={svLoading}
          />
        </CardContent>
      </Card>

      {/* 장비 상태 그리드 */}
      <Card>
        <CardHeader className="border-b border-gray-700/30">
          <CardTitle className="flex items-center gap-2 text-sm font-medium text-white">
            <span>🔧</span>
            <span>{tr('groupOverview.equipmentStatusTitle')}</span>
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-4">
          <EquipmentGrid tanks={tanks} stateVectors={stateVectors} />
        </CardContent>
      </Card>
    </div>
  );
}
