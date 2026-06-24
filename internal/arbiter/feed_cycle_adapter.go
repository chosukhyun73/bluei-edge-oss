package arbiter

// Phase 1c — feed_cycle.AIScheduler 는 import cycle 회피를 위해 좁은 인터페이스
// (feed_cycle.CycleArbiter) 만 의존한다. 이 파일은 *Arbiter 를 그 인터페이스에
// 맞추는 얇은 adapter 를 제공.
//
// 사용:
//
//	import "bluei.kr/edge/internal/arbiter"
//	import "bluei.kr/edge/internal/feed_cycle"
//
//	arb := arbiter.New(fcWorker, store)
//	ai := feed_cycle.NewAIScheduler(store, arbiter.NewFeedCycleAdapter(arb))

import (
	"context"

	"bluei.kr/edge/internal/feed_cycle"
)

// FeedCycleAdapter wraps *Arbiter to satisfy feed_cycle.CycleArbiter.
type FeedCycleAdapter struct {
	arb *Arbiter
}

// NewFeedCycleAdapter returns an adapter that lets feed_cycle.AIScheduler call
// arbiter.Submit without importing this package.
func NewFeedCycleAdapter(a *Arbiter) *FeedCycleAdapter { return &FeedCycleAdapter{arb: a} }

// Submit converts the feed_cycle-side request to the arbiter type and forwards.
func (f *FeedCycleAdapter) Submit(ctx context.Context, req feed_cycle.ArbiterCycleRequest) (feed_cycle.ArbiterDecision, error) {
	src := Source(req.Source)
	dec, err := f.arb.Submit(ctx, CycleRequest{
		TankID:       req.TankID,
		ControllerID: req.ControllerID,
		Source:       src,
		Mode:         req.Mode,
		Params:       req.Params,
		SubmittedAt:  req.SubmittedAt,
		IntentID:     req.IntentID,
	})
	if err != nil {
		return feed_cycle.ArbiterDecision{}, err
	}
	return feed_cycle.ArbiterDecision{
		Accepted:         dec.Accepted,
		RejectionReason:  dec.RejectionReason,
		ExistingCycleID:  dec.ExistingCycleID,
		ResultingCycleID: dec.ResultingCycleID,
		DecisionID:       dec.DecisionID,
		PreemptedCycleID: dec.PreemptedCycleID,
	}, nil
}
