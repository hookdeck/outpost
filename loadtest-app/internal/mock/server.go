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
}

func NewServer(callback DeliveryCallback) *Server {
	return &Server{
		routes:   make(map[routeKey]*route),
		callback: callback,
	}
}

func (s *Server) RegisterRoute(group, tenantIdx, destIdx string, profile *Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[routeKey{group, tenantIdx, destIdx}] = &route{profile: profile}
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
		slog.Warn("webhook for unregistered route", "group", group, "tenant", tenant, "dest", dest)
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

	// Fire callback after responding
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
