package simulator

import (
	"strings"
	"sync"
	"time"
)

// AllowlistEnv names the env var listing tenants allowed to use the judge simulator
// (comma-separated tenant IDs or tenant schemas, e.g. "t_toko_demo"). Empty = no tenant
// allowed (fail closed): the simulator triggers paid LLM calls and must stay demo-only.
const AllowlistEnv = "SimulatorTenantAllowlist"

// ParseAllowlist splits a comma/space separated allowlist into a set.
func ParseAllowlist(raw string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		if f = strings.TrimSpace(f); f != "" {
			out[f] = true
		}
	}
	return out
}

// TenantAllowed reports whether tenantID or tenantSchema (both from the server-side
// session, never from the request body) is on the allowlist.
func TenantAllowed(allow map[string]bool, tenantID, tenantSchema string) bool {
	if len(allow) == 0 {
		return false
	}
	id, sch := strings.TrimSpace(tenantID), strings.TrimSpace(tenantSchema)
	return (id != "" && allow[id]) || (sch != "" && allow[sch])
}

// MemLimiter is a per-instance fixed-window limiter used when Redis is unavailable,
// so the simulator path never fails open to unlimited.
type MemLimiter struct {
	mu   sync.Mutex
	now  func() time.Time
	hits map[string]memWindow
}

type memWindow struct {
	start time.Time
	n     int
}

func NewMemLimiter(now func() time.Time) *MemLimiter {
	if now == nil {
		now = time.Now
	}
	return &MemLimiter{now: now, hits: map[string]memWindow{}}
}

// Allow counts one hit for key and reports whether it is within max per window.
func (l *MemLimiter) Allow(key string, max int, window time.Duration) bool {
	if max <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	w := l.hits[key]
	if w.start.IsZero() || t.Sub(w.start) >= window {
		w = memWindow{start: t}
	}
	w.n++
	l.hits[key] = w
	if len(l.hits) > 10000 { // bound memory: drop expired windows
		for k, v := range l.hits {
			if t.Sub(v.start) >= window {
				delete(l.hits, k)
			}
		}
	}
	return w.n <= max
}
