package e2e_test

import "net/http"

func (s *basicSuite) TestHealth_ServerReportsHealthy() {
	var resp map[string]any
	status := s.doJSON(http.MethodGet, s.apiURL("/healthz"), nil, &resp)

	s.Require().Equal(http.StatusOK, status)
	s.NotEmpty(resp["status"], "status should be present")
	s.NotEmpty(resp["timestamp"], "timestamp should be present")
	s.NotNil(resp["workers"], "workers should be present")
}
