package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/idgen"
)

// TestLogAPI tests the new Log API endpoints (deliveries, events).
//
// Setup:
//  1. Create a tenant
//  2. Configure mock webhook server to accept deliveries
//  3. Create a destination pointing to the mock server
//  4. Publish an event and wait for delivery to complete
//
// Test Cases:
//   - GET /:tenantID/deliveries - List all deliveries with proper response structure
//   - GET /:tenantID/deliveries?destination_id=X - Filter deliveries by destination
//   - GET /:tenantID/deliveries?event_id=X - Filter deliveries by event
//   - GET /:tenantID/deliveries?expand=event - Expand event summary (without data)
//   - GET /:tenantID/deliveries?expand=event.data - Expand full event with payload data
//   - GET /:tenantID/events/:eventID - Retrieve a single event with full details
//   - GET /:tenantID/events/:eventID (non-existent) - Returns 404
func (suite *basicSuite) TestLogAPI() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := idgen.Event()

	// Setup: Create tenant, destination, and publish an event
	setupTests := []APITest{
		{
			Name: "PUT /:tenantID - create tenant",
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
			Name: "PUT mockserver/destinations - setup mock",
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
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations - create destination",
			Request: suite.AuthRequest(httpclient.Request{
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
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /publish - publish event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"id":                 eventID,
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": true,
					"data": map[string]interface{}{
						"user_id": "123",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), setupTests)

	// Wait for delivery to complete
	time.Sleep(2 * time.Second)

	// Test the new Log API endpoints
	logAPITests := []APITest{
		// GET /:tenantID/deliveries - list deliveries
		{
			Name: "GET /:tenantID/deliveries - list all deliveries",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/deliveries",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"data"},
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
									"items": map[string]interface{}{
										"type":     "object",
										"required": []interface{}{"id", "event", "destination", "status", "delivered_at"},
										"properties": map[string]interface{}{
											"id":           map[string]interface{}{"type": "string"},
											"event":        map[string]interface{}{"type": "string"}, // Event ID when not expanded
											"destination":  map[string]interface{}{"const": destinationID},
											"status":       map[string]interface{}{"type": "string"},
											"delivered_at": map[string]interface{}{"type": "string"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/deliveries?destination_id=X - filter by destination
		{
			Name: "GET /:tenantID/deliveries?destination_id=X - filter by destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/deliveries?destination_id=" + destinationID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"data"},
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
								},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/deliveries?event_id=X - filter by event
		{
			Name: "GET /:tenantID/deliveries?event_id=X - filter by event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/deliveries?event_id=" + eventID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"data"},
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
								},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/deliveries?expand=event - expand event (without data)
		{
			Name: "GET /:tenantID/deliveries?expand=event - expand event summary",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/deliveries?expand=event",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
									"items": map[string]interface{}{
										"type":     "object",
										"required": []interface{}{"event"},
										"properties": map[string]interface{}{
											"event": map[string]interface{}{
												"type":     "object",
												"required": []interface{}{"id", "topic", "time"},
												"properties": map[string]interface{}{
													"id":    map[string]interface{}{"type": "string"},
													"topic": map[string]interface{}{"type": "string"},
													"time":  map[string]interface{}{"type": "string"},
												},
												// expand=event should NOT include data
												"not": map[string]interface{}{
													"required": []interface{}{"data"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/deliveries?expand=event.data - expand full event with data
		{
			Name: "GET /:tenantID/deliveries?expand=event.data - expand full event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/deliveries?expand=event.data",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
									"items": map[string]interface{}{
										"type":     "object",
										"required": []interface{}{"event"},
										"properties": map[string]interface{}{
											"event": map[string]interface{}{
												"type":     "object",
												"required": []interface{}{"id", "topic", "time", "data"},
												"properties": map[string]interface{}{
													"id":    map[string]interface{}{"const": eventID},
													"topic": map[string]interface{}{"const": "user.created"},
													"time":  map[string]interface{}{"type": "string"},
													"data": map[string]interface{}{
														"type": "object",
														"properties": map[string]interface{}{
															"user_id": map[string]interface{}{"const": "123"},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/events/:eventID - retrieve single event
		{
			Name: "GET /:tenantID/events/:eventID - retrieve event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/events/" + eventID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"id", "topic", "time", "data"},
							"properties": map[string]interface{}{
								"id":    map[string]interface{}{"const": eventID},
								"topic": map[string]interface{}{"const": "user.created"},
								"data": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"user_id": map[string]interface{}{"const": "123"},
									},
								},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/events/:eventID - non-existent event
		{
			Name: "GET /:tenantID/events/:eventID - not found",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/events/" + idgen.Event(),
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), logAPITests)

	// Cleanup
	cleanupTests := []APITest{
		{
			Name: "DELETE mockserver/destinations/:destinationID",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
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
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}

// TestRetryAPI tests the retry endpoint.
//
// Setup:
//  1. Create a tenant
//  2. Configure mock webhook server to FAIL (return 500)
//  3. Create a destination pointing to the mock server
//  4. Publish an event with eligible_for_retry=false (fails once, no auto-retry)
//  5. Wait for delivery to fail, then fetch the delivery ID
//  6. Update mock server to SUCCEED (return 200)
//
// Test Cases:
//   - POST /:tenantID/deliveries/:deliveryID/retry - Successful retry returns 202 Accepted
//   - POST /:tenantID/deliveries/:deliveryID/retry (non-existent) - Returns 404
//   - Verify retry created new delivery - Event now has 2+ deliveries
//   - POST /:tenantID/deliveries/:deliveryID/retry (disabled destination) - Returns 400
func (suite *basicSuite) TestRetryAPI() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := idgen.Event()

	// Setup: create tenant, destination with failing webhook, and publish event
	setupTests := []APITest{
		{
			Name: "PUT /:tenantID - create tenant",
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
			Name: "PUT mockserver/destinations - setup mock to fail",
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
					"response": map[string]interface{}{
						"status": 500, // Fail deliveries
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
			Name: "POST /:tenantID/destinations - create destination",
			Request: suite.AuthRequest(httpclient.Request{
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
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /publish - publish event (will fail)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"id":                 eventID,
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false, // Disable auto-retry
					"data": map[string]interface{}{
						"user_id": "456",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), setupTests)

	// Wait for delivery to complete (and fail)
	time.Sleep(2 * time.Second)

	// Get the delivery ID
	deliveriesResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/deliveries?event_id=" + eventID,
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, deliveriesResp.StatusCode)

	body := deliveriesResp.Body.(map[string]interface{})
	data := body["data"].([]interface{})
	suite.Require().NotEmpty(data, "should have at least one delivery")
	firstDelivery := data[0].(map[string]interface{})
	deliveryID := firstDelivery["id"].(string)

	// Update mock to succeed for retry
	updateMockTests := []APITest{
		{
			Name: "PUT mockserver/destinations - setup mock to succeed",
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
					"response": map[string]interface{}{
						"status": 200, // Now succeed
					},
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), updateMockTests)

	// Test retry endpoint
	retryTests := []APITest{
		// POST /:tenantID/deliveries/:deliveryID/retry - successful retry
		{
			Name: "POST /:tenantID/deliveries/:deliveryID/retry - retry delivery",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/deliveries/" + deliveryID + "/retry",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
					Body: map[string]interface{}{
						"success": true,
					},
				},
			},
		},
		// POST /:tenantID/deliveries/:deliveryID/retry - non-existent delivery
		{
			Name: "POST /:tenantID/deliveries/:deliveryID/retry - not found",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/deliveries/" + idgen.Delivery() + "/retry",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), retryTests)

	// Wait for retry delivery to complete
	time.Sleep(2 * time.Second)

	// Verify we have more deliveries after retry
	verifyTests := []APITest{
		{
			Name: "GET /:tenantID/deliveries?event_id=X - verify retry created new delivery",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/deliveries?event_id=" + eventID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 2, // Original + retry
								},
							},
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), verifyTests)

	// Test retry on disabled destination
	disableTests := []APITest{
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "POST /:tenantID/deliveries/:deliveryID/retry - disabled destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/deliveries/" + deliveryID + "/retry",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusBadRequest,
					Body: map[string]interface{}{
						"message": "Destination is disabled",
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), disableTests)

	// Cleanup
	cleanupTests := []APITest{
		{
			Name: "DELETE mockserver/destinations/:destinationID",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
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
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}

// TestLegacyLogAPI tests the deprecated legacy endpoints for backward compatibility.
// All legacy endpoints return "Deprecation: true" header to signal migration.
//
// Setup:
//  1. Create a tenant
//  2. Configure mock webhook server to accept deliveries
//  3. Create a destination pointing to the mock server
//  4. Publish an event and wait for delivery to complete
//
// Test Cases:
//   - GET /:tenantID/destinations/:destID/events - Legacy list events (returns {data, count})
//   - GET /:tenantID/destinations/:destID/events/:eventID - Legacy retrieve event
//   - GET /:tenantID/events/:eventID/deliveries - Legacy list deliveries (returns bare array, not {data})
//   - POST /:tenantID/destinations/:destID/events/:eventID/retry - Legacy retry endpoint
//
// All responses include:
//   - Deprecation: true header
//   - X-Deprecated-Message header with migration guidance
func (suite *basicSuite) TestLegacyLogAPI() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := idgen.Event()

	// Setup
	setupTests := []APITest{
		{
			Name: "PUT /:tenantID - create tenant",
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
			Name: "PUT mockserver/destinations - setup mock",
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
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations - create destination",
			Request: suite.AuthRequest(httpclient.Request{
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
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /publish - publish event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"id":                 eventID,
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": true,
					"data": map[string]interface{}{
						"user_id": "789",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), setupTests)

	// Wait for delivery
	time.Sleep(2 * time.Second)

	// Test legacy endpoints - all should return deprecation headers
	legacyTests := []APITest{
		// GET /:tenantID/destinations/:destinationID/events - legacy list events by destination
		{
			Name: "GET /:tenantID/destinations/:destinationID/events - legacy endpoint",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "/events",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"headers": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"Deprecation": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"const": "true",
									},
								},
							},
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"data", "count"},
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
								},
								"count": map[string]interface{}{"type": "number"},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/destinations/:destinationID/events/:eventID - legacy retrieve event
		{
			Name: "GET /:tenantID/destinations/:destinationID/events/:eventID - legacy endpoint",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "/events/" + eventID,
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"headers": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"Deprecation": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"const": "true",
									},
								},
							},
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"id", "topic"},
							"properties": map[string]interface{}{
								"id":    map[string]interface{}{"const": eventID},
								"topic": map[string]interface{}{"const": "user.created"},
							},
						},
					},
				},
			},
		},
		// GET /:tenantID/events/:eventID/deliveries - legacy list deliveries by event
		{
			Name: "GET /:tenantID/events/:eventID/deliveries - legacy endpoint (returns bare array)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/events/" + eventID + "/deliveries",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 200},
						"headers": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"Deprecation": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"const": "true",
									},
								},
							},
						},
						// Legacy endpoint returns bare array, not {data: [...]}
						"body": map[string]interface{}{
							"type":     "array",
							"minItems": 1,
							"items": map[string]interface{}{
								"type":     "object",
								"required": []interface{}{"id", "status", "delivered_at"},
								"properties": map[string]interface{}{
									"id":           map[string]interface{}{"type": "string"},
									"status":       map[string]interface{}{"type": "string"},
									"delivered_at": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
		// POST /:tenantID/destinations/:destinationID/events/:eventID/retry - legacy retry
		{
			Name: "POST /:tenantID/destinations/:destinationID/events/:eventID/retry - legacy endpoint",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "/events/" + eventID + "/retry",
			}),
			Expected: APITestExpectation{
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{"const": 202},
						"headers": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"Deprecation": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"const": "true",
									},
								},
							},
						},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"success": map[string]interface{}{"const": true},
							},
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), legacyTests)

	// Cleanup
	cleanupTests := []APITest{
		{
			Name: "DELETE mockserver/destinations/:destinationID",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
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
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}
