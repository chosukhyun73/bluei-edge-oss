package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bluei.kr/edge/internal/config"
)

// device-auth (폰 승인) 로컬 클라이언트 — 대시보드 DeviceLoginScreen 이 호출하는
// 로컬 엔드포인트. 클라우드 /device/auth/* 로 프록시하고, approved 시 발급된 device
// 토큰을 디스크에 영속한다. 인증 없음(로그인 부트스트랩, /v1/pair 와 동일 성격).

// cloudBase: 설정된 sync endpoint(예 https://api.bluei.kr/gx10/sync/batches)에서
// origin(scheme://host)만 추출. device-auth 는 같은 origin 의 /device/auth/* 에 있다.
func (s *Server) cloudBase() string {
	if s.cfg.Sync.Endpoint != nil && *s.cfg.Sync.Endpoint != "" {
		if u, err := url.Parse(*s.cfg.Sync.Endpoint); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return "https://api.bluei.kr"
}

// cloudJSON: 클라우드에 JSON 요청을 보내고 응답 본문을 map 으로 파싱.
// (sync/transport.go 의 http 패턴 참고.) headers 로 인증(Bearer)·노드 헤더를 실는다.
func cloudJSON(ctx context.Context, method, urlStr string, body any, headers map[string]string) (map[string]any, int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	// 90s — Render 무료 콜드스타트(~50s) 대비. startup sync가 첫 요청에서 타임아웃 안 나게.
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, urlStr, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, resp.StatusCode, nil
}

// handleDeviceLoginStart — POST /v1/device/login/start.
// 대시보드 [로그인] 버튼. 클라우드에 device-auth 요청을 프록시하고 매칭번호(user_code)와
// 폴링용 device_code 를 반환한다. device_code 는 대시보드가 보관해 status 폴링에 되돌려준다.
func (s *Server) handleDeviceLoginStart(w http.ResponseWriter, r *http.Request) {
	deviceName := s.cfg.Site.Name
	if deviceName == "" {
		deviceName = s.cfg.Edge.EdgeID
	}
	reqBody := map[string]any{
		"node_code":   s.cfg.Sync.NodeCode,
		"device_name": deviceName,
	}
	out, code, err := cloudJSON(r.Context(), http.MethodPost, s.cloudBase()+"/device/auth/request", reqBody, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "CLOUD_UNREACHABLE", "device-auth request failed: "+err.Error(), "")
		return
	}
	if code != http.StatusOK {
		writeError(w, http.StatusBadGateway, "CLOUD_ERROR", fmt.Sprintf("cloud responded %d", code), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_code":   out["user_code"],
		"device_code": out["device_code"],
		"interval":    out["interval"],
		"expires_in":  out["expires_in"],
	})
}

// handleDeviceLoginStatus — GET /v1/device/login/status?device_code=.
// 클라우드 poll 프록시. approved 면 발급된 device 토큰(node_code/access_token/endpoint)을
// 디스크에 영속한 뒤 status 를 반환한다.
func (s *Server) handleDeviceLoginStatus(w http.ResponseWriter, r *http.Request) {
	deviceCode := strings.TrimSpace(r.URL.Query().Get("device_code"))
	if deviceCode == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "device_code required", "")
		return
	}
	out, code, err := cloudJSON(r.Context(), http.MethodGet,
		s.cloudBase()+"/device/auth/poll?device_code="+url.QueryEscape(deviceCode), nil, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "CLOUD_UNREACHABLE", "device-auth poll failed: "+err.Error(), "")
		return
	}
	// unknown device_code(404) → 대시보드엔 만료로 취급(폴링 종료).
	if code == http.StatusNotFound {
		writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
		return
	}
	if code != http.StatusOK {
		writeError(w, http.StatusBadGateway, "CLOUD_ERROR", fmt.Sprintf("cloud responded %d", code), "")
		return
	}
	status := asString(out["status"])
	if status == "approved" {
		creds := config.DeviceCreds{
			NodeCode:    asString(out["node_code"]),
			AccessToken: asString(out["access_token"]),
			Endpoint:    asString(out["endpoint"]),
		}
		if creds.AccessToken != "" {
			if err := config.SaveDeviceCreds(s.cfg.Edge.DataDir, creds); err != nil {
				slog.Error("persist device creds failed", "err", err)
			} else {
				slog.Info("device-auth approved — device token persisted", "node_code", creds.NodeCode)
				// 승인 직후 정체성(farm + 수조/그룹)을 클라우드에 동기화.
				s.syncIdentityToCloud(r.Context(), creds)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

// selfRegisterFarm: device-auth 승인 직후 GX10의 site 정보를 클라우드 /gx10/self-register
// 로 올려 owner의 landBased farm을 자동 생성(멱등)한다. 이게 있어야 앱 landBased 홈에
// 어장이 뜬다. 실패해도 status 응답엔 영향 없음(로그만 — 다음 승인/재시도에서 멱등 복구).
func (s *Server) selfRegisterFarm(ctx context.Context, creds config.DeviceCreds) {
	id := s.readSiteIdentity(ctx)
	body := map[string]any{
		"site_name":     id.SiteName,
		"site_type":     id.SiteType,
		"operator_name": id.Operator,
	}
	if id.LicenseNo != "" {
		body["license_no"] = id.LicenseNo
	}
	if id.Lat != 0 || id.Lng != 0 {
		body["lat"] = id.Lat
		body["lng"] = id.Lng
	}
	headers := map[string]string{"Authorization": "Bearer " + creds.AccessToken}
	if creds.NodeCode != "" {
		headers["X-GX10-Node"] = creds.NodeCode
	}
	out, code, err := cloudJSON(ctx, http.MethodPost, s.cloudBase()+"/gx10/self-register", body, headers)
	if err != nil {
		slog.Error("self-register failed", "err", err)
		return
	}
	if code != http.StatusOK {
		slog.Error("self-register rejected", "status", code, "resp", out)
		return
	}
	slog.Info("self-register ok — landBased farm linked", "idempotent", out["idempotent"])
}

// pushTanks: GX10의 수조/그룹 목록을 클라우드 /gx10/tanks로 올려 tanks를 멱등 생성하고
// gx10_tank_bindings를 자동 설정한다. 그룹은 tanks.meta에 보관. 실패해도 로그만.
func (s *Server) pushTanks(ctx context.Context, creds config.DeviceCreds) {
	tanks, err := s.store.ListTankProfiles(ctx)
	if err != nil {
		slog.Error("push tanks: list profiles failed", "err", err)
		return
	}
	if len(tanks) == 0 {
		return
	}
	groups, _ := s.store.ListGroupProfiles(ctx)
	groupOut := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		gm := map[string]any{
			"group_id": g.GroupID, "name": g.Name, "color": g.Color,
		}
		// 성장단계(hatchery 단계)를 플랫폼에 전달 → tanks.meta.group_stage_role.
		// 앱 landBased 대시보드가 그룹 헤더에 단계 배지로 표시한다.
		if sr, ok := g.Metadata["stage_role"].(string); ok && sr != "" {
			gm["stage_role"] = sr
		}
		groupOut = append(groupOut, gm)
	}
	tankOut := make([]map[string]any, 0, len(tanks))
	for _, t := range tanks {
		tankOut = append(tankOut, map[string]any{
			"gx10_tank_id":    t.TankID,
			"display_name":    t.DisplayName,
			"species":         t.Species,
			"group_id":        t.GroupID,
			"volume_m3":       t.VolumeM3,
			"fish_count":      t.FishCount,
			"avg_weight_g":    t.AvgWeightG,
			"lifecycle_stage": t.LifecycleStage,
		})
	}
	body := map[string]any{
		"site_name": s.readSiteIdentity(ctx).SiteName,
		"groups":    groupOut,
		"tanks":     tankOut,
	}
	headers := map[string]string{"Authorization": "Bearer " + creds.AccessToken}
	if creds.NodeCode != "" {
		headers["X-GX10-Node"] = creds.NodeCode
	}
	out, code, err := cloudJSON(ctx, http.MethodPost, s.cloudBase()+"/gx10/tanks", body, headers)
	if err != nil {
		slog.Error("push tanks failed", "err", err)
		return
	}
	if code != http.StatusOK {
		slog.Error("push tanks rejected", "status", code, "resp", out)
		return
	}
	slog.Info("push tanks ok", "count", out["count"])
}

// pushHatchery: 종묘장 4개 엔티티(어미계군·산란·자어·먹이배양)를 클라우드 /gx10/hatchery 로
// 올려 플랫폼 읽기 미러에 멱등 upsert 한다(B-1). 앱 landBased 가 단계별 상세를 조회한다.
// 엔티티는 그룹별 List 라 그룹을 순회해 수집한다. 실패해도 로그만. 엔티티 0이면 skip.
func (s *Server) pushHatchery(ctx context.Context, creds config.DeviceCreds) {
	groups, err := s.store.ListGroupProfiles(ctx)
	if err != nil || len(groups) == 0 {
		return
	}
	cohorts := []any{}
	spawns := []any{}
	larvals := []any{}
	feeds := []any{}
	for _, g := range groups {
		if v, e := s.store.ListBroodstockByGroup(ctx, g.GroupID); e == nil {
			for _, x := range v {
				cohorts = append(cohorts, x)
			}
		}
		if v, e := s.store.ListSpawnBatchesByGroup(ctx, g.GroupID); e == nil {
			for _, x := range v {
				spawns = append(spawns, x)
			}
		}
		if v, e := s.store.ListLarvalBatchesByGroup(ctx, g.GroupID); e == nil {
			for _, x := range v {
				larvals = append(larvals, x)
			}
		}
		if v, e := s.store.ListLiveFeedByGroup(ctx, g.GroupID); e == nil {
			for _, x := range v {
				feeds = append(feeds, x)
			}
		}
	}
	if len(cohorts)+len(spawns)+len(larvals)+len(feeds) == 0 {
		return
	}
	body := map[string]any{
		"cohorts":            cohorts,
		"spawn_batches":      spawns,
		"larval_batches":     larvals,
		"live_feed_cultures": feeds,
	}
	headers := map[string]string{"Authorization": "Bearer " + creds.AccessToken}
	if creds.NodeCode != "" {
		headers["X-GX10-Node"] = creds.NodeCode
	}
	out, code, err := cloudJSON(ctx, http.MethodPost, s.cloudBase()+"/gx10/hatchery", body, headers)
	if err != nil {
		slog.Error("push hatchery failed", "err", err)
		return
	}
	if code != http.StatusOK {
		slog.Error("push hatchery rejected", "status", code, "resp", out)
		return
	}
	slog.Info("push hatchery ok", "counts", out["counts"])
}

// syncIdentityToCloud: farm self-register + 수조/그룹 push + 종묘장 엔티티 push.
// device-auth 승인 직후와 엣지 startup(device 세션 보유 시)에 호출한다. 모두 멱등.
func (s *Server) syncIdentityToCloud(ctx context.Context, creds config.DeviceCreds) {
	s.selfRegisterFarm(ctx, creds)
	s.pushTanks(ctx, creds)
	s.pushHatchery(ctx, creds)
}

// SyncIdentityOnStartup: device 세션이 있으면 정체성(farm/수조)을 클라우드에 동기화.
// 엣지 재시작만으로 기존 기기의 farm/수조가 클라우드에 반영·갱신된다.
func (s *Server) SyncIdentityOnStartup() {
	creds, err := config.LoadDeviceCreds(s.cfg.Edge.DataDir)
	if err != nil || creds.AccessToken == "" {
		return
	}
	s.syncIdentityToCloud(context.Background(), creds)
}

// handleDeviceSession — GET /v1/device/session.
// device 토큰 보유 여부. 대시보드 로그인 게이트가 부팅 시 호출.
func (s *Server) handleDeviceSession(w http.ResponseWriter, r *http.Request) {
	c, err := config.LoadDeviceCreds(s.cfg.Edge.DataDir)
	authed := err == nil && c.AccessToken != ""
	resp := map[string]any{"authenticated": authed}
	if authed && c.UserEmail != "" {
		resp["user_email"] = c.UserEmail
	}
	writeJSON(w, http.StatusOK, resp)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
