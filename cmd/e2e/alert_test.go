package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (suite *basicSuite) TestConsecutiveFailuresAlert() {
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()
	secret := "testsecret1234567890abcdefghijklmnop"

	tests := []APITest{
		{
			Name: "PUT /:tenantID - Create tenant",
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
			Name: "POST /:tenantID/destinations",
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
	}
	suite.RunAPITests(suite.T(), tests)

	// Add 20 event publish requests that will fail
	tests = []APITest{}
	for i := 0; i < 20; i++ {
		tests = append(tests, APITest{
			Name: fmt.Sprintf("POST /publish - Publish event %d", i+1),
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false,
					"metadata": map[string]any{
						"meta":       "data",
						"should_err": "true",
					},
					"data": map[string]any{
						"index": i,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		})
	}
	suite.RunAPITests(suite.T(), tests)

	// Add final check for destination disabled state
	tests = []APITest{}
	tests = append(tests, APITest{
		Delay: time.Second / 2,
		Name:  "GET /:tenantID/destinations/:destinationID - Check disabled",
		Request: suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/" + tenantID + "/destinations/" + destinationID,
		}),
		Expected: APITestExpectation{
			Validate: makeDestinationDisabledValidator(destinationID, true),
		},
	})
	suite.RunAPITests(suite.T(), tests)

	// Assert alerts were received
	alerts := suite.alertServer.GetAlertsForDestination(destinationID)
	require.Len(suite.T(), alerts, 4, "should have 4 alerts")

	expectedCounts := []int{10, 14, 18, 20}
	for i, alert := range alerts {
		assert.Equal(suite.T(), fmt.Sprintf("Bearer %s", suite.config.APIKey), alert.AuthHeader, "auth header should match")
		assert.Equal(suite.T(), expectedCounts[i], alert.Alert.Data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
	}
}

func (suite *basicSuite) TestConsecutiveFailuresAlertReset() {
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()
	secret := "testsecret1234567890abcdefghijklmnop"

	// Setup phase - same as before
	tests := []APITest{
		{
			Name: "PUT /:tenantID - Create tenant",
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
			Name: "POST /:tenantID/destinations",
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
	}
	suite.RunAPITests(suite.T(), tests)

	// First batch - 14 failures
	tests = []APITest{}
	for i := 0; i < 14; i++ {
		tests = append(tests, APITest{
			Name: fmt.Sprintf("POST /publish - Publish failing event %d", i+1),
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false,
					"metadata": map[string]any{
						"meta":       "data",
						"should_err": "true",
					},
					"data": map[string]any{
						"index": i,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		})
	}

	// One successful delivery
	tests = append(tests, APITest{
		Delay: time.Second,
		Name:  "POST /publish - Publish successful event",
		Request: suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodPOST,
			Path:   "/publish",
			Body: map[string]interface{}{
				"tenant_id":          tenantID,
				"topic":              "user.created",
				"eligible_for_retry": false,
				"metadata": map[string]any{
					"meta":       "data",
					"should_err": "false",
				},
				"data": map[string]any{
					"success": true,
				},
			},
		}),
		Expected: APITestExpectation{
			Match: &httpclient.Response{
				StatusCode: http.StatusAccepted,
			},
		},
	})

	// Second batch - 14 more failures
	for i := 0; i < 14; i++ {
		tests = append(tests, APITest{
			Name: fmt.Sprintf("POST /publish - Publish failing event %d (second batch)", i+1),
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": false,
					"metadata": map[string]any{
						"meta":       "data",
						"should_err": "true",
					},
					"data": map[string]any{
						"index": i,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		})
	}
	suite.RunAPITests(suite.T(), tests)

	// Add final check for destination disabled state
	tests = []APITest{}
	tests = append(tests, APITest{
		Delay: time.Second / 2,
		Name:  "GET /:tenantID/destinations/:destinationID - Check disabled",
		Request: suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/" + tenantID + "/destinations/" + destinationID,
		}),
		Expected: APITestExpectation{
			Validate: makeDestinationDisabledValidator(destinationID, false),
		},
	})
	suite.RunAPITests(suite.T(), tests)

	// Assert alerts were received
	alerts := suite.alertServer.GetAlertsForDestination(destinationID)
	require.Len(suite.T(), alerts, 4, "should have 4 alerts")

	// First batch should have alerts at 10, 14
	// Second batch should have alerts at 10, 14 (after reset)
	expectedCounts := []int{10, 14, 10, 14}
	for i, alert := range alerts {
		assert.Equal(suite.T(), fmt.Sprintf("Bearer %s", suite.config.APIKey), alert.AuthHeader, "auth header should match")
		assert.Equal(suite.T(), expectedCounts[i], alert.Alert.Data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
	}
}
