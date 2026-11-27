package compliance

import "testing"

func TestProfilesLoad(t *testing.T) {
	ps, err := ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 7 {
		t.Fatalf("프로파일 7종이어야 함: %d", len(ps))
	}
	for _, p := range ps {
		if p.ID == "" || p.Name == "" || p.Version == "" || p.CheckedAt == "" || len(p.Items) == 0 {
			t.Errorf("%s: 이름·판·확인 일자·항목이 있어야 함", p.ID)
		}
		seen := map[string]bool{}
		for _, it := range p.Items {
			if it.ID == "" || it.Title == "" || seen[it.ID] {
				t.Errorf("%s: 항목 id·제목 필수, id 중복 금지: %q", p.ID, it.ID)
			}
			seen[it.ID] = true
		}
	}
}

func statusOf(t *testing.T, id, item string, st State) string {
	t.Helper()
	ps, _ := ListProfiles()
	for _, p := range ps {
		if p.ID != id {
			continue
		}
		for _, r := range Evaluate(p, st) {
			if r.ID == item {
				return r.Status
			}
		}
	}
	t.Fatalf("no item %s/%s", id, item)
	return ""
}

func TestEvaluate(t *testing.T) {
	empty := State{}
	full := State{LicenseComplete: true, HumanWelfareSet: true, RetentionYears: 3, HasReviewer: true,
		PublishedPlans: 1, AllCCPsAutoRecorded: true, ValidOwnCerts: map[string]bool{"asc_coc": true, "iso22000": true}}
	cases := []struct {
		profile, item string
		st            State
		want          string
	}{
		{"gdst", "license", empty, StatusUnmet},
		{"gdst", "license", full, StatusMet},
		{"gdst", "events", full, StatusLater},
		{"gdst", "food_safety", full, StatusManual},
		{"kr_haccp", "plan", empty, StatusUnmet},
		{"kr_haccp", "smart", full, StatusMet},
		{"kr_haccp", "retention", State{RetentionYears: 1}, StatusUnmet},
		{"msc_coc", "retention", State{RetentionYears: 2}, StatusUnmet},
		{"msc_coc", "cert", full, StatusUnmet}, // ASC 인증서는 MSC 항목을 채우지 않는다
		{"asc_coc", "cert", full, StatusMet},
		{"asc_coc", "food_safety", full, StatusMet},
		// 한국 HACCP 인증서만으로는 ASC 식품안전 항목이 채워지지 않는다
		{"asc_coc", "food_safety", State{HACCPCertNo: "H-1", ValidOwnCerts: map[string]bool{"haccp_kr": true}}, StatusUnmet},
	}
	for _, c := range cases {
		if got := statusOf(t, c.profile, c.item, c.st); got != c.want {
			t.Errorf("%s/%s = %s, want %s", c.profile, c.item, got, c.want)
		}
	}
}
