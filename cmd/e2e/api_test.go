package e2e_test

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (suite *basicSuite) TestAPIHealthz() {
	resp, err := suite.client.GET("/api/v1/healthz")
	require.NoError(suite.T(), err)
	defer resp.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
}

func (suite *basicSuite) TestTenantCreate() {
	tenantID := uuid.New().String()
	resp, err := suite.client.PUT("/api/v1/"+tenantID, nil)
	require.NoError(suite.T(), err)
	defer resp.Body.Close()
	assert.Equal(suite.T(), http.StatusCreated, resp.StatusCode)
	body, err := suite.client.ParseBody(resp)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), tenantID, body["id"])
}
