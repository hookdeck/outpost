package apirouter

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPayloadListTestRouter mounts the limiter on a list-style route, the way
// NewRouter mounts it on GET /events and GET /attempts.
func newPayloadListTestRouter(limiter *PayloadListLimiter, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ErrorHandlerMiddleware())
	r.GET("/events", PayloadListLimitMiddleware(limiter), handler)
	return r
}

func doList(r *gin.Engine, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/events"+query, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPayloadListLimit_DisabledByDefault(t *testing.T) {
	var inFlight, maxInFlight int
	var mu sync.Mutex

	limiter := NewPayloadListLimiter(0)
	require.Nil(t, limiter, "limit 0 must disable the limiter")

	r := newPayloadListTestRouter(limiter, func(c *gin.Context) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doList(r, "?include=payload")
			assert.Equal(t, http.StatusOK, rec.Code)
		}()
	}
	wg.Wait()

	assert.Greater(t, maxInFlight, 1, "with no limit, payload lists must run concurrently")
}

func TestPayloadListLimit_RequestsWithoutPayloadBypassTheLimit(t *testing.T) {
	released := make(chan struct{})
	holding := make(chan struct{})

	// One slot, held for the whole test by a payload request.
	r := newPayloadListTestRouter(NewPayloadListLimiter(1), func(c *gin.Context) {
		if c.Query("include") == "payload" {
			close(holding)
			<-released
		}
		c.Status(http.StatusOK)
	})

	payloadDone := make(chan struct{})
	go func() {
		defer close(payloadDone)
		doList(r, "?include=payload")
	}()
	<-holding

	// A list without payloads must not queue behind the busy payload slot.
	rec := doList(r, "?limit=10")
	assert.Equal(t, http.StatusOK, rec.Code)

	close(released)
	<-payloadDone
}

func TestPayloadListLimit_RefusesWhenAllSlotsBusy(t *testing.T) {
	released := make(chan struct{})
	holding := make(chan struct{})

	limiter := NewPayloadListLimiter(1)
	limiter.retryAfter = 30 * time.Millisecond

	r := newPayloadListTestRouter(limiter, func(c *gin.Context) {
		close(holding)
		<-released
		c.Status(http.StatusOK)
	})

	go doList(r, "?include=payload")
	<-holding

	rec := doList(r, "?include=payload")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"), "a 503 must advertise Retry-After")

	close(released)
}

func TestPayloadListLimit_RecognisesArrayForm(t *testing.T) {
	released := make(chan struct{})
	holding := make(chan struct{})

	limiter := NewPayloadListLimiter(1)
	limiter.retryAfter = 30 * time.Millisecond

	r := newPayloadListTestRouter(limiter, func(c *gin.Context) {
		close(holding)
		<-released
		c.Status(http.StatusOK)
	})

	// include[]=payload is the bracket form ParseArrayQueryParam also accepts.
	go doList(r, "?include[]=payload")
	<-holding

	rec := doList(r, "?include[]=payload")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "bracket form must take a slot too")

	close(released)
}

func TestPayloadListLimit_SlotsAreReturned(t *testing.T) {
	limiter := NewPayloadListLimiter(1)
	r := newPayloadListTestRouter(limiter, func(c *gin.Context) { c.Status(http.StatusOK) })

	// Sequential requests must all succeed: each returns its slot afterwards.
	for range 5 {
		rec := doList(r, "?include=payload")
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	assert.Len(t, limiter.slots, 0, "no slot may leak")
}
