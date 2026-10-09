package apirouter

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/deliverystatus"
	"github.com/hookdeck/outpost/internal/mcpevents"
)

// MCPNotifier queues terminated envelopes for subscriptions Outpost ends. It
// must not block. Satisfied by *mcpevents.Notifier.
type MCPNotifier interface {
	// Enqueue reports whether t was queued; a full queue drops it.
	Enqueue(t mcpevents.Termination) bool
}

// ParkedRetryResumer schedules the automatic retries a disabled MCP
// subscription held, once a refresh or an enable has moved them to a resume
// set (tenantstore.UpdateResult.ResumeKey). Resume returns at once and does
// the work in the background, on a context of its own.
type ParkedRetryResumer interface {
	Resume(ctx context.Context, tenantID, destinationID, resumeKey string)
}

// AlertResetter clears a destination's consecutive-failure count. Satisfied
// by alert.AlertStore.
type AlertResetter interface {
	ResetConsecutiveFailureCount(ctx context.Context, tenantID, destinationID string) error
}

// DeliveryStatusReader reads the latest delivery outcome of a destination,
// nil when none was recorded. Satisfied by *deliverystatus.RedisStore.
type DeliveryStatusReader interface {
	GetAttemptStatus(ctx context.Context, tenantID, destinationID string) (*deliverystatus.Status, error)
}

// BrokenSchemas reports the topic hashes a forced breaking change
// (TOPICS_ALLOW_BREAKING_CHANGES) left behind: subscriptions created against
// them must end. Satisfied by topicschema.BrokenSet, which app.PreRun reads
// once after applying the topic schemas, the only time it changes.
type BrokenSchemas interface {
	IsBroken(topic, schemaHash string) bool
}

// MCPHandlerConfig holds the MCP Events settings of the MCP endpoints.
type MCPHandlerConfig struct {
	// TTL is the subscription lifetime policy (MCP_TTL_*,
	// MCP_ALLOW_NO_EXPIRY).
	TTL mcpevents.TTLConfig
	// CodeProfile numbers the codes of mcp_error bodies and terminated
	// envelopes (MCP_ERROR_CODES); "" is sketch.
	CodeProfile mcpevents.CodeProfile
	// MaxSubscriptionsPerPrincipal limits the subscriptions of one principal
	// in a tenant (MAX_MCP_SUBSCRIPTIONS_PER_PRINCIPAL); 0 turns it off. The
	// per-tenant limit is the tenant store's TypeLimits for "mcp".
	MaxSubscriptionsPerPrincipal int
	// ServerURL is the operator's MCP server URL (MCP_SERVER_URL), filled
	// into the mcp destination type's instructions.
	ServerURL string
}

// MCPDeps are the dependencies of the MCP Events endpoints. Without them
// (RouterDeps.MCP nil) the endpoints are still routed in API v2 and answer
// 503. Every interface field is optional: a nil one skips its side effect.
type MCPDeps struct {
	// Notifier sends terminated envelopes when a subscription is revoked.
	Notifier MCPNotifier
	// Emitter emits tenant.subscription.updated and
	// mcp.subscription.expired. Handlers call it after responding, from a
	// background goroutine.
	Emitter SubscriptionEmitter
	// Resumer resumes the parked retries of a re-enabled subscription.
	Resumer ParkedRetryResumer
	// AlertResetter resets the consecutive-failure count of a re-enabled
	// subscription.
	AlertResetter AlertResetter
	// StatusReader reads deliveryStatus for refreshes. Without it a refresh
	// reports no delivery yet.
	StatusReader DeliveryStatusReader
	// BrokenSchemas makes a refresh of a subscription a forced breaking
	// change left behind end it instead. Without it such a subscription is
	// left to the mcp-subscriptions worker.
	BrokenSchemas BrokenSchemas
	Config        MCPHandlerConfig
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}
