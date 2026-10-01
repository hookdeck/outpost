package e2e_test

import (
	"net/http"

	"github.com/hookdeck/outpost/internal/idgen"
)

func (s *basicSuite) TestErrorResponses_DestinationNotFound() {
	tenant := s.createTenant()
	other := s.createTenant()
	otherDest := s.createWebhookDestination(other.ID, "*")
	deleted := s.createWebhookDestination(tenant.ID, "*")
	s.deleteDestination(tenant.ID, deleted.ID)

	destinations := []struct {
		name string
		id   string
	}{
		{"missing destination", idgen.Destination()},
		{"deleted destination", deleted.ID},
		{"destination of another tenant", otherDest.ID},
	}

	requests := []struct {
		name    string
		method  string
		suffix  string
		body    any
		message string
	}{
		{"get", http.MethodGet, "", nil, "destination not found"},
		{"update", http.MethodPatch, "", map[string]any{"topics": []string{"user.created"}}, "destination not found"},
		{"delete", http.MethodDelete, "", nil, "destination not found"},
		{"enable", http.MethodPut, "/enable", nil, "destination not found"},
		{"disable", http.MethodPut, "/disable", nil, "destination not found"},
		{"get attempt", http.MethodGet, "/attempts/att_missing", nil, "attempt not found"},
	}

	auths := []struct {
		name string
		auth string
	}{
		{"api key", s.adminAuth()},
		{"jwt", s.tenantAuth(tenant.ID)},
	}

	for _, destination := range destinations {
		s.Run(destination.name, func() {
			for _, request := range requests {
				for _, auth := range auths {
					s.Run(request.name+" with "+auth.name, func() {
						s.requireError(errorCase{
							method:  request.method,
							path:    "/tenants/" + tenant.ID + "/destinations/" + destination.id + request.suffix,
							auth:    auth.auth,
							body:    request.body,
							status:  http.StatusNotFound,
							message: request.message,
						})
					})
				}
			}
		})
	}

	s.runErrorCases([]errorCase{
		{name: "unknown destination type", method: http.MethodGet, path: "/destination-types/nope", auth: s.adminAuth(), status: http.StatusNotFound, message: "destination type not found"},
		{name: "unknown destination type with jwt", method: http.MethodGet, path: "/destination-types/nope", auth: auths[1].auth, status: http.StatusNotFound, message: "destination type not found"},
	})
}

func (s *basicSuite) TestErrorResponses_EventAndAttemptNotFound() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")
	other := s.createTenant()

	eventID := idgen.Event()
	s.publish(tenant.ID, "user.created", map[string]any{"test": "not_found"}, withEventID(eventID))
	attempts := s.waitForNewAttempts(tenant.ID, 1)
	attemptID, ok := attempts[0]["id"].(string)
	s.Require().True(ok, "attempt id should be a string")
	s.waitForEventInLogstore(eventID)

	// Created after the delivery: has no attempt for the event.
	laterDest := s.createWebhookDestination(tenant.ID, "*")

	admin := s.adminAuth()
	otherJWT := s.tenantAuth(other.ID)
	destinationPath := "/tenants/" + tenant.ID + "/destinations/"

	s.runErrorCases([]errorCase{
		{name: "missing event", method: http.MethodGet, path: "/events/evt_missing", auth: admin, status: http.StatusNotFound, message: "event not found"},
		{name: "event of another tenant with jwt", method: http.MethodGet, path: "/events/" + eventID, auth: otherJWT, status: http.StatusNotFound, message: "event not found"},
		{name: "event under another tenant_id", method: http.MethodGet, path: "/events/" + eventID + "?tenant_id=" + other.ID, auth: admin, status: http.StatusNotFound, message: "event not found"},

		{name: "missing attempt", method: http.MethodGet, path: "/attempts/att_missing", auth: admin, status: http.StatusNotFound, message: "attempt not found"},
		{name: "attempt of another tenant with jwt", method: http.MethodGet, path: "/attempts/" + attemptID, auth: otherJWT, status: http.StatusNotFound, message: "attempt not found"},
		{name: "attempt under another tenant_id", method: http.MethodGet, path: "/attempts/" + attemptID + "?tenant_id=" + other.ID, auth: admin, status: http.StatusNotFound, message: "attempt not found"},
		{name: "missing attempt of a destination", method: http.MethodGet, path: destinationPath + dest.ID + "/attempts/att_missing", auth: admin, status: http.StatusNotFound, message: "attempt not found"},
		{name: "attempt under the wrong destination", method: http.MethodGet, path: destinationPath + laterDest.ID + "/attempts/" + attemptID, auth: admin, status: http.StatusNotFound, message: "attempt not found"},

		{
			name:   "retry a missing event",
			method: http.MethodPost, path: "/retry", auth: admin,
			body:   map[string]any{"event_id": "evt_missing", "destination_id": dest.ID},
			status: http.StatusNotFound, message: "event not found",
		},
		{
			name:   "retry to a missing destination",
			method: http.MethodPost, path: "/retry", auth: admin,
			body:   map[string]any{"event_id": eventID, "destination_id": "des_missing"},
			status: http.StatusNotFound, message: "event not found",
		},
		{
			name:   "retry to a destination the event was not delivered to",
			method: http.MethodPost, path: "/retry", auth: admin,
			body:   map[string]any{"event_id": eventID, "destination_id": laterDest.ID},
			status: http.StatusNotFound, message: "event not found",
		},
		{
			name:   "retry an event of another tenant with jwt",
			method: http.MethodPost, path: "/retry", auth: otherJWT,
			body:   map[string]any{"event_id": eventID, "destination_id": dest.ID},
			status: http.StatusNotFound, message: "event not found",
		},
	})

	s.Run("retry to a deleted destination", func() {
		s.deleteDestination(tenant.ID, dest.ID)
		s.requireError(errorCase{
			method: http.MethodPost, path: "/retry", auth: admin,
			body:   map[string]any{"event_id": eventID, "destination_id": dest.ID},
			status: http.StatusNotFound, message: "destination not found",
		})
	})
}
