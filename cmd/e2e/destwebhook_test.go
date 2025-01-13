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
	secret := "testsecret1234567890abcdefghijklmnop"
	newSecret := "testsecret0987654321zyxwvutsrqponm"
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

	// Get initial secret and verify initial state
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

func (suite *basicSuite) TestDestwebhookTenantSecretManagement() {
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()

	// First create tenant and get JWT token
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

	// Get JWT token
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)

	bodyMap := tokenResp.Body.(map[string]interface{})
	token := bodyMap["token"].(string)
	suite.Require().NotEmpty(token)

	// Run tenant-scoped tests
	tests := []APITest{
		{
			Name: "POST /:tenantID/destinations - attempt to create destination with secret (should fail)",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
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
						"data": map[string]interface{}{
							"credentials.secret": "forbidden",
						},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations - create destination without secret",
			Request: suite.AuthJWTRequest(httpclient.Request{
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
		Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to update secret directly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
						"data": map[string]interface{}{
							"credentials.secret": "forbidden",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to set previous_secret directly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
						"data": map[string]interface{}{
							"credentials.previous_secret": "forbidden",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to set previous_secret_invalid_at directly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
						"data": map[string]interface{}{
							"credentials.previous_secret_invalid_at": "forbidden",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID - rotate secret properly",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "GET /:tenantID/destinations/:destinationID - verify rotation worked",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "DELETE /:tenantID to clean up",
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
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}

func (suite *basicSuite) TestDestwebhookAdminSecretManagement() {
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()
	secret := "testsecret1234567890abcdefghijklmnop"
	newSecret := "testsecret0987654321zyxwvutsrqponm"

	// First group: Test all creation flows
	createTests := []APITest{
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
		{
			Name: "POST /:tenantID/destinations - create destination without credentials",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
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
			Name: "GET /:tenantID/destinations/:destinationID - verify auto-generated secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "-1",
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
			Name: "POST /:tenantID/destinations - create destination with secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
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
			Name: "GET /:tenantID/destinations/:destinationID - verify custom secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "POST /:tenantID/destinations - attempt to create with rotate_secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
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
						"data": map[string]interface{}{
							"credentials.rotate_secret": "invalid",
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
			Name: "PATCH /:tenantID/destinations/:destinationID - update secret directly",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "GET /:tenantID/destinations/:destinationID - verify secret updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to set invalid previous_secret_invalid_at format",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
						"data": map[string]interface{}{
							"credentials.previous_secret_invalid_at": "pattern",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to set previous_secret without invalid_at",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to set previous_secret_invalid_at without previous_secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - overrides everything",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "GET /:tenantID/destinations/:destinationID - verify previous_secret set",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - rotate secret as admin",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - attempt to set previous_secret and previous_secret_invalid_at without secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
						"data": map[string]interface{}{
							"credentials.secret": "required",
						},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID - verify rotation worked",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "PATCH /:tenantID/destinations/:destinationID - admin unset previous_secret",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "GET /:tenantID/destinations/:destinationID - verify previous_secret was unset",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID,
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
			Name: "DELETE /:tenantID to clean up",
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
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}
