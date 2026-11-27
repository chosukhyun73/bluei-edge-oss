// 급이기 컨트롤러 능력(capability) 판별.
// onoff_feeder 경연 API 급이기는 on/off(run/stop)만 지원 — 배출량은 급이 시간(초)×배출률(9.4g/s)로 결정.
// ESP32 직접제어 급이기는 살포모터(rpm)·공급모터(amount)·정밀 펄스까지 제어.
// 급이 사이클 UI/실행이 이 능력에 따라 달라져야 한다(컨트롤러 종류별 분기).

import type { Controller, Tank } from './types';

export type FeederCapability =
  | 'onoff'  // onoff_feeder-api: run/stop only. rpm/amount 무의미.
  | 'motor'; // esp32: 살포 rpm + 공급 amount + 정밀 펄스.

// 배출률(g/s) — onoff_feeder on/off 급이기의 시간→그램 환산 기본값(백엔드 feeder_rate_g_per_sec 와 동일 default).
export const FEEDER_RATE_G_PER_SEC = 9.4;

// controllerFeederCapability — 컨트롤러 1개의 급이 능력.
// onoff_feeder 판정: control_backend=onoff_feeder | firmware=onoff_feeder-api | feeder_ext_id 존재.
export function controllerFeederCapability(ctrl: Controller | undefined | null): FeederCapability {
  if (!ctrl) return 'motor'; // 미상 → 기존(ESP32) 동작 유지
  const backend = (ctrl.metadata?.control_backend as string | undefined) ?? '';
  const hasExt = ctrl.metadata?.feeder_ext_id != null;
  if (backend === 'onoff_feeder' || ctrl.firmware_version === 'onoff_feeder-api' || hasExt) {
    return 'onoff';
  }
  return 'motor';
}

// feederControllerForTank — 수조에 바인딩된 급이기 컨트롤러를 찾는다(급이기만: feeder_ext_id 또는 feeder actuator).
// backend feed_cycle.feederControllerForTank 와 동일 취지.
export function feederControllerForTank(
  controllers: Controller[],
  tank: Pick<Tank, 'tank_id'>,
): Controller | undefined {
  return controllers.find(
    c => c.tank_id === tank.tank_id && c.metadata?.feeder_ext_id != null,
  ) ?? controllers.find(c => c.tank_id === tank.tank_id);
}
