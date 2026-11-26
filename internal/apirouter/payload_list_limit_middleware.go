package apirouter

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultPayloadListRetryAfter is the Retry-After advertised when every
// payload-list slot is busy.
const DefaultPayloadListRetryAfter = 2 * time.Second

// PayloadListLimiter bounds how many list requests that include payloads run at
// once in this process.
//
// Event and attempt list responses that include payloads are assembled in
// memory before they are sent, so several of them at once can exceed the
// process memory budget and get it OOM-killed. Capping the number in flight
// bounds what that costs regardless of page size, and combined with a result
// size cap it makes a container memory limit predictable.
//
// A nil limiter, or a limit of zero or less, allows every request: the default
// keeps today's behaviour.
type PayloadListLimiter struct {
	slots      chan struct{}
	retryAfter time.Duration
}

// NewPayloadListLimiter returns a limiter that allows n concurrent
// payload-list requests. n <= 0 disables the limit.
func NewPayloadListLimiter(n int) *PayloadListLimiter {
	if n <= 0 {
		return nil
	}
	return &PayloadListLimiter{
		slots:      make(chan struct{}, n),
		retryAfter: DefaultPayloadListRetryAfter,
	}
}

// acquire takes a slot, waiting up to wait when they are all busy. It reports
// whether a slot was taken; on false nothing is held and the caller must not
// call release.
func (l *PayloadListLimiter) acquire(ctx context.Context, wait time.Duration) (release func(), ok bool) {
	if l == nil {
		return func() {}, true
	}

	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, true
	default:
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

// PayloadListLimitMiddleware bounds concurrent list requests that include
// payloads. Requests without payload includes, and every other route it is
// mounted on, pass through untouched and hold no slot.
//
// When no slot frees within wait, the request fails with 503 and Retry-After
// rather than queueing without bound. A client that was cancelled while
// waiting gets a 499-style abort and releases nothing, since it never held a
// slot.
func PayloadListLimitMiddleware(limiter *PayloadListLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil || !includesPayload(c) {
			c.Next()
			return
		}

		release, ok := limiter.acquire(c.Request.Context(), limiter.retryAfter)
		if !ok {
			if c.Request.Context().Err() != nil {
				// The client went away while waiting; nothing was taken.
				c.Abort()
				return
			}
			AbortWithError(c, http.StatusServiceUnavailable, ErrorResponse{
				Code:       http.StatusServiceUnavailable,
				Message:    "too many concurrent list requests with payloads, retry shortly",
				RetryAfter: limiter.retryAfter,
			})
			return
		}
		defer release()

		c.Next()
	}
}

// includesPayload reports whether the request asks for payload data, which is
// what makes a list response expensive to build. The API accepts the same
// parameter as include=payload and include[]=payload.
func includesPayload(c *gin.Context) bool {
	for _, v := range ParseArrayQueryParam(c, "include") {
		if v == "payload" {
			return true
		}
	}
	return false
}
