package runtime

import (
	"sync"
	"time"
)

// ServiceStatus represents the health of a single service.
type ServiceStatus struct {
	Status      string     `json:"status"` // ok|degraded|offline|starting|failed|disabled
	LastEventAt *time.Time `json:"last_event_at,omitempty"`
	Detail      string     `json:"detail,omitempty"`
}

// HealthRegistry tracks the health of all registered services.
type HealthRegistry struct {
	mu       sync.RWMutex
	statuses map[string]*ServiceStatus
}

func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{statuses: make(map[string]*ServiceStatus)}
}

func (r *HealthRegistry) Set(name, status, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[name] = &ServiceStatus{Status: status, Detail: detail}
}

func (r *HealthRegistry) Touch(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if s, ok := r.statuses[name]; ok {
		s.LastEventAt = &now
	}
}

func (r *HealthRegistry) Get(name string) *ServiceStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.statuses[name]
	if s == nil {
		return &ServiceStatus{Status: "starting"}
	}
	return s
}

func (r *HealthRegistry) All() map[string]*ServiceStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*ServiceStatus, len(r.statuses))
	for k, v := range r.statuses {
		cp := *v
		out[k] = &cp
	}
	return out
}

// OverallHealth returns "ok" if all services are ok/degraded,
// "degraded" if any service is degraded or offline,
// "failed" if any service is failed.
func (r *HealthRegistry) OverallHealth() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := "ok"
	for _, s := range r.statuses {
		switch s.Status {
		case "failed":
			return "failed"
		case "degraded", "offline", "starting":
			result = "degraded"
		}
	}
	return result
}
