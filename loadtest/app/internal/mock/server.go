package mock

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type DeliveryRecord struct {
	EventID     string
	GroupName   string
	TenantIndex string
	DestIndex   string
	ReceivedAt  time.Time
}

type DeliveryCallback func(DeliveryRecord)

type routeKey struct {
	group  string
	tenant string
	dest   string
}

type route struct {
	profile *Profile
}

type Server struct {
	mu       sync.RWMutex
	routes   map[routeKey]*route
	callback DeliveryCallback
	// Routes already warned about, so the warning is per route rather than
	// per delivery. Separate from mu: it is written on the hot path, and the
	// route map is read there under RLock.
	unregistered sync.Map
}

func NewServer(callback DeliveryCallback) *Server {
	return &Server{
		routes:   make(map[routeKey]*route),
		callback: callback,
	}
}

func (s *Server) SetCallback(cb DeliveryCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback = cb
}

func (s *Server) RegisterRoute(group, tenantIdx, destIdx string, profile *Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := routeKey{group, tenantIdx, destIdx}
	s.routes[key] = &route{profile: profile}
	slog.Info("mock route registered", "group", group, "tenant", tenantIdx, "dest", destIdx, "total_routes", len(s.routes))
}

func (s *Server) UnregisterRoute(group, tenantIdx, destIdx string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.routes, routeKey{group, tenantIdx, destIdx})
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.handleWebhook)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path: /webhook/{group}/{tenant}/{dest}
	group := r.PathValue("group")
	tenant := r.PathValue("tenant")
	dest := r.PathValue("dest")

	if group == "" || tenant == "" || dest == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	key := routeKey{group, tenant, dest}
	s.mu.RLock()
	rt, ok := s.routes[key]
	s.mu.RUnlock()

	if !ok {
		// Once per route, not once per delivery. An unregistered route keeps
		// receiving for the rest of the run, so logging every one turns a
		// misconfigured destination into thousands of lines a second — the
		// same stdout backpressure the per-delivery log was removed for, in
		// precisely the situation where the volume is highest. The set is
		// bounded by the number of routes the spec creates.
		if _, seen := s.unregistered.LoadOrStore(key, struct{}{}); !seen {
			slog.Warn("webhook for unregistered route", "group", group, "tenant", tenant, "dest", dest)
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	eventID := r.Header.Get("X-Outpost-Event-ID")
	receivedAt := time.Now()

	// Apply mock profile
	if latency := rt.profile.GetLatency(); latency > 0 {
		time.Sleep(latency)
	}

	if rt.profile.ShouldError() {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "simulated error")
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}

	// Deliberately not logged. This handler runs once per delivery — at
	// benchmark rates that is thousands of lines a second, and slog writes to
	// stdout synchronously. When the log collector stops draining the pipe the
	// write blocks here, inside the receive path, and the harness stalls the
	// very deliveries it is timing. Per-delivery facts belong in the event log
	// and the counters, both of which are in memory.
	if s.callback != nil && eventID != "" {
		s.callback(DeliveryRecord{
			EventID:     eventID,
			GroupName:   group,
			TenantIndex: tenant,
			DestIndex:   dest,
			ReceivedAt:  receivedAt,
		})
	}
}
