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
							"config": "required",
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
						"message": "validation failed: url is required for webhook destination config",
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
					"config": map[string]interface{}{},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: url is required for webhook destination config",
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
