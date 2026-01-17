package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/idgen"
)

// parseTime parses a timestamp string (RFC3339 with optional nanoseconds)
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// TestLogAPI tests the Log API endpoints (deliveries, events).
//
// Setup:
//  1. Create a tenant and destination
//  2. Publish 10 events with small delays for distinct timestamps
//
// Test Groups:
//   - deliveries: list, filter, expand
//   - events: list, filter, retrieve
//   - sort_order: sort by time ascending/descending
//   - pagination: paginate through results
func (suite *basicSuite) TestLogAPI() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Generate 10 event IDs with readable numbers and unique prefix
	eventPrefix := idgen.String()[:8]
	eventIDs := make([]string, 10)
	for i := range eventIDs {
		eventIDs[i] = fmt.Sprintf("%s_event_%d", eventPrefix, i+1)
	}

	// Setup: Create tenant and destination
	setupTests := []APITest{
		{
			Name: "create tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusCreated},
			},
		},
		{
			Name: "setup mock server",
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
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "create destination",
			Request: suite.AuthRequest(httpclient.Request{
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
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusCreated},
			},
		},
	}
	suite.RunAPITests(suite.T(), setupTests)

	// Publish 10 events with small delays for distinct timestamps
	for i, eventID := range eventIDs {
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodPOST,
			Path:   "/publish",
			Body: map[string]interface{}{
				"id":                 eventID,
				"tenant_id":          tenantID,
				"topic":              "user.created",
				"eligible_for_retry": true,
				"data":               map[string]interface{}{"index": i},
			},
		}))
		suite.Require().NoError(err)
		suite.Require().Equal(http.StatusAccepted, resp.StatusCode, "failed to publish event %d", i)
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for all deliveries (30s timeout for slow CI environments)
	suite.waitForDeliveries(suite.T(), "/tenants/"+tenantID+"/deliveries", 10, 10*time.Second)

	// =========================================================================
	// Deliveries Tests
	// =========================================================================
	suite.Run("deliveries", func() {
		suite.Run("list all", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 10)

			// Verify structure
			first := data[0].(map[string]interface{})
			suite.NotEmpty(first["id"])
			suite.NotEmpty(first["event"])
			suite.Equal(destinationID, first["destination"])
			suite.NotEmpty(first["status"])
			suite.NotEmpty(first["delivered_at"])
		})

		suite.Run("filter by destination_id", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries?destination_id=" + destinationID,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 10)
		})

		suite.Run("filter by event_id", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries?event_id=" + eventIDs[0],
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 1)
		})

		suite.Run("include=event returns event object without data", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries?include=event&limit=1",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Require().Len(data, 1)

			delivery := data[0].(map[string]interface{})
			event := delivery["event"].(map[string]interface{})
			suite.NotEmpty(event["id"])
			suite.NotEmpty(event["topic"])
			suite.NotEmpty(event["time"])
			suite.Nil(event["data"]) // include=event should NOT include data
		})

		suite.Run("include=event.data returns event object with data", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries?include=event.data&limit=1",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Require().Len(data, 1)

			delivery := data[0].(map[string]interface{})
			event := delivery["event"].(map[string]interface{})
			suite.NotEmpty(event["id"])
			suite.NotNil(event["data"]) // include=event.data SHOULD include data
		})

		suite.Run("include=response_data returns response data", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries?include=response_data&limit=1",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Require().Len(data, 1)

			delivery := data[0].(map[string]interface{})
			suite.NotNil(delivery["response_data"])
		})
	})

	// =========================================================================
	// Events Tests
	// =========================================================================
	suite.Run("events", func() {
		suite.Run("list all", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 10)

			// Verify structure
			first := data[0].(map[string]interface{})
			suite.NotEmpty(first["id"])
			suite.NotEmpty(first["topic"])
			suite.NotEmpty(first["time"])
			suite.NotNil(first["data"])
		})

		suite.Run("filter by topic", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events?topic=user.created",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 10) // All events have topic=user.created
		})

		suite.Run("retrieve single event", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events/" + eventIDs[0],
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			suite.Equal(eventIDs[0], body["id"])
			suite.Equal("user.created", body["topic"])
			suite.NotNil(body["data"])
		})

		suite.Run("retrieve non-existent event returns 404", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events/" + idgen.Event(),
			}))
			suite.Require().NoError(err)
			suite.Equal(http.StatusNotFound, resp.StatusCode)
		})

		suite.Run("filter by start time excludes past events", func() {
			futureTime := url.QueryEscape(time.Now().Add(1 * time.Hour).Format(time.RFC3339))
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events?start=" + futureTime,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 0)
		})
	})

	// =========================================================================
	// Sort Order Tests
	// =========================================================================
	suite.Run("sort_order", func() {
		suite.Run("events desc returns newest first", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events?sort_order=desc",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Require().Len(data, 10)

			for i := 0; i < len(data)-1; i++ {
				curr := parseTime(data[i].(map[string]interface{})["time"].(string))
				next := parseTime(data[i+1].(map[string]interface{})["time"].(string))
				suite.True(curr.After(next) || curr.Equal(next), "events not in descending order at index %d", i)
			}
		})

		suite.Run("events asc returns oldest first", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events?sort_order=asc",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Require().Len(data, 10)

			for i := 0; i < len(data)-1; i++ {
				curr := parseTime(data[i].(map[string]interface{})["time"].(string))
				next := parseTime(data[i+1].(map[string]interface{})["time"].(string))
				suite.True(curr.Before(next) || curr.Equal(next), "events not in ascending order at index %d", i)
			}
		})

		suite.Run("events invalid sort_order returns 422", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/events?sort_order=invalid",
			}))
			suite.Require().NoError(err)
			suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
		})

	})

	// =========================================================================
	// Pagination Tests
	// =========================================================================
	suite.Run("pagination", func() {
		suite.Run("events limit=3 paginates correctly", func() {
			var allEventIDs []string
			nextCursor := ""
			pageCount := 0

			for {
				path := "/tenants/" + tenantID + "/events?limit=3&sort_order=asc"
				if nextCursor != "" {
					path += "&next=" + nextCursor
				}

				resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
					Method: httpclient.MethodGET,
					Path:   path,
				}))
				suite.Require().NoError(err)
				suite.Require().Equal(http.StatusOK, resp.StatusCode)

				body := resp.Body.(map[string]interface{})
				data := body["data"].([]interface{})
				pageCount++

				for _, item := range data {
					event := item.(map[string]interface{})
					allEventIDs = append(allEventIDs, event["id"].(string))
				}

				if next, ok := body["next"].(string); ok && next != "" {
					nextCursor = next
				} else {
					break
				}

				if pageCount > 10 {
					suite.Fail("too many pages")
					break
				}
			}

			suite.Equal(4, pageCount, "expected 4 pages (3+3+3+1)")
			suite.Len(allEventIDs, 10, "should have all 10 events")
		})
	})

	// Cleanup
	cleanupTests := []APITest{
		{
			Name: "cleanup mock server",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destinationID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "cleanup tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
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
				Path:   "/tenants/" + tenantID,
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
				Path:   "/tenants/" + tenantID + "/destinations",
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
	suite.waitForDeliveries(suite.T(), "/tenants/"+tenantID+"/deliveries?event_id="+eventID, 1, 5*time.Second)

	// Get the delivery ID
	deliveriesResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/deliveries?event_id=" + eventID,
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
				Path:   "/tenants/" + tenantID + "/deliveries/" + deliveryID + "/retry",
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
				Path:   "/tenants/" + tenantID + "/deliveries/" + idgen.Delivery() + "/retry",
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
	suite.waitForDeliveries(suite.T(), "/tenants/"+tenantID+"/deliveries?event_id="+eventID, 2, 5*time.Second)

	// Verify we have more deliveries after retry
	verifyTests := []APITest{
		{
			Name: "GET /:tenantID/deliveries?event_id=X - verify retry created new delivery",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/deliveries?event_id=" + eventID,
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
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID + "/disable",
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
				Path:   "/tenants/" + tenantID + "/deliveries/" + deliveryID + "/retry",
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
				Path:   "/tenants/" + tenantID,
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
				Path:   "/tenants/" + tenantID + "/destinations",
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
	suite.waitForDeliveries(suite.T(), "/tenants/"+tenantID+"/deliveries", 1, 5*time.Second)

	// Test legacy endpoints - all should return deprecation headers
	legacyTests := []APITest{
		// GET /:tenantID/destinations/:destinationID/events - legacy list events by destination
		{
			Name: "GET /:tenantID/destinations/:destinationID/events - legacy endpoint",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID + "/events",
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
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID + "/events/" + eventID,
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
				Path:   "/tenants/" + tenantID + "/events/" + eventID + "/deliveries",
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
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationID + "/events/" + eventID + "/retry",
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

// TestAdminLogEndpoints tests the admin-only /events and /deliveries endpoints.
//
// These endpoints allow cross-tenant queries with optional tenant_id filter.
//
// Setup:
//  1. Create two tenants with destinations
//  2. Publish events to each tenant
//  3. Wait for deliveries to complete
//
// Test Cases:
//   - GET /events without auth returns 401
//   - GET /deliveries without auth returns 401
//   - GET /events with JWT returns 401 (admin-only)
//   - GET /deliveries with JWT returns 401 (admin-only)
//   - GET /events with admin key returns all events (cross-tenant)
//   - GET /deliveries with admin key returns all deliveries (cross-tenant)
//   - GET /events?tenant_id=X filters to single tenant
//   - GET /deliveries?tenant_id=X filters to single tenant
func (suite *basicSuite) TestAdminLogEndpoints() {
	tenant1ID := idgen.String()
	tenant2ID := idgen.String()
	destination1ID := idgen.Destination()
	destination2ID := idgen.Destination()
	event1ID := idgen.Event()
	event2ID := idgen.Event()

	// Setup: create two tenants with destinations
	setupTests := []APITest{
		{
			Name: "create tenant1",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenant1ID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusCreated},
			},
		},
		{
			Name: "create tenant2",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenant2ID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusCreated},
			},
		},
		{
			Name: "setup mock server for tenant1",
			Request: httpclient.Request{
				Method:  httpclient.MethodPUT,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations",
				Body: map[string]interface{}{
					"id":   destination1ID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destination1ID),
					},
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "setup mock server for tenant2",
			Request: httpclient.Request{
				Method:  httpclient.MethodPUT,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations",
				Body: map[string]interface{}{
					"id":   destination2ID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destination2ID),
					},
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "create destination for tenant1",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenant1ID + "/destinations",
				Body: map[string]interface{}{
					"id":     destination1ID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destination1ID),
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusCreated},
			},
		},
		{
			Name: "create destination for tenant2",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenant2ID + "/destinations",
				Body: map[string]interface{}{
					"id":     destination2ID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destination2ID),
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusCreated},
			},
		},
		{
			Name: "publish event to tenant1",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"id":        event1ID,
					"tenant_id": tenant1ID,
					"topic":     "user.created",
					"data":      map[string]interface{}{"tenant": "1"},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusAccepted},
			},
		},
		{
			Name: "publish event to tenant2",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"id":        event2ID,
					"tenant_id": tenant2ID,
					"topic":     "user.created",
					"data":      map[string]interface{}{"tenant": "2"},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusAccepted},
			},
		},
	}
	suite.RunAPITests(suite.T(), setupTests)

	// Wait for deliveries for both tenants
	suite.waitForDeliveries(suite.T(), "/tenants/"+tenant1ID+"/deliveries", 1, 5*time.Second)
	suite.waitForDeliveries(suite.T(), "/tenants/"+tenant2ID+"/deliveries", 1, 5*time.Second)

	// Get JWT token for tenant1 to test that JWT auth is rejected on admin endpoints
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenant1ID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)
	bodyMap := tokenResp.Body.(map[string]interface{})
	jwtToken := bodyMap["token"].(string)
	suite.Require().NotEmpty(jwtToken)

	// =========================================================================
	// Auth Tests: verify endpoints require admin API key
	// =========================================================================
	suite.Run("auth", func() {
		suite.Run("GET /events without auth returns 401", func() {
			resp, err := suite.client.Do(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events",
			})
			suite.Require().NoError(err)
			suite.Equal(http.StatusUnauthorized, resp.StatusCode)
		})

		suite.Run("GET /deliveries without auth returns 401", func() {
			resp, err := suite.client.Do(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/deliveries",
			})
			suite.Require().NoError(err)
			suite.Equal(http.StatusUnauthorized, resp.StatusCode)
		})

		suite.Run("GET /events with JWT returns 401 (admin-only)", func() {
			resp, err := suite.client.Do(suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events",
			}, jwtToken))
			suite.Require().NoError(err)
			suite.Equal(http.StatusUnauthorized, resp.StatusCode)
		})

		suite.Run("GET /deliveries with JWT returns 401 (admin-only)", func() {
			resp, err := suite.client.Do(suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/deliveries",
			}, jwtToken))
			suite.Require().NoError(err)
			suite.Equal(http.StatusUnauthorized, resp.StatusCode)
		})
	})

	// =========================================================================
	// Cross-tenant query tests
	// =========================================================================
	suite.Run("cross_tenant", func() {
		suite.Run("GET /events returns events from all tenants", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			// Should have at least 2 events (one from each tenant we created)
			suite.GreaterOrEqual(len(data), 2)

			// Verify we have events from both tenants by checking event IDs
			eventsSeen := map[string]bool{}
			for _, item := range data {
				event := item.(map[string]interface{})
				if id, ok := event["id"].(string); ok {
					eventsSeen[id] = true
				}
			}
			suite.True(eventsSeen[event1ID], "should include tenant1 event")
			suite.True(eventsSeen[event2ID], "should include tenant2 event")
		})

		suite.Run("GET /deliveries returns deliveries from all tenants", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/deliveries?include=event",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			// Should have at least 2 deliveries (one from each tenant we created)
			suite.GreaterOrEqual(len(data), 2)

			// Verify we have deliveries from both tenants by checking event IDs
			eventsSeen := map[string]bool{}
			for _, item := range data {
				delivery := item.(map[string]interface{})
				if event, ok := delivery["event"].(map[string]interface{}); ok {
					if id, ok := event["id"].(string); ok {
						eventsSeen[id] = true
					}
				}
			}
			suite.True(eventsSeen[event1ID], "should include tenant1 delivery")
			suite.True(eventsSeen[event2ID], "should include tenant2 delivery")
		})
	})

	// =========================================================================
	// tenant_id filter tests
	// =========================================================================
	suite.Run("tenant_id_filter", func() {
		suite.Run("GET /events?tenant_id=X filters to single tenant", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenant1ID,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 1)

			// Verify only tenant1 event by ID
			event := data[0].(map[string]interface{})
			suite.Equal(event1ID, event["id"])
		})

		suite.Run("GET /deliveries?tenant_id=X filters to single tenant", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/deliveries?tenant_id=" + tenant2ID + "&include=event",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			data := body["data"].([]interface{})
			suite.Len(data, 1)

			// Verify only tenant2 delivery by event ID
			delivery := data[0].(map[string]interface{})
			event := delivery["event"].(map[string]interface{})
			suite.Equal(event2ID, event["id"])
		})
	})

	// Cleanup
	cleanupTests := []APITest{
		{
			Name: "cleanup mock server tenant1",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destination1ID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "cleanup mock server tenant2",
			Request: httpclient.Request{
				Method:  httpclient.MethodDELETE,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations/" + destination2ID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "cleanup tenant1",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenant1ID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
		{
			Name: "cleanup tenant2",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenant2ID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{StatusCode: http.StatusOK},
			},
		},
	}
	suite.RunAPITests(suite.T(), cleanupTests)
}
