package apirouter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"go.uber.org/zap"
)

const (
	// mcpHandlerTimeout bounds each MCP endpoint, callback verification (up
	// to 10s by itself) included. The docs tell operators to wait longer.
	mcpHandlerTimeout = 20 * time.Second
	// mcpMaxPrincipalBytes bounds the principal of subscribe and unsubscribe.
	mcpMaxPrincipalBytes = 512
	// mcpMaxFilterBytes bounds a subscribe filter override, as canonical JSON.
	mcpMaxFilterBytes = 8 << 10
	// mcpRevocationTTL is how long a revoke by principal keeps rejecting the
	// subscribe calls that started before it.
	mcpRevocationTTL = 10 * time.Minute
	// mcpAsyncTimeout bounds the work done after responding.
	mcpAsyncTimeout = 10 * time.Second
	// mcpMaxAsync bounds the goroutines doing it. Past it, the work runs in
	// the request instead.
	mcpMaxAsync = 256

	// events/list paging.
	mcpEventsDefaultLimit = 100
	mcpEventsMaxLimit     = 100
	// mcpEventsPageBytes is the size a page grows to at most, unless its
	// first entry alone is larger.
	mcpEventsPageBytes = 1 << 20
	// mcpEventsCursorPrefix starts a decoded events/list cursor, followed by
	// the last name of the previous page.
	mcpEventsCursorPrefix = "t:"
	// mcpMaxCursorBytes bounds the cursor parameter: a topic name, encoded.
	mcpMaxCursorBytes = 4 << 10

	// Subscription lifetimes when MCPHandlerConfig.TTL leaves them unset:
	// the MCP_TTL_* defaults.
	defaultMCPTTL    = time.Hour
	defaultMCPTTLMin = 5 * time.Minute
	defaultMCPTTLMax = 24 * time.Hour
)

// mcp destination config and credential keys.
const (
	mcpConfigURL            = "url"
	mcpConfigSubscriptionID = "subscription_id"
	mcpConfigPrincipal      = "principal"
	mcpConfigEvent          = "event"
	mcpConfigArguments      = "arguments"
	mcpConfigSchemaHash     = "schema_hash"

	mcpCredentialSecret                  = "secret"
	mcpCredentialPreviousSecret          = "previous_secret"
	mcpCredentialPreviousSecretInvalidAt = "previous_secret_invalid_at"
)

// invalid_params fields of the request envelope, beside those of params.
const (
	mcpFieldPrincipal     = "principal"
	mcpFieldFilter        = "filter"
	mcpFieldMetadata      = "metadata"
	mcpFieldAllowedTopics = "allowed_topics"
	mcpFieldID            = "id"
	mcpFieldCursor        = "cursor"
	mcpFieldLimit         = "limit"
)

// TopicMCPSubscriptionExpired is the operator event for an MCP subscription
// deleted after its expires_at.
const TopicMCPSubscriptionExpired = opevents.TopicMCPSubscriptionExpired

// MCPSubscriptionExpiredData is the data of mcp.subscription.expired.
type MCPSubscriptionExpiredData = opevents.MCPSubscriptionExpiredData

// MCPSubscriptionExpiredEvent builds the mcp.subscription.expired event of an
// expired subscription, as the mcp-subscriptions worker does.
func MCPSubscriptionExpiredEvent(d *models.Destination) opevents.Event {
	return opevents.MCPSubscriptionExpiredEvent(opevents.NewMCPSubscriptionExpiredData(d))
}

// MCPHandlers serves the MCP Events endpoints of API v2: the events/list,
// events/subscribe and events/unsubscribe results an operator's MCP server
// forwards, and the operator's own subscription endpoints.
type MCPHandlers struct {
	logger      *logging.Logger
	telemetry   telemetry.Telemetry
	tenantStore tenantstore.TenantStore
	registry    destregistry.Registry
	// deps is nil when MCP Events isn't configured.
	deps    *MCPDeps
	profile mcpevents.CodeProfile
	ttl     mcpevents.TTLConfig
	now     func() time.Time

	// events are the events/list entries, in TOPICS order; eventIndex maps a
	// name to its position. Both are immutable.
	events     []mcpEventEntry
	eventIndex map[string]int
	// schemaHashes holds the hash recorded on subscriptions to each
	// MCP-enabled topic.
	schemaHashes map[string]string

	// async bounds the goroutines doing work after responses.
	async chan struct{}
}

type mcpEventEntry struct {
	name string
	json []byte
}

// NewMCPHandlers builds the MCP handlers over the topic catalog. deps nil
// makes every MCP endpoint answer 503.
func NewMCPHandlers(logger *logging.Logger, telemetry telemetry.Telemetry, tenantStore tenantstore.TenantStore, registry destregistry.Registry, catalog *topicschema.Catalog, deps *MCPDeps) *MCPHandlers {
	h := &MCPHandlers{
		logger:       logger,
		telemetry:    telemetry,
		tenantStore:  tenantStore,
		registry:     registry,
		deps:         deps,
		now:          time.Now,
		eventIndex:   map[string]int{},
		schemaHashes: map[string]string{},
		async:        make(chan struct{}, mcpMaxAsync),
	}
	if deps != nil {
		h.profile = deps.Config.CodeProfile
		if deps.Now != nil {
			h.now = deps.Now
		}
		// A zero lifetime would expire subscriptions as they are granted:
		// unset values get the documented defaults.
		ttl := deps.Config.TTL
		if ttl.Default <= 0 {
			ttl.Default = defaultMCPTTL
		}
		if ttl.Min <= 0 {
			ttl.Min = min(defaultMCPTTLMin, ttl.Default)
		}
		if ttl.Max <= 0 {
			ttl.Max = max(defaultMCPTTLMax, ttl.Default)
		}
		h.ttl = ttl
	}
	snapshot := catalog.Snapshot()
	for _, event := range catalog.MCPEvents() {
		h.eventIndex[event.Name] = len(h.events)
		h.events = append(h.events, mcpEventEntry{name: event.Name, json: event.JSON})
		h.schemaHashes[event.Name] = snapshot.TopicHash(event.Name)
	}
	return h
}

// RequireConfigured answers 503 while MCP Events isn't configured. It runs
// after authentication.
func (h *MCPHandlers) RequireConfigured(c *gin.Context) {
	if h.deps == nil {
		AbortWithError(c, http.StatusServiceUnavailable, ErrorResponse{
			Code:    http.StatusServiceUnavailable,
			Message: "mcp events is not configured",
		})
		return
	}
	c.Next()
}

// ===== events/list =====

// ListEvents handles GET /tenants/:tenant_id/mcp/events: the events/list
// result. The catalog is instance-wide, so the tenant isn't looked up.
func (h *MCPHandlers) ListEvents(c *gin.Context) {
	query := c.Request.URL.Query()

	limit := mcpEventsDefaultLimit
	if v := query.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > mcpEventsMaxLimit {
			h.abortWithMCPError(c, mcpevents.InvalidParams(mcpFieldLimit, mcpevents.ReasonInvalid))
			return
		}
		limit = n
	}

	start := 0
	if v := query.Get("cursor"); v != "" {
		i, ok := h.decodeEventsCursor(v)
		if !ok {
			h.abortWithMCPError(c, mcpevents.InvalidParams(mcpFieldCursor, mcpevents.ReasonInvalid))
			return
		}
		start = i + 1
	}

	// topics narrows the list: comma-separated, repeated or both. Present
	// but empty lists nothing.
	var allowed map[string]struct{}
	if values, ok := query["topics"]; ok {
		allowed = map[string]struct{}{}
		for _, v := range values {
			for name := range strings.SplitSeq(v, ",") {
				if _, known := h.eventIndex[name]; known {
					allowed[name] = struct{}{}
				}
			}
		}
	}

	c.Data(http.StatusOK, jsonContentType, h.eventsPage(start, limit, allowed))
}

// eventsPage renders the events/list result from position start: up to limit
// allowed entries (nil allows all) within mcpEventsPageBytes, at least one.
func (h *MCPHandlers) eventsPage(start, limit int, allowed map[string]struct{}) []byte {
	const prefix, suffix = `{"events":[`, `]}`
	page := make([]int, 0, min(limit, len(h.events)))
	size := len(prefix) + len(suffix)
	more := false
	for i := start; i < len(h.events); i++ {
		if allowed != nil {
			if _, ok := allowed[h.events[i].name]; !ok {
				continue
			}
		}
		entrySize := len(h.events[i].json) + 1 // and a comma
		if len(page) == limit || (len(page) > 0 && size+entrySize > mcpEventsPageBytes) {
			more = true
			break
		}
		page = append(page, i)
		size += entrySize
	}

	var cursor string
	if more {
		cursor = base64.RawURLEncoding.EncodeToString([]byte(mcpEventsCursorPrefix + h.events[page[len(page)-1]].name))
		size += len(`,"nextCursor":""`) + len(cursor)
	}
	b := make([]byte, 0, size)
	b = append(b, `{"events":[`...)
	for n, i := range page {
		if n > 0 {
			b = append(b, ',')
		}
		b = append(b, h.events[i].json...)
	}
	b = append(b, ']')
	if more {
		// The cursor is base64url: nothing to escape.
		b = append(b, `,"nextCursor":"`...)
		b = append(b, cursor...)
		b = append(b, '"')
	}
	return append(b, '}')
}

// decodeEventsCursor returns the position of the last entry of the page the
// cursor ends.
func (h *MCPHandlers) decodeEventsCursor(cursor string) (int, bool) {
	if len(cursor) > mcpMaxCursorBytes {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(cursor, "="))
	if err != nil {
		return 0, false
	}
	name, ok := strings.CutPrefix(string(raw), mcpEventsCursorPrefix)
	if !ok {
		return 0, false
	}
	i, ok := h.eventIndex[name]
	return i, ok
}

// ===== operator endpoints =====

// MCPSubscription is a subscription as GET .../mcp/subscriptions lists it.
type MCPSubscription struct {
	ID         string          `json:"id"`
	Principal  string          `json:"principal"`
	Event      string          `json:"event"`
	Arguments  json.RawMessage `json:"arguments"`
	URL        string          `json:"url"`
	Filter     models.Filter   `json:"filter"`
	ExpiresAt  *time.Time      `json:"expires_at"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	DisabledAt *time.Time      `json:"disabled_at"`
	Metadata   models.Metadata `json:"metadata"`
}

func newMCPSubscription(d *models.Destination) MCPSubscription {
	args := json.RawMessage(d.Config[mcpConfigArguments])
	if len(args) == 0 || args[0] != '{' || !json.Valid(args) {
		args = json.RawMessage("{}")
	}
	return MCPSubscription{
		ID:         d.ID,
		Principal:  d.Config[mcpConfigPrincipal],
		Event:      mcpEventName(d),
		Arguments:  args,
		URL:        d.Config[mcpConfigURL],
		Filter:     d.Filter,
		ExpiresAt:  d.ExpiresAt,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
		DisabledAt: d.DisabledAt,
		Metadata:   d.Metadata,
	}
}

// mcpEventName is the subscribed event of an mcp destination.
func mcpEventName(d *models.Destination) string {
	if name := d.Config[mcpConfigEvent]; name != "" {
		return name
	}
	if len(d.Topics) > 0 {
		return d.Topics[0]
	}
	return ""
}

// ListSubscriptions handles GET /tenants/:tenant_id/mcp/subscriptions, with
// optional principal and topic filters.
func (h *MCPHandlers) ListSubscriptions(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), mcpHandlerTimeout)
	defer cancel()
	tenant := mustTenantFromContext(c)
	principal, topic := c.Query("principal"), c.Query("topic")

	destinations, err := h.tenantStore.ListDestination(ctx, tenantstore.ListDestinationRequest{
		TenantID: tenant.ID,
		Type:     []string{models.DestinationTypeMCP},
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	subscriptions := make([]MCPSubscription, 0, len(destinations))
	for i := range destinations {
		d := &destinations[i]
		if d.Type != models.DestinationTypeMCP ||
			(principal != "" && d.Config[mcpConfigPrincipal] != principal) ||
			(topic != "" && mcpEventName(d) != topic) {
			continue
		}
		subscriptions = append(subscriptions, newMCPSubscription(d))
	}
	c.JSON(http.StatusOK, subscriptions)
}

// DeleteSubscription handles DELETE
// /tenants/:tenant_id/mcp/subscriptions/:subscription_id: it revokes one
// subscription and sends it a terminated envelope. Tenant JWTs may call it
// (the portal's disconnect).
func (h *MCPHandlers) DeleteSubscription(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), mcpHandlerTimeout)
	defer cancel()
	tenant := mustTenantFromContext(c)
	prev := tenantSnapshotOf(tenant)
	id := c.Param("subscription_id")

	// A second pass covers a subscription replaced (expired, then created
	// again) between the read and the delete.
	for range 2 {
		destination, err := h.tenantStore.RetrieveDestination(ctx, tenant.ID, id)
		if err != nil && !errors.Is(err, tenantstore.ErrDestinationDeleted) && !errors.Is(err, tenantstore.ErrDestinationNotFound) {
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		if destination == nil || destination.Type != models.DestinationTypeMCP {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("subscription"))
			return
		}

		createdAt := destination.CreatedAt
		result, err := h.tenantStore.DeleteDestinationIf(ctx, tenant.ID, id, tenantstore.DeleteCondition{
			Type:              models.DestinationTypeMCP,
			ExpectedCreatedAt: &createdAt,
			Reason:            tenantstore.DeleteReasonRevoked,
		})
		if err != nil {
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		switch {
		case result.Deleted:
			h.terminate(ctx, destination)
			h.emitTenantUpdate(ctx, tenant.ID, prev)
			h.logger.Ctx(ctx).Audit("mcp subscription revoked",
				zap.String("tenant_id", tenant.ID),
				zap.String("destination_id", id),
				zap.String("destination_type", models.DestinationTypeMCP),
			)
			c.JSON(http.StatusOK, gin.H{"success": true})
			return
		case result.Gone:
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("subscription"))
			return
		}
	}
	AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(errors.New("subscription changed while being revoked")))
}

// DeleteSubscriptions handles DELETE
// /tenants/:tenant_id/mcp/subscriptions?principal=: it revokes every
// subscription of a principal, after fencing the subscribe calls of that
// principal already in flight.
func (h *MCPHandlers) DeleteSubscriptions(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), mcpHandlerTimeout)
	defer cancel()
	tenant := mustTenantFromContext(c)
	prev := tenantSnapshotOf(tenant)
	principal := c.Query("principal")
	if principal == "" {
		h.abortWithMCPError(c, mcpevents.InvalidParams(mcpFieldPrincipal, mcpevents.ReasonRequired))
		return
	}

	// The fence goes first: a subscribe that started before it can't create
	// or refresh a subscription of the principal anymore, so the list below
	// misses none that would outlive this call.
	if err := h.tenantStore.WriteFence(ctx, tenant.ID, mcpRevocationFence(principal), h.now(), mcpRevocationTTL); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	destinations, err := h.tenantStore.ListDestination(ctx, tenantstore.ListDestinationRequest{
		TenantID: tenant.ID,
		Type:     []string{models.DestinationTypeMCP},
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	deleted := 0
	for i := range destinations {
		d := &destinations[i]
		if d.Type != models.DestinationTypeMCP || d.Config[mcpConfigPrincipal] != principal {
			continue
		}
		createdAt := d.CreatedAt
		result, err := h.tenantStore.DeleteDestinationIf(ctx, tenant.ID, d.ID, tenantstore.DeleteCondition{
			Type:              models.DestinationTypeMCP,
			ExpectedCreatedAt: &createdAt,
			Reason:            tenantstore.DeleteReasonRevoked,
		})
		if err != nil {
			if deleted > 0 {
				h.emitTenantUpdate(ctx, tenant.ID, prev)
			}
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		if result.Deleted {
			deleted++
			h.terminate(ctx, d)
		}
	}

	if deleted > 0 {
		h.emitTenantUpdate(ctx, tenant.ID, prev)
	}
	h.logger.Ctx(ctx).Audit("mcp subscriptions of principal revoked",
		zap.String("tenant_id", tenant.ID),
		zap.Int("deleted", deleted),
	)
	c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
}

// terminate queues the terminated envelope (Forbidden, access_revoked) of a
// revoked subscription. Never blocks: a full queue drops it.
func (h *MCPHandlers) terminate(ctx context.Context, d *models.Destination) {
	if h.deps == nil || h.deps.Notifier == nil {
		return
	}
	if !h.deps.Notifier.Enqueue(mcpevents.Termination{
		TenantID:       d.TenantID,
		SubscriptionID: d.ID,
		URL:            d.Config[mcpConfigURL],
		Secrets:        mcpSecrets(d.Credentials),
		CreatedAt:      d.CreatedAt,
		Error:          mcpevents.AccessRevoked().WithProfile(h.profile),
	}) {
		h.logger.Ctx(ctx).Warn("mcp terminated envelope not queued",
			zap.String("tenant_id", d.TenantID),
			zap.String("destination_id", d.ID))
	}
}

// mcpSecrets returns the signing secrets of an mcp destination: the current
// one, and the previous one until its invalid_at during a rotation.
// Unreadable secrets are left out.
func mcpSecrets(credentials map[string]string) []mcpevents.Secret {
	var secrets []mcpevents.Secret
	if key, err := mcpevents.DecodeSecret(credentials[mcpCredentialSecret]); err == nil {
		secrets = append(secrets, mcpevents.Secret{Key: key})
	}
	if previous := credentials[mcpCredentialPreviousSecret]; previous != "" {
		key, err := mcpevents.DecodeSecret(previous)
		invalidAt, terr := time.Parse(time.RFC3339, credentials[mcpCredentialPreviousSecretInvalidAt])
		if err == nil && terr == nil {
			secrets = append(secrets, mcpevents.Secret{Key: key, InvalidAt: &invalidAt})
		}
	}
	return secrets
}

// ===== shared =====

// abortWithMCPError answers 422 with the {"mcp_error": {...}} body, codes
// from the configured profile. The error is attached to the context for the
// request log only: the error handler leaves written responses alone.
func (h *MCPHandlers) abortWithMCPError(c *gin.Context, e *mcpevents.Error) {
	e = e.WithProfile(h.profile)
	body, err := e.MarshalJSON()
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.Data(http.StatusUnprocessableEntity, jsonContentType, body)
	c.Abort()
	_ = c.Error(e)
}

// decodeMCPBody decodes the JSON object body of an MCP request into v. A body
// that isn't a JSON object gets the standard error (413, 422): only the
// operator's code builds it. A field of the wrong type is invalid_params.
func (h *MCPHandlers) decodeMCPBody(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			field, _, _ := strings.Cut(typeErr.Field, ".")
			h.abortWithMCPError(c, mcpevents.InvalidParams(field, mcpevents.ReasonInvalidType))
			return false
		}
		AbortWithValidationError(c, err)
		return false
	}
	return true
}

// parsePrincipal reads the principal of subscribe and unsubscribe: a string
// of 1 to 512 bytes.
func parsePrincipal(raw json.RawMessage) (string, *mcpevents.Error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", mcpevents.InvalidParams(mcpFieldPrincipal, mcpevents.ReasonRequired)
	}
	var principal string
	if err := json.Unmarshal(raw, &principal); err != nil {
		return "", mcpevents.InvalidParams(mcpFieldPrincipal, mcpevents.ReasonInvalidType)
	}
	if principal == "" {
		return "", mcpevents.InvalidParams(mcpFieldPrincipal, mcpevents.ReasonRequired)
	}
	if len(principal) > mcpMaxPrincipalBytes {
		return "", mcpevents.InvalidParams(mcpFieldPrincipal, mcpevents.ReasonTooLarge)
	}
	return principal, nil
}

func principalDigest(principal string) string {
	sum := sha256.Sum256([]byte(principal))
	return hex.EncodeToString(sum[:])
}

// mcpPrincipalBucket names the limit bucket of a principal's subscriptions.
func mcpPrincipalBucket(principal string) string {
	return mcpPrincipalBucketPrefix + principalDigest(principal)[:32]
}

const mcpPrincipalBucketPrefix = "mcp_principal:"

// mcpRevocationFence names the fence a revoke by principal writes.
func mcpRevocationFence(principal string) string {
	return "mcp_revoked:" + principalDigest(principal)
}

// goAsync runs fn after the response, on a context detached from the
// request's cancellation and bounded by mcpAsyncTimeout. When too much is
// already running, fn runs right away instead.
func (h *MCPHandlers) goAsync(ctx context.Context, name string, fn func(ctx context.Context)) {
	ctx = context.WithoutCancel(ctx)
	run := func() {
		defer func() {
			if r := recover(); r != nil {
				h.logger.Ctx(ctx).Error("mcp background task panicked",
					zap.String("task", name), zap.Any("panic", r))
			}
		}()
		ctx, cancel := context.WithTimeout(ctx, mcpAsyncTimeout)
		defer cancel()
		fn(ctx)
	}
	select {
	case h.async <- struct{}{}:
		go func() {
			defer func() { <-h.async }()
			run()
		}()
	default:
		run()
	}
}

// emitTenantUpdate emits tenant.subscription.updated, after the response,
// when the tenant's topics or destination count differ from prev.
func (h *MCPHandlers) emitTenantUpdate(ctx context.Context, tenantID string, prev tenantSnapshot) {
	if h.deps == nil || h.deps.Emitter == nil {
		return
	}
	h.goAsync(ctx, "tenant.subscription.updated", func(ctx context.Context) {
		tenant, err := h.tenantStore.RetrieveTenant(ctx, tenantID)
		if err != nil || tenant == nil {
			if err != nil && !errors.Is(err, tenantstore.ErrTenantDeleted) {
				h.logger.Ctx(ctx).Error("failed to retrieve tenant for subscription update", zap.Error(err))
			}
			return
		}
		if slices.Equal(tenant.Topics, prev.topics) && tenant.DestinationsCount == prev.destinationsCount {
			return
		}
		if err := h.deps.Emitter.Emit(ctx, opevents.TenantSubscriptionUpdatedEvent(opevents.TenantSubscriptionUpdatedData{
			TenantID:                  tenantID,
			Topics:                    tenant.Topics,
			PreviousTopics:            prev.topics,
			DestinationsCount:         tenant.DestinationsCount,
			PreviousDestinationsCount: prev.destinationsCount,
		})); err != nil {
			h.logger.Ctx(ctx).Error("failed to emit subscription update", zap.Error(err))
		}
	})
}

// emitTenantUpdateAfterDelete is emitTenantUpdate for a deletion without a
// snapshot from before it: the previous state is the current one plus the
// deleted destination.
func (h *MCPHandlers) emitTenantUpdateAfterDelete(ctx context.Context, tenantID string, deletedTopics []string) {
	if h.deps == nil || h.deps.Emitter == nil {
		return
	}
	h.goAsync(ctx, "tenant.subscription.updated", func(ctx context.Context) {
		tenant, err := h.tenantStore.RetrieveTenant(ctx, tenantID)
		if err != nil || tenant == nil {
			if err != nil && !errors.Is(err, tenantstore.ErrTenantDeleted) {
				h.logger.Ctx(ctx).Error("failed to retrieve tenant for subscription update", zap.Error(err))
			}
			return
		}
		prevTopics := tenant.Topics
		if !slices.Equal(tenant.Topics, []string{"*"}) {
			set := slices.Clone(tenant.Topics)
			for _, topic := range deletedTopics {
				if !slices.Contains(set, topic) {
					set = append(set, topic)
				}
			}
			slices.Sort(set)
			prevTopics = set
		}
		if err := h.deps.Emitter.Emit(ctx, opevents.TenantSubscriptionUpdatedEvent(opevents.TenantSubscriptionUpdatedData{
			TenantID:                  tenantID,
			Topics:                    tenant.Topics,
			PreviousTopics:            prevTopics,
			DestinationsCount:         tenant.DestinationsCount,
			PreviousDestinationsCount: tenant.DestinationsCount + 1,
		})); err != nil {
			h.logger.Ctx(ctx).Error("failed to emit subscription update", zap.Error(err))
		}
	})
}

// emitExpired emits mcp.subscription.expired for a subscription this request
// deleted because it had expired.
func (h *MCPHandlers) emitExpired(ctx context.Context, d *models.Destination) {
	if h.deps == nil || h.deps.Emitter == nil {
		return
	}
	event := MCPSubscriptionExpiredEvent(d)
	h.goAsync(ctx, TopicMCPSubscriptionExpired, func(ctx context.Context) {
		if err := h.deps.Emitter.Emit(ctx, event); err != nil {
			h.logger.Ctx(ctx).Error("failed to emit mcp subscription expiry", zap.Error(err))
		}
	})
}

// resetAlerts resets the consecutive-failure count of a destination being
// re-enabled, so that its next failure doesn't disable it again. It reports
// whether the count was reset.
func (h *MCPHandlers) resetAlerts(ctx context.Context, tenantID, destinationID string) bool {
	if h.deps == nil || h.deps.AlertResetter == nil {
		return true
	}
	if err := h.deps.AlertResetter.ResetConsecutiveFailureCount(ctx, tenantID, destinationID); err != nil {
		h.logger.Ctx(ctx).Error("failed to reset consecutive failures of mcp subscription",
			zap.String("tenant_id", tenantID),
			zap.String("destination_id", destinationID),
			zap.Error(err))
		return false
	}
	return true
}

// afterReenable finishes a write that re-enabled a subscription or moved its
// parked retries: the alert reset, unless done before the write, and the
// resume of the parked retries.
func (h *MCPHandlers) afterReenable(ctx context.Context, tenantID, destinationID string, result tenantstore.UpdateResult, alertsReset bool) {
	if h.deps == nil {
		return
	}
	if (result.WasDisabled || result.ResumeKey != "") && !alertsReset {
		h.goAsync(ctx, "mcp alert reset", func(ctx context.Context) {
			h.resetAlerts(ctx, tenantID, destinationID)
		})
	}
	if result.ResumeKey != "" && h.deps.Resumer != nil {
		h.deps.Resumer.Resume(context.WithoutCancel(ctx), tenantID, destinationID, result.ResumeKey)
	}
}

// reenable is PUT .../destinations/:id/enable for an mcp destination: like a
// refresh, it clears disabled_at only while the subscription is live, resets
// its consecutive failures first and resumes its parked retries. It returns
// the subscription as stored after the enable.
func (h *MCPHandlers) reenable(ctx context.Context, destination *models.Destination) (*models.Destination, error) {
	if destination.DisabledAt == nil {
		return destination, nil
	}
	alertsReset := h.resetAlerts(ctx, destination.TenantID, destination.ID)

	// Only disabled_at is cleared, so a refresh landing meanwhile keeps its
	// expiry and secret; the parked retries move to a resume set in the
	// same step.
	var opts []tenantstore.WriteOption
	if h.deps != nil && h.deps.Resumer != nil {
		opts = append(opts, tenantstore.WithResumeParkedRetries())
	}
	result, err := h.tenantStore.EnableDestination(ctx, destination.TenantID, destination.ID, opts...)
	if err != nil {
		if errors.Is(err, tenantstore.ErrDestinationDeleted) {
			return nil, tenantstore.ErrDestinationNotFound
		}
		return nil, err
	}
	h.afterReenable(ctx, destination.TenantID, destination.ID, result, alertsReset)

	current, err := h.tenantStore.RetrieveDestination(ctx, destination.TenantID, destination.ID)
	if errors.Is(err, tenantstore.ErrDestinationDeleted) || (err == nil && current == nil) {
		return nil, tenantstore.ErrDestinationNotFound
	}
	if err != nil {
		return nil, err
	}
	return current, nil
}

// errMCPConflict reports a subscription that kept changing under concurrent
// calls.
var errMCPConflict = errors.New("mcp subscription changed concurrently")

// mcpStoreError maps a failed subscription write to its mcp_error, or nil
// for an internal error.
func mcpStoreError(err error) *mcpevents.Error {
	var limit *tenantstore.ErrLimitReached
	switch {
	case errors.As(err, &limit):
		if strings.HasPrefix(limit.Bucket, mcpPrincipalBucketPrefix) {
			return mcpevents.ResourceExhausted(mcpevents.LimitPrincipalSubscriptions, limit.Max, 0)
		}
		return mcpevents.ResourceExhausted(mcpevents.LimitSubscriptions, limit.Max, 0)
	case errors.Is(err, tenantstore.ErrMaxDestinationsPerTenantReached):
		return mcpevents.ResourceExhausted(mcpevents.LimitSubscriptions, 0, 0)
	case errors.Is(err, tenantstore.ErrDestinationRevoked):
		return mcpevents.NotFound(mcpevents.NotFoundSubscription)
	}
	return nil
}

// unknownValidationError is the detail ValidateDestination reports for a
// provider error that isn't a validation error.
var unknownValidationError = destregistry.ValidationErrorDetail{Field: "root", Type: "unknown"}

// mcpValidationFields maps the destination fields of a provider validation
// error to the request fields they come from.
var mcpValidationFields = map[string]string{
	"config.url":         mcpevents.FieldDeliveryURL,
	"credentials.secret": mcpevents.FieldDeliverySecret,
	"config.arguments":   mcpevents.FieldArguments,
	"config.event":       mcpevents.FieldName,
	"topics":             mcpevents.FieldName,
}

// mcpValidationError maps an error of ValidateDestination or
// PreprocessDestination to its mcp_error: the provider's own when it carries
// one, else invalid_params from the first field error. Other errors are
// internal and return nil: no provider, and the root error the registry
// substitutes for a provider error that isn't a validation error.
func mcpValidationError(err error) *mcpevents.Error {
	var mcpErr *mcpevents.Error
	if errors.As(err, &mcpErr) {
		return mcpErr
	}
	var validationErr *destregistry.ErrDestinationValidation
	if errors.As(err, &validationErr) {
		if len(validationErr.Errors) == 1 && validationErr.Errors[0] == unknownValidationError {
			return nil
		}
		field, reason := mcpevents.FieldParams, mcpevents.ReasonInvalid
		if len(validationErr.Errors) > 0 {
			detail := validationErr.Errors[0]
			field, reason = detail.Field, detail.Type
			if mapped, ok := mcpValidationFields[field]; ok {
				field = mapped
			}
		}
		return mcpevents.InvalidParams(field, reason)
	}
	return nil
}

// tenantSnapshotOf captures the tenant's derived state before a destination
// mutation.
func tenantSnapshotOf(tenant *models.Tenant) tenantSnapshot {
	return tenantSnapshot{
		topics:            tenant.Topics,
		destinationsCount: tenant.DestinationsCount,
	}
}
