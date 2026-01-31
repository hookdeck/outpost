package e2e_test

import (
	"fmt"
	"net/http"
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

// TestLogAPI tests the Log API endpoints (attempts, events).
//
// Setup:
//  1. Create a tenant and destination
//  2. Publish 10 events with small delays for distinct timestamps
//
// Test Groups:
//   - attempts: list, filter, expand
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

	// Publish 10 events with explicit timestamps (1 second apart)
	baseTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	for i, eventID := range eventIDs {
		eventTime := baseTime.Add(time.Duration(i) * time.Second)
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodPOST,
			Path:   "/publish",
			Body: map[string]interface{}{
				"id":                 eventID,
				"tenant_id":          tenantID,
				"topic":              "user.created",
				"eligible_for_retry": true,
				"time":               eventTime.Format(time.RFC3339Nano),
				"data":               map[string]interface{}{"index": i},
			},
		}))
		suite.Require().NoError(err)
		suite.Require().Equal(http.StatusAccepted, resp.StatusCode, "failed to publish event %d", i)
	}

	// Wait for all attempts (30s timeout for slow CI environments)
	suite.waitForAttempts(suite.T(), "/attempts?tenant_id="+tenantID, 10, 10*time.Second)

	// =========================================================================
	// Attempts Tests
	// =========================================================================
	suite.Run("attempts", func() {
		suite.Run("list all", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/attempts?tenant_id=" + tenantID,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Len(models, 10)

			// Verify structure
			first := models[0].(map[string]interface{})
			suite.NotEmpty(first["id"])
			suite.NotEmpty(first["event"])
			suite.Equal(destinationID, first["destination"])
			suite.NotEmpty(first["status"])
			suite.NotEmpty(first["delivered_at"])
			suite.Equal(float64(0), first["attempt_number"], "attempt_number should be present and equal to 0 for first attempt")
		})

		suite.Run("filter by destination_id", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/attempts?tenant_id=" + tenantID + "&destination_id=" + destinationID,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Len(models, 10)
		})

		suite.Run("filter by event_id", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/attempts?tenant_id=" + tenantID + "&event_id=" + eventIDs[0],
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Len(models, 1)
		})

		suite.Run("include=event returns event object without data", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/attempts?tenant_id=" + tenantID + "&include=event&limit=1",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Require().Len(models, 1)

			attempt := models[0].(map[string]interface{})
			event := attempt["event"].(map[string]interface{})
			suite.NotEmpty(event["id"])
			suite.NotEmpty(event["topic"])
			suite.NotEmpty(event["time"])
			suite.Nil(event["data"]) // include=event should NOT include data
		})

		suite.Run("include=event.data returns event object with data", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/attempts?tenant_id=" + tenantID + "&include=event.data&limit=1",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Require().Len(models, 1)

			attempt := models[0].(map[string]interface{})
			event := attempt["event"].(map[string]interface{})
			suite.NotEmpty(event["id"])
			suite.NotNil(event["data"]) // include=event.data SHOULD include data
		})

		suite.Run("include=response_data returns response data", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/attempts?tenant_id=" + tenantID + "&include=response_data&limit=1",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Require().Len(models, 1)

			attempt := models[0].(map[string]interface{})
			suite.NotNil(attempt["response_data"])
		})
	})

	// =========================================================================
	// Events Tests
	// =========================================================================
	suite.Run("events", func() {
		suite.Run("list all", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Len(models, 10)

			// Verify structure
			first := models[0].(map[string]interface{})
			suite.NotEmpty(first["id"])
			suite.NotEmpty(first["topic"])
			suite.NotEmpty(first["time"])
			suite.NotNil(first["data"])
		})

		suite.Run("filter by topic", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID + "&topic=user.created",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Len(models, 10) // All events have topic=user.created
		})

		suite.Run("retrieve single event", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events/" + eventIDs[0],
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
				Path:   "/events/" + idgen.Event(),
			}))
			suite.Require().NoError(err)
			suite.Equal(http.StatusNotFound, resp.StatusCode)
		})

		suite.Run("filter by time[gte] excludes past events", func() {
			futureTime := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID + "&time[gte]=" + futureTime,
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Len(models, 0)
		})
	})

	// =========================================================================
	// Sort Order Tests
	// =========================================================================
	suite.Run("sort_order", func() {
		suite.Run("events desc returns newest first", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID + "&dir=desc",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Require().Len(models, 10)

			for i := 0; i < len(models)-1; i++ {
				curr := parseTime(models[i].(map[string]interface{})["time"].(string))
				next := parseTime(models[i+1].(map[string]interface{})["time"].(string))
				suite.True(curr.After(next) || curr.Equal(next), "events not in descending order at index %d", i)
			}
		})

		suite.Run("events asc returns oldest first", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID + "&dir=asc",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Require().Len(models, 10)

			for i := 0; i < len(models)-1; i++ {
				curr := parseTime(models[i].(map[string]interface{})["time"].(string))
				next := parseTime(models[i+1].(map[string]interface{})["time"].(string))
				suite.True(curr.Before(next) || curr.Equal(next), "events not in ascending order at index %d", i)
			}
		})

		suite.Run("events invalid dir returns 422", func() {
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID + "&dir=invalid",
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
				path := "/events?tenant_id=" + tenantID + "&limit=3&dir=asc"
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
				models := body["models"].([]interface{})
				pageCount++

				for _, item := range models {
					event := item.(map[string]interface{})
					allEventIDs = append(allEventIDs, event["id"].(string))
				}

				pagination, _ := body["pagination"].(map[string]interface{})
				if next, ok := pagination["next"].(string); ok && next != "" {
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

		suite.Run("cursor pagination with time filter", func() {
			// Get all events to establish a time window
			resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/events?tenant_id=" + tenantID + "&dir=asc&limit=10",
			}))
			suite.Require().NoError(err)
			suite.Require().Equal(http.StatusOK, resp.StatusCode)

			body := resp.Body.(map[string]interface{})
			models := body["models"].([]interface{})
			suite.Require().Len(models, 10)

			// Use the 3rd and 7th events to create a time window
			event3 := models[2].(map[string]interface{})
			event7 := models[6].(map[string]interface{})
			timeGTE := event3["time"].(string)
			timeLTE := event7["time"].(string)
			timeGTEParsed := parseTime(timeGTE)
			timeLTEParsed := parseTime(timeLTE)

			// Paginate within the time window with limit=2
			var windowEvents []map[string]interface{}
			nextCursor := ""
			pageCount := 0

			for {
				path := "/events?tenant_id=" + tenantID + "&dir=asc&limit=2"
				path += "&time[gte]=" + timeGTE + "&time[lte]=" + timeLTE
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
				windowModels := body["models"].([]interface{})
				pageCount++

				for _, item := range windowModels {
					event := item.(map[string]interface{})
					windowEvents = append(windowEvents, event)
				}

				pagination, _ := body["pagination"].(map[string]interface{})
				if next, ok := pagination["next"].(string); ok && next != "" {
					nextCursor = next
				} else {
					break
				}

				if pageCount > 10 {
					suite.Fail("too many pages")
					break
				}
			}

			// Verify time filter worked: should have fewer events than total
			suite.Greater(len(windowEvents), 0, "should have some events in window")
			suite.Less(len(windowEvents), 10, "time filter should exclude some events")

			// Verify pagination worked: multiple pages needed
			suite.Greater(pageCount, 1, "should require multiple pages")

			// Verify all returned events are within the time window
			for _, event := range windowEvents {
				eventTime := parseTime(event["time"].(string))
				suite.True(!eventTime.Before(timeGTEParsed), "event time %v should be >= %v", eventTime, timeGTEParsed)
				suite.True(!eventTime.After(timeLTEParsed), "event time %v should be <= %v", eventTime, timeLTEParsed)
			}
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
//  5. Wait for attempt to fail, then fetch the attempt ID
//  6. Update mock server to SUCCEED (return 200)
//
// Test Cases:
//   - POST /retry - Successful retry returns 202 Accepted
//   - POST /retry (non-existent event) - Returns 404
//   - Verify retry created new attempt - Event now has 2+ attempts
//   - POST /retry (disabled destination) - Returns 400
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
						"status": 500, // Fail attempts
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

	// Wait for attempt to complete (and fail)
	suite.waitForAttempts(suite.T(), "/attempts?tenant_id="+tenantID+"&event_id="+eventID, 1, 5*time.Second)

	// Get the attempt ID
	attemptsResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/attempts?tenant_id=" + tenantID + "&event_id=" + eventID,
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, attemptsResp.StatusCode)

	body := attemptsResp.Body.(map[string]interface{})
	models := body["models"].([]interface{})
	suite.Require().NotEmpty(models, "should have at least one attempt")
	firstAttempt := models[0].(map[string]interface{})

	// Verify first attempt has attempt_number=0
	suite.Equal(float64(0), firstAttempt["attempt_number"], "first attempt should have attempt_number=0")

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
		// POST /retry - successful retry
		{
			Name: "POST /retry - retry event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/retry",
				Body: map[string]interface{}{
					"event_id":       eventID,
					"destination_id": destinationID,
				},
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
		// POST /retry - non-existent event
		{
			Name: "POST /retry - not found",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/retry",
				Body: map[string]interface{}{
					"event_id":       idgen.Event(),
					"destination_id": destinationID,
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), retryTests)

	// Wait for retry attempt to complete
	suite.waitForAttempts(suite.T(), "/attempts?tenant_id="+tenantID+"&event_id="+eventID, 2, 5*time.Second)

	// Verify retry created a new attempt with incremented attempt_number
	verifyResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/attempts?tenant_id=" + tenantID + "&event_id=" + eventID + "&dir=asc",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, verifyResp.StatusCode)

	verifyBody := verifyResp.Body.(map[string]interface{})
	verifyModels := verifyBody["models"].([]interface{})
	suite.Require().Len(verifyModels, 2, "should have original + retry attempt")

	// Both attempts should have attempt_number=0 (manual retry resets to 0)
	for _, m := range verifyModels {
		atm := m.(map[string]interface{})
		suite.Equal(float64(0), atm["attempt_number"], "attempt should have attempt_number=0")
	}

	// Verify we have one manual=true (retry) and one manual=false (original)
	manualCount := 0
	for _, m := range verifyModels {
		atm := m.(map[string]interface{})
		if manual, ok := atm["manual"].(bool); ok && manual {
			manualCount++
		}
	}
	suite.Equal(1, manualCount, "should have exactly one manual retry attempt")

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
			Name: "POST /retry - disabled destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/retry",
				Body: map[string]interface{}{
					"event_id":       eventID,
					"destination_id": destinationID,
				},
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
