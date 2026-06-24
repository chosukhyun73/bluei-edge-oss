package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// spaHandler returns an HTTP handler that serves a SPA (Single Page Application)
// from dir. Behavior:
//   - "/<path>" : if dir/<path> exists as a regular file → serve it (assets/, favicon.ico, …)
//   - otherwise → serve dir/index.html (client-side router handles the route)
//   - dir/index.html missing → 404 with a build hint
//
// Why a fallback to index.html: React Router (and similar) use client-side routes
// like "/tanks/tank_01/settings" which don't map to a file on disk. The server
// must hand back the SPA shell so the JS router can pick up.
//
// Security: requests are confined to dir via filepath.Clean + prefix check. A
// path like "/../../etc/passwd" cannot escape dir.
func spaHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// API 또는 health path 는 SPA fallback 적용 안 함 — 등록 안 됐으면 404.
		// 이게 없으면 unregistered API path (예: /v1/foo) 가 dashboard index.html 을
		// 반환해서 클라이언트가 HTML 을 JSON 으로 파싱 시도 → 무한 디버깅.
		if strings.HasPrefix(r.URL.Path, "/v1/") ||
			r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			http.NotFound(w, r)
			return
		}

		urlPath := strings.TrimPrefix(r.URL.Path, "/")
		clean := filepath.Clean(urlPath)
		// disallow path escape: clean must not start with ".." after Clean.
		if strings.HasPrefix(clean, "..") {
			http.NotFound(w, r)
			return
		}

		if urlPath != "" {
			candidate := filepath.Join(dir, clean)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				http.ServeFile(w, r, candidate)
				return
			}
		}

		// SPA fallback — serve index.html for any unmatched route.
		indexPath := filepath.Join(dir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			// no-store on the shell so deploys are picked up immediately by operator browsers.
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, indexPath)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(
			"bluei-edge dashboard build (" + dir + "/index.html) not found.\n" +
				"Run: cd web/dashboard && npm install && npm run build\n",
		))
	})
}
