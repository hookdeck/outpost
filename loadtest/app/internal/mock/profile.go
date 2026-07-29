package mock

import (
	"math/rand"
	"sync/atomic"
	"time"
)

type Profile struct {
	latencyMs  atomic.Int64
	jitterMs   atomic.Int64
	errorRate  atomic.Int64 // stored as error_rate * 10000 (e.g., 0.05 = 500)
}

func NewProfile(latencyMs, jitterMs int64, errorRate float64) *Profile {
	p := &Profile{}
	p.latencyMs.Store(latencyMs)
	p.jitterMs.Store(jitterMs)
	p.errorRate.Store(int64(errorRate * 10000))
	return p
}

func (p *Profile) SetLatency(ms int64) {
	p.latencyMs.Store(ms)
}

func (p *Profile) SetJitter(ms int64) {
	p.jitterMs.Store(ms)
}

func (p *Profile) SetErrorRate(rate float64) {
	p.errorRate.Store(int64(rate * 10000))
}

func (p *Profile) GetLatency() time.Duration {
	base := p.latencyMs.Load()
	jitter := p.jitterMs.Load()
	if jitter > 0 {
		base += rand.Int63n(2*jitter+1) - jitter
	}
	if base < 0 {
		base = 0
	}
	return time.Duration(base) * time.Millisecond
}

func (p *Profile) ShouldError() bool {
	rate := p.errorRate.Load()
	if rate <= 0 {
		return false
	}
	return rand.Int63n(10000) < rate
}
