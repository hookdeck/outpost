package apirouter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifyGate blocks the fake provider's verification until released, so a
// test can act while subscribe calls are in flight.
type verifyGate struct {
	entered chan string
	release chan struct{}
}

func (m *mcpTest) gateVerification() *verifyGate {
	g := &verifyGate{entered: make(chan string, 16), release: make(chan struct{})}
	m.provider.setVerify(func(ctx context.Context, d *models.Destination) error {
		g.entered <- d.ID
		select {
		case <-g.release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	m.t.Cleanup(func() { m.provider.setVerify(nil) })
	return g
}

// waitEntered waits for n calls to reach the verification.
func (g *verifyGate) waitEntered(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-g.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("subscribe never reached verification")
		}
	}
}

// subscribeAsync runs a subscribe in the background.
func (m *mcpTest) subscribeAsync(body any) <-chan *httptest.ResponseRecorder {
	req := m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions", body))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		m.router.ServeHTTP(w, req)
		done <- w
	}()
	return done
}

func await(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-done:
		require.NoError(t, testutil.CheckJSONResponse(w.Header(), w.Body.Bytes()))
		return w
	case <-time.After(10 * time.Second):
		t.Fatal("subscribe didn't return")
		return nil
	}
}

func (m *mcpTest) revokePrincipal(principal string) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions?principal="+principal, nil)))
}

func TestMCP_Race_RevokeByPrincipalFencesInFlightSubscribe(t *testing.T) {
	m := newMCPTest(t)
	gate := m.gateVerification()
	body := subscribeBody("alice", "order.created")
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)

	// The subscription doesn't exist yet: nothing to delete, but the fence
	// stops the create once verification ends.
	resp := m.revokePrincipal("alice")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.JSONEq(t, `{"success":true,"deleted":0}`, resp.Body.String())

	close(gate.release)
	requireMCPError(t, await(t, done), "not_found", -32011, map[string]any{"kind": "subscription"})
	assert.Nil(t, m.destination(subscriptionID(t, body)))

	// Another principal isn't fenced.
	m.provider.setVerify(nil)
	m.mustSubscribe(subscribeBody("bob", "order.created"))
}

func TestMCP_Race_RevokeByPrincipalDuringRefresh(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("alice", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)

	gate := m.gateVerification()
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)

	resp := m.revokePrincipal("alice")
	assert.JSONEq(t, `{"success":true,"deleted":1}`, resp.Body.String())
	assert.Len(t, m.notifier.sent(), 1)

	close(gate.release)
	requireMCPError(t, await(t, done), "not_found", -32011, map[string]any{"kind": "subscription"})
	assert.Nil(t, m.destination(id), "the refresh doesn't bring the revoked subscription back")
}

func TestMCP_Race_RevokeOneDuringRefresh(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("alice", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)

	gate := m.gateVerification()
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)

	resp := m.do(m.withJWT(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+id, nil), mcpTenant))
	require.Equal(t, http.StatusOK, resp.Code)

	close(gate.release)
	requireMCPError(t, await(t, done), "not_found", -32011, map[string]any{"kind": "subscription"})
	assert.Nil(t, m.destination(id))
}

func TestMCP_Race_UnsubscribeDuringRefresh(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("alice", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)

	gate := m.gateVerification()
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)

	require.Equal(t, http.StatusOK, m.unsubscribe(mcpTenant, body).Code)

	close(gate.release)
	requireMCPError(t, await(t, done), "not_found", -32011, map[string]any{"kind": "subscription"})
	assert.Nil(t, m.destination(id))
}

func TestMCP_Race_ExpiryDuringRefreshCreatesANewSubscription(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("alice", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)
	createdAt := m.destination(id).CreatedAt

	gate := m.gateVerification()
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)

	// The sweeper deletes it meanwhile (as if it had expired).
	result, err := m.tenantStore.DeleteDestinationIf(t.Context(), mcpTenant, id, tenantstore.DeleteCondition{Reason: tenantstore.DeleteReasonExpired})
	require.NoError(t, err)
	require.True(t, result.Deleted)
	time.Sleep(2 * time.Millisecond)

	close(gate.release)
	resp := await(t, done)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Nil(t, rawField(t, resp.Body.Bytes(), "deliveryStatus"), "created, not refreshed")
	d := m.destination(id)
	require.NotNil(t, d)
	assert.NotEqual(t, createdAt.UnixMilli(), d.CreatedAt.UnixMilli(), "a new generation")
}

func TestMCP_Race_ConcurrentCreatesOfOneSubscription(t *testing.T) {
	m := newMCPTest(t, withMCPLimits(1, 1))
	gate := m.gateVerification()
	body := subscribeBody("alice", "order.created")
	first := m.subscribeAsync(body)
	second := m.subscribeAsync(subscribeBody("alice", "order.created", withSecret(testSecret(2))))
	gate.waitEntered(t, 2)
	close(gate.release)

	responses := []*httptest.ResponseRecorder{await(t, first), await(t, second)}
	refreshed := 0
	for _, resp := range responses {
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		if rawField(t, resp.Body.Bytes(), "deliveryStatus") != nil {
			refreshed++
		}
	}
	assert.Equal(t, 1, refreshed, "the create that lost became a refresh")

	list, err := m.tenantStore.ListDestination(t.Context(), listAll(mcpTenant))
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestMCP_Race_ConcurrentCreatesRespectThePrincipalLimit(t *testing.T) {
	m := newMCPTest(t, withMCPLimits(100, 2))
	gate := m.gateVerification()
	var dones []<-chan *httptest.ResponseRecorder
	for i := range 5 {
		dones = append(dones, m.subscribeAsync(subscribeBody("alice", "order.created",
			withArguments(map[string]any{"currency": fmt.Sprintf("C%d", i)}))))
	}
	gate.waitEntered(t, 5)
	close(gate.release)

	created, exhausted := 0, 0
	for _, done := range dones {
		resp := await(t, done)
		switch resp.Code {
		case http.StatusOK:
			created++
		case http.StatusUnprocessableEntity:
			requireMCPError(t, resp, "resource_exhausted", -32013, map[string]any{"limit": "principal_subscriptions", "max": float64(2)})
			exhausted++
		default:
			t.Fatalf("unexpected %d: %s", resp.Code, resp.Body.String())
		}
	}
	assert.Equal(t, 2, created)
	assert.Equal(t, 3, exhausted)
}

func TestMCP_Race_DisabledDuringRefresh(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("alice", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)

	gate := m.gateVerification()
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)
	// Auto-disabled after the lookup saw it enabled.
	_, err := m.tenantStore.DisableDestination(t.Context(), mcpTenant, id, time.Now())
	require.NoError(t, err)
	close(gate.release)

	resp := await(t, done)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var result apirouter.MCPSubscribeResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.NotNil(t, result.DeliveryStatus)
	assert.False(t, result.DeliveryStatus.Active)
	assert.Nil(t, m.destination(id).DisabledAt)

	// The failures weren't reset before the write: they are after it.
	require.Eventually(t, func() bool {
		resets, _ := m.alerts.calls()
		return len(resets) == 1
	}, time.Second, 5*time.Millisecond)
}

// Many concurrent subscribe, refresh, unsubscribe and revoke calls: the
// handlers stay consistent with the store and race-free (run with -race).
func TestMCP_Race_Mixed(t *testing.T) {
	m := newMCPTest(t, withMCPLimits(1000, 0))
	var wg sync.WaitGroup
	for i := range 8 {
		principal := fmt.Sprintf("p%d", i%3)
		body := subscribeBody(principal, "order.created", withArguments(map[string]any{"currency": fmt.Sprintf("C%d", i%2)}))
		wg.Go(func() {
			for range 5 {
				w := httptest.NewRecorder()
				m.router.ServeHTTP(w, m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions", body)))
				assert.Contains(t, []int{http.StatusOK, http.StatusUnprocessableEntity}, w.Code, w.Body.String())
				w = httptest.NewRecorder()
				m.router.ServeHTTP(w, m.withAPIKey(m.jsonReq(http.MethodPost, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/unsubscribe", body)))
				assert.Equal(t, http.StatusOK, w.Code)
			}
		})
		wg.Go(func() {
			w := httptest.NewRecorder()
			m.router.ServeHTTP(w, m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions?principal="+principal, nil)))
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
	wg.Wait()
}
