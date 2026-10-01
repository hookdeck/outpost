package e2e_test

import (
	"net/http"

	"github.com/hookdeck/outpost/internal/idgen"
)

type errorResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// Errors raised outside the handlers (auth, routing) carry the JSON error
// envelope. The doJSON helpers fail on any API response that is not JSON.
func (s *basicSuite) TestErrorResponses_AuthAndRoutingErrorsAreJSON() {
	tenant := s.createTenant()
	other := s.createTenant()

	var token struct {
		Token string `json:"token"`
	}
	status := s.doJSON(http.MethodGet, s.apiURL("/tenants/"+tenant.ID+"/token"), nil, &token)
	s.Require().Equal(http.StatusOK, status)
	jwt := "Bearer " + token.Token

	tests := []struct {
		name    string
		method  string
		path    string
		auth    string
		status  int
		message string
	}{
		{"no auth header", http.MethodGet, "/tenants/" + tenant.ID, "", http.StatusUnauthorized, "unauthorized"},
		{"invalid token", http.MethodGet, "/tenants/" + tenant.ID, "Bearer not-a-token", http.StatusUnauthorized, "unauthorized"},
		{"jwt on another tenant", http.MethodGet, "/tenants/" + other.ID, jwt, http.StatusForbidden, "forbidden"},
		{"jwt on admin-only route", http.MethodGet, "/tenants/" + tenant.ID + "/token", jwt, http.StatusForbidden, "forbidden"},
		{"missing tenant", http.MethodGet, "/tenants/" + idgen.String(), "Bearer " + s.config.APIKey, http.StatusNotFound, "tenant not found"},
		{"unknown route", http.MethodGet, "/nope", "", http.StatusNotFound, "not found"},
		{"wrong method", http.MethodDelete, "/publish", "Bearer " + s.config.APIKey, http.StatusNotFound, "not found"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var resp errorResponse
			status := s.doJSONWithAuth(tt.method, s.apiURL(tt.path), tt.auth, nil, &resp)

			s.Require().Equal(tt.status, status)
			s.Equal(tt.status, resp.Status)
			s.Equal(tt.message, resp.Message)
		})
	}
}
