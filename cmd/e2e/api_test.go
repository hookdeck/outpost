package e2e_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{
							"type": "string",
						},
						"timestamp": map[string]interface{}{
							"type": "string",
						},
						"workers": map[string]interface{}{
							"type": "object",
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantsAPI() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
	tests := []APITest{
		{
			Name: "GET /tenants/:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID without tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
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
			Name: "GET /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
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
			Name: "PUT /tenants/:tenantID again",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
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
			Name: "POST /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "GET /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
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
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
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
			Name: "GET /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
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
			Name: "DELETE /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
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
			Name: "DELETE /tenants/:tenantID",
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
		{
			Name: "GET /tenants/:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID should override deleted tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
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
		// Metadata tests
		{
			Name: "PUT /tenants/:tenantID with metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"environment": "production",
						"team":        "platform",
						"region":      "us-east-1",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"environment": "production",
							"team":        "platform",
							"region":      "us-east-1",
						},
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID retrieves metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"environment": "production",
							"team":        "platform",
							"region":      "us-east-1",
						},
					},
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID replaces metadata (full replacement)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"team":  "engineering",
						"owner": "alice",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"team":  "engineering",
							"owner": "alice",
							// Note: environment and region are gone (full replacement)
						},
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID verifies metadata was replaced",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"team":  "engineering",
							"owner": "alice",
						},
					},
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID without metadata clears it",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID verifies metadata is nil",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
						// metadata field should not be present (omitempty)
					},
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID - Create new tenant with metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + idgen.String(),
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"stage": "development",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"metadata": map[string]interface{}{
							"stage": "development",
						},
					},
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID with metadata value auto-converted (number to string)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + idgen.String(),
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"count":   42,
						"enabled": true,
						"ratio":   3.14,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"metadata": map[string]interface{}{
							"count":   "42",
							"enabled": "true",
							"ratio":   "3.14",
						},
					},
				},
			},
		},
		{
			Name: "PUT /tenants/:tenantID with empty body (no metadata)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + idgen.String(),
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantAPIInvalidJSON() {
	t := suite.T()
	tenantID := idgen.String()
	baseURL := fmt.Sprintf("http://localhost:%d/api/v1", suite.config.APIPort)

	// Create tenant with malformed JSON (send raw bytes)
	jsonBody := []byte(`{"metadata": invalid json}`)
	req, err := http.NewRequest(httpclient.MethodPUT, baseURL+"/tenants/"+tenantID, bytes.NewReader(jsonBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+suite.config.APIKey)

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "Malformed JSON should return 400")
}

func (suite *basicSuite) TestListTenantsAPI() {
	t := suite.T()

	if !suite.hasRediSearch {
		// Skip full test on backends without verified RediSearch support
		// Note: Some backends (like Dragonfly) may pass the FT._LIST probe
		// but not fully support FT.SEARCH, so we just skip the test
		t.Skip("skipping ListTenant test - RediSearch not verified for this backend")
	}

	// With RediSearch, test full list functionality
	// Create some tenants first, with 1 second apart to ensure distinct timestamps
	// (Dragonfly's FT.SEARCH SORTBY + LIMIT has issues with duplicate sort keys)
	tenantIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		tenantIDs[i] = idgen.String()
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodPUT,
			Path:   "/tenants/" + tenantIDs[i],
		}))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	// Test list without parameters
	t.Run("list all tenants", func(t *testing.T) {
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants",
		}))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, ok := resp.Body.(map[string]interface{})
		require.True(t, ok, "response should be a map")
		models, ok := body["models"].([]interface{})
		require.True(t, ok, "models should be an array")
		assert.GreaterOrEqual(t, len(models), 3, "should have at least 3 tenants")
	})

	// Test list with limit
	t.Run("list with limit", func(t *testing.T) {
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=2",
		}))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, ok := resp.Body.(map[string]interface{})
		require.True(t, ok, "response should be a map")
		models, ok := body["models"].([]interface{})
		require.True(t, ok, "models should be an array")
		assert.Equal(t, 2, len(models), "should have exactly 2 tenants")
	})

	// Test invalid limit
	t.Run("invalid limit returns 400", func(t *testing.T) {
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=notanumber",
		}))
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// Test forward pagination
	t.Run("forward pagination with next cursor", func(t *testing.T) {
		// Get first page
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=2",
		}))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, ok := resp.Body.(map[string]interface{})
		require.True(t, ok, "response should be a map")
		models, ok := body["models"].([]interface{})
		require.True(t, ok, "models should be an array")
		assert.Equal(t, 2, len(models), "page 1 should have 2 tenants")

		pagination, _ := body["pagination"].(map[string]interface{})
		next, _ := pagination["next"].(string)
		require.NotEmpty(t, next, "should have next cursor")

		// Get second page using next cursor
		resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=2&next=" + next,
		}))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, ok = resp.Body.(map[string]interface{})
		require.True(t, ok, "response should be a map")
		models, ok = body["models"].([]interface{})
		require.True(t, ok, "models should be an array")
		assert.GreaterOrEqual(t, len(models), 1, "page 2 should have at least 1 tenant")

		pagination, _ = body["pagination"].(map[string]interface{})
		prev, _ := pagination["prev"].(string)
		assert.NotEmpty(t, prev, "page 2 should have prev cursor")
	})

	// Test prev cursor returns newer items (keyset pagination)
	t.Run("backward pagination with prev cursor", func(t *testing.T) {
		// Get first page
		resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=2",
		}))
		require.NoError(t, err)
		body, ok := resp.Body.(map[string]interface{})
		require.True(t, ok)

		pagination, _ := body["pagination"].(map[string]interface{})
		next, _ := pagination["next"].(string)
		require.NotEmpty(t, next, "should have next cursor")

		// Go to page 2
		resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=2&next=" + next,
		}))
		require.NoError(t, err)
		body, ok = resp.Body.(map[string]interface{})
		require.True(t, ok)

		pagination, _ = body["pagination"].(map[string]interface{})
		prev, _ := pagination["prev"].(string)
		require.NotEmpty(t, prev, "page 2 should have prev cursor")

		// Using prev cursor returns items with newer timestamps (keyset pagination)
		// This is NOT the same as "going back to page 1" in offset pagination
		resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodGET,
			Path:   "/tenants?limit=2&prev=" + prev,
		}))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, ok = resp.Body.(map[string]interface{})
		require.True(t, ok, "response should be a map")
		models, ok := body["models"].([]interface{})
		require.True(t, ok, "models should be an array")
		assert.NotEmpty(t, models, "prev cursor should return items")
	})

	// Cleanup
	for _, id := range tenantIDs {
		_, _ = suite.client.Do(suite.AuthRequest(httpclient.Request{
			Method: httpclient.MethodDELETE,
			Path:   "/" + id,
		}))
	}
}

func (suite *basicSuite) TestDestinationsAPI() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
	destinationWithMetadataID := idgen.Destination()
	destinationWithFilterID := idgen.Destination()
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
			Name: "GET /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations with no body JSON",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations with empty body JSON",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"type is required",
							"topics is required",
						},
					},
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations with invalid topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations with invalid topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations with invalid config",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
						"message": "validation error",
						"data": []interface{}{
							"config.url is required",
						},
					},
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations with user-provided ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations with delivery_metadata and metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationWithMetadataID,
					"type":   "webhook",
					"topics": []string{"user.created"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
					"delivery_metadata": map[string]interface{}{
						"X-App-ID":  "test-app",
						"X-Version": "1.0",
					},
					"metadata": map[string]interface{}{
						"environment": "test",
						"team":        "platform",
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID with delivery_metadata and metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithMetadataID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",
							"X-Version": "1.0",
						},
						"metadata": map[string]interface{}{
							"environment": "test",
							"team":        "platform",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID update delivery_metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithMetadataID,
				Body: map[string]interface{}{
					"delivery_metadata": map[string]interface{}{
						"X-Version": "2.0",       // Overwrite existing value (was "1.0")
						"X-Region":  "us-east-1", // Add new key
					},
					// Note: X-App-ID not included, should be preserved from original
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",  // PRESERVED: Not in PATCH request
							"X-Version": "2.0",       // OVERWRITTEN: Updated from "1.0"
							"X-Region":  "us-east-1", // NEW: Added by PATCH request
						},
						"metadata": map[string]interface{}{
							"environment": "test",
							"team":        "platform",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID update metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithMetadataID,
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"team":   "engineering", // Overwrite existing value (was "platform")
						"region": "us",          // Add new key
					},
					// Note: environment not included, should be preserved from original
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",
							"X-Version": "2.0",
							"X-Region":  "us-east-1",
						},
						"metadata": map[string]interface{}{
							"environment": "test",        // PRESERVED: Not in PATCH request
							"team":        "engineering", // OVERWRITTEN: Updated from "platform"
							"region":      "us",          // NEW: Added by PATCH request
						},
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID verify merged fields",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithMetadataID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						// Verify delivery_metadata merge behavior persists:
						// - Original: {"X-App-ID": "test-app", "X-Version": "1.0"}
						// - After PATCH 1: {"X-Version": "2.0", "X-Region": "us-east-1"}
						// - Result: Preserved X-App-ID, overwrote X-Version, added X-Region
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",
							"X-Version": "2.0",
							"X-Region":  "us-east-1",
						},
						// Verify metadata merge behavior persists:
						// - Original: {"environment": "test", "team": "platform"}
						// - After PATCH 2: {"team": "engineering", "region": "us"}
						// - Result: Preserved environment, overwrote team, added region
						"metadata": map[string]interface{}{
							"environment": "test",
							"team":        "engineering",
							"region":      "us",
						},
					},
				},
			},
		},
		// Filter tests: create, update, and unset
		{
			Name: "POST /tenants/:tenantID/destinations with filter",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationWithFilterID,
					"type":   "webhook",
					"topics": []string{"user.created"},
					"filter": map[string]interface{}{
						"data": map[string]interface{}{
							"amount": map[string]interface{}{
								"$gte": 100,
							},
						},
					},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"id": destinationWithFilterID,
						"filter": map[string]interface{}{
							"data": map[string]interface{}{
								"amount": map[string]interface{}{
									"$gte": float64(100),
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID verify filter",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithFilterID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": destinationWithFilterID,
						"filter": map[string]interface{}{
							"data": map[string]interface{}{
								"amount": map[string]interface{}{
									"$gte": float64(100),
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID update filter",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithFilterID,
				Body: map[string]interface{}{
					"filter": map[string]interface{}{
						"data": map[string]interface{}{
							"status": "active",
						},
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": destinationWithFilterID,
						"filter": map[string]interface{}{
							"data": map[string]interface{}{
								"status": "active",
							},
						},
					},
				},
			},
		},
		{
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID unset filter with empty object",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithFilterID,
				Body: map[string]interface{}{
					"filter": map[string]interface{}{},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID verify filter unset",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + destinationWithFilterID,
			}),
			Expected: APITestExpectation{
				// Use JSON schema validation to verify filter is NOT present
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"id", "type", "topics"},
							"not": map[string]interface{}{
								"required": []interface{}{"filter"},
							},
						},
					},
				},
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations with duplicate ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "GET /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(4), // 3 original + 1 with filter
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
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
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
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
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
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
			Name: "PATCH /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"config": map[string]interface{}{
						"url": "",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": []interface{}{
							"config.url is required",
						},
					},
				},
			},
		},
		{
			Name: "DELETE /tenants/:tenantID/destinations/:destinationID with invalid destination ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID + "/destinations/" + idgen.Destination(),
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "DELETE /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3), // 4 - 1 deleted = 3
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations with metadata auto-conversion",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
					"metadata": map[string]interface{}{
						"priority": 10,
						"enabled":  true,
						"version":  1.5,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"metadata": map[string]interface{}{
							"priority": "10",
							"enabled":  "true",
							"version":  "1.5",
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestEntityUpdatedAt() {
	t := suite.T()
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Create tenant and verify timestamps in PUT response directly
	resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/tenants/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	body := resp.Body.(map[string]interface{})
	require.NotNil(t, body["created_at"], "created_at should be present")
	require.NotNil(t, body["updated_at"], "updated_at should be present")

	tenantCreatedAt := body["created_at"].(string)
	tenantUpdatedAt := body["updated_at"].(string)
	// On creation, created_at and updated_at should be very close (within 1 second)
	createdTime, err := time.Parse(time.RFC3339Nano, tenantCreatedAt)
	require.NoError(t, err)
	updatedTime, err := time.Parse(time.RFC3339Nano, tenantUpdatedAt)
	require.NoError(t, err)
	require.WithinDuration(t, createdTime, updatedTime, time.Second, "created_at and updated_at should be close on creation")

	// Wait to ensure different timestamp (Unix timestamps have second precision)
	time.Sleep(1100 * time.Millisecond)

	// Update tenant
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/tenants/" + tenantID,
		Body: map[string]interface{}{
			"metadata": map[string]interface{}{
				"env": "production",
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Get tenant again and verify updated_at changed but created_at didn't
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body = resp.Body.(map[string]interface{})
	newTenantCreatedAt := body["created_at"].(string)
	newTenantUpdatedAt := body["updated_at"].(string)

	// Parse timestamps to compare actual times (format may differ between responses)
	newCreatedTime, err := time.Parse(time.RFC3339Nano, newTenantCreatedAt)
	require.NoError(t, err)
	newUpdatedTime, err := time.Parse(time.RFC3339Nano, newTenantUpdatedAt)
	require.NoError(t, err)

	require.Equal(t, createdTime.Unix(), newCreatedTime.Unix(), "created_at should not change")
	require.NotEqual(t, updatedTime.Unix(), newUpdatedTime.Unix(), "updated_at should change")
	require.True(t, newUpdatedTime.After(updatedTime), "updated_at should be newer")

	// Create destination and verify timestamps in POST response directly
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPOST,
		Path:   "/tenants/" + tenantID + "/destinations",
		Body: map[string]interface{}{
			"id":     destinationID,
			"type":   "webhook",
			"topics": []string{"*"},
			"config": map[string]interface{}{
				"url": "http://host.docker.internal:4444",
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	body = resp.Body.(map[string]interface{})
	require.NotNil(t, body["created_at"], "created_at should be present")
	require.NotNil(t, body["updated_at"], "updated_at should be present")

	destCreatedAt := body["created_at"].(string)
	destUpdatedAt := body["updated_at"].(string)
	// On creation, created_at and updated_at should be very close (within 1 second)
	createdTime, err = time.Parse(time.RFC3339Nano, destCreatedAt)
	require.NoError(t, err)
	updatedTime, err = time.Parse(time.RFC3339Nano, destUpdatedAt)
	require.NoError(t, err)
	require.WithinDuration(t, createdTime, updatedTime, time.Second, "created_at and updated_at should be close on creation")

	// Wait to ensure different timestamp (Unix timestamps have second precision)
	time.Sleep(1100 * time.Millisecond)

	// Update destination
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPATCH,
		Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
		Body: map[string]interface{}{
			"topics": []string{"user.created"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Get destination again and verify updated_at changed but created_at didn't
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/destinations/" + destinationID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body = resp.Body.(map[string]interface{})
	newDestCreatedAt := body["created_at"].(string)
	newDestUpdatedAt := body["updated_at"].(string)

	// Parse timestamps to compare actual times (format may differ between responses)
	newDestCreatedTime, err := time.Parse(time.RFC3339Nano, newDestCreatedAt)
	require.NoError(t, err)
	newDestUpdatedTime, err := time.Parse(time.RFC3339Nano, newDestUpdatedAt)
	require.NoError(t, err)

	require.Equal(t, createdTime.Unix(), newDestCreatedTime.Unix(), "created_at should not change")
	require.NotEqual(t, updatedTime.Unix(), newDestUpdatedTime.Unix(), "updated_at should change")
	require.True(t, newDestUpdatedTime.After(updatedTime), "updated_at should be newer")
}

func (suite *basicSuite) TestDestinationsListAPI() {
	tenantID := idgen.String()
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
			Name: "POST /tenants/:tenantID/destinations type=webhook topics=*",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations type=webhook topics=user.created",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "POST /tenants/:tenantID/destinations type=webhook topics=user.created user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "GET /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?type=webhook",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?type=webhook",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?type=rabbitmq",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?type=rabbitmq",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?topics=*",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?topics=*",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(1),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?topics=user.created",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?topics=user.created",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?type=webhook&topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?type=webhook&topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations?type=rabbitmq&topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations?type=rabbitmq&topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationEnableDisableAPI() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
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
			Name: "POST /tenants/:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
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
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /tenants/:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "PUT /tenants/:tenantID/destinations/:destinationID/enable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID + "/enable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /tenants/:tenantID/destinations/:destinationID/enable duplicate",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID + "/enable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /tenants/:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "PUT /tenants/:tenantID/destinations/:destinationID/disable duplicate",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTopicsAPI() {
	tenantID := idgen.String()
	tests := []APITest{
		{
			Name: "PUT /tenants/:tenantID - Create tenant",
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
			Name: "GET /tenants/:tenantID/topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/topics",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body:       suite.config.Topics,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationTypesAPI() {
	providerFieldSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"key", "type", "label", "description", "required"},
		"properties": map[string]interface{}{
			"key":         map[string]interface{}{"type": "string"},
			"type":        map[string]interface{}{"type": "string"},
			"label":       map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"required":    map[string]interface{}{"type": "boolean"},
		},
	}

	providerSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"type", "label", "description", "icon", "config_fields", "credential_fields"},
		"properties": map[string]interface{}{
			"type":         map[string]interface{}{"type": "string"},
			"label":        map[string]interface{}{"type": "string"},
			"description":  map[string]interface{}{"type": "string"},
			"icon":         map[string]interface{}{"type": "string"},
			"instructions": map[string]interface{}{"type": "string"},
			"config_fields": map[string]interface{}{
				"type":  "array",
				"items": providerFieldSchema,
			},
			"credential_fields": map[string]interface{}{
				"type":  "array",
				"items": providerFieldSchema,
			},
			"validation": map[string]interface{}{
				"type": "object",
			},
		},
	}

	tenantID := idgen.String()
	tests := []APITest{
		{
			Name: "PUT /tenants/:tenantID - Create tenant",
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
			Name: "GET /tenants/:tenantID/destination-types",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destination-types",
			}),
			Expected: APITestExpectation{
				Validate: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"statusCode": map[string]any{"const": 200},
						"body": map[string]interface{}{
							"type":        "array",
							"items":       providerSchema,
							"minItems":    8,
							"maxItems":    8,
							"uniqueItems": true,
						},
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destination-types/webhook",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destination-types/webhook",
			}),
			Expected: APITestExpectation{
				Validate: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"statusCode": map[string]any{"const": 200},
						"body":       providerSchema,
					},
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destination-types/invalid",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destination-types/invalid",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantScopedAPI() {
	// Step 1: Create tenant and get JWT token
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Create tenant first using admin auth
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

	// Step 2: Get JWT token - need to do this manually since we need to extract the token
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)

	bodyMap := tokenResp.Body.(map[string]interface{})
	token := bodyMap["token"].(string)
	suite.Require().NotEmpty(token)

	// Verify tenant_id is returned and matches the requested tenant
	returnedTenantID := bodyMap["tenant_id"].(string)
	suite.Require().Equal(tenantID, returnedTenantID, "tenant_id in token response should match the requested tenant")

	// Step 3: Test various endpoints with JWT auth
	jwtTests := []APITest{
		// Test tenant-specific routes with tenantID param
		{
			Name: "GET /tenants/:tenantID with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destinations",
			}, token),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "POST /tenants/:tenantID/destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/tenants/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},

		// Test tenant routes with JWT auth
		{
			Name: "GET /tenants/:tenantID/destination-types with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/destination-types",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /tenants/:tenantID/topics with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/topics",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},

		// Test wrong tenantID
		{
			Name: "GET /tenants/wrong-tenant-id with JWT should fail",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + idgen.String(),
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},

		// Clean up - delete tenant
		{
			Name: "DELETE /tenants/:tenantID with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/tenants/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}

	suite.RunAPITests(suite.T(), jwtTests)
}

func (suite *basicSuite) TestAdminOnlyRoutesRejectJWT() {
	// Step 1: Create tenant and get JWT token
	tenantID := idgen.String()

	// Create tenant first using admin auth
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

	// Step 2: Get JWT token
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/tenants/" + tenantID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)

	bodyMap := tokenResp.Body.(map[string]interface{})
	token := bodyMap["token"].(string)
	suite.Require().NotEmpty(token)

	// Step 3: Test admin-only routes with JWT auth should be rejected
	adminOnlyTests := []APITest{
		// PUT /tenants/:id is admin-only (create/update tenant)
		{
			Name: "PUT /tenants/:tenantID with JWT should return 401",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/tenants/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		// GET /tenants/:id/token is admin-only (retrieve token)
		{
			Name: "GET /tenants/:tenantID/token with JWT should return 401",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/token",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		// GET /tenants/:id/portal is admin-only (retrieve portal redirect)
		{
			Name: "GET /tenants/:tenantID/portal with JWT should return 401",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants/" + tenantID + "/portal",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		// GET /tenants (list) is admin-only
		{
			Name: "GET /tenants with JWT should return 401",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/tenants",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		// POST /publish is admin-only
		{
			Name: "POST /publish with JWT should return 401",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id": tenantID,
					"topic":     "user.created",
					"data":      map[string]interface{}{"test": "data"},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
	}

	suite.RunAPITests(suite.T(), adminOnlyTests)

	// Cleanup: delete tenant using admin auth
	cleanupTests := []APITest{
		{
			Name: "DELETE /tenants/:tenantID cleanup",
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
