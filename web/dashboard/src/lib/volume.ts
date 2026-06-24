// volume.ts — 수조 형태/치수 → 용적 자동 계산 헬퍼.
//
// 사용처: TankSettingsSection (편집 모드), AdminRegistry TankCard, GroupTankComparison TankAddForm.
// 운영자가 form_factor / 치수 입력 시 즉시 m³ 단위 용적을 산출. 부분 입력은 null.

import { t } from './i18n';

export type FormFactor = '' | 'round' | 'square' | 'rectangular';

export interface VolumeInputs {
  formFactor: FormFactor;
  diameterM?: number;
  lengthM?: number;
  widthM?: number;
  depthM?: number;
}

// computeVolumeM3 — null 이면 입력 부족.
// round:       π × (diameter/2)² × depth
// square:      length² × depth
// rectangular: length × width × depth
// 결과는 소수 2 자리로 반올림 + Number(toFixed) 정규화.
export function computeVolumeM3(inputs: VolumeInputs): number | null {
  const { formFactor, diameterM, lengthM, widthM, depthM } = inputs;
  if (!formFactor || depthM == null || depthM <= 0) return null;
  let raw: number | null = null;
  if (formFactor === 'round') {
    if (diameterM == null || diameterM <= 0) return null;
    const r = diameterM / 2;
    raw = Math.PI * r * r * depthM;
  } else if (formFactor === 'square') {
    if (lengthM == null || lengthM <= 0) return null;
    raw = lengthM * lengthM * depthM;
  } else if (formFactor === 'rectangular') {
    if (lengthM == null || lengthM <= 0) return null;
    if (widthM == null || widthM <= 0) return null;
    raw = lengthM * widthM * depthM;
  }
  if (raw == null) return null;
  return Number(raw.toFixed(2));
}

// computeVolumeFromStrings — UI 의 string 입력을 직접 받아 처리하는 편의 함수.
export function computeVolumeFromStrings(
  formFactor: FormFactor,
  diameter: string,
  length: string,
  width: string,
  depth: string,
): number | null {
  const parseNum = (s: string): number | undefined => {
    const t = s.trim();
    if (t === '') return undefined;
    const n = Number(t);
    return Number.isFinite(n) ? n : undefined;
  };
  return computeVolumeM3({
    formFactor,
    diameterM: parseNum(diameter),
    lengthM: parseNum(length),
    widthM: parseNum(width),
    depthM: parseNum(depth),
  });
}

// volumeHint — 사용자 hint 텍스트 ("자동: 31.83 m³ (원형, ⌀4.5m × 2m)" 형식).
// 입력 부족 시 null → caller 가 hint 숨김.
export function volumeHint(inputs: VolumeInputs): string | null {
  const v = computeVolumeM3(inputs);
  if (v == null) return null;
  const ff = inputs.formFactor;
  let dims = '';
  if (ff === 'round') {
    dims = `⌀${inputs.diameterM}m × ${inputs.depthM}m`;
  } else if (ff === 'square') {
    dims = `${inputs.lengthM}m × ${inputs.lengthM}m × ${inputs.depthM}m`;
  } else if (ff === 'rectangular') {
    dims = `${inputs.lengthM}m × ${inputs.widthM}m × ${inputs.depthM}m`;
  }
  const label = ff === 'round' ? t('volume.formRound')
    : ff === 'square' ? t('volume.formSquare')
    : t('volume.formRectangular');
  return t('volume.autoHint', { volume: v, label, dims });
}
