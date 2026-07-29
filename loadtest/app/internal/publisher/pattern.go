package publisher

import (
	"context"
	"math"
	"sync/atomic"
	"time"
)

// PatternController adjusts the rate of a group publisher over time.
type PatternController interface {
	Start(ctx context.Context, rate *atomic.Int64, baseRate int64)
}

type constantPattern struct{}

func (constantPattern) Start(ctx context.Context, rate *atomic.Int64, baseRate int64) {
	// No-op: rate stays constant
}

type rampPattern struct {
	durationSeconds int
}

func (p rampPattern) Start(ctx context.Context, rate *atomic.Int64, baseRate int64) {
	if p.durationSeconds <= 0 {
		return
	}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	elapsed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed++
			progress := float64(elapsed) / float64(p.durationSeconds)
			if progress > 1.0 {
				progress = 1.0
			}
			newRate := int64(float64(baseRate) * progress)
			rate.Store(newRate)
			if progress >= 1.0 {
				return
			}
		}
	}
}

type burstPattern struct {
	multiplier      float64
	burstDuration   int // seconds
	burstInterval   int // seconds between bursts
}

func (p burstPattern) Start(ctx context.Context, rate *atomic.Int64, baseRate int64) {
	mult := p.multiplier
	if mult <= 0 {
		mult = 10
	}
	burstDur := p.burstDuration
	if burstDur <= 0 {
		burstDur = 30
	}
	interval := p.burstInterval
	if interval <= 0 {
		interval = 300
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	elapsed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed++
			cycleLen := interval + burstDur
			posInCycle := elapsed % cycleLen
			if posInCycle >= interval {
				// In burst phase
				rate.Store(int64(float64(baseRate) * mult))
			} else {
				rate.Store(baseRate)
			}
		}
	}
}

type sinePattern struct {
	amplitude     float64 // fraction of base rate (0.5 = ±50%)
	periodSeconds int
}

func (p sinePattern) Start(ctx context.Context, rate *atomic.Int64, baseRate int64) {
	amp := p.amplitude
	if amp <= 0 {
		amp = 0.5
	}
	period := p.periodSeconds
	if period <= 0 {
		period = 600
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	elapsed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed++
			sine := math.Sin(2 * math.Pi * float64(elapsed) / float64(period))
			newRate := float64(baseRate) * (1.0 + amp*sine)
			if newRate < 1 {
				newRate = 1
			}
			rate.Store(int64(newRate))
		}
	}
}

func newPattern(name string, cfg map[string]any) PatternController {
	switch name {
	case "ramp":
		dur := intFromMap(cfg, "ramp_duration_seconds", 60)
		return rampPattern{durationSeconds: dur}
	case "burst":
		return burstPattern{
			multiplier:    floatFromMap(cfg, "burst_multiplier", 10),
			burstDuration: intFromMap(cfg, "burst_duration_seconds", 30),
			burstInterval: intFromMap(cfg, "burst_interval_seconds", 300),
		}
	case "sine":
		return sinePattern{
			amplitude:     floatFromMap(cfg, "sine_amplitude", 0.5),
			periodSeconds: intFromMap(cfg, "sine_period_seconds", 600),
		}
	default:
		return constantPattern{}
	}
}

func intFromMap(m map[string]any, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
}

func floatFromMap(m map[string]any, key string, def float64) float64 {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return def
	}
}
