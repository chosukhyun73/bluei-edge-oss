package api

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveBootstrapSnapshotDataURL covers the data-URL form that the browser
// canvas.toDataURL() produces.
func TestSaveBootstrapSnapshotDataURL(t *testing.T) {
	tmp := t.TempDir()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	// 200B 이상의 가짜 페이로드 (saveBootstrapSnapshot 의 minimum 통과용)
	payload := strings.Repeat("X", 256)
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	dataURL := "data:image/jpeg;base64," + encoded

	path, err := saveBootstrapSnapshot("lbl_test1", dataURL)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasSuffix(path, "lbl_test1.jpg") {
		t.Errorf("unexpected path: %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != payload {
		t.Errorf("payload mismatch, got %d bytes", len(body))
	}
}

// TestSaveBootstrapSnapshotRawBase64 covers the case where the frontend has
// already stripped the data URL prefix.
func TestSaveBootstrapSnapshotRawBase64(t *testing.T) {
	tmp := t.TempDir()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("Y", 300)))
	path, err := saveBootstrapSnapshot("lbl_test2", encoded)
	if err != nil {
		t.Fatalf("save raw base64: %v", err)
	}
	if filepath.Base(path) != "lbl_test2.jpg" {
		t.Errorf("filename: %q", filepath.Base(path))
	}
}

// TestSaveBootstrapSnapshotRejectsTooSmall ensures we don't accept obviously
// broken payloads (canvas not loaded, etc.).
func TestSaveBootstrapSnapshotRejectsTooSmall(t *testing.T) {
	tmp := t.TempDir()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	// 50 byte payload — under the 200B floor
	encoded := base64.StdEncoding.EncodeToString([]byte("tiny"))
	if _, err := saveBootstrapSnapshot("lbl_tiny", encoded); err == nil {
		t.Fatal("expected rejection for too-small payload")
	}
}

// TestSaveBootstrapSnapshotRejectsBadBase64 ensures invalid base64 is
// surfaced as an error rather than silently writing garbage.
func TestSaveBootstrapSnapshotRejectsBadBase64(t *testing.T) {
	tmp := t.TempDir()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	if _, err := saveBootstrapSnapshot("lbl_bad", "data:image/jpeg;base64,!@#$not_base64$%^"); err == nil {
		t.Fatal("expected rejection for malformed base64")
	}
}
