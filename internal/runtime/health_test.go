package runtime

import (
	"testing"
)

func TestHealthRegistry_SetAndGet(t *testing.T) {
	r := NewHealthRegistry()
	r.Set("collector", "ok", "")
	s := r.Get("collector")
	if s.Status != "ok" {
		t.Errorf("expected ok, got %s", s.Status)
	}
}

func TestHealthRegistry_OverallHealth(t *testing.T) {
	r := NewHealthRegistry()
	r.Set("a", "ok", "")
	r.Set("b", "ok", "")
	if r.OverallHealth() != "ok" {
		t.Error("expected ok")
	}

	r.Set("c", "degraded", "sync offline")
	if r.OverallHealth() != "degraded" {
		t.Error("expected degraded when one service is degraded")
	}

	r.Set("d", "failed", "storage error")
	if r.OverallHealth() != "failed" {
		t.Error("expected failed when one service is failed")
	}
}

func TestHealthRegistry_UnknownService(t *testing.T) {
	r := NewHealthRegistry()
	s := r.Get("nonexistent")
	if s.Status != "starting" {
		t.Errorf("expected starting for unknown service, got %s", s.Status)
	}
}
