package e2e_test

import (
	"net/http"

	"github.com/hookdeck/outpost/internal/idgen"
)

const (
	validationError = "validation error"
	invalidJSON     = "invalid JSON"
)

func (s *basicSuite) TestErrorResponses_PublishValidation() {
	tenant := s.createTenant()
	admin := s.adminAuth()

	publish := func(name string, body any, message, detail string) errorCase {
		return errorCase{
			name: name, method: http.MethodPost, path: "/publish", auth: admin, body: body,
			status: http.StatusUnprocessableEntity, message: message, detail: detail,
		}
	}
	data := map[string]any{"key": "value"}

	s.runErrorCases([]errorCase{
		publish("malformed JSON", rawBody("{"), invalidJSON, ""),
		publish("no body", nil, invalidJSON, ""),
		publish("wrong field type", map[string]any{"tenant_id": 1, "topic": "user.created", "data": data}, invalidJSON, ""),
		publish("missing tenant_id", map[string]any{"topic": "user.created", "data": data}, validationError, "tenant_id is required"),
		publish("missing data", map[string]any{"tenant_id": tenant.ID, "topic": "user.created"}, validationError, "data is required"),
		publish("data is not an object", map[string]any{"tenant_id": tenant.ID, "topic": "user.created", "data": "text"}, validationError, "data must be a valid JSON object"),
		publish("missing topic", map[string]any{"tenant_id": tenant.ID, "data": data}, validationError, "topic is required"),
		publish("unknown topic", map[string]any{"tenant_id": tenant.ID, "topic": "nope", "data": data}, validationError, "topic is invalid"),
	})
}

func (s *basicSuite) TestErrorResponses_RetryValidation() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")
	admin := s.adminAuth()

	eventID := idgen.Event()
	s.publish(tenant.ID, "user.created", map[string]any{"test": "retry_validation"}, withEventID(eventID))
	s.waitForNewAttempts(tenant.ID, 1)

	retry := func(name string, body any, status int, message, detail string) errorCase {
		return errorCase{
			name: name, method: http.MethodPost, path: "/retry", auth: admin, body: body,
			status: status, message: message, detail: detail,
		}
	}
	retryBody := map[string]any{"event_id": eventID, "destination_id": dest.ID}

	s.runErrorCases([]errorCase{
		retry("malformed JSON", rawBody("{"), http.StatusUnprocessableEntity, invalidJSON, ""),
		retry("no body", nil, http.StatusUnprocessableEntity, invalidJSON, ""),
		retry("missing event_id", map[string]any{"destination_id": dest.ID}, http.StatusUnprocessableEntity, validationError, "event_id is required"),
		retry("missing destination_id", map[string]any{"event_id": eventID}, http.StatusUnprocessableEntity, validationError, "destination_id is required"),
	})

	destinationURL := s.apiURL("/tenants/" + tenant.ID + "/destinations/" + dest.ID)

	s.Run("destination does not match the event", func() {
		status := s.doJSON(http.MethodPatch, destinationURL, map[string]any{"topics": []string{"user.deleted"}}, nil)
		s.Require().Equal(http.StatusOK, status)

		s.requireError(retry("", retryBody, http.StatusBadRequest, "destination does not match event", ""))
	})

	s.Run("disabled destination", func() {
		s.disableDestination(tenant.ID, dest.ID)

		s.requireError(retry("", retryBody, http.StatusBadRequest, "Destination is disabled", "destination_disabled"))
	})
}

func (s *basicSuite) TestErrorResponses_TenantValidation() {
	tenant := s.createTenant()
	admin := s.adminAuth()

	cases := []errorCase{
		{name: "upsert with malformed JSON", method: http.MethodPut, path: "/tenants/" + tenant.ID, body: rawBody("{"), status: http.StatusUnprocessableEntity, message: invalidJSON},
		{name: "upsert with metadata of the wrong type", method: http.MethodPut, path: "/tenants/" + tenant.ID, body: map[string]any{"metadata": "text"}, status: http.StatusUnprocessableEntity, message: invalidJSON},
		{name: "create with malformed JSON", method: http.MethodPut, path: "/tenants/" + idgen.String(), body: rawBody("{"), status: http.StatusUnprocessableEntity, message: invalidJSON},

		{name: "list with invalid dir", method: http.MethodGet, path: "/tenants?dir=sideways", status: http.StatusUnprocessableEntity, message: validationError, detail: "must be 'asc' or 'desc'"},
		{name: "list with next and prev", method: http.MethodGet, path: "/tenants?next=abc&prev=def", status: http.StatusBadRequest, message: "cannot specify both 'next' and 'prev' cursors"},
		{name: "list with a limit that is not a number", method: http.MethodGet, path: "/tenants?limit=ten", status: http.StatusBadRequest, message: "invalid limit: must be an integer"},
		{name: "list with a limit of zero", method: http.MethodGet, path: "/tenants?limit=0", status: http.StatusBadRequest, message: "invalid limit: must be between 1 and 100"},
		{name: "list with a limit over the maximum", method: http.MethodGet, path: "/tenants?limit=101", status: http.StatusBadRequest, message: "invalid limit: must be between 1 and 100"},
		{name: "list with an invalid next cursor", method: http.MethodGet, path: "/tenants?next=not-a-cursor", status: http.StatusBadRequest, message: "invalid cursor: invalid cursor"},
		{name: "list with an invalid prev cursor", method: http.MethodGet, path: "/tenants?prev=not-a-cursor", status: http.StatusBadRequest, message: "invalid cursor: invalid cursor"},
	}
	for i := range cases {
		cases[i].auth = admin
	}
	s.runErrorCases(cases)
}

func (s *basicSuite) TestErrorResponses_CreateDestinationValidation() {
	tenant := s.createTenant()
	admin := s.adminAuth()
	path := "/tenants/" + tenant.ID + "/destinations"

	webhook := func(overrides map[string]any) map[string]any {
		body := map[string]any{
			"type":   "webhook",
			"topics": []string{"*"},
			"config": map[string]any{"url": "https://example.com/hook"},
		}
		for k, v := range overrides {
			if v == nil {
				delete(body, k)
			} else {
				body[k] = v
			}
		}
		return body
	}
	create := func(name string, body any, status int, message, detail string) errorCase {
		return errorCase{
			name: name, method: http.MethodPost, path: path, auth: admin, body: body,
			status: status, message: message, detail: detail,
		}
	}

	const unprocessable = http.StatusUnprocessableEntity
	s.runErrorCases([]errorCase{
		create("malformed JSON", rawBody("{"), unprocessable, invalidJSON, ""),
		create("no body", nil, unprocessable, invalidJSON, ""),
		create("config of the wrong type", webhook(map[string]any{"config": "text"}), unprocessable, invalidJSON, ""),
		create("missing type", webhook(map[string]any{"type": nil}), unprocessable, validationError, "type is required"),
		create("missing topics", webhook(map[string]any{"topics": nil}), unprocessable, validationError, "topics is required"),
		create("empty topics", webhook(map[string]any{"topics": []string{}}), unprocessable, "validation failed: invalid topics", ""),
		create("unknown topic", webhook(map[string]any{"topics": []string{"nope"}}), unprocessable, "validation failed: invalid topics", ""),
		create("topics of the wrong type", webhook(map[string]any{"topics": 1}), unprocessable, "validation failed: invalid topics format", ""),
		create("unknown type", webhook(map[string]any{"type": "nope"}), unprocessable, "no provider registered for destination type: nope", ""),
		create("missing config", webhook(map[string]any{"config": nil}), unprocessable, validationError, "config.url is required"),
		create("invalid url", webhook(map[string]any{"config": map[string]any{"url": "not a url"}}), unprocessable, validationError, "config.url"),
		create("created_at in the future", webhook(map[string]any{"created_at": "2999-01-01T00:00:00Z"}), unprocessable, "created_at cannot be in the future", ""),
		create("updated_at in the future", webhook(map[string]any{"updated_at": "2999-01-01T00:00:00Z"}), unprocessable, "updated_at cannot be in the future", ""),
		create("disabled_at in the future", webhook(map[string]any{"disabled_at": "2999-01-01T00:00:00Z"}), unprocessable, "disabled_at cannot be in the future", ""),
	})

	s.Run("duplicate destination id", func() {
		dest := s.createWebhookDestination(tenant.ID, "*")

		s.requireError(create("", webhook(map[string]any{"id": dest.ID}), http.StatusBadRequest, "destination already exists", ""))
	})

	s.Run("maximum number of destinations", func() {
		full := s.createTenant()
		fullURL := s.apiURL("/tenants/" + full.ID + "/destinations")
		for i := 0; i < s.config.MaxDestinationsPerTenant; i++ {
			status := s.doJSON(http.MethodPost, fullURL, webhook(nil), nil)
			s.Require().Equal(http.StatusCreated, status, "failed to create destination %d", i+1)
		}

		s.requireError(errorCase{
			method: http.MethodPost, path: "/tenants/" + full.ID + "/destinations", auth: admin, body: webhook(nil),
			status: http.StatusBadRequest, message: "maximum number of destinations per tenant reached",
		})
	})
}

func (s *basicSuite) TestErrorResponses_UpdateDestinationValidation() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")
	admin := s.adminAuth()
	path := "/tenants/" + tenant.ID + "/destinations/" + dest.ID

	update := func(name string, body any, message, detail string) errorCase {
		return errorCase{
			name: name, method: http.MethodPatch, path: path, auth: admin, body: body,
			status: http.StatusUnprocessableEntity, message: message, detail: detail,
		}
	}

	s.runErrorCases([]errorCase{
		update("malformed JSON", rawBody("{"), invalidJSON, ""),
		update("no body", nil, invalidJSON, ""),
		update("empty topics", map[string]any{"topics": []string{}}, "validation failed: invalid topics", ""),
		update("unknown topic", map[string]any{"topics": []string{"nope"}}, "validation failed: invalid topics", ""),
		update("topics of the wrong type", map[string]any{"topics": 1}, "validation failed: invalid topics format", ""),
		update("type change", map[string]any{"type": "aws_sqs"}, "type cannot be updated", ""),
		update("config of the wrong type", map[string]any{"config": "text"}, invalidJSON, ""),
		update("credentials of the wrong type", map[string]any{"credentials": "text"}, invalidJSON, ""),
		update("filter of the wrong type", map[string]any{"filter": "text"}, invalidJSON, ""),
		update("delivery_metadata of the wrong type", map[string]any{"delivery_metadata": "text"}, invalidJSON, ""),
		update("metadata of the wrong type", map[string]any{"metadata": "text"}, invalidJSON, ""),
		update("disabled_at of the wrong type", map[string]any{"disabled_at": 1}, invalidJSON, ""),
		update("disabled_at in the future", map[string]any{"disabled_at": "2999-01-01T00:00:00Z"}, "disabled_at cannot be in the future", ""),
		update("invalid url", map[string]any{"config": map[string]any{"url": "not a url"}}, validationError, "config.url"),
	})
}

func (s *basicSuite) TestErrorResponses_LogQueryValidation() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")
	admin := s.adminAuth()

	lists := []struct {
		name string
		path string
	}{
		{"events", "/events"},
		{"attempts", "/attempts"},
		{"destination attempts", "/tenants/" + tenant.ID + "/destinations/" + dest.ID + "/attempts"},
	}

	queries := []struct {
		name    string
		query   string
		status  int
		message string
		detail  string
	}{
		{"invalid dir", "dir=sideways", http.StatusUnprocessableEntity, validationError, "must be 'asc' or 'desc'"},
		{"invalid order_by", "order_by=id", http.StatusUnprocessableEntity, validationError, "must be one of: [time]"},
		{"invalid time[gte]", "time[gte]=yesterday", http.StatusUnprocessableEntity, validationError, "query.time[gte]"},
		{"invalid time[lte]", "time[lte]=yesterday", http.StatusUnprocessableEntity, validationError, "query.time[lte]"},
		{"invalid time[gt]", "time[gt]=yesterday", http.StatusUnprocessableEntity, validationError, "query.time[gt]"},
		{"invalid time[lt]", "time[lt]=yesterday", http.StatusUnprocessableEntity, validationError, "query.time[lt]"},
		{"unsupported time operator", "time[any]=2024-01-01", http.StatusBadRequest, "operator 'any' is not supported, use 'gte', 'lte', 'gt', or 'lt'", ""},
		{"next and prev", "next=abc&prev=def", http.StatusBadRequest, "cannot specify both 'next' and 'prev' cursors", ""},
		{"invalid next cursor", "next=not-a-cursor", http.StatusBadRequest, "invalid cursor", ""},
		{"invalid prev cursor", "prev=not-a-cursor", http.StatusBadRequest, "invalid cursor", ""},
	}

	for _, list := range lists {
		s.Run(list.name, func() {
			for _, query := range queries {
				s.Run(query.name, func() {
					s.requireError(errorCase{
						method: http.MethodGet, path: list.path + "?" + query.query, auth: admin,
						status: query.status, message: query.message, detail: query.detail,
					})
				})
			}
		})
	}
}

func (s *basicSuite) TestErrorResponses_MetricsValidation() {
	admin := s.adminAuth()
	timeRange := "time[start]=2024-01-01T00:00:00Z&time[end]=2024-01-02T00:00:00Z"

	endpoints := []struct {
		name string
		path string
	}{
		{"event metrics", "/metrics/events"},
		{"attempt metrics", "/metrics/attempts"},
	}

	queries := []struct {
		name    string
		query   string
		message string
	}{
		{"no time range", "measures[0]=count", "time[start] and time[end] are required"},
		{"no time[end]", "time[start]=2024-01-01T00:00:00Z&measures[0]=count", "time[start] and time[end] are required"},
		{"invalid time[start]", "time[start]=yesterday&time[end]=2024-01-02T00:00:00Z&measures[0]=count", `invalid time[start]: parsing time "yesterday" as "2006-01-02T15:04:05Z07:00": cannot parse "yesterday" as "2006"`},
		{"invalid time[end]", "time[start]=2024-01-01T00:00:00Z&time[end]=tomorrow&measures[0]=count", `invalid time[end]: parsing time "tomorrow" as "2006-01-02T15:04:05Z07:00": cannot parse "tomorrow" as "2006"`},
		{"start after end", "time[start]=2024-01-02T00:00:00Z&time[end]=2024-01-01T00:00:00Z&measures[0]=count", "invalid time range: start must be before end"},
		{"invalid granularity", timeRange + "&measures[0]=count&granularity=hourly", `invalid granularity "hourly": must match <number><unit> where unit is one of s,m,h,d,w,M`},
		{"granularity of zero", timeRange + "&measures[0]=count&granularity=0h", `invalid granularity "0h": value must be > 0`},
		{"granularity over the maximum", timeRange + "&measures[0]=count&granularity=25h", `invalid granularity "25h": h value must be between 1 and 24`},
		{"no measures", timeRange, "at least one measures[] is required"},
		{"unknown measure", timeRange + "&measures[0]=nope", `unknown measure "nope"`},
		{"unknown dimension", timeRange + "&measures[0]=count&dimensions[0]=nope", `unknown dimension "nope"`},
		{"too many time buckets", "time[start]=2024-01-01T00:00:00Z&time[end]=2024-01-03T00:00:00Z&measures[0]=count&granularity=1s", "query too broad: try fewer dimensions, more filters, or a shorter time range"},
	}

	for _, endpoint := range endpoints {
		s.Run(endpoint.name, func() {
			for _, query := range queries {
				s.Run(query.name, func() {
					s.requireError(errorCase{
						method: http.MethodGet, path: endpoint.path + "?" + query.query, auth: admin,
						status: http.StatusBadRequest, message: query.message,
					})
				})
			}
		})
	}
}
