// Package environmental_safety implements the C-3w gate.
//
// C-3w: 해상 케이지(marine)에서 풍속/파고/조수 등 기상해양 조건이 임계값을 초과하면
// 급이 사이클을 차단한다.
// 육상 RAS(land)는 환경 조건이 무관하므로 항상 허용.
//
// Source 추상화:
//   - MockSource: 오프라인/테스트용 정적 값 반환 (기본값)
//   - HTTPSource: 실제 기상청/해양수산부 API 연동 (stub, TODO)
package environmental_safety
