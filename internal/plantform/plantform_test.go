package plantform

import "testing"

// 기본 양식은 모두 틀 검사를 통과해야 하고, 묶음 안에서 id 가 겹치지 않아야 한다.
func TestDefaultPacksValid(t *testing.T) {
	packs, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatal("기본 양식 묶음이 없다")
	}
	for _, p := range packs {
		seen := map[string]bool{}
		for i := range p.Forms {
			f := &p.Forms[i]
			if seen[f.ID] {
				t.Errorf("%s: 양식 id %q 가 겹친다", p.ID, f.ID)
			}
			seen[f.ID] = true
			for _, issue := range Validate(f) {
				t.Errorf("%s/%s: %s", p.ID, f.ID, issue)
			}
		}
	}
}

// 일일 점검표의 구역별 문항 수는 실제 사례(작업 전 6, 중 4, 후 5, 입고 1, 출하 1)와 같다.
func TestKRSmallDailyChecklistShape(t *testing.T) {
	p, err := LoadPack("kr-small")
	if err != nil || p == nil {
		t.Fatalf("kr-small: %v", err)
	}
	want := []int{6, 4, 5, 1, 1}
	for _, f := range p.Forms {
		if f.ID != "daily_hygiene" {
			continue
		}
		if len(f.Sections) != len(want) {
			t.Fatalf("구역 %d개, 기대 %d개", len(f.Sections), len(want))
		}
		for i, s := range f.Sections {
			if len(s.Questions) != want[i] {
				t.Errorf("구역 %s: 문항 %d개, 기대 %d개", s.ID, len(s.Questions), want[i])
			}
		}
		return
	}
	t.Fatal("daily_hygiene 이 없다")
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]Form{
		"kind":      {ID: "a", Name: "a", Kind: "table", Signers: []string{"x"}},
		"no signer": {ID: "a", Name: "a", Kind: KindLog, Columns: []Column{{ID: "c", Name: "c"}}},
		"dup id": {ID: "a", Name: "a", Kind: KindLog, Signers: []string{"x"},
			Columns: []Column{{ID: "c", Name: "c"}, {ID: "c", Name: "d"}}},
		"cover": {ID: "a", Name: "a", Kind: KindLog, Signers: []string{"x"}, Covers: []string{"P18"},
			Columns: []Column{{ID: "c", Name: "c"}}},
		"empty checklist": {ID: "a", Name: "a", Kind: KindChecklist, Signers: []string{"x"}},
	}
	for name, f := range cases {
		if len(Validate(&f)) == 0 {
			t.Errorf("%s: 걸러야 하는데 통과했다", name)
		}
	}
}
