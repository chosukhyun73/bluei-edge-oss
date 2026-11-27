// Package plantapi is the HTTP API of processing-bluei, the processing-plant
// management service: plant setup, HACCP plans, certificates and scheme
// compliance. It shares storage and the event log with bluei-edge but carries
// no aquaculture handlers and no equipment control.
// References: docs/54-haccp-process-flow-ccp-design.md §19, §25.
package plantapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

var domainIDRe = regexp.MustCompile(`^[a-z0-9_\-]{1,64}$`)

// Server is the processing-bluei API server. It implements runtime.Service.
type Server struct {
	cfg     *config.Config
	app     *runtime.App
	store   storage.Store
	httpSrv *http.Server
}

func NewServer(cfg *config.Config, app *runtime.App, store storage.Store) *Server {
	s := &Server{cfg: cfg, app: app, store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, s.handleHealthz))
	mux.HandleFunc("/v1/system/shutdown", method(http.MethodPost, s.authMiddleware(s.handleShutdown)))
	mux.HandleFunc("/v1/plants", s.authMiddleware(s.handlePlantsCollection))
	mux.HandleFunc("/v1/plants/demo", method(http.MethodPost, s.authMiddleware(s.handleCreateDemoPlant)))
	mux.HandleFunc("/v1/process/lots", s.authMiddleware(s.handleLotsCollection))
	mux.HandleFunc("/v1/process/lots/", s.authMiddleware(s.handleLotRoute))
	mux.HandleFunc("/v1/documents", s.authMiddleware(s.handleDocumentsCollection))
	mux.HandleFunc("/v1/documents/", method(http.MethodGet, s.authMiddleware(s.handleDocumentFile)))
	mux.HandleFunc("/v1/plant/", s.authMiddleware(s.handlePlantRoute))
	mux.HandleFunc("/v1/forms/defaults", method(http.MethodGet, s.authMiddleware(s.handleFormDefaults)))
	mux.HandleFunc("/v1/haccp/templates", method(http.MethodGet, s.authMiddleware(s.handleHACCPTemplates)))
	mux.HandleFunc("/v1/haccp/plans", s.authMiddleware(s.handleHACCPPlansCollection))
	mux.HandleFunc("/v1/haccp/plans/", s.authMiddleware(s.handleHACCPPlanItemRoute))
	mux.HandleFunc("/v1/compliance/profiles", method(http.MethodGet, s.authMiddleware(s.handleComplianceProfiles)))

	appDir := os.Getenv("PROCESSING_BLUEI_APP_DIR")
	if appDir == "" {
		appDir = "web/processing/dist"
	}
	mux.Handle("/", spaHandler(appDir))

	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.API.BindHost, cfg.API.Port),
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
	slog.Info("api server listening", "addr", s.httpSrv.Addr)
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("api server error", "error", err)
		}
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error { return s.httpSrv.Shutdown(ctx) }

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "processing-bluei"})
}

// POST /v1/system/shutdown — 프로그램을 끝낸다(데스크톱 앱의 종료 버튼). 응답을 보낸 뒤 스스로에게 종료 신호를 보낸다.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	go func() {
		time.Sleep(200 * time.Millisecond)
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}()
}

// ── 공장 등록 ────────────────────────────────────────────────────────────────

type newPlantRequest struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// GET /v1/plants — 이 기기에 등록된 가공공장. POST — 공장 등록.
func (s *Server) handlePlantsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sites, err := s.store.ListSites(r.Context(), "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		out := []map[string]any{}
		for _, m := range sites {
			if m["site_type"] == string(site.TypeProcessing) {
				out = append(out, map[string]any{"site_id": m["site_id"], "name": m["name"], "address": m["address"]})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"plants": out, "count": len(out)})
	case http.MethodPost:
		s.handleCreatePlant(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreatePlant(w http.ResponseWriter, r *http.Request) {
	var req newPlantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "name 필수", "")
		return
	}
	ctx := r.Context()
	// 사업장은 사업자(farm) 아래에 놓인다. 등록된 사업자가 없으면 공장 이름으로 하나 만든다.
	farms, err := s.store.ListFarms(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var farmID string
	if len(farms) > 0 {
		farmID = farms[0].FarmID
	} else {
		farmID = common.NewID("farm")
		if err := s.store.UpsertFarm(ctx, &farm.Farm{FarmID: farmID, Operator: req.Name}); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
	}
	sl := &site.SiteLand{
		SiteID: common.NewID("plant"), FarmID: farmID, Name: req.Name, Timezone: s.cfg.Site.Timezone,
		Location: site.LandLocation{Address: strings.TrimSpace(req.Address)},
	}
	if sl.Timezone == "" {
		sl.Timezone = "Asia/Seoul"
	}
	if err := s.store.UpsertSiteProcessing(ctx, sl); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "plant": map[string]any{"site_id": sl.SiteID, "name": sl.Name, "address": sl.Location.Address},
	})
}

// ── 공통 ─────────────────────────────────────────────────────────────────────

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

// authMiddleware enforces token auth when api.auth.enabled is true.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.API.Auth.Enabled {
			token := os.Getenv(s.cfg.API.Auth.OperatorTokenEnv)
			if token == "" || r.Header.Get("Authorization") != "Bearer "+token {
				slog.Warn("auth failed", "remote_addr", r.RemoteAddr)
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid token", "")
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, msg, corrID string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{"code": errCode, "message": msg, "correlation_id": corrID},
	})
}

// spaHandler serves the app's static build from dir, falling back to
// index.html for client-side routes. API and health paths never fall back.
func spaHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 등록되지 않은 API 경로는 앱 화면으로 넘기지 않는다.
		if strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasPrefix(clean, "..") {
			http.NotFound(w, r)
			return
		}
		if clean != "." {
			candidate := filepath.Join(dir, clean)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				http.ServeFile(w, r, candidate)
				return
			}
		}
		indexPath := filepath.Join(dir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, indexPath)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("processing-bluei app build (" + dir + "/index.html) not found.\n" +
			"Run: cd web/processing && npm install && npm run build\n"))
	})
}
