package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/stretchr/testify/require"
)

// TestingT is an interface wrapper around *testing.T
type TestingT interface {
	Errorf(format string, args ...interface{})
}

func (suite *basicSuite) TestDestwebhookPublish() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
	eventIDs := []string{
		idgen.Event(),
		idgen.Event(),
		idgen.Event(),
		idgen.Event(),
	}
	secret := "testsecret1234567890abcdefghijklmnop"
	newSecret := "testsecret0987654321zyxwvutsrqponm"
	tests := []APITest{
		{
			Name: "PUT /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
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
			Name: "POST /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
					StatusCode: http.StatusAccepted,
				},
			},
		},
		{
			WaitFor: &MockServerPoll{BaseURL: suite.mockServerBaseURL, DestID: sampleDestinationID, MinCount: 1, Timeout: 5 * time.Second},
			Name:    "GET mockserver/destinations/:destinationID/events - verify signature",
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
					StatusCode: http.StatusAccepted,
				},
			},
		},
		{
			WaitFor: &MockServerPoll{BaseURL: suite.mockServerBaseURL, DestID: sampleDestinationID, MinCount: 1, Timeout: 5 * time.Second},
			Name:    "GET mockserver/destinations/:destinationID/events - verify rotated signature",
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
			Name: "PATCH /tenants/:tenantID/destinations - update outpost destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
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
					StatusCode: http.StatusAccepted,
				},
			},
		},
		{
			WaitFor: &MockServerPoll{BaseURL: suite.mockServerBaseURL, DestID: sampleDestinationID, MinCount: 1, Timeout: 5 * time.Second},
			Name:    "GET mockserver/destinations/:destinationID/events - verify new signature",
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
					StatusCode: http.StatusAccepted,
				},
			},
		},
		{
			WaitFor: &MockServerPoll{BaseURL: suite.mockServerBaseURL, DestID: sampleDestinationID, MinCount: 1, Timeout: 5 * time.Second},
			Name:    "GET mockserver/destinations/:destinationID/events - verify signature fails",
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
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Setup tenant
	resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/tenants/" + tenantID,
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusCreated, resp.StatusCode)

	// Create destination without secret
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPOST,
		Path:   "/tenants/" + tenantID + "/destinations",
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

	// Get initial secret and verify initial state
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
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
		Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
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
		Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
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

func (suite *basicSuite) TestDestwebhookTenantSecretManagement() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// First create tenant and get JWT token
	createTenantTests := []APITest{
		{
			Name: "PUT /tenants/:tenantID to create tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), createTenantTests)

	// Get JWT token
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)

	bodyMap := tokenResp.Body.(map[string]interface{})
	token := bodyMap["token"].(string)
	suite.Require().NotEmpty(token)

	// Run tenant-scoped tests
	tests := []APITest{
		{
			Name: "POST /tenants/:tenantID/destinations - attempt to create destination with secret (should fail)",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
					},
					"credentials": map[string]interface{}{
						"secret": "any-secret",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.secret failed forbidden validation",
						},
					},
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations - create destination without secret",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)

	// Get initial secret and verify initial state
	resp, err := suite.client.Do(suite.AuthJWTRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
	}, token))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, resp.StatusCode)

	dest := resp.Body.(map[string]interface{})
	creds := dest["credentials"].(map[string]interface{})
	initialSecret := creds["secret"].(string)
	suite.Require().NotEmpty(initialSecret)
	suite.Require().Nil(creds["previous_secret"])
	suite.Require().Nil(creds["previous_secret_invalid_at"])

	// Continue with permission tests
	permissionTests := []APITest{
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to update secret directly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"secret": "new-secret",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.secret failed forbidden validation",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to set previous_secret directly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"previous_secret": "another-secret",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.previous_secret failed forbidden validation",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to set previous_secret_invalid_at directly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.previous_secret_invalid_at failed forbidden validation",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - rotate secret properly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"rotate_secret": true,
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify rotation worked",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
			}, token),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"credentials"},
							"properties": map[string]interface{}{
								"credentials": map[string]interface{}{
									"type":     "object",
									"required": []interface{}{"secret", "previous_secret", "previous_secret_invalid_at"},
									"properties": map[string]interface{}{
										"secret": map[string]interface{}{
											"type":      "string",
											"minLength": 32,
											"pattern":   "^[a-zA-Z0-9]+$",
										},
										"previous_secret": map[string]interface{}{
											"type":  "string",
											"const": initialSecret,
										},
										"previous_secret_invalid_at": map[string]interface{}{
											"type":    "string",
											"format":  "date-time",
											"pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}",
										},
									},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), permissionTests)

	// Clean up using admin auth
	cleanupTests := []APITest{
		{
			Name: "DELETE /tenants/:tenantID to clean up",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}

func (suite *basicSuite) TestDestwebhookAdminSecretManagement() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	secret := "testsecret1234567890abcdefghijklmnop"
	newSecret := "testsecret0987654321zyxwvutsrqponm"

	// First group: Test all creation flows
	createTests := []APITest{
		{
			Name: "PUT /tenants/:tenantID to create tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations - create destination without credentials",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID + "-1",
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify auto-generated secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID + "-1",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"credentials"},
							"properties": map[string]interface{}{
								"credentials": map[string]interface{}{
									"type":     "object",
									"required": []interface{}{"secret"},
									"properties": map[string]interface{}{
										"secret": map[string]interface{}{
											"type":      "string",
											"minLength": 32,
											"pattern":   "^[a-zA-Z0-9]+$",
										},
									},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations - create destination with secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID, // Use main destinationID for update tests
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify custom secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"credentials": map[string]interface{}{
							"secret": secret,
						},
					},
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations - attempt to create with rotate_secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID + "-3",
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
					},
					"credentials": map[string]interface{}{
						"rotate_secret": true,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.rotate_secret failed invalid validation",
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), createTests)

	updatedPreviousSecret := secret + "_2"
	updatedPreviousSecretInvalidAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	// Second group: Test update flows using the destination with custom secret
	updateTests := []APITest{
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - update secret directly",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"secret": newSecret,
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify secret updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"credentials": map[string]interface{}{
							"secret": newSecret,
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to set invalid previous_secret_invalid_at format",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"secret":                     newSecret,
						"previous_secret":            secret,
						"previous_secret_invalid_at": "invalid-date",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.previous_secret_invalid_at failed pattern validation",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to set previous_secret without invalid_at",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"previous_secret": updatedPreviousSecret,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"credentials": map[string]interface{}{
							"previous_secret": updatedPreviousSecret,
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to set previous_secret_invalid_at without previous_secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"previous_secret_invalid_at": updatedPreviousSecretInvalidAt,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"credentials": map[string]interface{}{
							"previous_secret":            updatedPreviousSecret,
							"previous_secret_invalid_at": updatedPreviousSecretInvalidAt,
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - overrides everything",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify previous_secret set",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"credentials"},
							"properties": map[string]interface{}{
								"credentials": map[string]interface{}{
									"type":     "object",
									"required": []interface{}{"secret", "previous_secret", "previous_secret_invalid_at"},
									"properties": map[string]interface{}{
										"secret": map[string]interface{}{
											"type":      "string",
											"minLength": 32,
											"pattern":   "^[a-zA-Z0-9]+$",
										},
										"previous_secret": map[string]interface{}{
											"type":  "string",
											"const": secret,
										},
										"previous_secret_invalid_at": map[string]interface{}{
											"type":    "string",
											"format":  "date-time",
											"pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}",
										},
									},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - rotate secret as admin",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"rotate_secret": true,
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
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - attempt to set previous_secret and previous_secret_invalid_at without secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"secret":                     "",
						"previous_secret":            secret,
						"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"credentials.secret is required",
						},
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify rotation worked",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"credentials"},
							"properties": map[string]interface{}{
								"credentials": map[string]interface{}{
									"type":     "object",
									"required": []interface{}{"secret", "previous_secret", "previous_secret_invalid_at"},
									"properties": map[string]interface{}{
										"secret": map[string]interface{}{
											"type":      "string",
											"minLength": 32,
											"pattern":   "^[a-zA-Z0-9]+$",
										},
										"previous_secret": map[string]interface{}{
											"type":  "string",
											"const": newSecret,
										},
										"previous_secret_invalid_at": map[string]interface{}{
											"type":    "string",
											"format":  "date-time",
											"pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}",
										},
									},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID - admin unset previous_secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
				Body: map[string]interface{}{
					"credentials": map[string]interface{}{
						"previous_secret":            "",
						"previous_secret_invalid_at": "",
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID - verify previous_secret was unset",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"credentials"},
							"properties": map[string]interface{}{
								"credentials": map[string]interface{}{
									"type":     "object",
									"required": []interface{}{"secret"},
									"properties": map[string]interface{}{
										"secret": map[string]interface{}{
											"type":      "string",
											"minLength": 32,
											"pattern":   "^[a-zA-Z0-9]+$",
										},
									},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), updateTests)

	// Clean up
	cleanupTests := []APITest{
		{
			Name: "DELETE /tenants/:tenantID to clean up",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}

func (suite *basicSuite) TestDestwebhookFilter() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventMatchID := idgen.Event()
	eventNoMatchID := idgen.Event()
	secret := "testsecret1234567890abcdefghijklmnop"

	tests := []APITest{
		{
			Name: "PUT /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
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
					"id":   destinationID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
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
			Name: "POST /tenants/:tenantID/destinations - create destination with filter using $gte operator",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"filter": map[string]interface{}{
						"data": map[string]interface{}{
							"amount": map[string]interface{}{
								"$gte": 100,
							},
						},
					},
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
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
			Name: "POST /publish - event matches filter (amount >= 100)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false,
					"data": map[string]any{
						"event_id": eventMatchID,
						"amount":   150, // >= 100, matches filter
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		},
		{
			WaitFor: &MockServerPoll{BaseURL: suite.mockServerBaseURL, DestID: destinationID, MinCount: 1, Timeout: 5 * time.Second},
			Name:    "GET mockserver - verify event was delivered",
			Request: httpclient.Request{
				Method:  httpclient.MethodGET,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: []interface{}{
						map[string]interface{}{
							"success":  true,
							"verified": true,
							"payload": map[string]interface{}{
								"event_id": eventMatchID,
								"amount":   float64(150),
							},
						},
					},
				},
			},
		},
		{
			Name: "DELETE mockserver events - clear for next test",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "POST /publish - event does NOT match filter (amount < 100)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false,
					"data": map[string]any{
						"event_id": eventNoMatchID,
						"amount":   50, // < 100, doesn't match filter
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		},
		{
			Delay: 500 * time.Millisecond, // Can't poll for absence, but 500ms is enough for processing
			Name:  "GET mockserver - verify event was NOT delivered (filter mismatch)",
			Request: httpclient.Request{
				Method:  httpclient.MethodGET,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID + "/events",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body:       []interface{}{}, // empty - no events delivered
				},
			},
		},
		{
			Name: "DELETE /tenants/:tenantID to clean up",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

// TestDeliveryRetry tests that failed deliveries are scheduled for retry via RSMQ.
// This exercises the RSMQ Lua scripts that are known to fail with Dragonfly.
func (suite *basicSuite) TestDeliveryRetry() {
	t := suite.T()
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	secret := "testsecret1234567890abcdefghijklmnop"

	// Setup: create tenant
	resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/tenants/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Setup: configure mock server destination
	resp, err = suite.client.Do(httpclient.Request{
		Method:  httpclient.MethodPUT,
		BaseURL: suite.mockServerBaseURL,
		Path:    "/destinations",
		Body: map[string]interface{}{
			"id":   destinationID,
			"type": "webhook",
			"config": map[string]interface{}{
				"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
			},
			"credentials": map[string]interface{}{
				"secret": secret,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Setup: create destination in outpost
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPOST,
		Path:   "/tenants/" + tenantID + "/destinations",
		Body: map[string]interface{}{
			"id":     destinationID,
			"type":   "webhook",
			"topics": "*",
			"config": map[string]interface{}{
				"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
			},
			"credentials": map[string]interface{}{
				"secret": secret,
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Publish event with retry enabled and should_err to force failure
	// This will trigger the RSMQ retry scheduler
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPOST,
		Path:   "/publish",
		Body: map[string]interface{}{
			"tenant_id":          tenantID,
			"topic":              "user.created",
			"eligible_for_retry": true, // Enable retry - exercises RSMQ!
			"metadata": map[string]any{
				"should_err": "true", // Force delivery to fail
			},
			"data": map[string]any{
				"test": "retry",
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Wait for retry to be scheduled and attempted (poll for at least 2 delivery attempts)
	suite.waitForMockServerEvents(t, destinationID, 2, 5*time.Second)

	// Wait for attempts to be logged, then verify attempt_number increments on automated retry
	suite.waitForAttempts(t, "/tenants/"+tenantID+"/attempts", 2, 5*time.Second)

	atmResponse, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/attempts?dir=asc",
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, atmResponse.StatusCode)

	atmBody := atmResponse.Body.(map[string]interface{})
	atmModels := atmBody["models"].([]interface{})
	require.GreaterOrEqual(t, len(atmModels), 2, "should have at least 2 attempts from automated retry")

	// Sorted asc by time: attempt_number should increment (0, 1, 2, ...)
	for i, m := range atmModels {
		attempt := m.(map[string]interface{})
		require.Equal(t, float64(i), attempt["attempt_number"],
			"attempt %d should have attempt_number=%d (automated retry increments)", i, i)
	}

	// Cleanup
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodDELETE,
		Path:   "/tenants/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
