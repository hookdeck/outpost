package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
)

func (suite *basicSuite) TestPublishAPI() {
	suite.T().Parallel()
	tenantID := uuid.New().String()
	sampleDestinationID := uuid.New().String()
	eventIDs := []string{uuid.New().String(), uuid.New().String()}
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
			Name: "PUT mockserver/destinations",
			Request: httpclient.Request{
				Method:  httpclient.MethodPUT,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
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
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, sampleDestinationID),
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
			Name: "POST /publish",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false,
					"metadata": map[string]any{
						"meta": "data",
					},
					"data": map[string]any{
						"event_id": eventIDs[0],
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Delay: 1 * time.Second,
			Name:  "GET mockserver/destinations/:destinationID/events",
			Request: httpclient.Request{
				Method:  httpclient.MethodGET,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + sampleDestinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: []interface{}{
						map[string]interface{}{
							"success": true,
							"payload": map[string]interface{}{
								"event_id": eventIDs[0],
							},
						},
					},
				},
			},
		},
		{
			Name: "POST /publish with should_err metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": true,
					"metadata": map[string]any{
						"meta":       "data",
						"should_err": "true",
					},
					"data": map[string]any{
						"event_id": eventIDs[1],
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Delay: 10 * time.Second, // retries - 1s + 2s + 4s
			Name:  "GET mockserver/destinations/:destinationID/events",
			Request: httpclient.Request{
				Method:  httpclient.MethodGET,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + sampleDestinationID + "/events",
			},
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": http.StatusOK,
						},
						"body": map[string]interface{}{
							"type":     "array",
							"minItems": 5, // 1 initial success, 1 second error, 3 retry errors
							"maxItems": 5,
						},
					},
				},
			},
		},
		{
			Name: "DELETE mockserver/destinations/:destinationID",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + sampleDestinationID,
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
