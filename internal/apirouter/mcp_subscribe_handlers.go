package apirouter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
)

type MCPSubscribeHandlers struct {
	// store is the unfiltered store: it holds the subscriptions that the
	// destinations API hides.
	store   tenantstore.TenantStore
	dest    *DestinationHandlers
	tenants *TenantHandlers
	// ttl is nil while MCP is off; the routes then answer 404.
	ttl *mcpevents.TTLConfig
	// unavailable is why the event definitions are unusable; the routes then answer 503.
	unavailable error
}

func NewMCPSubscribeHandlers(store tenantstore.TenantStore, dest *DestinationHandlers, tenants *TenantHandlers, ttl *mcpevents.TTLConfig, unavailable error) *MCPSubscribeHandlers {
	return &MCPSubscribeHandlers{store: store, dest: dest, tenants: tenants, ttl: ttl, unavailable: unavailable}
}

type mcpSubscribeRequest struct {
	Principal     string          `json:"principal"`
	AllowedTopics []string        `json:"allowed_topics"`
	Params        json.RawMessage `json:"params"`
}

type mcpSubscribeResult struct {
	ResultType     string                 `json:"resultType"`
	ID             string                 `json:"id"`
	RefreshBefore  string                 `json:"refreshBefore"`
	Cursor         *string                `json:"cursor"`
	Truncated      bool                   `json:"truncated"`
	DeliveryStatus *mcpDeliveryStatusInfo `json:"deliveryStatus,omitempty"`
}

type mcpDeliveryStatusInfo struct {
	Active bool `json:"active"`
}

// abortMCPError answers 422 with the error under mcp_error, ready to be
// rethrown as the error of a JSON-RPC response.
func abortMCPError(c *gin.Context, e *mcpevents.Error) {
	_ = c.Error(e)
	c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"mcp_error": e.RPCError()})
}

func abortMCPFailure(c *gin.Context, err error) {
	var mcpErr *mcpevents.Error
	if errors.As(err, &mcpErr) {
		abortMCPError(c, mcpErr)
		return
	}
	var unavailable *destregistry.ErrDestinationUnavailable
	if errors.As(err, &unavailable) {
		AbortWithError(c, http.StatusServiceUnavailable, ErrorResponse{
			Code:    http.StatusServiceUnavailable,
			Message: "verification unavailable",
			Data:    map[string]string{"code": unavailable.Code},
		})
		return
	}
	if errors.Is(err, errSubscriptionBusy) {
		AbortWithError(c, http.StatusServiceUnavailable, ErrorResponse{
			Code:       http.StatusServiceUnavailable,
			Message:    err.Error(),
			RetryAfter: time.Second,
		})
		return
	}
	AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
}

// bindMCPRequest reads the JSON body, or answers the failure and returns false.
func bindMCPRequest(c *gin.Context, req any) bool {
	err := c.ShouldBindJSON(req)
	switch {
	case err == nil:
		return true
	case isRequestBodyTooLarge(err):
		AbortWithValidationError(c, err)
	default:
		abortMCPError(c, mcpevents.InvalidParams(mcpevents.FieldParams, mcpevents.ReasonInvalid))
	}
	return false
}

// enabled answers 404 while MCP is off and 503 while its event definitions
// are invalid.
func (h *MCPSubscribeHandlers) enabled(c *gin.Context) bool {
	if h.ttl == nil {
		AbortWithError(c, http.StatusNotFound, ErrorResponse{Code: http.StatusNotFound, Message: "not found"})
		return false
	}
	if h.unavailable != nil {
		abortMCPUnavailable(c, h.unavailable)
		return false
	}
	return true
}

// tenant returns the tenant of the path, or nil when it does not exist.
func (h *MCPSubscribeHandlers) tenant(ctx context.Context, tenantID string) (*models.Tenant, error) {
	tenant, err := h.store.RetrieveTenant(ctx, tenantID)
	if err != nil && !errors.Is(err, tenantstore.ErrTenantDeleted) {
		return nil, err
	}
	return tenant, nil
}

func (h *MCPSubscribeHandlers) Subscribe(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	var req mcpSubscribeRequest
	if !bindMCPRequest(c, &req) {
		return
	}
	result, err := h.subscribe(c.Request.Context(), c.Param("tenant_id"), req)
	if err != nil {
		abortMCPFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// subscribe creates the subscription, or refreshes it when the same principal
// already holds a live one for the same event, arguments and callback URL.
// The tenant is created when missing.
func (h *MCPSubscribeHandlers) subscribe(ctx context.Context, tenantID string, req mcpSubscribeRequest) (*mcpSubscribeResult, error) {
	if req.Principal == "" {
		return nil, mcpevents.Forbidden(mcpevents.ReasonUnauthenticated)
	}
	params, err := mcpevents.ParseSubscribeParams(req.Params)
	if err != nil {
		return nil, err
	}
	if strings.Contains(params.Name, "*") {
		return nil, mcpevents.NotFound(mcpevents.NotFoundEvent)
	}
	if req.AllowedTopics != nil && !slices.Contains(req.AllowedTopics, params.Name) {
		return nil, mcpevents.Forbidden(mcpevents.ReasonTopicNotAllowed)
	}

	tenant, err := h.tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		if tenant, err = h.tenants.createTenant(ctx, tenantID, nil); err != nil {
			return nil, err
		}
	}

	id := mcpevents.DeriveSubscriptionID(req.Principal, params.URL, params.Name, params.Arguments)
	expiresAt := mcpevents.GrantTTL(params.TTL, *h.ttl, time.Now())

	// A create that loses a race to another subscribe of the same key retries
	// as a refresh.
	for range mcpSubscribeMaxAttempts {
		existing, err := h.store.RetrieveDestination(ctx, tenant.ID, id)
		if err != nil && !errors.Is(err, tenantstore.ErrDestinationDeleted) {
			return nil, err
		}
		if existing != nil && existing.Type != models.DestinationTypeMCP {
			return nil, mcpevents.InvalidParams(mcpevents.FieldParams, mcpevents.ReasonInvalid)
		}
		expired := existing != nil && existing.IsExpired(time.Now())
		if existing != nil && !expired {
			result, err := h.refresh(ctx, tenant, existing, params.Secret, expiresAt)
			if isDestinationWriteRace(err) {
				continue
			}
			return result, err
		}

		// A missing or expired subscription starts fresh.
		destination := h.newDestination(tenant.ID, id, req.Principal, params)
		destination.ExpiresAt = &expiresAt
		if expired {
			destination.Version = existing.Version
		}
		err = h.write(ctx, &destination, nil, expired)
		switch {
		case err == nil:
			h.dest.telemetry.DestinationCreated(ctx, destination.Type)
			h.dest.logger.Ctx(ctx).Audit("mcp subscription created",
				zap.String("tenant_id", tenant.ID),
				zap.String("destination_id", destination.ID),
			)
			return &mcpSubscribeResult{ResultType: "complete", ID: id, RefreshBefore: mcpevents.FormatRefreshBefore(expiresAt)}, nil
		case errors.Is(err, tenantstore.ErrDuplicateDestination) || isDestinationWriteRace(err):
			continue
		case errors.Is(err, tenantstore.ErrTenantNotFound):
			return nil, errSubscriptionBusy
		default:
			return nil, err
		}
	}
	return nil, errSubscriptionBusy
}

// refresh renews a live subscription: same id, a new secret that rotates in
// next to the old one, a new expiry, and re-enabled if it was suspended.
func (h *MCPSubscribeHandlers) refresh(ctx context.Context, tenant *models.Tenant, existing *models.Destination, secret string, expiresAt time.Time) (*mcpSubscribeResult, error) {
	destination := *existing
	destination.Credentials = map[string]string{destmcp.CredentialSecret: secret}
	destination.DisabledAt = nil
	destination.ExpiresAt = &expiresAt
	destination.UpdatedAt = time.Now()

	if err := h.write(ctx, &destination, existing, true); err != nil {
		return nil, err
	}
	h.dest.logger.Ctx(ctx).Audit("mcp subscription refreshed",
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", destination.ID),
	)
	return &mcpSubscribeResult{
		ResultType:     "complete",
		ID:             destination.ID,
		RefreshBefore:  mcpevents.FormatRefreshBefore(expiresAt),
		DeliveryStatus: &mcpDeliveryStatusInfo{Active: existing.DisabledAt == nil},
	}, nil
}

// write validates, verifies and stores a subscription: a refresh of prev, or a
// fresh start when prev is nil. replace overwrites the record that exists
// instead of refusing a duplicate. Validation failures come back as MCP errors,
// store errors as they are.
func (h *MCPSubscribeHandlers) write(ctx context.Context, destination, prev *models.Destination, replace bool) error {
	if prev == nil {
		if err := destination.Validate(h.dest.topics, h.dest.topicsAllowWildcards); err != nil {
			return mapMCPValidationError(err)
		}
		destination.Topics = destination.Topics.Normalize()
		if provider, err := h.dest.registry.ResolveProvider(destination); err == nil {
			if v, ok := provider.(destregistry.CreateValidator); ok {
				if err := v.ValidateCreate(destination); err != nil {
					return mapMCPValidationError(err)
				}
			}
		}
	}
	if err := h.dest.registry.ValidateDestination(ctx, destination); err != nil {
		return mapMCPValidationError(err)
	}
	if err := h.dest.registry.PreprocessDestination(destination, prev, &destregistry.PreprocessDestinationOpts{
		Role: RoleAdmin,
		Request: destregistry.PreprocessRequest{
			Config:      destination.Config,
			Credentials: destination.Credentials,
		},
	}); err != nil {
		return mapMCPValidationError(err)
	}
	if replace {
		return h.store.UpdateDestination(ctx, *destination)
	}
	return h.store.CreateDestination(ctx, *destination)
}

// mcpSubscribeMaxAttempts bounds the re-reads of one subscribe. Each lost write
// means another writer won, so concurrent subscribes of one key finish within
// as many attempts as there are callers.
const mcpSubscribeMaxAttempts = 20

// errSubscriptionBusy answers a subscribe that kept losing to other writers, or
// whose tenant was deleted mid-way. Nothing is left half-written beyond an
// empty tenant, and a retry succeeds.
var errSubscriptionBusy = errors.New("subscription is being changed, retry")

// isDestinationWriteRace reports a write that lost to a concurrent change of
// the same subscription; the caller re-reads and decides again.
func isDestinationWriteRace(err error) bool {
	return errors.Is(err, tenantstore.ErrDestinationConflict) || errors.Is(err, tenantstore.ErrDestinationNotFound)
}

func (h *MCPSubscribeHandlers) newDestination(tenantID, id, principal string, params *mcpevents.SubscribeParams) models.Destination {
	config := map[string]string{
		destmcp.ConfigURL:       params.URL,
		destmcp.ConfigPrincipal: principal,
	}
	if string(params.Arguments) != "{}" {
		config[destmcp.ConfigArguments] = string(params.Arguments)
	}
	now := time.Now()
	return models.Destination{
		ID:          id,
		TenantID:    tenantID,
		Type:        models.DestinationTypeMCP,
		Topics:      models.Topics{params.Name},
		Config:      config,
		Credentials: map[string]string{destmcp.CredentialSecret: params.Secret},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

type mcpUnsubscribeRequest struct {
	Principal string          `json:"principal"`
	Params    json.RawMessage `json:"params"`
}

func (h *MCPSubscribeHandlers) Unsubscribe(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	var req mcpUnsubscribeRequest
	if !bindMCPRequest(c, &req) {
		return
	}
	if req.Principal == "" {
		abortMCPError(c, mcpevents.Forbidden(mcpevents.ReasonUnauthenticated))
		return
	}
	params, err := mcpevents.ParseUnsubscribeParams(req.Params)
	if err != nil {
		abortMCPFailure(c, err)
		return
	}
	ctx := c.Request.Context()
	tenant, err := h.tenant(ctx, c.Param("tenant_id"))
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if tenant == nil {
		abortMCPError(c, mcpevents.NotFound(mcpevents.NotFoundSubscription))
		return
	}
	id := mcpevents.DeriveSubscriptionID(req.Principal, params.URL, params.Name, params.Arguments)
	destination, err := h.store.RetrieveDestination(ctx, tenant.ID, id)
	if err != nil && !errors.Is(err, tenantstore.ErrDestinationDeleted) {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil || destination.Type != models.DestinationTypeMCP || destination.IsExpired(time.Now()) {
		abortMCPError(c, mcpevents.NotFound(mcpevents.NotFoundSubscription))
		return
	}
	if err := h.store.DeleteDestination(ctx, tenant.ID, id); err != nil {
		if errors.Is(err, tenantstore.ErrDestinationNotFound) {
			abortMCPError(c, mcpevents.NotFound(mcpevents.NotFoundSubscription))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	h.dest.logger.Ctx(ctx).Audit("mcp subscription deleted",
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", id),
	)
	c.JSON(http.StatusOK, gin.H{"resultType": "complete"})
}

// mapMCPValidationError turns a destination validation failure into the MCP
// error a client sees: the field and type Outpost's validation reports, or the
// verification failure category. Endpoint output never reaches the client.
func mapMCPValidationError(err error) error {
	if errors.Is(err, models.ErrInvalidTopics) {
		return mcpevents.NotFound(mcpevents.NotFoundEvent)
	}
	var rateLimited *destregistry.ErrDestinationRateLimited
	if errors.As(err, &rateLimited) {
		return mcpevents.ResourceExhausted(mcpevents.LimitVerificationRate, 0, rateLimited.RetryAfter)
	}
	var unavailable *destregistry.ErrDestinationUnavailable
	if errors.As(err, &unavailable) {
		return err
	}
	var validation *destregistry.ErrDestinationValidation
	if !errors.As(err, &validation) || len(validation.Errors) == 0 {
		return mcpevents.InvalidParams(mcpevents.FieldParams, mcpevents.ReasonInvalid)
	}
	for _, d := range validation.Errors {
		switch {
		case d.Field == "topics" && d.Type == "not_found":
			return mcpevents.NotFound(mcpevents.NotFoundEvent)
		case isCallbackFailure(d.Type):
			return mcpevents.CallbackEndpointError(d.Type)
		}
	}
	d := validation.Errors[0]
	return mcpevents.InvalidParams(d.Field, d.Type)
}

func isCallbackFailure(typ string) bool {
	switch typ {
	case mcpevents.ReasonConnectionRefused, mcpevents.ReasonTimeout, mcpevents.ReasonTLSError,
		mcpevents.ReasonChallengeFailed:
		return true
	}
	return false
}
