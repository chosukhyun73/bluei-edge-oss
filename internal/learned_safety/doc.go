// Package learned_safety implements the C-3l gate.
//
// C-3l: 운영자 이의제기 + 사고 로그에서 마이닝된 학습 규칙으로 급이 사이클을 차단한다.
// 동일 메트릭/임계값 패턴이 7일 이내 ≥3회 반복되면 규칙으로 등록.
// 안전 기본값: 학습 안전이 비활성화되어 있거나 규칙 없으면 항상 허용.
package learned_safety
