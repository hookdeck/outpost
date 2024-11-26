package e2e_test

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
)

func (suite *basicSuite) TestPublishAPI() {
	tenantID := uuid.New().String()
	sampleDestinationID := uuid.New().String()
	sampleEventDataID := uuid.New().String()
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
				BaseURL: "http://localhost:5555",
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
						"url": "http://localhost:5555/webhook/" + sampleDestinationID,
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
						"event_id": sampleEventDataID,
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
				BaseURL: "http://localhost:5555",
				Path:    "/destinations/" + sampleDestinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: []interface{}{
						map[string]interface{}{
							"event_id": sampleEventDataID,
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}
