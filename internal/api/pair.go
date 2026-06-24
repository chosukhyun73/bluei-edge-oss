package api

import (
	"context"
	"net/http"
)

// siteIdentity — GX10이 보유한 사업장/farm-site 정체성. edge.db 등록 site/farm을
// 소스 오브 트루스로, 없으면 cfg.Site로 폴백한다. /v1/pair(QR)와 device-auth
// self-register(/gx10/self-register 호출)가 같은 정체성을 쓰도록 공유한다.
type siteIdentity struct {
	SiteName         string
	SiteType         string
	Operator         string
	FarmID           string
	LicenseNo        string
	FisheryLicenseNo string
	Lat              float64
	Lng              float64
}

// readSiteIdentity: edge.db 등록 site/farm을 우선 읽고 cfg.Site로 폴백.
// (DB의 site_type은 land/marine 거친 분류라 양식방식 세분류를 담지 못해 site_type은
// 항상 cfg.Site에서 가져온다.)
func (s *Server) readSiteIdentity(ctx context.Context) siteIdentity {
	id := siteIdentity{
		SiteName: s.cfg.Site.Name,
		SiteType: s.cfg.Site.SiteType,
		Operator: s.cfg.Site.Operator,
		Lat:      s.cfg.Site.Lat,
		Lng:      s.cfg.Site.Lng,
	}
	sites, err := s.store.ListSites(ctx, "")
	if err != nil || len(sites) == 0 {
		return id
	}
	site := sites[0]
	if v, ok := site["name"].(string); ok && v != "" {
		id.SiteName = v
	}
	if v, ok := site["farm_id"].(string); ok {
		id.FarmID = v
	}
	if v, ok := site["lat"].(float64); ok {
		id.Lat = v
	}
	if v, ok := site["lon"].(float64); ok {
		id.Lng = v
	}
	if meta, ok := site["metadata"].(map[string]any); ok {
		if v, ok := meta["fishery_license_no"].(string); ok {
			id.FisheryLicenseNo = v
		}
	}
	if id.FarmID != "" {
		if f, err := s.store.GetFarm(ctx, id.FarmID); err == nil && f != nil {
			if f.Operator != "" {
				id.Operator = f.Operator
			}
			id.LicenseNo = f.LicenseNo
		}
	}
	return id
}

// handlePair — GET /v1/pair. bluei 앱이 스캔할 페어링 QR 페이로드를 반환한다.
// node_code/access_token은 config.ApplyPairCreds(시작 시)가 채운 cfg.Sync에서,
// site 정보는 cfg.Site에서 가져온다. 인증 없음(페어링 부트스트랩, 로컬 대시보드용).
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	endpoint := "https://api.bluei.kr/gx10/sync/batches"
	if s.cfg.Sync.Endpoint != nil && *s.cfg.Sync.Endpoint != "" {
		endpoint = *s.cfg.Sync.Endpoint
	}
	payload := map[string]any{
		"kind":          "gx10_pair",
		"node_code":     s.cfg.Sync.NodeCode,
		"access_token":  s.cfg.Sync.AccessToken,
		"endpoint":      endpoint,
		"site_name":     s.cfg.Site.Name,
		"site_type":     s.cfg.Site.SiteType,
		"operator_name": s.cfg.Site.Operator,
	}
	if s.cfg.Site.Lat != 0 || s.cfg.Site.Lng != 0 {
		payload["lat"] = s.cfg.Site.Lat
		payload["lng"] = s.cfg.Site.Lng
	}
	writeJSON(w, http.StatusOK, payload)
}
