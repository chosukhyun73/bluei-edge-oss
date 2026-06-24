package api

import (
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"bluei.kr/edge/internal/capture"
)

// handleCaptureDisk — R15. captures 디렉토리 디스크 사용량 + retention 정책 상태.
// GET /v1/capture/disk
//
// 응답:
//
//	{
//	  "captures_dir": "/tmp/bluei-edge/captures",
//	  "total_bytes": 16384000000,
//	  "free_bytes": 7521792000,
//	  "used_bytes": 8862208000,
//	  "used_percent": 54.1,
//	  "captures": {
//	    "count": 25, "size_bytes": 8589934,
//	  },
//	  "excluded": {
//	    "occlusion":      { "count": 1, "size_bytes": 352157 },
//	    "low_visibility": { "count": 0, "size_bytes": 0 },
//	    "other":          { "count": 0, "size_bytes": 0 }
//	  }
//	}
//
// dashboard 위젯 + 강릉 D-18 외장 하드 운영 시 디스크 사용량 모니터링.
func (s *Server) handleCaptureDisk(w http.ResponseWriter, r *http.Request) {
	captureDir := capture.DefaultTempDir

	// 1. 디스크 전체 (df-equivalent — Linux statfs)
	var fs syscall.Statfs_t
	totalBytes, freeBytes, usedBytes := int64(0), int64(0), int64(0)
	usedPercent := 0.0
	if err := syscall.Statfs(captureDir, &fs); err == nil {
		totalBytes = int64(fs.Blocks) * int64(fs.Bsize)
		freeBytes = int64(fs.Bavail) * int64(fs.Bsize)
		usedBytes = totalBytes - freeBytes
		if totalBytes > 0 {
			usedPercent = float64(usedBytes) / float64(totalBytes) * 100
		}
	}

	// 2. captures 디렉토리 (정상 영상 = root mp4)
	capCount, capSize := scanMP4(captureDir, false)

	// 3. excluded 하위 (reason 별)
	excludedDir := filepath.Join(captureDir, "excluded")
	excluded := map[string]map[string]int64{
		"occlusion":      {"count": 0, "size_bytes": 0},
		"low_visibility": {"count": 0, "size_bytes": 0},
		"other":          {"count": 0, "size_bytes": 0},
	}
	if entries, err := os.ReadDir(excludedDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			c, sz := scanMP4(filepath.Join(excludedDir, e.Name()), false)
			excluded[e.Name()] = map[string]int64{"count": int64(c), "size_bytes": sz}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"captures_dir": captureDir,
		"disk": map[string]any{
			"total_bytes":  totalBytes,
			"free_bytes":   freeBytes,
			"used_bytes":   usedBytes,
			"used_percent": usedPercent,
		},
		"captures": map[string]any{
			"count":      capCount,
			"size_bytes": capSize,
		},
		"excluded": excluded,
	})
}

// scanMP4 — dir 안의 *.mp4 파일 개수 + 합산 size. recursive=false (즉시 하위만).
func scanMP4(dir string, recursive bool) (int, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	count := 0
	var size int64
	for _, e := range entries {
		if e.IsDir() {
			if recursive {
				c, s := scanMP4(filepath.Join(dir, e.Name()), true)
				count += c
				size += s
			}
			continue
		}
		if filepath.Ext(e.Name()) != ".mp4" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		count++
		size += info.Size()
	}
	return count, size
}
