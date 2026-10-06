package apirouter_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	tooLargeMessage = "request body too large"
	tooLargeBody    = `{"status":413,"message":"request body too large"}`
)

// paddedBody returns a JSON object of exactly size bytes: fields, plus a
// metadata value that fills the rest.
func paddedBody(t *testing.T, fields string, size int) string {
	t.Helper()
	prefix := `{` + fields + `"metadata":{"pad":"`
	suffix := `"}}`
	fill := size - len(prefix) - len(suffix)
	require.Greater(t, fill, 0)
	return prefix + strings.Repeat("a", fill) + suffix
}

func TestAPI_RequestBodyLimit(t *testing.T) {
	const limit = apirouter.MaxRequestBodyBytes
	const destinationFields = `"type":"webhook","topics":["user.created"],"config":{"url":"https://example.com/hook"},`

	routes := []struct {
		name       string
		method     string
		path       string
		fields     string
		wantStatus int
		// Tenant upsert ignores a body that has no declared length.
		readsUndeclaredLength bool
	}{
		{"tenant upsert", http.MethodPut, "/api/v1/tenants/t1", "", http.StatusOK, false},
		{"destination create", http.MethodPost, "/api/v1/tenants/t1/destinations", destinationFields, http.StatusCreated, true},
		{"destination update", http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", "", http.StatusOK, true},
	}
	auths := []struct {
		name string
		auth func(h *apiTest, req *http.Request) *http.Request
	}{
		{"api key", func(h *apiTest, req *http.Request) *http.Request { return h.withAPIKey(req) }},
		{"jwt", func(h *apiTest, req *http.Request) *http.Request { return h.withJWT(req, "t1") }},
	}

	setup := func(t *testing.T) *apiTest {
		h := newAPITest(t)
		require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
		require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))))
		return h
	}
	newReq := func(method, path, body string) *http.Request {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	for _, route := range routes {
		for _, auth := range auths {
			t.Run(route.name+" at the limit with "+auth.name, func(t *testing.T) {
				h := setup(t)
				resp := h.do(auth.auth(h, newReq(route.method, route.path, paddedBody(t, route.fields, limit))))
				require.Equal(t, route.wantStatus, resp.Code)
			})

			t.Run(route.name+" over the limit with "+auth.name, func(t *testing.T) {
				h := setup(t)
				resp := h.do(auth.auth(h, newReq(route.method, route.path, paddedBody(t, route.fields, limit+1))))
				testutil.RequireErrorResponse(t, resp, http.StatusRequestEntityTooLarge, tooLargeMessage)
			})

			if !route.readsUndeclaredLength {
				continue
			}
			t.Run(route.name+" over the limit without a declared length with "+auth.name, func(t *testing.T) {
				h := setup(t)
				req := newReq(route.method, route.path, paddedBody(t, route.fields, limit+1))
				req.ContentLength = -1
				resp := h.do(auth.auth(h, req))
				testutil.RequireErrorResponse(t, resp, http.StatusRequestEntityTooLarge, tooLargeMessage)
			})
		}
	}

	t.Run("retry over the limit", func(t *testing.T) {
		h := setup(t)
		body := paddedBody(t, `"event_id":"e1","destination_id":"d1",`, limit+1)
		for _, contentLength := range []int64{int64(len(body)), -1} {
			req := newReq(http.MethodPost, "/api/v1/retry", body)
			req.ContentLength = contentLength
			resp := h.do(h.withAPIKey(req))
			testutil.RequireErrorResponse(t, resp, http.StatusRequestEntityTooLarge, tooLargeMessage)
		}
	})

	t.Run("declared length over the limit on a route that ignores the body", func(t *testing.T) {
		h := setup(t)
		req := newReq(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", paddedBody(t, "", limit+1))
		resp := h.do(h.withAPIKey(req))
		testutil.RequireErrorResponse(t, resp, http.StatusRequestEntityTooLarge, tooLargeMessage)
	})

	t.Run("nothing is stored when the body is over the limit", func(t *testing.T) {
		h := setup(t)
		resp := h.do(h.withJWT(newReq(http.MethodPut, "/api/v1/tenants/t1", paddedBody(t, "", limit+1)), "t1"))
		require.Equal(t, http.StatusRequestEntityTooLarge, resp.Code)

		tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
		require.NoError(t, err)
		assert.Empty(t, tenant.Metadata["pad"])
	})

	t.Run("ids containing publish are limited like any other", func(t *testing.T) {
		h := setup(t)
		require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("publisher-1"))))
		require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("publish-x"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))))

		requests := []struct {
			method, path, fields, tenantID string
			contentLengths                 []int64
		}{
			{http.MethodPatch, "/api/v1/tenants/t1/destinations/publish-x", "", "t1", []int64{limit + 1, -1}},
			{http.MethodPut, "/api/v1/tenants/publisher-1", "", "publisher-1", []int64{limit + 1}},
			{http.MethodPost, "/api/v1/tenants/publisher-1/destinations", destinationFields, "publisher-1", []int64{limit + 1, -1}},
		}
		for _, r := range requests {
			for _, contentLength := range r.contentLengths {
				req := newReq(r.method, r.path, paddedBody(t, r.fields, limit+1))
				req.ContentLength = contentLength
				resp := h.do(h.withJWT(req, r.tenantID))
				testutil.RequireErrorResponse(t, resp, http.StatusRequestEntityTooLarge, tooLargeMessage)
			}
		}
	})

	t.Run("publish is not limited", func(t *testing.T) {
		h := setup(t)
		body := `{"tenant_id":"t1","topic":"user.created","data":{"pad":"` + strings.Repeat("a", 2*limit) + `"}}`
		resp := h.do(h.withAPIKey(newReq(http.MethodPost, "/api/v1/publish", body)))
		require.Equal(t, http.StatusAccepted, resp.Code)
	})

	t.Run("publish with a trailing slash is still redirected", func(t *testing.T) {
		h := setup(t)
		body := `{"tenant_id":"t1","topic":"user.created","data":{"pad":"` + strings.Repeat("a", 2*limit) + `"}}`
		resp := httptest.NewRecorder()
		h.router.ServeHTTP(resp, h.withAPIKey(newReq(http.MethodPost, "/api/v1/publish/", body)))
		require.Equal(t, http.StatusTemporaryRedirect, resp.Code)
		assert.Equal(t, "/api/v1/publish", resp.Header().Get("Location"))
	})

	t.Run("chunked body over HTTP", func(t *testing.T) {
		h := setup(t)
		server := httptest.NewServer(h.router)
		defer server.Close()

		send := func(size int) (int, string) {
			// A reader of unknown length makes the client send a chunked body.
			body := io.MultiReader(strings.NewReader(paddedBody(t, destinationFields, size)))
			req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/tenants/t1/destinations", body)
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(h.withJWT(req, "t1"))
			require.NoError(t, err)
			defer resp.Body.Close()
			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			return resp.StatusCode, string(respBody)
		}

		status, _ := send(limit)
		require.Equal(t, http.StatusCreated, status)

		status, body := send(limit + 1)
		require.Equal(t, http.StatusRequestEntityTooLarge, status)
		assert.JSONEq(t, tooLargeBody, body)

		status, body = send(8 * limit)
		require.Equal(t, http.StatusRequestEntityTooLarge, status)
		assert.JSONEq(t, tooLargeBody, body)
	})
}

// A body whose declared length is over the limit is answered without reading
// any of it, so the client is not asked to send it.
func TestAPI_RequestBodyLimit_DeclaredLengthIsNotRead(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	h := newAPITest(t, withLogger(logging.NewTestLogger(zap.New(core))))
	require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	server := httptest.NewServer(h.router)
	defer server.Close()

	tests := []struct {
		name   string
		expect string
	}{
		{"headers only", ""},
		{"expect 100-continue", "Expect: 100-continue\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.TakeAll()

			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			require.NoError(t, err)
			defer conn.Close()

			_, err = fmt.Fprintf(conn, "PUT /api/v1/tenants/t1 HTTP/1.1\r\nHost: outpost\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n%s\r\n",
				testAPIKey, apirouter.MaxRequestBodyBytes+1, tt.expect)
			require.NoError(t, err)

			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			require.NoError(t, err, "no response before any body byte was sent")
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
			assert.JSONEq(t, tooLargeBody, string(body))

			var logged bool
			for _, entry := range logs.FilterMessage("request completed").All() {
				if entry.ContextMap()["status"] == int64(http.StatusRequestEntityTooLarge) {
					logged = true
				}
			}
			assert.True(t, logged, "the 413 response should be logged")
		})
	}
}
