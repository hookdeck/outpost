package alert

// Default alert values, applied when the corresponding config value is unset.
const (
	DefaultConsecutiveFailureCount       = 100
	DefaultExhaustedRetriesWindowSeconds = 3600
)

// Settings is the resolved, operational alert configuration consumed by the
// service builder. The config package produces it from raw env/yaml values via
// AlertConfig.ToConfig, so the rest of the codebase never deals with the raw
// unset / empty / value strings.
type Settings struct {
	ConsecutiveFailure     ConsecutiveFailureSetting
	ExhaustedRetries       ExhaustedRetriesSetting
	AutoDisableDestination bool
}

// ConsecutiveFailureSetting controls consecutive-failure alerting. When Enabled
// is false the monitor never tracks or alerts on consecutive failures, and
// therefore never auto-disables a destination regardless of AutoDisableDestination.
type ConsecutiveFailureSetting struct {
	Enabled bool
	Count   int
}

// ExhaustedRetriesSetting controls exhausted-retries alerting. When Enabled is
// false the monitor never emits exhausted_retries alerts. WindowSeconds is the
// suppression window for duplicate alerts; 0 means no suppression (alert on
// every exhaustion).
type ExhaustedRetriesSetting struct {
	Enabled       bool
	WindowSeconds int
}
