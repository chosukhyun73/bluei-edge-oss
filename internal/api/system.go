package api

import (
	"net/http"
	"syscall"
	"time"
)

// POST /v1/system/shutdown — 운영자 의도적 graceful 종료 (GX10 전원 끄기 직전용).
// 응답을 먼저 보낸 뒤 자기 자신에게 SIGTERM 을 보내 기존 graceful shutdown 경로를 탄다
// (워커 정지 + WAL flush + RecordShutdown). 강제 kill 이 아니라 깨끗한 종료.
func (s *Server) handleSystemShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "graceful shutdown initiated",
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// 응답 전송 여유를 둔 뒤 self-SIGTERM → main 의 signal 핸들러가 graceful 종료.
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()
}
