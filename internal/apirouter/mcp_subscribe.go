package apirouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
)

// mcpSubscribeAttempts bounds the writes of one subscribe while concurrent
// calls keep changing the subscription: a refresh that finds it replaced
// creates it, and a create that finds it already there refreshes it.
const mcpSubscribeAttempts = 3

// mcpSubscribeRequest is the body of PUT /tenants/:tenant_id/mcp/subscriptions.
// Fields stay raw so each is checked, and reported, on its own.
type mcpSubscribeRequest struct {
	Principal     json.RawMessage `json:"principal"`
	Params        json.RawMessage `json:"params"`
	Filter        json.RawMessage `json:"filter"`
	Metadata      json.RawMessage `json:"metadata"`
	AllowedTopics json.RawMessage `json:"allowed_topics"`
}

// mcpUnsubscribeRequest is the body of POST
// /tenants/:tenant_id/mcp/subscriptions/unsubscribe.
type mcpUnsubscribeRequest struct {
	Principal json.RawMessage `json:"principal"`
	Params    json.RawMessage `json:"params"`
}

// MCPSubscribeResult is the events/subscribe result.
type MCPSubscribeResult struct {
	ID string `json:"id"`
	// RefreshBefore is null for a subscription granted no expiry.
	RefreshBefore *string `json:"refreshBefore"`
	// Cursor is always null: Outpost doesn't replay events.
	Cursor    *string `json:"cursor"`
	Truncated bool    `json:"truncated"`
	// DeliveryStatus is set on refreshes only.
	DeliveryStatus *MCPDeliveryStatus `json:"deliveryStatus,omitempty"`
}

// MCPDeliveryStatus is the deliveryStatus of a refreshed subscription.
type MCPDeliveryStatus struct {
	// Active is false when the refresh re-enabled the subscription.
	Active bool `json:"active"`
	// LastDeliveryAt is the last successful delivery, RFC 3339 UTC.
	LastDeliveryAt *string `json:"lastDeliveryAt"`
	// LastError is the category of the last attempt when it failed.
	LastError *string `json:"lastError"`
}

// subscription is a validated subscribe request.
type subscription struct {
	startedAt time.Time
	tenant    *models.Tenant
	principal string
	params    *mcpevents.SubscribeParams
	id        string
	filter    models.Filter
	metadata  models.Metadata
	// metadataSet is false when the request has no metadata: a refresh
	// keeps the stored metadata.
	metadataSet bool
	// expiredDeleted is set once the request deleted an expired
	// subscription with the same ID.
	expiredDeleted bool
	// brokenDeleted is set once the request ended a subscription with the
	// same ID that a breaking change left behind.
	brokenDeleted bool
}

// subscribeOutcome is a successful subscription write.
type subscribeOutcome struct {
	destination models.Destination
	// refreshed is false for a new subscription.
	refreshed bool
	update    tenantstore.UpdateResult
}

// Subscribe handles PUT /tenants/:tenant_id/mcp/subscriptions: it creates or
// refreshes the subscription of an events/subscribe request and returns the
// events/subscribe result, 200 either way.
func (h *MCPHandlers) Subscribe(c *gin.Context) {
	// Deletes and revocations from this instant on win over this call.
	startedAt := h.now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), mcpHandlerTimeout)
	defer cancel()

	var req mcpSubscribeRequest
	if !h.decodeMCPBody(c, &req) {
		return
	}
	principal, mcpErr := parsePrincipal(req.Principal)
	if mcpErr != nil {
		h.abortWithMCPError(c, mcpErr)
		return
	}
	filter, filterSet, mcpErr := parseFilterOverride(req.Filter)
	if mcpErr != nil {
		h.abortWithMCPError(c, mcpErr)
		return
	}
	metadata, metadataSet, mcpErr := parseMetadata(req.Metadata)
	if mcpErr != nil {
		h.abortWithMCPError(c, mcpErr)
		return
	}
	allowedTopics, mcpErr := parseAllowedTopics(req.AllowedTopics)
	if mcpErr != nil {
		h.abortWithMCPError(c, mcpErr)
		return
	}

	tenant, err := h.retrieveTenant(ctx, c.Param("tenant_id"))
	if err != nil {
		h.abortWithError(c, err)
		return
	}
	if tenant == nil {
		h.abortWithMCPError(c, mcpevents.NotFound(mcpevents.NotFoundTenant))
		return
	}

	params, err := mcpevents.ParseSubscribeParams(req.Params)
	if err != nil {
		h.abortWithError(c, err)
		return
	}
	// An event outside allowed_topics gets the error of an unknown one, so
	// the response doesn't reveal that it exists.
	if _, ok := h.eventIndex[params.Name]; !ok || (allowedTopics != nil && !slices.Contains(allowedTopics, params.Name)) {
		h.abortWithMCPError(c, mcpevents.NotFound(mcpevents.NotFoundEvent))
		return
	}
	if !filterSet {
		filter = mcpevents.ArgumentsToFilter(params.ArgumentsMap)
	}

	s := &subscription{
		startedAt:   startedAt,
		tenant:      tenant,
		principal:   principal,
		params:      params,
		id:          mcpevents.DeriveSubscriptionID(principal, params.Delivery.URLString, params.Name, params.Arguments),
		filter:      filter,
		metadata:    metadata,
		metadataSet: metadataSet,
	}
	outcome, err := h.subscribe(ctx, s)
	if (err == nil && !outcome.refreshed) || s.expiredDeleted || s.brokenDeleted {
		h.emitTenantUpdate(ctx, tenant.ID, tenantSnapshotOf(tenant))
	}
	if s.brokenDeleted {
		h.logger.Ctx(ctx).Audit("mcp subscription ended by a schema change",
			zap.String("tenant_id", tenant.ID),
			zap.String("destination_id", s.id),
			zap.String("destination_type", models.DestinationTypeMCP),
			zap.String("topic", params.Name),
		)
	}
	if err != nil {
		h.abortWithError(c, err)
		return
	}

	result := MCPSubscribeResult{
		ID:            s.id,
		RefreshBefore: mcpevents.FormatRefreshBefore(outcome.destination.ExpiresAt),
	}
	action := "mcp subscription created"
	if outcome.refreshed {
		action = "mcp subscription refreshed"
		result.DeliveryStatus = h.deliveryStatus(ctx, &outcome.destination, outcome.update.WasDisabled)
	} else {
		h.resetNewAlerts(ctx, tenant.ID, s.id)
		h.telemetry.DestinationCreated(ctx, models.DestinationTypeMCP)
	}
	h.logger.Ctx(ctx).Audit(action,
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", s.id),
		zap.String("destination_type", models.DestinationTypeMCP),
		zap.String("topic", params.Name),
	)
	c.JSON(http.StatusOK, result)
}

// subscribe writes the subscription: a refresh of the live one, or a new one.
func (h *MCPHandlers) subscribe(ctx context.Context, s *subscription) (*subscribeOutcome, error) {
	existing, err := h.lookupSubscription(ctx, s)
	if err != nil {
		return nil, err
	}

	// Validation verifies the callback, which can take seconds; it holds
	// for every write below, whichever path they take.
	candidate := h.buildSubscription(s)
	if err := h.registry.ValidateDestination(ctx, &candidate); err != nil {
		if mcpErr := mcpValidationError(err); mcpErr != nil {
			return nil, mcpErr
		}
		return nil, err
	}
	candidate.ExpiresAt = mcpevents.GrantTTL(s.params.TTL, h.ttl, h.now())

	alertsReset := false
	for range mcpSubscribeAttempts {
		if existing == nil {
			d, err := h.prepareSubscription(&candidate, nil, s)
			if err != nil {
				return nil, err
			}
			err = h.tenantStore.CreateDestination(ctx, d, h.createOptions(s)...)
			if err == nil {
				return &subscribeOutcome{destination: d}, nil
			}
			if !errors.Is(err, tenantstore.ErrDuplicateDestination) {
				return nil, mcpWriteError(err)
			}
			// Created meanwhile by a concurrent call: refresh it.
			if existing, err = h.lookupSubscription(ctx, s); err != nil {
				return nil, err
			}
			continue
		}

		// Reset the failure count before re-enabling, so the first failure
		// after it doesn't disable the subscription again.
		if existing.DisabledAt != nil && !alertsReset {
			alertsReset = h.resetAlerts(ctx, s.tenant.ID, s.id)
		}
		d, err := h.prepareSubscription(&candidate, existing, s)
		if err != nil {
			return nil, err
		}
		result, err := h.tenantStore.UpdateDestinationIfLive(ctx, d, existing.CreatedAt, h.refreshOptions(s)...)
		if err == nil {
			h.afterReenable(ctx, s.tenant.ID, s.id, result, alertsReset)
			return &subscribeOutcome{destination: d, refreshed: true, update: result}, nil
		}
		if !errors.Is(err, tenantstore.ErrDestinationConflict) &&
			!errors.Is(err, tenantstore.ErrDestinationDeleted) &&
			!errors.Is(err, tenantstore.ErrDestinationNotFound) {
			return nil, mcpWriteError(err)
		}
		// Deleted or replaced meanwhile: create it. A delete since the call
		// started (unsubscribe, revoke) makes the create fail as revoked.
		existing = nil
	}
	return nil, errMCPConflict
}

// lookupSubscription returns the live subscription with the request's ID, or
// nil. An expired one is deleted (and reported) first, so the request
// creates a new generation instead of extending it. One created against a
// payload schema that a breaking change broke is deleted too, and the
// request fails with schema_changed: refreshing it would record the current
// schema hash on it and hide it from the mcp-subscriptions worker, while its
// client still expects the old payloads. The client learns it from the
// error, so no terminated envelope is sent; it re-reads events/list and
// subscribes again. Another destination type with the ID is a conflict.
func (h *MCPHandlers) lookupSubscription(ctx context.Context, s *subscription) (*models.Destination, error) {
	for range 2 {
		existing, err := h.tenantStore.RetrieveDestination(ctx, s.tenant.ID, s.id)
		if errors.Is(err, tenantstore.ErrDestinationDeleted) || errors.Is(err, tenantstore.ErrDestinationNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, nil
		}
		if existing.Type != models.DestinationTypeMCP {
			return nil, mcpevents.InvalidParams(mcpFieldID, mcpevents.ReasonConflict)
		}
		now := h.now()
		if !existing.IsExpired(now) {
			if !h.schemaBroken(existing) {
				return existing, nil
			}
			createdAt := existing.CreatedAt
			result, err := h.tenantStore.DeleteDestinationIf(ctx, s.tenant.ID, s.id, tenantstore.DeleteCondition{
				Type:              models.DestinationTypeMCP,
				ExpectedCreatedAt: &createdAt,
				Reason:            tenantstore.DeleteReasonTerminated,
			})
			if err != nil {
				return nil, err
			}
			if result.Deleted {
				s.brokenDeleted = true
			}
			if result.Deleted || result.Gone {
				return nil, mcpevents.SchemaChanged()
			}
			// Changed since the read (refreshed or replaced): read it again.
			continue
		}
		createdAt := existing.CreatedAt
		result, err := h.tenantStore.DeleteDestinationIf(ctx, s.tenant.ID, s.id, tenantstore.DeleteCondition{
			Type:              models.DestinationTypeMCP,
			ExpectedCreatedAt: &createdAt,
			ExpiredBefore:     &now,
			Reason:            tenantstore.DeleteReasonExpired,
		})
		if err != nil {
			return nil, err
		}
		if result.Deleted {
			s.expiredDeleted = true
			h.emitExpired(ctx, existing)
			return nil, nil
		}
		if result.Gone {
			return nil, nil
		}
		// Changed since the read (refreshed or replaced): read it again.
	}
	return nil, errMCPConflict
}

// schemaBroken reports whether d was created against a payload schema that a
// breaking change broke. This instance's own schema never counts: an
// instance running a rolled-back configuration keeps refreshing its
// subscriptions, which the worker of the applied configuration ends.
func (h *MCPHandlers) schemaBroken(d *models.Destination) bool {
	if h.deps == nil || h.deps.BrokenSchemas == nil {
		return false
	}
	event, hash := mcpEventName(d), d.Config[mcpConfigSchemaHash]
	return hash != h.schemaHashes[event] && h.deps.BrokenSchemas.IsBroken(event, hash)
}

// buildSubscription builds the mcp destination of the request, without the
// fields that depend on the write (timestamps, metadata, expiry).
func (h *MCPHandlers) buildSubscription(s *subscription) models.Destination {
	return models.Destination{
		ID:       s.id,
		TenantID: s.tenant.ID,
		Type:     models.DestinationTypeMCP,
		Topics:   models.Topics{s.params.Name},
		Filter:   s.filter,
		Config: models.Config{
			mcpConfigURL:            s.params.Delivery.URLString,
			mcpConfigSubscriptionID: s.id,
			mcpConfigPrincipal:      s.principal,
			mcpConfigEvent:          s.params.Name,
			mcpConfigArguments:      string(s.params.Arguments),
			mcpConfigSchemaHash:     h.schemaHashes[s.params.Name],
		},
		Credentials: models.Credentials{
			mcpCredentialSecret: s.params.Delivery.Secret,
		},
	}
}

// prepareSubscription returns the destination to write: a copy of candidate
// for a new subscription (existing nil) or a refresh of existing, run
// through the provider's Preprocess (secret rotation).
func (h *MCPHandlers) prepareSubscription(candidate *models.Destination, existing *models.Destination, s *subscription) (models.Destination, error) {
	d := *candidate
	d.Config = maps.Clone(candidate.Config)
	d.Credentials = maps.Clone(candidate.Credentials)
	d.DisabledAt = nil
	// Stores keep milliseconds: the generation (created_at) must read back
	// as written.
	now := h.now().Truncate(time.Millisecond)
	d.UpdatedAt = now
	d.Metadata = s.metadata
	if existing == nil {
		d.CreatedAt = now
	} else {
		d.CreatedAt = existing.CreatedAt
		if !s.metadataSet {
			d.Metadata = existing.Metadata
		}
	}
	err := h.registry.PreprocessDestination(&d, existing, &destregistry.PreprocessDestinationOpts{
		Role: RoleAdmin,
		Request: destregistry.PreprocessRequest{
			Config:      maps.Clone(candidate.Config),
			Credentials: maps.Clone(candidate.Credentials),
		},
	})
	if err != nil {
		if mcpErr := mcpValidationError(err); mcpErr != nil {
			return d, mcpErr
		}
		return d, err
	}
	return d, nil
}

// createOptions guard a new subscription: the tenant's and the principal's
// limits, and deletes or revocations since the call started.
func (h *MCPHandlers) createOptions(s *subscription) []tenantstore.WriteOption {
	// The store sets the limit of the type bucket (TypeLimits).
	buckets := []tenantstore.Bucket{{Name: tenantstore.TypeBucket(models.DestinationTypeMCP)}}
	if max := h.deps.Config.MaxSubscriptionsPerPrincipal; max > 0 {
		buckets = append(buckets, tenantstore.Bucket{Name: mcpPrincipalBucket(s.principal), Max: max})
	}
	return []tenantstore.WriteOption{
		tenantstore.WithBuckets(buckets...),
		tenantstore.WithNotDeletedSince(s.startedAt),
		tenantstore.WithFence(mcpRevocationFence(s.principal)),
	}
}

// refreshOptions guard a refresh against a revocation since the call
// started, and move parked retries out when it re-enables the subscription.
func (h *MCPHandlers) refreshOptions(s *subscription) []tenantstore.WriteOption {
	opts := []tenantstore.WriteOption{
		tenantstore.WithNotDeletedSince(s.startedAt),
		tenantstore.WithFence(mcpRevocationFence(s.principal)),
	}
	if h.deps.Resumer != nil {
		opts = append(opts, tenantstore.WithResumeParkedRetries())
	}
	return opts
}

// deliveryStatus reads the deliveryStatus of a refreshed subscription, or
// nil when its record can't be read. Records of an earlier generation of the
// ID are ignored.
func (h *MCPHandlers) deliveryStatus(ctx context.Context, d *models.Destination, wasDisabled bool) *MCPDeliveryStatus {
	status := &MCPDeliveryStatus{Active: !wasDisabled}
	if h.deps.StatusReader == nil {
		return status
	}
	record, err := h.deps.StatusReader.GetAttemptStatus(ctx, d.TenantID, d.ID)
	if err != nil {
		h.logger.Ctx(ctx).Warn("failed to read mcp delivery status",
			zap.String("tenant_id", d.TenantID),
			zap.String("destination_id", d.ID),
			zap.Error(err))
		return nil
	}
	if record == nil {
		return status
	}
	generation := d.CreatedAt.Truncate(time.Millisecond)
	if !record.LastSuccessAt.IsZero() && !record.LastSuccessAt.Before(generation) {
		at := record.LastSuccessAt.UTC().Format(time.RFC3339)
		status.LastDeliveryAt = &at
	}
	if record.LastAttemptFailed() && !record.LastAttemptAt.Before(generation) {
		category := mcpevents.LastErrorCategory(record.LastCode)
		status.LastError = &category
	}
	return status
}

// Unsubscribe handles POST /tenants/:tenant_id/mcp/subscriptions/unsubscribe:
// it deletes the subscription the events/unsubscribe params name and returns
// {} whether or not there was one. The event isn't checked against the
// catalog, so subscriptions to an ended topic can still be removed.
func (h *MCPHandlers) Unsubscribe(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), mcpHandlerTimeout)
	defer cancel()

	var req mcpUnsubscribeRequest
	if !h.decodeMCPBody(c, &req) {
		return
	}
	principal, mcpErr := parsePrincipal(req.Principal)
	if mcpErr != nil {
		h.abortWithMCPError(c, mcpErr)
		return
	}
	params, err := mcpevents.ParseUnsubscribeParams(req.Params)
	if err != nil {
		h.abortWithError(c, err)
		return
	}

	// The principal is part of the ID: only its own subscription matches.
	tenantID := c.Param("tenant_id")
	id := mcpevents.DeriveSubscriptionID(principal, params.URLString, params.Name, params.Arguments)
	result, err := h.tenantStore.DeleteDestinationIf(ctx, tenantID, id, tenantstore.DeleteCondition{
		Type:   models.DestinationTypeMCP,
		Reason: tenantstore.DeleteReasonUnsubscribed,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if result.Deleted {
		h.emitTenantUpdateAfterDelete(ctx, tenantID, result.Topics)
		h.logger.Ctx(ctx).Audit("mcp subscription unsubscribed",
			zap.String("tenant_id", tenantID),
			zap.String("destination_id", id),
			zap.String("destination_type", models.DestinationTypeMCP),
		)
	}
	c.JSON(http.StatusOK, gin.H{})
}

// abortWithError answers an error of the MCP endpoints: an *mcpevents.Error
// as mcp_error; an outage the client can retry through (callback
// verification that couldn't run, the call's deadline or cancellation) as
// 503; anything else as an internal error.
func (h *MCPHandlers) abortWithError(c *gin.Context, err error) {
	var mcpErr *mcpevents.Error
	if errors.As(err, &mcpErr) {
		h.abortWithMCPError(c, mcpErr)
		return
	}
	// The message of a validation error leaves out its details and cause,
	// which the server error's log needs.
	var validationErr *destregistry.ErrDestinationValidation
	if errors.As(err, &validationErr) {
		if validationErr.Cause != nil {
			err = validationErr.Cause
		} else {
			err = fmt.Errorf("%w: %v", err, validationErr.Errors)
		}
	}
	if errors.Is(err, destmcp.ErrVerificationUnavailable) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		AbortWithError(c, http.StatusServiceUnavailable, NewErrServiceUnavailable(err))
		return
	}
	AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
}

// mcpWriteError maps a failed subscription write to its mcp_error when it
// has one.
func mcpWriteError(err error) error {
	if errors.Is(err, tenantstore.ErrTenantDeleted) {
		return mcpevents.NotFound(mcpevents.NotFoundTenant)
	}
	if mcpErr := mcpStoreError(err); mcpErr != nil {
		return mcpErr
	}
	return err
}

// retrieveTenant returns the tenant, or nil when it doesn't exist or was
// deleted.
func (h *MCPHandlers) retrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error) {
	if tenantID == "" {
		return nil, nil
	}
	tenant, err := h.tenantStore.RetrieveTenant(ctx, tenantID)
	if errors.Is(err, tenantstore.ErrTenantDeleted) || errors.Is(err, tenantstore.ErrTenantNotFound) {
		return nil, nil
	}
	return tenant, err
}

// parseFilterOverride reads the optional filter of subscribe: an object of
// at most 8 KiB as canonical JSON. An empty object means no filter.
func parseFilterOverride(raw json.RawMessage) (models.Filter, bool, *mcpevents.Error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, false, nil
	}
	// Canonical numbers can be longer than their source ("1e20"), so only a
	// generous bound is checked before parsing.
	if len(raw) > 8*mcpMaxFilterBytes {
		return nil, false, mcpevents.InvalidParams(mcpFieldFilter, mcpevents.ReasonTooLarge)
	}
	canonical, err := mcpevents.CanonicalizeJSON(raw)
	if err != nil {
		return nil, false, mcpevents.InvalidParams(mcpFieldFilter, mcpevents.ReasonInvalid)
	}
	if canonical[0] != '{' {
		return nil, false, mcpevents.InvalidParams(mcpFieldFilter, mcpevents.ReasonInvalidType)
	}
	if len(canonical) > mcpMaxFilterBytes {
		return nil, false, mcpevents.InvalidParams(mcpFieldFilter, mcpevents.ReasonTooLarge)
	}
	var filter models.Filter
	if err := json.Unmarshal(canonical, &filter); err != nil {
		return nil, false, mcpevents.InvalidParams(mcpFieldFilter, mcpevents.ReasonInvalid)
	}
	if len(filter) == 0 {
		filter = nil
	}
	return filter, true, nil
}

// parseMetadata reads the optional metadata of subscribe, a string map.
func parseMetadata(raw json.RawMessage) (models.Metadata, bool, *mcpevents.Error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, false, nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, false, mcpevents.InvalidParams(mcpFieldMetadata, mcpevents.ReasonInvalidType)
	}
	return metadata, true, nil
}

// parseAllowedTopics reads the optional allowed_topics of subscribe: nil when
// absent, empty (allowing nothing) when an empty array.
func parseAllowedTopics(raw json.RawMessage) ([]string, *mcpevents.Error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var topics []string
	if err := json.Unmarshal(raw, &topics); err != nil {
		return nil, mcpevents.InvalidParams(mcpFieldAllowedTopics, mcpevents.ReasonInvalidType)
	}
	if topics == nil {
		topics = []string{}
	}
	return topics, nil
}
