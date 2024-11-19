package e2e_test

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (suite *basicSuite) TestAPIHealthz() {
	req := httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/healthz",
	}
	resp, err := suite.client.Do(req)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
}

func (suite *basicSuite) TestTenantCreate() {
	tenantID := uuid.New().String()
	req := suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/" + tenantID,
	})
	resp, err := suite.client.Do(req)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusCreated, resp.StatusCode)
	assert.Equal(suite.T(), tenantID, resp.Body["id"])
}
