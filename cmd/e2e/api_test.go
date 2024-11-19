package e2e_test

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
)

func (suite *basicSuite) TestHealthzAPI() {
	tests := []APITest{
		{
			Name: "GET /healthz",
			Request: httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/healthz",
			},
			Expected: httpclient.Response{
				StatusCode: http.StatusOK,
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantAPI() {
	tenantID := uuid.New().String()
	tests := []APITest{
		{
			Name: "GET /:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			},
			Expected: httpclient.Response{
				StatusCode: http.StatusUnauthorized,
			},
		},
		{
			Name: "GET /:tenantID without tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: httpclient.Response{
				StatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "PUT /:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			},
			Expected: httpclient.Response{
				StatusCode: http.StatusUnauthorized,
			},
		},
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: httpclient.Response{
				StatusCode: http.StatusCreated,
				Body: map[string]interface{}{
					"id": tenantID,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: httpclient.Response{
				StatusCode: http.StatusOK,
				Body: map[string]interface{}{
					"id": tenantID,
				},
			},
		},
		{
			Name: "PUT /:tenantID again",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: httpclient.Response{
				StatusCode: http.StatusOK,
				Body: map[string]interface{}{
					"id": tenantID,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}
