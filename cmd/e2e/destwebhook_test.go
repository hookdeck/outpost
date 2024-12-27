package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
)

// TestingT is an interface wrapper around *testing.T
type TestingT interface {
	Errorf(format string, args ...interface{})
}

func (suite *basicSuite) TestDestwebhookPublish() {
	tenantID := uuid.New().String()
	sampleDestinationID := uuid.New().String()
	eventIDs := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}
	secret := "test-secret-1"
	newSecret := "test-secret-2"
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
					"id":   sampleDestinationID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, sampleDestinationID),
					},
					"credentials": map[string]interface{}{
						"secret": secret,
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
					"credentials": map[string]interface{}{
						"secret": secret,
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
			Name:  "GET mockserver/destinations/:destinationID/events - verify signature",
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
							"success":  true,
							"verified": true,
							"payload": map[string]interface{}{
								"event_id": eventIDs[0],
							},
						},
					},
				},
			},
		},
		{
			Name: "DELETE mockserver/destinations/:destinationID/events - clear events",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + sampleDestinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "PUT mockserver/destinations - manual secret rotation",
			Request: httpclient.Request{
				Method:  httpclient.MethodPUT,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations",
				Body: map[string]interface{}{
					"id":   sampleDestinationID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, sampleDestinationID),
					},
					"credentials": map[string]interface{}{
						"secret":                     newSecret,
						"previous_secret":            secret,
						"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
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
			Name: "POST /publish - after manual rotation",
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
			Delay: 1 * time.Second,
			Name:  "GET mockserver/destinations/:destinationID/events - verify rotated signature",
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
							"success":  true,
							"verified": true,
							"payload": map[string]interface{}{
								"event_id": eventIDs[1],
							},
						},
					},
				},
			},
		},
		{
			Name: "DELETE mockserver/destinations/:destinationID/events - clear events again",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + sampleDestinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations - update outpost destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, sampleDestinationID),
					},
					"credentials": map[string]interface{}{
						"secret":                     newSecret,
						"previous_secret":            secret,
						"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
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
			Name: "POST /publish - after outpost update",
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
						"event_id": eventIDs[2],
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
			Name:  "GET mockserver/destinations/:destinationID/events - verify new signature",
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
							"success":  true,
							"verified": true,
							"payload": map[string]interface{}{
								"event_id": eventIDs[2],
							},
						},
					},
				},
			},
		},
		{
			Name: "DELETE mockserver/destinations/:destinationID/events - clear events before wrong secret test",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + sampleDestinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "PUT mockserver/destinations - update with wrong secret",
			Request: httpclient.Request{
				Method:  httpclient.MethodPUT,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations",
				Body: map[string]interface{}{
					"id":   sampleDestinationID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, sampleDestinationID),
					},
					"credentials": map[string]interface{}{
						"secret": "wrong-secret",
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
			Name: "POST /publish - with wrong secret",
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
						"event_id": eventIDs[3],
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
			Name:  "GET mockserver/destinations/:destinationID/events - verify signature fails",
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
							"success":  true,
							"verified": false,
							"payload": map[string]interface{}{
								"event_id": eventIDs[3],
							},
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

func (suite *basicSuite) TestDestwebhookSecretRotation() {
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()

	// Setup tenant
	resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/" + tenantID,
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusCreated, resp.StatusCode)

	// Create destination without secret
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPOST,
		Path:   "/" + tenantID + "/destinations",
		Body: map[string]interface{}{
			"id":     destinationID,
			"type":   "webhook",
			"topics": "*",
			"config": map[string]interface{}{
				"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
			},
		},
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusCreated, resp.StatusCode)

	// Get destination and verify initial state
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/destinations/" + destinationID,
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, resp.StatusCode)

	dest := resp.Body.(map[string]interface{})
	creds, ok := dest["credentials"].(map[string]interface{})
	suite.Require().True(ok)
	suite.Require().NotEmpty(creds["secret"])
	suite.Require().Nil(creds["previous_secret"])
	suite.Require().Nil(creds["previous_secret_invalid_at"])

	initialSecret := creds["secret"].(string)

	// Rotate secret
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPATCH,
		Path:   "/" + tenantID + "/destinations/" + destinationID,
		Body: map[string]interface{}{
			"credentials": map[string]interface{}{
				"rotate_secret": true,
			},
		},
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, resp.StatusCode)

	// Get destination and verify rotated state
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/destinations/" + destinationID,
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, resp.StatusCode)

	dest = resp.Body.(map[string]interface{})
	creds, ok = dest["credentials"].(map[string]interface{})
	suite.Require().True(ok)
	suite.Require().NotEmpty(creds["secret"])
	suite.Require().NotEmpty(creds["previous_secret"])
	suite.Require().NotEmpty(creds["previous_secret_invalid_at"])
	suite.Require().Equal(initialSecret, creds["previous_secret"])
	suite.Require().NotEqual(initialSecret, creds["secret"])
}
