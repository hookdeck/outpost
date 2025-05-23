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
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantsAPI() {
	tenantID := uuid.New().String()
	sampleDestinationID := uuid.New().String()
	tests := []APITest{
		{
			Name: "GET /:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		{
			Name: "GET /:tenantID without tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "PUT /:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "PUT /:tenantID again",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 1,
						"topics":             suite.config.Topics,
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"topics": []string{suite.config.Topics[0]},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 1,
						"topics":             []string{suite.config.Topics[0]},
					},
				},
			},
		},
		{
			Name: "DELETE /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "DELETE /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "PUT /:tenantID should override deleted tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationsAPI() {
	tenantID := uuid.New().String()
	sampleDestinationID := uuid.New().String()
	tests := []APITest{
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with no body JSON",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusBadRequest,
					Body: map[string]interface{}{
						"message": "invalid JSON",
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with empty body JSON",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": map[string]interface{}{
							"topics": "required",
							"type":   "required",
						},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with invalid topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "invalid",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: invalid topics format",
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with invalid topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"invalid"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: invalid topics",
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with invalid config",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"user.created"},
					"config": map[string]interface{}{},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": map[string]interface{}{
							"config.url": "required",
						},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with user-provided ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with duplicate ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusBadRequest,
					Body: map[string]interface{}{
						"message": "destination already exists",
					},
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     sampleDestinationID,
						"type":   "webhook",
						"topics": []string{"*"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"topics": []string{"user.created"},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     sampleDestinationID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     sampleDestinationID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"topics": []string{""},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: invalid topics",
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"config": map[string]interface{}{
						"url": "",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": map[string]interface{}{
							"config.url": "required",
						},
					},
				},
			},
		},
		{
			Name: "DELETE /:tenantID/destinations/:destinationID with invalid destination ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID + "/destinations/" + uuid.New().String(),
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "DELETE /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(1),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationsListAPI() {
	tenantID := uuid.New().String()
	tests := []APITest{
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations type=webhook topics=*",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations type=webhook topics=user.created",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"user.created"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations type=webhook topics=user.created user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"user.created", "user.updated"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=webhook",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=webhook",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=rabbitmq",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=rabbitmq",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=*",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=*",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(1),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=user.created",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=user.created",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=webhook&topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=webhook&topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=rabbitmq&topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=rabbitmq&topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationEnableDisableAPI() {
	tenantID := uuid.New().String()
	sampleDestinationID := uuid.New().String()
	tests := []APITest{
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/enable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/enable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/enable duplicate",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/enable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable duplicate",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTopicsAPI() {
	tests := []APITest{
		{
			Name: "GET /topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/topics",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body:       suite.config.Topics,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationTypesAPI() {
	providerFieldSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"key", "type", "label", "description", "required"},
		"properties": map[string]interface{}{
			"key":         map[string]interface{}{"type": "string"},
			"type":        map[string]interface{}{"type": "string"},
			"label":       map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"required":    map[string]interface{}{"type": "boolean"},
		},
	}

	providerSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"type", "label", "description", "icon", "config_fields", "credential_fields"},
		"properties": map[string]interface{}{
			"type":         map[string]interface{}{"type": "string"},
			"label":        map[string]interface{}{"type": "string"},
			"description":  map[string]interface{}{"type": "string"},
			"icon":         map[string]interface{}{"type": "string"},
			"instructions": map[string]interface{}{"type": "string"},
			"config_fields": map[string]interface{}{
				"type":  "array",
				"items": providerFieldSchema,
			},
			"credential_fields": map[string]interface{}{
				"type":  "array",
				"items": providerFieldSchema,
			},
			"validation": map[string]interface{}{
				"type": "object",
			},
		},
	}

	tests := []APITest{
		{
			Name: "GET /destination-types",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types",
			}),
			Expected: APITestExpectation{
				Validate: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"statusCode": map[string]any{"const": 200},
						"body": map[string]interface{}{
							"type":        "array",
							"items":       providerSchema,
							"minItems":    5,
							"maxItems":    5,
							"uniqueItems": true,
						},
					},
				},
			},
		},
		{
			Name: "GET /destination-types/webhook",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types/webhook",
			}),
			Expected: APITestExpectation{
				Validate: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"statusCode": map[string]any{"const": 200},
						"body":       providerSchema,
					},
				},
			},
		},
		{
			Name: "GET /destination-types/invalid",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types/invalid",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestJWTAuthAPI() {
	// Step 1: Create tenant and get JWT token
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()

	// Create tenant first using admin auth
	createTenantTests := []APITest{
		{
			Name: "PUT /:tenantID to create tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), createTenantTests)

	// Step 2: Get JWT token - need to do this manually since we need to extract the token
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)

	bodyMap := tokenResp.Body.(map[string]interface{})
	token := bodyMap["token"].(string)
	suite.Require().NotEmpty(token)

	// Step 3: Test various endpoints with JWT auth
	jwtTests := []APITest{
		// Test tenant-specific routes with tenantID param
		{
			Name: "GET /:tenantID with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}, token),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "POST /:tenantID/destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},

		// Test tenant-specific routes without tenantID param
		{
			Name: "GET /destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destinations",
			}, token),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(1),
			},
		},
		{
			Name: "POST /destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},

		// Test tenant-agnostic routes with tenantID param
		{
			Name: "GET /:tenantID/destination-types with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destination-types",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID/topics with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/topics",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},

		// Test tenant-agnostic routes without tenantID param
		{
			Name: "GET /destination-types with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /topics with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/topics",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},

		// Test wrong tenantID
		{
			Name: "GET /wrong-tenant-id with JWT should fail",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + uuid.New().String(),
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},

		// Clean up - delete tenant
		{
			Name: "DELETE /:tenantID with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}

	suite.RunAPITests(suite.T(), jwtTests)
}

func makeDestinationListValidator(length int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"statusCode": map[string]any{
				"const": 200,
			},
			"body": map[string]any{
				"type":     "array",
				"minItems": length,
				"maxItems": length,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"type": map[string]any{
							"type": "string",
						},
						"config": map[string]any{
							"type": "object",
						},
						"credentials": map[string]any{
							"type": "object",
						},
					},
					"required": []any{"id", "type", "config", "credentials"},
				},
			},
		},
	}
}

func makeDestinationDisabledValidator(id string, disabled bool) map[string]any {
	var disabledValidator map[string]any
	if disabled {
		disabledValidator = map[string]any{
			"type":      "string",
			"minLength": 1,
		}
	} else {
		disabledValidator = map[string]any{
			"type": "null",
		}
	}
	return map[string]interface{}{
		"properties": map[string]interface{}{
			"statusCode": map[string]interface{}{
				"const": 200,
			},
			"body": map[string]interface{}{
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"const": id,
					},
					"disabled_at": disabledValidator,
				},
			},
		},
	}
}
