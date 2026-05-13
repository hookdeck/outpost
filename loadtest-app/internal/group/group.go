package group

import (
	"github.com/hookdeck/outpost/loadtest-app/internal/eventlog"
	"github.com/hookdeck/outpost/loadtest-app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest-app/internal/mock"
)

type Config struct {
	Name                  string   `json:"name"`
	TenantPrefix          string   `json:"tenant_prefix"`
	TenantCount           int      `json:"tenant_count"`
	DestinationsPerTenant int      `json:"destinations_per_tenant"`
	Topics                []string `json:"topics"`
	Publish               PublishConfig `json:"publish"`
	MockProfile           MockProfileConfig `json:"mock_profile"`
	GracePeriodSeconds    int      `json:"grace_period_seconds,omitempty"` // default 30
}

type PublishConfig struct {
	RatePerTenant int            `json:"rate_per_tenant"`
	Pattern       string         `json:"pattern"` // constant, ramp, burst, sine
	PatternParams map[string]any `json:"pattern_params,omitempty"`
	PayloadBytes  int            `json:"payload_bytes"`
}

type MockProfileConfig struct {
	LatencyMs  int64   `json:"latency_ms"`
	JitterMs   int64   `json:"latency_jitter_ms"`
	ErrorRate  float64 `json:"error_rate"`
}

type ProvisionedTenant struct {
	TenantID       string
	DestinationIDs []string
}

type Group struct {
	Config             Config
	State              State
	ProvisionedTenants []ProvisionedTenant
	Metrics            *metrics.GroupMetrics
	MockProfile        *mock.Profile
	EventLog           *eventlog.Log
	Error              string
}

func New(cfg Config) *Group {
	if cfg.GracePeriodSeconds == 0 {
		cfg.GracePeriodSeconds = 30
	}
	if cfg.Publish.Pattern == "" {
		cfg.Publish.Pattern = "constant"
	}
	if cfg.Publish.PayloadBytes == 0 {
		cfg.Publish.PayloadBytes = 512
	}
	if cfg.DestinationsPerTenant == 0 {
		cfg.DestinationsPerTenant = 1
	}

	return &Group{
		Config:      cfg,
		State:       StateCreated,
		Metrics:     metrics.NewGroupMetrics(),
		MockProfile: mock.NewProfile(cfg.MockProfile.LatencyMs, cfg.MockProfile.JitterMs, cfg.MockProfile.ErrorRate),
		EventLog:    eventlog.NewDefault(),
	}
}

func (g *Group) Transition(to State) error {
	if err := ValidateTransition(g.State, to); err != nil {
		return err
	}
	g.State = to
	if to != StateError {
		g.Error = ""
	}
	return nil
}

func (g *Group) SetError(err error) {
	g.State = StateError
	g.Error = err.Error()
}

func (g *Group) TenantID(index int) string {
	return g.Config.TenantPrefix + "-" + itoa(index)
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
