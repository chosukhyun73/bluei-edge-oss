package api

import (
	"net/http"
	"sync"
	"time"
)

// state-vector 결과 캐시 (2026-10-01).
//
// 왜: 대시보드가 5초마다 4수조를 조회한다(분당 48회). 한 건을 만드는 데 DB 조회가
// 수십 번 들어가는데, 10-01 에 WAL 비대로 한 건이 4.4초까지 늘어나자 요청이 서로
// 겹쳐 CPU 가 포화됐고 백엔드 전체가 멎었다(스케줄러 정지 · 대시보드 연결 실패).
//
// 이 값은 수 초 단위로 급변하는 지표가 아니다(급이 사이클·수질·신뢰도). 30초 캐시면
// 화면 체감은 그대로면서 부하는 1/6 로 떨어진다. 같은 수조에 동시 요청이 몰려도
// 한 번만 계산하도록 수조별 잠금을 건다 — 느려졌을 때 겹침이 문제의 핵심이었다.
const stateVectorTTL = 30 * time.Second

type stateVectorEntry struct {
	mu   sync.Mutex // 같은 수조 동시 요청 직렬화 (중복 계산 방지)
	val  *TankStateVector
	at   time.Time
	errS string
}

type stateVectorCache struct {
	mu sync.Mutex
	m  map[string]*stateVectorEntry
}

func newStateVectorCache() *stateVectorCache {
	return &stateVectorCache{m: map[string]*stateVectorEntry{}}
}

func (c *stateVectorCache) entry(tankID string) *stateVectorEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[tankID]
	if !ok {
		e = &stateVectorEntry{}
		c.m[tankID] = e
	}
	return e
}

// buildTankStateVectorCached — TTL 안이면 캐시, 아니면 1회만 계산해 공유한다.
func (s *Server) buildTankStateVectorCached(r *http.Request, tankID string) (*TankStateVector, error) {
	if s.svCache == nil {
		return s.buildTankStateVector(r, tankID)
	}
	e := s.svCache.entry(tankID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.val != nil && time.Since(e.at) < stateVectorTTL {
		return e.val, nil
	}
	v, err := s.buildTankStateVector(r, tankID)
	if err != nil {
		return nil, err
	}
	e.val, e.at = v, time.Now()
	return v, nil
}
