package run

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hookdeck/outpost/loadtest/app/internal/group"
)

// Spec is the complete description of a benchmark run: what to publish, for
// how long, and against what. It is also the reproduction artifact — a run
// export embeds the spec that produced it, so a published number can be
// re-derived rather than taken on trust.
type Spec struct {
	Name    string `yaml:"name" json:"name"`
	Notes   string `yaml:"notes,omitempty" json:"notes,omitempty"`
	Target  string `yaml:"target" json:"target"`                       // direct | gateway — which path was exercised
	Version string `yaml:"version,omitempty" json:"version,omitempty"` // Outpost image/tag under test

	// ConcurrencyBudget is the number of simultaneous in-flight deliveries the
	// deployment is provisioned for. A spec that exceeds it measures
	// saturation instead of the dimension it means to sweep, so the run
	// refuses to start rather than producing a plausible wrong answer.
	ConcurrencyBudget int `yaml:"concurrency_budget" json:"concurrency_budget"`

	// BudgetTolerance allows deliberate overcommit, e.g. 1.2 for 20% over.
	BudgetTolerance float64 `yaml:"budget_tolerance,omitempty" json:"budget_tolerance,omitempty"`

	// Topics every profile publishes to unless it overrides them. They must
	// already exist in the deployment's configured topic list, or destination
	// creation fails with "invalid topics".
	Topics []string `yaml:"topics,omitempty" json:"topics,omitempty"`

	Warmup Duration `yaml:"warmup" json:"warmup"`
	Window Duration `yaml:"window" json:"window"`
	Drain  Duration `yaml:"drain" json:"drain"`

	Profiles []Profile `yaml:"profiles" json:"profiles"`
}

// Profile is one tenant shape in the mixed workload. Every profile publishes
// into the same deployment; the report breaks results out per profile.
type Profile struct {
	Name          string   `yaml:"name" json:"name"`
	TenantCount   int      `yaml:"tenants" json:"tenants"`
	Destinations  int      `yaml:"destinations_per_tenant" json:"destinations_per_tenant"`
	RatePerTenant int      `yaml:"rate_per_tenant" json:"rate_per_tenant"`
	PayloadBytes  int      `yaml:"payload_bytes" json:"payload_bytes"`
	Topics        []string `yaml:"topics,omitempty" json:"topics,omitempty"`

	// ResponseMs is how long the mock receiver takes to respond. It is a swept
	// input, not a result: delivery latency is measured to first byte, so this
	// spends concurrency without inflating the reported number.
	ResponseMs int64   `yaml:"response_ms" json:"response_ms"`
	JitterMs   int64   `yaml:"response_jitter_ms,omitempty" json:"response_jitter_ms,omitempty"`
	ErrorRate  float64 `yaml:"error_rate,omitempty" json:"error_rate,omitempty"`

	Pattern       string         `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	PatternParams map[string]any `yaml:"pattern_params,omitempty" json:"pattern_params,omitempty"`
}

// Rate is the profile's total offered events per second.
func (p Profile) Rate() int { return p.TenantCount * p.RatePerTenant }

// Concurrency is the profile's share of the delivery budget, by Little's law:
// in-flight = arrival rate × time in system.
func (p Profile) Concurrency() float64 {
	return float64(p.Rate()) * float64(p.Destinations) * (float64(p.ResponseMs) / 1000)
}

// BytesPerSec is the profile's share of the bandwidth budget.
func (p Profile) BytesPerSec() float64 {
	return float64(p.Rate()) * float64(p.Destinations) * float64(p.PayloadBytes)
}

func (p Profile) GroupConfig() group.Config {
	topics := p.Topics
	if len(topics) == 0 {
		topics = []string{"user.created"}
	}
	return group.Config{
		Name:                  p.Name,
		TenantPrefix:          p.Name,
		TenantCount:           p.TenantCount,
		DestinationsPerTenant: p.Destinations,
		Topics:                topics,
		Publish: group.PublishConfig{
			RatePerTenant: p.RatePerTenant,
			Pattern:       p.Pattern,
			PatternParams: p.PatternParams,
			PayloadBytes:  p.PayloadBytes,
		},
		MockProfile: group.MockProfileConfig{
			LatencyMs: p.ResponseMs,
			JitterMs:  p.JitterMs,
			ErrorRate: p.ErrorRate,
		},
	}
}

// Budget is the run's two resource totals, both design targets and reported
// outputs. A latency figure floating free of the load that produced it is not
// a checkable claim.
type Budget struct {
	Concurrency  float64 `json:"concurrency"`
	BytesPerSec  float64 `json:"bytes_per_sec"`
	OfferedRate  int     `json:"offered_rate"`
	BudgetTarget int     `json:"budget_target"`
	WithinBudget bool    `json:"within_budget"`
}

func (s *Spec) Budget() Budget {
	b := Budget{BudgetTarget: s.ConcurrencyBudget}
	for _, p := range s.Profiles {
		b.Concurrency += p.Concurrency()
		b.BytesPerSec += p.BytesPerSec()
		b.OfferedRate += p.Rate()
	}
	b.WithinBudget = s.ConcurrencyBudget == 0 || b.Concurrency <= float64(s.ConcurrencyBudget)*s.tolerance()
	return b
}

func (s *Spec) tolerance() float64 {
	if s.BudgetTolerance > 0 {
		return s.BudgetTolerance
	}
	return 1.0
}

func (s *Spec) Validate() error {
	var errs []string
	if s.Name == "" {
		errs = append(errs, "name is required")
	}
	if s.Target != "direct" && s.Target != "gateway" {
		errs = append(errs, `target must be "direct" or "gateway" — the report states which path was exercised`)
	}
	if s.Window.Duration() <= 0 {
		errs = append(errs, "window must be > 0")
	}
	if len(s.Profiles) == 0 {
		errs = append(errs, "at least one profile is required")
	}

	seen := map[string]bool{}
	for i, p := range s.Profiles {
		where := fmt.Sprintf("profile[%d]", i)
		if p.Name == "" {
			errs = append(errs, where+": name is required")
			continue
		}
		if seen[p.Name] {
			errs = append(errs, where+": duplicate name "+p.Name)
		}
		seen[p.Name] = true
		if p.TenantCount <= 0 {
			errs = append(errs, p.Name+": tenants must be > 0")
		}
		if p.RatePerTenant <= 0 {
			errs = append(errs, p.Name+": rate_per_tenant must be > 0")
		}
		if p.Destinations <= 0 {
			errs = append(errs, p.Name+": destinations_per_tenant must be > 0")
		}
		if p.PayloadBytes <= 0 {
			errs = append(errs, p.Name+": payload_bytes must be > 0")
		}
	}

	if b := s.Budget(); !b.WithinBudget {
		errs = append(errs, fmt.Sprintf(
			"concurrency budget exceeded: %.0f concurrent deliveries against a target of %d. "+
				"Past the budget the run measures saturation, not the dimension it sweeps — "+
				"lower a rate, fan-out, or response time, or raise concurrency_budget deliberately",
			b.Concurrency, s.ConcurrencyBudget))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid spec:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSpec(data)
}

func ParseSpec(data []byte) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true) // a typo'd key must not silently mean "default"
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if s.Drain.Duration() == 0 {
		s.Drain = Duration(2 * time.Minute)
	}
	if len(s.Topics) == 0 {
		s.Topics = []string{"user.created"}
	}
	for i := range s.Profiles {
		if len(s.Profiles[i].Topics) == 0 {
			s.Profiles[i].Topics = s.Topics
		}
	}
	return &s, nil
}

// Duration is a time.Duration that reads as "30s" / "10m" in YAML and JSON.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }
func (d Duration) String() string          { return time.Duration(d).String() }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}
