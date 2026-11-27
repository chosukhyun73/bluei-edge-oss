package storage

import "context"

// SumTankCountAdjustment — 활성 입식(stockingID) 기준 마릿수 증감 합. 현재 마릿수 =
// InitialCount + 이 값. 이벤트가 정본이라 재시작해도 같은 값으로 재계산된다.
//
//	− 폐사            tank.mortality.recorded (dead_count)
//	− 분조로 나간 수   tank.transfer.recorded split, from_stocking_id = 이 입식
//	+ 합치기로 들어온 수 tank.transfer.recorded merge, to_stocking_id = 이 입식, merged_into_existing
//	− 부분 출하        site.harvest.recorded 라인(full_close=false, stocking_id = 이 입식)
//
// 합치기로 빈 수조에 새 입식이 생긴 경우는 InitialCount 에 이미 들어가 있어 더하지 않는다
// (merged_into_existing=false). 부분 출하 라인의 stocking_id 는 2026-11-15 이후 기록에만 있다.
func (s *sqliteStore) SumTankCountAdjustment(ctx context.Context, tankID, stockingID string) (int, error) {
	var adj int
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  -(SELECT COALESCE(SUM(json_extract(payload_json,'$.dead_count')),0) FROM events
		     WHERE event_type='tank.mortality.recorded'
		       AND json_extract(payload_json,'$.tank_id')=?1
		       AND json_extract(payload_json,'$.stocking_id')=?2)
		  -(SELECT COALESCE(SUM(json_extract(payload_json,'$.moved_count')),0) FROM events
		     WHERE event_type='tank.transfer.recorded'
		       AND json_extract(payload_json,'$.transfer_type')='split'
		       AND json_extract(payload_json,'$.from_tank_id')=?1
		       AND json_extract(payload_json,'$.from_stocking_id')=?2)
		  +(SELECT COALESCE(SUM(json_extract(payload_json,'$.moved_count')),0) FROM events
		     WHERE event_type='tank.transfer.recorded'
		       AND json_extract(payload_json,'$.transfer_type')='merge'
		       AND json_extract(payload_json,'$.merged_into_existing')=1
		       AND json_extract(payload_json,'$.to_tank_id')=?1
		       AND json_extract(payload_json,'$.to_stocking_id')=?2)
		  -(SELECT COALESCE(SUM(json_extract(l.value,'$.count')),0)
		     FROM events e, json_each(e.payload_json,'$.lines') l
		     WHERE e.event_type='site.harvest.recorded'
		       AND json_extract(l.value,'$.tank_id')=?1
		       AND json_extract(l.value,'$.stocking_id')=?2
		       AND COALESCE(json_extract(l.value,'$.full_close'),0)=0)`,
		tankID, stockingID).Scan(&adj)
	return adj, err
}
