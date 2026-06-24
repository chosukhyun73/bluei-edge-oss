package media

import (
	"testing"
	"time"
)

func TestRetentionPolicyValidate(t *testing.T) {
	p := RetentionPolicy{MaxBytes: 1024, MaxAge: time.Hour}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRetentionPolicyRejectsUnsetLimits(t *testing.T) {
	p := RetentionPolicy{}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for unset limits")
	}
}

func TestRetentionPolicyShouldDelete(t *testing.T) {
	p := RetentionPolicy{MaxBytes: 1000, MaxAge: time.Hour}
	now := time.Unix(2000, 0)
	old := MediaFile{Path: "old", SizeBytes: 100, ModTime: now.Add(-2 * time.Hour)}
	if !p.ShouldDelete(old, now, 500) {
		t.Fatal("expected old file deletion")
	}
	large := MediaFile{Path: "large", SizeBytes: 100, ModTime: now}
	if !p.ShouldDelete(large, now, 1100) {
		t.Fatal("expected deletion when total exceeds max bytes")
	}
	fresh := MediaFile{Path: "fresh", SizeBytes: 100, ModTime: now}
	if p.ShouldDelete(fresh, now, 500) {
		t.Fatal("did not expect fresh file deletion")
	}
}
