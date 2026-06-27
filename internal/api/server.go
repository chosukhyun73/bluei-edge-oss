package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"bluei.kr/edge/internal/arbiter"
	"bluei.kr/edge/internal/collector"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/knowledge"
	"bluei.kr/edge/internal/llm"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
	bsync "bluei.kr/edge/internal/sync"
	"bluei.kr/edge/internal/training"
)

// AISchedulerHook — Phase 1e-A.
// 운영자 의도/이의제기 입력 시 즉시 호출되는 좁은 인터페이스.
// feed_cycle.AIScheduler 가 이 인터페이스를 구현. api 패키지가 feed_cycle 의
// 구체타입에 더 깊이 의존하지 않도록 추출.
type AISchedulerHook interface {
	RebuildScheduleForTank(ctx context.Context, tankID string) error
	// RebuildScheduleForTankWithOverride — F.2: 1회성 LLM 분석 override 적용.
	RebuildScheduleForTankWithOverride(ctx context.Context, tankID string, override *feed_cycle.PolicyOverride) error
}

// LiveWeightProvider exposes the most recent UDP weight per controller.
// Implemented by udp_listener.Listener. Optional — when nil the API
// returns 503 for live-weight requests.
//
// grams 는 EMA + dead band 적용 후 안정값 (표시용).
// rawGrams 는 필터 전 값 (디버그/그래프용).
type LiveWeightProvider interface {
	GetLiveWeight(controllerID string) (grams, rawGrams float64, raw int64, mode string, rssi int, ageMs int64, ok bool)
	// SetZero 는 현재 안정값을 영점으로 잡고 그 offset 값을 반환.
	// 빈 통 측정 후 운영자가 영점 버튼을 누를 때 호출. ok=false 면 패킷 미수신.
	SetZero(controllerID string) (offset float64, ok bool)
	// ClearZero 는 영점을 해제.
	ClearZero(controllerID string)
}

// Server is the local HTTP API server.
type Server struct {
	cfg         *config.Config
	app         *runtime.App
	store       storage.Store
	col         *collector.Service
	ctrl        *control.Service
	sync        *bsync.Service
	train       *training.Service
	feedCycle   *feed_cycle.Worker
	arbiter     *arbiter.Arbiter
	aiScheduler AISchedulerHook
	llmClient   *llm.Client
	knowledge   *knowledge.Retriever
	liveWeight  LiveWeightProvider
	httpSrv     *http.Server
}

// SetLiveWeightProvider wires UDP live-weight cache into the API.
// Must be called before Start.
func (s *Server) SetLiveWeightProvider(p LiveWeightProvider) { s.liveWeight = p }

// SetKnowledgeRetriever wires the RAG knowledge retriever into the assistant.
// nil 이면 어시스턴트는 RAG 없이 동작. Must be called before Start.
func (s *Server) SetKnowledgeRetriever(r *knowledge.Retriever) { s.knowledge = r }

func NewServer(
	cfg *config.Config,
	app *runtime.App,
	store storage.Store,
	col *collector.Service,
	ctrl *control.Service,
	sync *bsync.Service,
	fcWorker *feed_cycle.Worker,
	arb *arbiter.Arbiter,
	aiScheduler AISchedulerHook,
	llmClient *llm.Client,
) *Server {
	s := &Server{
		cfg:         cfg,
		app:         app,
		store:       store,
		col:         col,
		ctrl:        ctrl,
		sync:        sync,
		train:       training.New(app, training.Config{}),
		feedCycle:   fcWorker,
		arbiter:     arb,
		aiScheduler: aiScheduler,
		llmClient:   llmClient,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, s.handleHealthz))
	mux.HandleFunc("/readyz", method(http.MethodGet, s.handleReadyz))
	mux.HandleFunc("/v1/status", method(http.MethodGet, s.handleStatus))
	// 페어링 QR 페이로드 — 인증 없음(앱이 스캔하기 전 부트스트랩, 로컬 대시보드용).
	mux.HandleFunc("/v1/pair", method(http.MethodGet, s.handlePair))
	// Device auth (login bootstrap·session) — 폰 푸시 승인 device-login 흐름. 무인증(부트스트랩).
	mux.HandleFunc("/v1/device/login/start", method(http.MethodPost, s.handleDeviceLoginStart))
	mux.HandleFunc("/v1/device/login/status", method(http.MethodGet, s.handleDeviceLoginStatus))
	mux.HandleFunc("/v1/device/session", method(http.MethodGet, s.handleDeviceSession))
	mux.HandleFunc("/v1/operations/status", method(http.MethodGet, s.authMiddleware(s.handleOperationalStatus)))
	mux.HandleFunc("/v1/readings/recent", method(http.MethodGet, s.authMiddleware(s.handleReadingsRecent)))
	mux.HandleFunc("/v1/readings/latest", method(http.MethodGet, s.authMiddleware(s.handleReadingsLatest)))
	mux.HandleFunc("/v1/tanks", s.authMiddleware(s.handleTanksRoute))
	// /v1/tanks/{id}/* — GET (state, state-vector, profile) 와 POST (baseline/score) 모두 dispatch 하므로 method 제한 없음.
	mux.HandleFunc("/v1/tanks/", s.authMiddleware(s.handleTankRoute))
	// 운영자 의도적 graceful 종료 (GX10 전원 끄기 직전용).
	mux.HandleFunc("/v1/system/shutdown", method(http.MethodPost, s.authMiddleware(s.handleSystemShutdown)))
	// 운영자 가이드 채팅 — bluei-edge-assistant(26B) + 라이브 상태 주입.
	mux.HandleFunc("/v1/assistant/chat", method(http.MethodPost, s.authMiddleware(s.handleAssistantChat)))
	// GDST 증빙 서류 다운로드 — 업로드/목록은 /v1/tanks/{id}/documents (handleTankRoute dispatch).
	mux.HandleFunc("/v1/documents/", method(http.MethodGet, s.authMiddleware(s.handleDocumentDownload)))
	// 재고관리 — 품목/구매/사용 + 구매 서류.
	mux.HandleFunc("/v1/inventory", method(http.MethodGet, s.authMiddleware(s.handleListInventory)))
	mux.HandleFunc("/v1/inventory/items", method(http.MethodPost, s.authMiddleware(s.handleCreateInventoryItem)))
	mux.HandleFunc("/v1/inventory/purchase", method(http.MethodPost, s.authMiddleware(s.handlePurchase)))
	mux.HandleFunc("/v1/inventory/consume", method(http.MethodPost, s.authMiddleware(s.handleConsume)))
	mux.HandleFunc("/v1/inventory/purchases/", s.authMiddleware(s.handleInventoryPurchasesRoute))
	// 거래처(공급처/구매처) 마스터 + 거래처 서류.
	mux.HandleFunc("/v1/partners", s.authMiddleware(s.handlePartnersCollection))
	mux.HandleFunc("/v1/partners/", s.authMiddleware(s.handlePartnerItemRoute))
	// 사업자(site) 단위 거래 — 입식 배치 / 출하 건 + 거래 서류.
	mux.HandleFunc("/v1/site-stockings", s.authMiddleware(s.handleSiteStockingsCollection))
	mux.HandleFunc("/v1/site-stockings/", s.authMiddleware(s.handleSiteStockingItemRoute))
	mux.HandleFunc("/v1/site-harvests", s.authMiddleware(s.handleSiteHarvestsCollection))
	mux.HandleFunc("/v1/site-harvests/", s.authMiddleware(s.handleSiteHarvestItemRoute))
	mux.HandleFunc("/v1/groups", s.authMiddleware(s.handleGroupsRoute))
	mux.HandleFunc("/v1/groups/", s.authMiddleware(s.handleGroupRoute))
	// Hatchery (broodstock·spawn·larval·live-feed) — 종묘장 단계 엔티티.
	mux.HandleFunc("/v1/broodstock", s.authMiddleware(s.handleBroodstockRoute))
	mux.HandleFunc("/v1/broodstock/", s.authMiddleware(s.handleBroodstockItemRoute))
	mux.HandleFunc("/v1/spawn-batches", s.authMiddleware(s.handleSpawnBatchRoute))
	mux.HandleFunc("/v1/spawn-batches/", s.authMiddleware(s.handleSpawnBatchItemRoute))
	mux.HandleFunc("/v1/larval-batches", s.authMiddleware(s.handleLarvalRoute))
	mux.HandleFunc("/v1/larval-batches/", s.authMiddleware(s.handleLarvalItemRoute))
	mux.HandleFunc("/v1/live-feed", s.authMiddleware(s.handleLiveFeedRoute))
	mux.HandleFunc("/v1/live-feed/", s.authMiddleware(s.handleLiveFeedItemRoute))
	mux.HandleFunc("/v1/hatchery-treatments", s.authMiddleware(s.handleHatcheryTreatmentRoute))
	mux.HandleFunc("/v1/hatchery-treatments/", s.authMiddleware(s.handleHatcheryTreatmentItemRoute))
	mux.HandleFunc("/v1/cameras", s.authMiddleware(s.handleCamerasRoute))
	// 카메라 자동 탐지 (ONVIF + 포트스캔) — /v1/cameras/{id} 보다 구체 경로라 우선 매칭.
	mux.HandleFunc("/v1/cameras/discover", method(http.MethodGet, s.authMiddleware(s.handleCameraDiscover)))
	mux.HandleFunc("/v1/cameras/", s.authMiddleware(s.handleCameraRoute))
	// C-11 camera model library — vendor/lens/특성 라이브러리. camera_profiles.model_id 가 참조.
	mux.HandleFunc("/v1/camera-models", s.authMiddleware(s.handleCameraModelsRoute))
	mux.HandleFunc("/v1/camera-models/", s.authMiddleware(s.handleCameraModelItem))
	mux.HandleFunc("/v1/devices", method(http.MethodGet, s.authMiddleware(s.handleDevices)))
	mux.HandleFunc("/v1/alerts/open", method(http.MethodGet, s.authMiddleware(s.handleOpenAlerts)))
	mux.HandleFunc("/v1/alerts/", s.authMiddleware(s.handleAlertItemRoute))
	mux.HandleFunc("/v1/feedings", method(http.MethodPost, s.authMiddleware(s.handlePostFeedingRecord)))
	mux.HandleFunc("/v1/feedings/recent", method(http.MethodGet, s.authMiddleware(s.handleRecentFeedings)))
	mux.HandleFunc("/v1/feedings/today", method(http.MethodGet, s.authMiddleware(s.handleTodayFeedingSummary)))
	mux.HandleFunc("/v1/feedings/impact/", method(http.MethodGet, s.authMiddleware(s.handleFeedingImpactRoute)))
	mux.HandleFunc("/v1/water-quality/buckets", method(http.MethodGet, s.authMiddleware(s.handleWaterQualityBuckets)))
	mux.HandleFunc("/v1/vision/algorithms", method(http.MethodGet, s.authMiddleware(s.handleVisionAlgorithms)))
	mux.HandleFunc("/v1/vision/tank-applications", method(http.MethodGet, s.authMiddleware(s.handleVisionTankApplications)))
	mux.HandleFunc("/v1/vision/observations", s.authMiddleware(s.handleVisionObservationsRoute))
	mux.HandleFunc("/v1/vision/observations/", s.authMiddleware(s.handleVisionObservationRoute))
	mux.HandleFunc("/v1/vision/bootstrap/labels", s.authMiddleware(s.handleVisionBootstrapLabelsRoute))
	mux.HandleFunc("/v1/vision/training/status", method(http.MethodGet, s.authMiddleware(s.handleTrainingStatus)))
	mux.HandleFunc("/v1/vision/training/jobs", s.authMiddleware(s.handleTrainingJobsRoute))
	mux.HandleFunc("/v1/vision/training/jobs/", s.authMiddleware(s.handleTrainingJobRoute))
	mux.HandleFunc("/v1/vision/training-pool", method(http.MethodGet, s.authMiddleware(s.handleVisionTrainingPool)))
	mux.HandleFunc("/v1/vision/feeding-score", method(http.MethodPost, s.authMiddleware(s.handlePostFeedingScore)))
	mux.HandleFunc("/v1/vision/algorithms/", s.authMiddleware(s.handleVisionAlgorithmActionRoute))
	mux.HandleFunc("/v1/media/clips", s.authMiddleware(s.handleMediaClipsRoute))
	mux.HandleFunc("/v1/media/clips/", s.authMiddleware(s.handleMediaClipStream))
	mux.HandleFunc("/v1/capture/disk", method(http.MethodGet, s.authMiddleware(s.handleCaptureDisk)))
	mux.HandleFunc("/v1/gateway/readings", method(http.MethodPost, s.authMiddleware(s.handlePostGatewayReadings)))
	mux.HandleFunc("/v1/gateway/device-health", method(http.MethodPost, s.authMiddleware(s.handlePostGatewayDeviceHealth)))
	mux.HandleFunc("/v1/farms", s.authMiddleware(s.handleFarmsRoute))
	mux.HandleFunc("/v1/farms/", s.authMiddleware(s.handleFarmItem))
	mux.HandleFunc("/v1/sites", s.authMiddleware(s.handleSitesRoute))
	mux.HandleFunc("/v1/sites/", s.authMiddleware(s.handleSiteItem))
	mux.HandleFunc("/v1/water-treatment-groups", s.authMiddleware(s.handleWTGsRoute))
	mux.HandleFunc("/v1/water-treatment-groups/", s.authMiddleware(s.handleWTGItem))
	mux.HandleFunc("/v1/actuators", s.authMiddleware(s.handleActuatorsRoute))
	mux.HandleFunc("/v1/actuators/", s.authMiddleware(s.handleActuatorItem))
	mux.HandleFunc("/v1/sensors", s.authMiddleware(s.handleSensorsRoute))
	mux.HandleFunc("/v1/sensors/", s.authMiddleware(s.handleSensorItem))
	mux.HandleFunc("/v1/species-profiles", s.authMiddleware(s.handleSpeciesProfilesRoute))
	mux.HandleFunc("/v1/species-profiles/", s.authMiddleware(s.handleSpeciesProfileItem))
	mux.HandleFunc("/v1/controllers", s.authMiddleware(s.handleControllerRoute))
	mux.HandleFunc("/v1/controllers/", s.authMiddleware(s.handleControllerRoute))
	// ESP32 provisioning (USB flash + 등록 자동화)
	mux.HandleFunc("/v1/provision", method(http.MethodPost, s.authMiddleware(s.handleProvisionStart)))
	mux.HandleFunc("/v1/provision/ports", method(http.MethodGet, s.authMiddleware(s.handleProvisionPorts)))
	mux.HandleFunc("/v1/provision/", method(http.MethodGet, s.authMiddleware(s.handleProvisionItem)))
	mux.HandleFunc("/v1/control/commands", method(http.MethodPost, s.authMiddleware(s.handlePostCommand)))
	mux.HandleFunc("/v1/control/commands/", method(http.MethodGet, s.authMiddleware(s.handleGetCommand)))
	mux.HandleFunc("/v1/sync/status", method(http.MethodGet, s.authMiddleware(s.handleSyncStatus)))
	mux.HandleFunc("/v1/setup/draft", s.authMiddleware(s.handleSetupDraftRoute))
	mux.HandleFunc("/v1/setup/draft/validate", method(http.MethodGet, s.authMiddleware(s.handleSetupDraftValidate)))
	mux.HandleFunc("/v1/setup/draft/preview", method(http.MethodGet, s.authMiddleware(s.handleSetupDraftPreview)))
	mux.HandleFunc("/v1/feed-cycles", s.authMiddleware(s.handleFeedCyclesRoute))
	mux.HandleFunc("/v1/feed-cycles/", s.authMiddleware(s.handleFeedCycleRoute))
	mux.HandleFunc("/v1/feeding-schedules", s.authMiddleware(s.handleFeedingSchedulesRoute))
	mux.HandleFunc("/v1/feeding-schedules/", s.authMiddleware(s.handleFeedingScheduleRoute))
	// Phase 4 D-7 + C-3p predictive endpoints (read-only)
	mux.HandleFunc("/v1/predictive/forecast", method(http.MethodGet, s.authMiddleware(s.handleGetPredictiveForecast)))
	mux.HandleFunc("/v1/predictive/blocks", method(http.MethodGet, s.authMiddleware(s.handleGetPredictiveBlocks)))
	// Phase 4 C-3l learned safety endpoints
	// Phase 5: 운영자 의도 메모
	mux.HandleFunc("/v1/operator/intents", s.authMiddleware(s.handleOperatorIntentsRoute))
	// F.2: intent apply (1회성 override) — /v1/operator/intents/{id}/apply
	mux.HandleFunc("/v1/operator/intents/", s.authMiddleware(s.handleOperatorIntentItemRoute))
	mux.HandleFunc("/v1/operator/disputes", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			s.handlePostOperatorDispute(w, r)
		case http.MethodGet:
			s.handleListOperatorDisputes(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.HandleFunc("/v1/learned-rules", method(http.MethodGet, s.authMiddleware(s.handleListLearnedRules)))
	mux.HandleFunc("/v1/learned-rules/", s.authMiddleware(s.handleLearnedRulesRoute))
	// Phase 4 C-3w environmental safety endpoints
	mux.HandleFunc("/v1/environmental/current", method(http.MethodGet, s.authMiddleware(s.handleGetEnvironmentalCurrent)))
	mux.HandleFunc("/v1/environmental/history", method(http.MethodGet, s.authMiddleware(s.handleGetEnvironmentalHistory)))
	mux.HandleFunc("/v1/environmental/snapshot", method(http.MethodPost, s.authMiddleware(s.handlePostEnvironmentalSnapshot)))
	// C-4 arbiter decision audit log
	mux.HandleFunc("/v1/arbiter/decisions", method(http.MethodGet, s.authMiddleware(s.handleListArbiterDecisions)))
	// C-5 safety gate aggregated status
	mux.HandleFunc("/v1/safety-gates/status", method(http.MethodGet, s.authMiddleware(s.handleGetSafetyGatesStatus)))
	// C-13a sensor model library — 센서 모델 vendor/measurement 라이브러리. sensors.model_id 가 참조.
	mux.HandleFunc("/v1/sensor-models", s.authMiddleware(s.handleSensorModelsRoute))
	mux.HandleFunc("/v1/sensor-models/", s.authMiddleware(s.handleSensorModelItem))
	// C-13b actuator model library — vendor/category/control_method 라이브러리. actuators.model_id 가 참조.
	mux.HandleFunc("/v1/actuator-models", s.authMiddleware(s.handleActuatorModelsRoute))
	mux.HandleFunc("/v1/actuator-models/", s.authMiddleware(s.handleActuatorModelItem))
	// Dashboard 정적 서빙 + SPA fallback (Phase 1 of packaging).
	// production: bluei-edge 가 :8080 단일 port 로 dashboard + API 동시 서빙 → 외부 인프라 0.
	// dev: vite dev server :5173 도 별도로 동작 (HMR). 사용자가 어느 쪽 접근해도 OK.
	// 우선순위는 mux 등록 순서대로 — 위 라우트들 (/v1/*, /healthz 등) 가 먼저 매칭됨.
	dashboardDir := os.Getenv("BLUEI_EDGE_DASHBOARD_DIR")
	if dashboardDir == "" {
		dashboardDir = "web/dashboard/dist"
	}
	mux.Handle("/", spaHandler(dashboardDir))

	addr := fmt.Sprintf("%s:%d", cfg.API.BindHost, cfg.API.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.API.RequestTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.API.RequestTimeoutSec) * time.Second,
	}
	return s
}

func (s *Server) Name() string { return "api" }

func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("api listen %s: %w", s.httpSrv.Addr, err)
	}
	s.app.Health.Set("api", "ok", "")
	slog.Info("api server listening", "addr", s.httpSrv.Addr)
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("api server error", "error", err)
			s.app.Health.Set("api", "failed", err.Error())
		}
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func method(allowed string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowed {
			w.Header().Set("Allow", allowed)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func commandIDFromPath(path string) string {
	return strings.TrimPrefix(path, "/v1/control/commands/")
}

// authMiddleware enforces token auth when api.auth.enabled is true.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.API.Auth.Enabled {
			token := os.Getenv(s.cfg.API.Auth.OperatorTokenEnv)
			bearer := r.Header.Get("Authorization")
			if token == "" || bearer != "Bearer "+token {
				slog.Warn("auth failed", "remote_addr", r.RemoteAddr)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid token", "")
				return
			}
		}
		next(w, r)
	}
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeError writes the common error envelope.
func writeError(w http.ResponseWriter, code int, errCode, msg, corrID string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{
			"code":           errCode,
			"message":        msg,
			"correlation_id": corrID,
		},
	})
}
