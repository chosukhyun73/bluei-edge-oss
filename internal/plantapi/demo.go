package plantapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

// 예시 공장 — 화면과 흐름을 바로 볼 수 있게, 설정을 마치고 계획을 게시한 공장 하나를 만든다.
// 값은 모두 예시다. 한계기준은 템플릿의 예시값이고 자사 검증을 거친 값이 아니다.

// 예시 공장 계정의 PIN. 화면에 그대로 안내한다(시연용 공장에만 쓰인다).
var demoUsers = []struct{ id, name, role, pin string }{
	{"demo_admin", "김관리(예시)", roleAdmin, "1111"},
	{"demo_reviewer", "이검토(예시)", roleReviewer, "2222"},
	{"demo_monitor", "박담당(예시)", roleMonitor, "3333"},
}

// POST /v1/plants/demo
func (s *Server) handleCreateDemoPlant(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.seedDemoPlant(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DEMO_FAILED", err.Error(), "")
		return
	}
	users := []map[string]any{}
	for _, u := range demoUsers {
		users = append(users, map[string]any{"name": u.name, "role": u.role, "pin": u.pin})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "plant": map[string]any{"site_id": siteID}, "users": users})
}

func (s *Server) seedDemoPlant(ctx context.Context) (string, error) {
	farms, err := s.store.ListFarms(ctx)
	if err != nil {
		return "", err
	}
	farmID := ""
	if len(farms) > 0 {
		farmID = farms[0].FarmID
	} else {
		farmID = common.NewID("farm")
		if err := s.store.UpsertFarm(ctx, &farm.Farm{FarmID: farmID, Operator: "예시 수산"}); err != nil {
			return "", err
		}
	}
	siteID := common.NewID("plant")
	sfx := siteID[len(siteID)-6:] // 구역·센서 id 가 다른 공장과 겹치지 않게
	if err := s.store.UpsertSiteProcessing(ctx, &site.SiteLand{SiteID: siteID, FarmID: farmID,
		Name: "예시 가공공장 (시연용)", Timezone: "Asia/Seoul", Location: site.LandLocation{Address: "부산광역시 (예시 주소)"}}); err != nil {
		return "", err
	}
	if err := s.store.UpsertPlantProfile(ctx, &storage.PlantProfile{SiteID: siteID, BusinessNo: "000-00-00000",
		LicenseName: "수산물가공업 신고(예시)", LicenseNo: "예시-0000", LicenseAuthority: "예시 구청",
		HumanWelfarePolicy: "internal_policy", RetentionYears: 3}); err != nil {
		return "", err
	}
	userID := map[string]string{}
	for _, u := range demoUsers {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return "", err
		}
		hash, err := hashPIN(u.pin, salt)
		if err != nil {
			return "", err
		}
		id := u.id + "_" + sfx
		userID[u.role] = id
		if err := s.store.InsertLocalUser(ctx, &storage.LocalUser{UserID: id, SiteID: siteID, Name: u.name, Role: u.role,
			PinSalt: hex.EncodeToString(salt), PinHash: hash, Active: true}); err != nil {
			return "", err
		}
	}
	zone := func(key, name, hygiene, temp string) (string, error) {
		id := key + "_" + sfx
		return id, s.store.UpsertPlantZone(ctx, &storage.PlantZone{ZoneID: id, SiteID: siteID, Name: name, HygieneClass: hygiene, TempClass: temp})
	}
	zRecv, err := zone("recv", "입고장", "general", "ambient")
	if err != nil {
		return "", err
	}
	zRaw, err := zone("cold_raw", "원료 냉장고", "general", "chilled")
	if err != nil {
		return "", err
	}
	zWork, err := zone("work", "가공실", "clean", "ambient")
	if err != nil {
		return "", err
	}
	zFin, err := zone("cold_fin", "완제품 냉장고", "clean", "chilled")
	if err != nil {
		return "", err
	}
	sensor := func(key, zoneID string) (string, error) {
		id := key + "_" + sfx
		return id, s.store.UpsertPlantSensor(ctx, &storage.PlantSensor{SensorID: id, SiteID: siteID, ZoneID: zoneID,
			Metric: "temperature", Unit: "℃", IsMock: true})
	}
	snRaw, err := sensor("temp_cold_raw", zRaw)
	if err != nil {
		return "", err
	}
	snWork, err := sensor("temp_work", zWork)
	if err != nil {
		return "", err
	}
	snFin, err := sensor("temp_cold_fin", zFin)
	if err != nil {
		return "", err
	}
	if err := s.store.SetPlantComplianceProfiles(ctx, siteID, []string{"kr_haccp", "gdst"}); err != nil {
		return "", err
	}

	// 히스타민 어종 템플릿을 공장 값으로 채워 게시한다.
	tpl, err := haccp.LoadTemplate("fresh_histamine_fish")
	if err != nil || tpl == nil {
		return "", err
	}
	c := tpl.Content
	c.Product.Name = "냉장 방어 로인 (예시)"
	stepZone := map[string]string{"receive": zRecv, "chill_store": zRaw, "cut": zWork, "grade": zWork, "pack": zWork, "finished_store": zFin}
	for i := range c.Steps {
		c.Steps[i].ZoneID = stepZone[c.Steps[i].StepID]
	}
	for i := range c.Hazards {
		c.Hazards[i].Confirmed = true
	}
	limitSensor := map[string]string{"lim_chill_temp": snRaw, "lim_processing_exposure": snWork, "lim_finished_temp": snFin}
	for i := range c.CCPs {
		k := &c.CCPs[i]
		k.MonitorPrimary, k.MonitorBackup = userID[roleMonitor], userID[roleReviewer]
		k.OutageAfterSec = 60
		for j := range k.Limits {
			l := &k.Limits[j]
			l.SensorID = limitSensor[l.LimitID]
			if l.Item == "wait_time" {
				v := 30.0
				l.Max = &v
				l.BasisRef = "예시값(30분). 자사 검증 필요"
			}
			if l.SensorID != "" {
				l.SampleIntervalSec = int(mockInterval.Seconds())
			}
			l.Confirmed = true
		}
	}
	issues := append(haccp.ValidateForPublish(&c), haccp.ValidateRefs(&c, haccp.SiteRefs{
		Zones:     map[string]bool{zRecv: true, zRaw: true, zWork: true, zFin: true},
		Equipment: map[string]bool{}, Sensors: map[string]bool{snRaw: true, snWork: true, snFin: true}})...)
	if len(issues) > 0 {
		b, _ := json.Marshal(issues)
		return "", &demoError{string(b)}
	}
	contentJSON, _ := json.Marshal(c)
	hash, err := haccp.ContentHash(&c)
	if err != nil {
		return "", err
	}
	now := common.NowUTC()
	plan := &storage.HACCPPlan{PlanID: common.NewID("hplan"), SiteID: siteID, ProductGroup: tpl.ProductGroup,
		Name: "냉장 방어 로인 (예시)", CreatedAt: now, UpdatedAt: now}
	v := &storage.HACCPPlanVersion{VersionID: common.NewID("hver"), PlanID: plan.PlanID, VersionNo: 1, Status: haccpStatusDraft,
		TemplateRef: tpl.ID, ContentJSON: string(contentJSON), CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateHACCPPlan(ctx, plan, v); err != nil {
		return "", err
	}
	v.ContentHash, v.OnsiteConfirmedBy, v.OnsiteConfirmedAt = hash, userID[roleReviewer], &now
	v.PublishedBy, v.PublishedAt = userID[roleAdmin], &now
	if _, err := s.store.PublishHACCPPlanVersion(ctx, v); err != nil {
		return "", err
	}
	return siteID, nil
}

type demoError struct{ msg string }

func (e *demoError) Error() string {
	return "예시 계획이 게시 조건을 채우지 못했습니다: " + e.msg
}
