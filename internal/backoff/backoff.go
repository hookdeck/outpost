package backoff

import "time"

type Backoff interface {
	// Duration returns the duration to wait before retrying the operation.
	// Duration accepts the numeber of times the operation has been retried.
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
