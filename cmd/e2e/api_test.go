package e2e_test

import (
	"net/http"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (suite *basicSuite) TestAPIHealthz() {
	resp, err := suite.client.Get("/api/v1/healthz")
	require.NoError(suite.T(), err, "Failed to make GET request to /api/v1/healthz: %v", err)
	defer resp.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
}
