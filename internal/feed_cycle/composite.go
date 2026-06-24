package feed_cycle

import "fmt"

// CompositeGate checks multiple SafetyGates in order.
// Returns block=true as soon as any gate blocks — short-circuit evaluation.
// 이름이 있는 gate 목록을 순서대로 확인한다. 첫 번째 차단 gate 의 이름과 사유를 반환.
type CompositeGate struct {
	gates []namedGate
}

type namedGate struct {
	name string
	gate SafetyGate
}

// NewCompositeGate creates a CompositeGate from name→gate pairs.
// gates slice: [name0, gate0, name1, gate1, ...]
// name은 로그/reason 식별용 (예: "C-3p", "C-3l", "C-3w").
func NewCompositeGate(pairs ...any) *CompositeGate {
	cg := &CompositeGate{}
	for i := 0; i+1 < len(pairs); i += 2 {
		name, _ := pairs[i].(string)
		gate, _ := pairs[i+1].(SafetyGate)
		if gate != nil {
			cg.gates = append(cg.gates, namedGate{name: name, gate: gate})
		}
	}
	return cg
}

// Check implements SafetyGate. Blocks if any contained gate blocks.
func (c *CompositeGate) Check(tankID string) (bool, string) {
	for _, ng := range c.gates {
		if blocked, reason := ng.gate.Check(tankID); blocked {
			return true, fmt.Sprintf("[%s] %s", ng.name, reason)
		}
	}
	return false, ""
}
