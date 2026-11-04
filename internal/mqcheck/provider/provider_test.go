package provider_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/provider"
)

// Names as the runner makes them, judged as Sweep judges them.
func TestShouldSweep(t *testing.T) {
	old := provider.ResourceName("mqcheck", provider.RunName(time.Now().Add(-3*time.Hour)), "c4-1") + "-dlq-sub"
	fresh := provider.ResourceName("mqcheck", provider.RunName(time.Now()), "c4-1") + "-sub"
	other := provider.ResourceName("mqcheck-alex", provider.RunName(time.Now().Add(-3*time.Hour)), "c4-1")
	for _, tc := range []struct {
		name, prefix string
		olderThan    time.Duration
		want         bool
	}{
		{old, "mqcheck", 2 * time.Hour, true},
		{fresh, "mqcheck", 2 * time.Hour, false},
		{fresh, "mqcheck", 0, true},
		{old, "mq", 2 * time.Hour, false},                         // a prefix that only starts the same way
		{other, "mqcheck", 0, false},                              // someone else's run under "mqcheck-alex"
		{"mqcheck-manual-queue", "mqcheck", 0, false},             // no run id: never swept
		{"mqcheck-manualqueu-x", "mqcheck", 2 * time.Hour, false}, // 10 chars but no plausible time
		{"other-queue", "mqcheck", 0, false},
	} {
		if got := provider.ShouldSweep(tc.name, tc.prefix, tc.olderThan); got != tc.want {
			t.Errorf("ShouldSweep(%q, %q, %s) = %v, want %v", tc.name, tc.prefix, tc.olderThan, got, tc.want)
		}
	}
}

func TestNameTime(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	got, ok := provider.NameTime(provider.ResourceName("mqcheck", provider.RunName(now), "c1-1"), "mqcheck")
	if !ok || !got.Equal(now) {
		t.Fatalf("NameTime = %v, %v; want %v", got, ok, now)
	}
}

func TestPublishEnvParsesOutpostConfig(t *testing.T) {
	qc, err := provider.OutpostQueueConfig(t.Context(), map[string]string{
		"AWS_SQS_REGION": "us-east-1", "AWS_SQS_DELIVERY_QUEUE": "q1", "AWS_SQS_ENDPOINT": "http://localhost:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if qc.AWSSQS == nil || qc.AWSSQS.Topic != "q1" {
		t.Fatalf("unexpected queue config %+v", qc)
	}
}
