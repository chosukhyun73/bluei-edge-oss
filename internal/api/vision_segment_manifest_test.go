package api

import "testing"

func TestManifestSplitDeterministic(t *testing.T) {
	// 같은 cycle_id + val_ratio → 항상 같은 split (재export 안정성).
	c := "cycle_01KWRFXYAESB1ZBMF9TTNEK5SA"
	first := manifestSplit(c, 0.2)
	for i := 0; i < 100; i++ {
		if manifestSplit(c, 0.2) != first {
			t.Fatalf("split not deterministic for %s", c)
		}
	}

	// val_ratio=0 → 전부 train, val_ratio→1 근처 → 전부 val.
	if manifestSplit(c, 0) != "train" {
		t.Errorf("val_ratio=0 should be train")
	}
	if manifestSplit(c, 0.999) != "val" {
		t.Errorf("val_ratio~1 should be val")
	}

	// 대략적 비율 확인 — 다양한 cycle_id 로 val 비중이 ratio 근처인지.
	val := 0
	n := 2000
	for i := 0; i < n; i++ {
		id := "cycle_test_" + string(rune('A'+i%26)) + string(rune('0'+i%10)) + string(rune(i))
		if manifestSplit(id, 0.25) == "val" {
			val++
		}
	}
	frac := float64(val) / float64(n)
	if frac < 0.15 || frac > 0.35 { // 0.25 ± 여유
		t.Errorf("val fraction %.2f far from 0.25", frac)
	}
}
