package llm

import (
	"context"
	"log/slog"

	"bluei.kr/edge/internal/storage"
)

// GatherIntentContext — storage 에서 LLM 프롬프트 컨텍스트 수집.
//
// 모든 항목은 best-effort: 조회 실패해도 default 값으로 계속 진행.
// (LLM 분석 실패가 운영자 intent 저장을 막아서는 안 된다.)
func GatherIntentContext(ctx context.Context, store storage.Store, tankID, reason string) OperatorIntentContext {
	out := OperatorIntentContext{
		TankID:         tankID,
		OperatorReason: reason,
	}

	// Tank profile (species, fish_count, avg_weight, biomass).
	if tank, err := store.GetTankProfile(ctx, tankID); err == nil && tank != nil {
		out.Species = tank.Species
		out.FishCount = tank.FishCount
		out.AvgWeightG = tank.AvgWeightG
		out.BiomassKg = tank.BiomassKg
		out.Stage = tank.LifecycleStage
	} else if err != nil {
		slog.Debug("llm: tank profile lookup failed", "tank_id", tankID, "error", err)
	}

	// Effective feeding policy.
	if policy, err := store.GetEffectiveFeedingPolicy(ctx, tankID); err == nil && policy != nil {
		out.BsfMode = policy.BsfMode
		out.OperatingMode = policy.OperatingMode
		out.MaxDailyCycles = policy.MaxDailyCycles
	} else if err != nil {
		slog.Debug("llm: feeding policy lookup failed", "tank_id", tankID, "error", err)
	}

	// Latest sensor readings — water temperature & DO.
	out.TempC = latestMetric(ctx, store, tankID, "water_temperature")
	out.DO = latestMetric(ctx, store, tankID, "dissolved_oxygen")
	slog.Info("llm: gather context", "tank_id", tankID, "temp_c", out.TempC, "do", out.DO, "species", out.Species, "stage", out.Stage)

	// Last feed cycle (최근 1건).
	if cycles, err := store.ListRecentFeedCycles(ctx, tankID, 1); err == nil && len(cycles) > 0 {
		c := cycles[0]
		out.LastCycle = c.CycleID
		if c.CompletedAt != nil {
			if c.TerminationReason != "" {
				out.LastCycleStatus = "completed: " + c.TerminationReason
			} else {
				out.LastCycleStatus = "completed"
			}
		} else {
			out.LastCycleStatus = "active"
		}
	}

	return out
}

// latestMetric — 가장 최근 sensor reading 값. 없거나 nil 이면 0 반환.
func latestMetric(ctx context.Context, store storage.Store, tankID, metric string) float64 {
	rs, err := store.LatestSensorReadings(ctx, storage.LatestReadingFilter{
		TankID: tankID,
		Metric: metric,
		Limit:  1,
	})
	if err != nil || len(rs) == 0 || rs[0].Value == nil {
		return 0
	}
	return *rs[0].Value
}
