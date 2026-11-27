package storage

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"
)

// WAL 정리 워커 (2026-10-01).
//
// 왜 필요한가: SQLite 는 읽기 트랜잭션이 하나도 없는 순간에만 WAL 을 본체로 합친다.
// 대시보드가 5초마다 4수조 state-vector 를 조회하면서 그 순간이 12일간 없었고,
// WAL 이 3.2GB 까지 자랐다. 그 상태에서 순수 Go sqlite 드라이버는 같은 조회가
// 명령행 대비 400배 느려진다(실측 4ms → 1,687ms). 결과는 CPU 포화 · 스케줄러 정지 ·
// 대시보드 "백엔드 연결 실패" 였다.
//
// 자동 checkpoint(기본 1000페이지)는 읽기가 끊이지 않으면 굶는다. 그래서 주기적으로
// 직접 시도한다. PASSIVE 는 읽기와 공존하지만 밀린 만큼만 처리하고, TRUNCATE 는
// 파일까지 잘라내되 읽기가 있으면 실패한다 — 실패는 정상이고 다음 주기에 재시도한다.
const (
	walPassiveInterval  = 10 * time.Minute
	walTruncateInterval = 1 * time.Hour
	walWarnBytes        = 200 << 20 // 200MB — 이 이상이면 정리가 밀리고 있다는 신호
	walCriticalBytes    = 1 << 30   // 1GB — 성능 저하가 체감되는 수준
)

// StartWALMaintenance 는 주기적 checkpoint 워커를 띄운다. ctx 취소 시 종료.
func StartWALMaintenance(ctx context.Context, db *sql.DB, dbPath string, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	go func() {
		passive := time.NewTicker(walPassiveInterval)
		truncate := time.NewTicker(walTruncateInterval)
		defer passive.Stop()
		defer truncate.Stop()
		log.Info("WAL 정리 워커 시작",
			"passive_interval", walPassiveInterval.String(),
			"truncate_interval", walTruncateInterval.String())
		for {
			select {
			case <-ctx.Done():
				return
			case <-passive.C:
				walCheckpoint(ctx, db, dbPath, "PASSIVE", log)
			case <-truncate.C:
				walCheckpoint(ctx, db, dbPath, "TRUNCATE", log)
			}
		}
	}()
}

// walCheckpoint 는 checkpoint 1회 + 크기 점검. busy 는 오류가 아니라 정상(읽기 중).
func walCheckpoint(ctx context.Context, db *sql.DB, dbPath, mode string, log *slog.Logger) {
	before := walSizeBytes(dbPath)

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var busy, logPages, checkpointed int
	err := db.QueryRowContext(cctx, "PRAGMA wal_checkpoint("+mode+")").
		Scan(&busy, &logPages, &checkpointed)
	after := walSizeBytes(dbPath)

	switch {
	case err != nil:
		log.Warn("WAL checkpoint 실패", "mode", mode, "error", err, "wal_mb", after>>20)
	case busy == 1:
		// 읽기 트랜잭션에 막힘 — 다음 주기 재시도. TRUNCATE 는 흔하게 막힌다.
		log.Info("WAL checkpoint 보류 (읽기 중)", "mode", mode, "wal_mb", after>>20)
	default:
		log.Info("WAL checkpoint 완료", "mode", mode,
			"pages", logPages, "checkpointed", checkpointed,
			"wal_mb_before", before>>20, "wal_mb_after", after>>20)
	}

	// 크기 경보 — 12일간 아무도 모르던 상황을 막는다.
	switch {
	case after >= walCriticalBytes:
		log.Error("WAL 이 1GB 를 넘었다 — 조회 지연·CPU 포화로 이어진다. 읽기 부하를 줄이거나 재기동 필요",
			"wal_mb", after>>20)
	case after >= walWarnBytes:
		log.Warn("WAL 이 커지고 있다 — 정리가 읽기에 계속 막히는 중", "wal_mb", after>>20)
	}
}

func walSizeBytes(dbPath string) int64 {
	fi, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return 0
	}
	return fi.Size()
}
