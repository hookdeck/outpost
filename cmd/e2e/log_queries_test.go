package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
)

// parseTime parses a timestamp string (RFC3339 with optional nanoseconds).
// Panics if the string cannot be parsed (caught by the test framework as a failure).
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			panic(fmt.Sprintf("parseTime: failed to parse %q: %v", s, err))
		}
	}
	return t
}

// logQuerySetup holds shared state for log query tests.
type logQuerySetup struct {
	tenantID      string
	destinationID string
	eventIDs      []string
	baseTime      time.Time
}

func (s *basicSuite) setupLogQueryData() logQuerySetup {
	s.T().Helper()

	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")

	// Generate 10 event IDs with readable prefix
	eventPrefix := idgen.String()[:8]
	eventIDs := make([]string, 10)
	for i := range eventIDs {
		eventIDs[i] = fmt.Sprintf("%s_event_%d", eventPrefix, i+1)
	}

	// Publish 10 events with explicit timestamps (1 second apart)
	baseTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	for i, eventID := range eventIDs {
		eventTime := baseTime.Add(time.Duration(i) * time.Second)
		s.publish(tenant.ID, "user.created", map[string]any{
			"index": i,
		}, withEventID(eventID), withTime(eventTime))
	}

	// Wait for all attempts
	s.waitForNewAttempts(tenant.ID, 10)

	return logQuerySetup{
		tenantID:      tenant.ID,
		destinationID: dest.ID,
		eventIDs:      eventIDs,
		baseTime:      baseTime,
	}
}

func (s *basicSuite) TestLogQueries_Attempts() {
	setup := s.setupLogQueryData()

	s.Run("list all", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+setup.tenantID), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Len(resp.Models, 10)

		first := resp.Models[0]
		s.NotEmpty(first["id"])
		s.NotEmpty(first["event"])
		s.Equal(setup.destinationID, first["destination"])
		s.NotEmpty(first["status"])
		s.NotEmpty(first["delivered_at"])
		s.Equal(float64(0), first["attempt_number"])
	})

	s.Run("filter by destination_id", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+setup.tenantID+"&destination_id="+setup.destinationID), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Len(resp.Models, 10)
	})

	s.Run("filter by event_id", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+setup.tenantID+"&event_id="+setup.eventIDs[0]), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Len(resp.Models, 1)
	})

	s.Run("include=event returns event object without data", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+setup.tenantID+"&include=event&limit=1"), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Require().Len(resp.Models, 1)

		event := resp.Models[0]["event"].(map[string]any)
		s.NotEmpty(event["id"])
		s.NotEmpty(event["topic"])
		s.NotEmpty(event["time"])
		s.Nil(event["data"]) // include=event should NOT include data
	})

	s.Run("include=event.data returns event object with data", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+setup.tenantID+"&include=event.data&limit=1"), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Require().Len(resp.Models, 1)

		event := resp.Models[0]["event"].(map[string]any)
		s.NotEmpty(event["id"])
		s.NotNil(event["data"]) // include=event.data SHOULD include data
	})

	s.Run("include=response_data returns response data", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+setup.tenantID+"&include=response_data&limit=1"), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Require().Len(resp.Models, 1)
		s.NotNil(resp.Models[0]["response_data"])
	})
}

func (s *basicSuite) TestLogQueries_Events() {
	setup := s.setupLogQueryData()

	s.Run("list all", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/events?tenant_id="+setup.tenantID), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Len(resp.Models, 10)

		first := resp.Models[0]
		s.NotEmpty(first["id"])
		s.NotEmpty(first["topic"])
		s.NotEmpty(first["time"])
		s.NotNil(first["data"])
	})

	s.Run("filter by topic", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/events?tenant_id="+setup.tenantID+"&topic=user.created"), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Len(resp.Models, 10)
	})

	s.Run("retrieve single event", func() {
		var resp map[string]any
		status := s.doJSON(http.MethodGet, s.apiURL("/events/"+setup.eventIDs[0]), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Equal(setup.eventIDs[0], resp["id"])
		s.Equal("user.created", resp["topic"])
		s.NotNil(resp["data"])
	})

	s.Run("filter by time[gte] excludes past events", func() {
		futureTime := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/events?tenant_id="+setup.tenantID+"&time[gte]="+futureTime), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Len(resp.Models, 0)
	})
}

func (s *basicSuite) TestLogQueries_SortOrder() {
	setup := s.setupLogQueryData()

	s.Run("events desc returns newest first", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/events?tenant_id="+setup.tenantID+"&dir=desc"), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Require().Len(resp.Models, 10)

		for i := 0; i < len(resp.Models)-1; i++ {
			curr := parseTime(resp.Models[i]["time"].(string))
			next := parseTime(resp.Models[i+1]["time"].(string))
			s.True(curr.After(next) || curr.Equal(next), "events not in descending order at index %d", i)
		}
	})

	s.Run("events asc returns oldest first", func() {
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/events?tenant_id="+setup.tenantID+"&dir=asc"), nil, &resp)
		s.Require().Equal(http.StatusOK, status)
		s.Require().Len(resp.Models, 10)

		for i := 0; i < len(resp.Models)-1; i++ {
			curr := parseTime(resp.Models[i]["time"].(string))
			next := parseTime(resp.Models[i+1]["time"].(string))
			s.True(curr.Before(next) || curr.Equal(next), "events not in ascending order at index %d", i)
		}
	})
}

func (s *basicSuite) TestLogQueries_Pagination() {
	setup := s.setupLogQueryData()

	s.Run("events limit=3 paginates correctly", func() {
		var allEventIDs []string
		nextCursor := ""
		pageCount := 0

		for {
			path := "/events?tenant_id=" + setup.tenantID + "&limit=3&dir=asc"
			if nextCursor != "" {
				path += "&next=" + nextCursor
			}

			var resp struct {
				Models     []map[string]any `json:"models"`
				Pagination map[string]any   `json:"pagination"`
			}
			status := s.doJSON(http.MethodGet, s.apiURL(path), nil, &resp)
			s.Require().Equal(http.StatusOK, status)
			pageCount++

			for _, event := range resp.Models {
				allEventIDs = append(allEventIDs, event["id"].(string))
			}

			if next, ok := resp.Pagination["next"].(string); ok && next != "" {
				nextCursor = next
			} else {
				break
			}

			if pageCount > 10 {
				s.Fail("too many pages")
				break
			}
		}

		s.Equal(4, pageCount, "expected 4 pages (3+3+3+1)")
		s.Len(allEventIDs, 10, "should have all 10 events")
	})

	s.Run("cursor pagination with time filter", func() {
		// Get all events to establish a time window
		var allResp struct {
			Models []map[string]any `json:"models"`
		}
		status := s.doJSON(http.MethodGet, s.apiURL("/events?tenant_id="+setup.tenantID+"&dir=asc&limit=10"), nil, &allResp)
		s.Require().Equal(http.StatusOK, status)
		s.Require().Len(allResp.Models, 10)

		// Use the 3rd and 7th events to create a time window
		timeGTE := allResp.Models[2]["time"].(string)
		timeLTE := allResp.Models[6]["time"].(string)
		timeGTEParsed := parseTime(timeGTE)
		timeLTEParsed := parseTime(timeLTE)

		// Paginate within the time window with limit=2
		var windowEvents []map[string]any
		nextCursor := ""
		pageCount := 0

		for {
			path := "/events?tenant_id=" + setup.tenantID + "&dir=asc&limit=2"
			path += "&time[gte]=" + timeGTE + "&time[lte]=" + timeLTE
			if nextCursor != "" {
				path += "&next=" + nextCursor
			}

			var resp struct {
				Models     []map[string]any `json:"models"`
				Pagination map[string]any   `json:"pagination"`
			}
			status := s.doJSON(http.MethodGet, s.apiURL(path), nil, &resp)
			s.Require().Equal(http.StatusOK, status)
			pageCount++

			windowEvents = append(windowEvents, resp.Models...)

			if next, ok := resp.Pagination["next"].(string); ok && next != "" {
				nextCursor = next
			} else {
				break
			}

			if pageCount > 10 {
				s.Fail("too many pages")
				break
			}
		}

		// Verify time filter worked
		s.Greater(len(windowEvents), 0, "should have some events in window")
		s.Less(len(windowEvents), 10, "time filter should exclude some events")
		s.Greater(pageCount, 1, "should require multiple pages")

		// Verify all returned events are within the time window
		for _, event := range windowEvents {
			eventTime := parseTime(event["time"].(string))
			s.True(!eventTime.Before(timeGTEParsed), "event time %v should be >= %v", eventTime, timeGTEParsed)
			s.True(!eventTime.After(timeLTEParsed), "event time %v should be <= %v", eventTime, timeLTEParsed)
		}
	})
}
