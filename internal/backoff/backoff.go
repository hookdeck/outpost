package backoff

import (
	"math/rand/v2"
	"time"
)

type Backoff interface {
	// Duration returns the duration to wait before retrying the operation.
	// Duration accepts the number of times the operation has been retried.
	// If the operation has never been retried, the number should be 0.
	Duration(int) time.Duration
}

type ExponentialBackoff struct {
	Interval time.Duration
	Base     int
}

var _ Backoff = &ExponentialBackoff{}

func (b *ExponentialBackoff) Duration(retries int) time.Duration {
	return b.Interval * time.Duration(intPow(b.Base, retries))
}

// @see https://stackoverflow.com/a/75657949
func intPow(base, exp int) int {
	result := 1
	for {
		if exp&1 == 1 {
			result *= base
		}
		exp >>= 1
		if exp == 0 {
			break
		}
		base *= base
	}
	return result
}

type ConstantBackoff struct {
	Interval time.Duration
}

var _ Backoff = &ConstantBackoff{}

func (b *ConstantBackoff) Duration(retries int) time.Duration {
	return b.Interval
}

// ScheduledBackoff uses a predefined schedule of delays for each retry attempt.
// If the retry attempt exceeds the schedule length, it returns the last value.
type ScheduledBackoff struct {
	Schedule []time.Duration
}

var _ Backoff = &ScheduledBackoff{}

func (b *ScheduledBackoff) Duration(retries int) time.Duration {
	if len(b.Schedule) == 0 {
		return 0
	}
	if retries >= len(b.Schedule) {
		// Return last value for attempts beyond schedule
		return b.Schedule[len(b.Schedule)-1]
	}
	return b.Schedule[retries]
}

// JitteredBackoff spreads Backoff's delays uniformly by up to ±Jitter, a
// fraction of the delay (0.2 for ±20%), so retries of failures that happened
// together don't all come due together.
type JitteredBackoff struct {
	Backoff Backoff
	Jitter  float64
	// Rand returns a number in [0, 1); nil means math/rand/v2.Float64.
	Rand func() float64
}

var _ Backoff = &JitteredBackoff{}

func (b *JitteredBackoff) Duration(retries int) time.Duration {
	d := b.Backoff.Duration(retries)
	if b.Jitter <= 0 || d <= 0 {
		return d
	}
	random := b.Rand
	if random == nil {
		random = rand.Float64
	}
	return time.Duration(float64(d) * (1 + b.Jitter*(2*random()-1)))
}
