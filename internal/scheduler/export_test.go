package scheduler

// WithRandForTest replaces the jitter source so tests get exact backoffs.
func WithRandForTest(r func() float64) Option {
	return func(c *config) {
		c.rand = r
	}
}
