package apirouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	destmetadata "github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/maputil"
	"go.uber.org/zap"
)

// SubscriptionEmitter emits operator events for subscription changes.
// Satisfied by opevents.Emitter.
type SubscriptionEmitter interface {
	Emit(ctx context.Context, ev opevents.Event) error
}

type DestinationHandlers struct {
	logger               *logging.Logger
	telemetry            telemetry.Telemetry
	tenantStore          tenantstore.TenantStore
	emitter              SubscriptionEmitter
	topics               []string
	topicsAllowWildcards bool
	registry             destregistry.Registry
	displayer            *destinationDisplayer

	// maxDestinationsPerTenant, when positive, lets Create refuse a full
	// tenant before a callback verification is sent.
	maxDestinationsPerTenant int
}

func NewDestinationHandlers(logger *logging.Logger, telemetry telemetry.Telemetry, tenantStore tenantstore.TenantStore, emitter SubscriptionEmitter, topics []string, topicsAllowWildcards bool, registry destregistry.Registry, displayer *destinationDisplayer, maxDestinationsPerTenant int) *DestinationHandlers {
	return &DestinationHandlers{
		logger:               logger,
		telemetry:            telemetry,
		tenantStore:          tenantStore,
		emitter:              emitter,
		topics:               topics,
		topicsAllowWildcards: topicsAllowWildcards,
		registry:             registry,
		displayer:            displayer,

		maxDestinationsPerTenant: maxDestinationsPerTenant,
	}
}

func (h *DestinationHandlers) List(c *gin.Context) {
	tenant := mustTenantFromContext(c)

	destinations, err := h.tenantStore.ListDestination(c.Request.Context(), tenantstore.ListDestinationRequest{
		TenantID:       tenant.ID,
		Type:           ParseArrayQueryParam(c, "type"),
		Topics:         ParseArrayQueryParam(c, "topics"),
		AllowWildcards: h.topicsAllowWildcards,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	displayDestinations, err := h.displayer.DisplayList(destinations)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	c.JSON(http.StatusOK, displayDestinations)
}

func (h *DestinationHandlers) Create(c *gin.Context) {
	var input CreateDestinationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if mustRoleFromContext(c) != RoleAdmin && (input.CreatedAt != nil || input.UpdatedAt != nil) {
		AbortWithError(c, http.StatusForbidden, ErrorResponse{
			Code:    http.StatusForbidden,
			Message: "created_at and updated_at can only be set with API key authentication",
		})
		return
	}
	now := time.Now()
	if input.CreatedAt != nil && input.CreatedAt.After(now) {
		AbortWithValidationError(c, errors.New("created_at cannot be in the future"))
		return
	}
	if input.UpdatedAt != nil && input.UpdatedAt.After(now) {
		AbortWithValidationError(c, errors.New("updated_at cannot be in the future"))
		return
	}
	if input.DisabledAt != nil && input.DisabledAt.After(now) {
		AbortWithValidationError(c, errors.New("disabled_at cannot be in the future"))
		return
	}

	if input.Type == models.DestinationTypeMCP && h.mcpEnabled() && mustRoleFromContext(c) != RoleAdmin {
		abortMCPAdminOnly(c)
		return
	}
	if input.ExpiresAt != nil {
		if err := validateExpiresAt(input.Type, *input.ExpiresAt, now); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}

	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)

	destination := input.ToDestination(tenant.ID)
	if err := destination.Validate(h.topics, h.topicsAllowWildcards); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	destination.Topics = destination.Topics.Normalize()
	if destination.Type == models.DestinationTypeMCP && !h.canCreateDestination(c, &destination) {
		return
	}
	if provider, err := h.registry.ResolveProvider(&destination); err == nil {
		if v, ok := provider.(destregistry.CreateValidator); ok {
			if err := v.ValidateCreate(&destination); err != nil {
				AbortWithValidationError(c, err)
				return
			}
		}
	}
	if err := h.registry.ValidateDestination(c.Request.Context(), &destination); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.registry.PreprocessDestination(&destination, nil, &destregistry.PreprocessDestinationOpts{
		Role: mustRoleFromContext(c),
		Request: destregistry.PreprocessRequest{
			Config:      destination.Config,
			Credentials: destination.Credentials,
		},
	}); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.tenantStore.CreateDestination(c.Request.Context(), destination); err != nil {
		h.handleDestinationWriteError(c, err)
		return
	}
	h.telemetry.DestinationCreated(c.Request.Context(), destination.Type)
	h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)
	h.logger.Ctx(c.Request.Context()).Audit("destination created",
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", destination.ID),
		zap.String("destination_type", destination.Type),
	)

	display, err := h.displayer.Display(&destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusCreated, display)
}

// canCreateDestination refuses a duplicate ID or a full tenant before the
// create sends a verification challenge to a caller-chosen URL. It writes the
// error response and returns false when the create cannot succeed. The store
// still enforces both atomically.
func (h *DestinationHandlers) canCreateDestination(c *gin.Context, destination *models.Destination) bool {
	ctx := c.Request.Context()
	if existing, err := h.tenantStore.RetrieveDestination(ctx, destination.TenantID, destination.ID); err == nil && existing != nil {
		h.handleDestinationWriteError(c, tenantstore.ErrDuplicateDestination)
		return false
	}
	if h.maxDestinationsPerTenant <= 0 {
		return true
	}
	destinations, err := h.tenantStore.ListDestination(ctx, tenantstore.ListDestinationRequest{TenantID: destination.TenantID})
	if err != nil {
		return true
	}
	now := time.Now()
	active := 0
	for i := range destinations {
		if !destinations[i].IsExpired(now) {
			active++
		}
	}
	if active >= h.maxDestinationsPerTenant {
		h.handleDestinationWriteError(c, tenantstore.ErrMaxDestinationsPerTenantReached)
		return false
	}
	return true
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	destination := mustRetrieveDestination(c, h.tenantStore, tenant.ID, c.Param("destination_id"))
	if destination == nil {
		return
	}

	display, err := h.displayer.Display(destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) Update(c *gin.Context) {
	// Parse & validate request.
	var input UpdateDestinationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	// Retrieve, apply and write; retry from a fresh read when another write
	// landed in between.
	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)
	var updatedDestination *models.Destination
	disabilityChanged := false
	for attempt := 0; ; attempt++ {
		originalDestination := mustRetrieveDestination(c, h.tenantStore, tenant.ID, c.Param("destination_id"))
		if originalDestination == nil {
			return
		}
		var ok bool
		updatedDestination, disabilityChanged, ok = h.applyDestinationUpdate(c, input, originalDestination)
		if !ok {
			return
		}
		err := h.tenantStore.UpdateDestination(c.Request.Context(), *updatedDestination)
		if err == nil {
			break
		}
		if errors.Is(err, tenantstore.ErrDestinationConflict) && attempt+1 < tenantstore.MaxDestinationWriteAttempts {
			continue
		}
		h.handleDestinationWriteError(c, err)
		return
	}
	h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)
	h.logger.Ctx(c.Request.Context()).Audit("destination updated",
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", updatedDestination.ID),
		zap.String("destination_type", updatedDestination.Type),
	)
	if disabilityChanged {
		action := "destination enabled"
		if updatedDestination.DisabledAt != nil {
			action = "destination disabled"
		}
		h.logger.Ctx(c.Request.Context()).Audit(action,
			zap.String("tenant_id", tenant.ID),
			zap.String("destination_id", updatedDestination.ID),
			zap.String("destination_type", updatedDestination.Type),
		)
	}

	display, err := h.displayer.Display(updatedDestination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

// applyDestinationUpdate applies the PATCH input to the destination as it
// stands now. It answers the request and returns ok=false when the input is
// invalid for that destination.
func (h *DestinationHandlers) applyDestinationUpdate(c *gin.Context, input UpdateDestinationRequest, originalDestination *models.Destination) (updated *models.Destination, disabilityChanged bool, ok bool) {
	if originalDestination.Type == models.DestinationTypeMCP && mustRoleFromContext(c) != RoleAdmin {
		abortMCPAdminOnly(c)
		return nil, false, false
	}

	updatedDestination := *originalDestination

	// Validate.
	if input.Topics != nil {
		updatedDestination.Topics = input.Topics
		if err := updatedDestination.Validate(h.topics, h.topicsAllowWildcards); err != nil {
			AbortWithValidationError(c, err)
			return nil, false, false
		}
		updatedDestination.Topics = updatedDestination.Topics.Normalize()
	}
	shouldRevalidate := false
	if input.Type != "" && input.Type != originalDestination.Type {
		AbortWithValidationError(c, errors.New("type cannot be updated"))
		return nil, false, false
	}

	// Config (merge-patch)
	configResult, configRequest, configChanged, err := applyMergePatchStringMap(originalDestination.Config, input.Config)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid config: %w", err))
		return nil, false, false
	}
	if configChanged {
		shouldRevalidate = true
		updatedDestination.Config = configResult
	}

	// Credentials (merge-patch)
	credsResult, credsRequest, credsChanged, err := applyMergePatchStringMap(originalDestination.Credentials, input.Credentials)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid credentials: %w", err))
		return nil, false, false
	}
	if credsChanged {
		shouldRevalidate = true
		updatedDestination.Credentials = credsResult
	}

	// Filter (full replacement)
	if input.Filter != nil {
		if isJSONNull(input.Filter) {
			updatedDestination.Filter = nil
		} else {
			var filter models.Filter
			if err := json.Unmarshal(input.Filter, &filter); err != nil {
				AbortWithValidationError(c, fmt.Errorf("invalid filter: %w", err))
				return nil, false, false
			}
			updatedDestination.Filter = filter
		}
	}

	// DeliveryMetadata (merge-patch)
	dmResult, _, dmChanged, err := applyMergePatchStringMap(originalDestination.DeliveryMetadata, input.DeliveryMetadata)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid delivery_metadata: %w", err))
		return nil, false, false
	}
	if dmChanged {
		updatedDestination.DeliveryMetadata = dmResult
	}

	// Metadata (merge-patch)
	metaResult, _, metaChanged, err := applyMergePatchStringMap(originalDestination.Metadata, input.Metadata)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid metadata: %w", err))
		return nil, false, false
	}
	if metaChanged {
		updatedDestination.Metadata = metaResult
	}

	// DisabledAt
	//   omitted: leave alone
	//   null:    enable (clear)
	//   <ts>:    disable at that time
	disabilityChanged = false
	if input.DisabledAt != nil {
		if isJSONNull(input.DisabledAt) {
			if updatedDestination.DisabledAt != nil {
				updatedDestination.DisabledAt = nil
				disabilityChanged = true
			}
		} else {
			var ts time.Time
			if err := json.Unmarshal(input.DisabledAt, &ts); err != nil {
				var parseErr *time.ParseError
				if errors.As(err, &parseErr) {
					err = errors.New(expectedRFC3339)
				}
				AbortWithValidationError(c, fmt.Errorf("invalid disabled_at: %w", err))
				return nil, false, false
			}
			if ts.After(time.Now()) {
				AbortWithValidationError(c, errors.New("disabled_at cannot be in the future"))
				return nil, false, false
			}
			if updatedDestination.DisabledAt == nil || !updatedDestination.DisabledAt.Equal(ts) {
				updatedDestination.DisabledAt = &ts
				disabilityChanged = true
			}
		}
	}

	// ExpiresAt
	//   omitted: leave alone
	//   null:    clear
	//   <ts>:    expire at that time
	if input.ExpiresAt != nil {
		if isJSONNull(input.ExpiresAt) {
			updatedDestination.ExpiresAt = nil
		} else {
			var ts time.Time
			if err := json.Unmarshal(input.ExpiresAt, &ts); err != nil {
				var parseErr *time.ParseError
				if errors.As(err, &parseErr) {
					err = errors.New(expectedRFC3339)
				}
				AbortWithValidationError(c, fmt.Errorf("invalid expires_at: %w", err))
				return nil, false, false
			}
			if err := validateExpiresAt(originalDestination.Type, ts, time.Now()); err != nil {
				AbortWithValidationError(c, err)
				return nil, false, false
			}
			updatedDestination.ExpiresAt = &ts
		}
	}

	if originalDestination.Type == models.DestinationTypeMCP {
		if err := checkMCPImmutable(originalDestination, &updatedDestination); err != nil {
			AbortWithValidationError(c, err)
			return nil, false, false
		}
	}

	// Always preprocess before updating
	if err := h.registry.PreprocessDestination(&updatedDestination, originalDestination, &destregistry.PreprocessDestinationOpts{
		Role: mustRoleFromContext(c),
		Request: destregistry.PreprocessRequest{
			Config:      configRequest,
			Credentials: credsRequest,
		},
	}); err != nil {
		AbortWithValidationError(c, err)
		return nil, false, false
	}

	if shouldRevalidate {
		if err := h.registry.ValidateDestination(c.Request.Context(), &updatedDestination); err != nil {
			AbortWithValidationError(c, err)
			return nil, false, false
		}
	}

	updatedDestination.UpdatedAt = time.Now()
	return &updatedDestination, disabilityChanged, true
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)
	destination := mustRetrieveDestination(c, h.tenantStore, tenant.ID, c.Param("destination_id"))
	if destination == nil {
		return
	}
	if err := h.tenantStore.DeleteDestination(c.Request.Context(), destination.TenantID, destination.ID); err != nil {
		if errors.Is(err, tenantstore.ErrDestinationNotFound) {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)
	h.logger.Ctx(c.Request.Context()).Audit("destination deleted",
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", destination.ID),
		zap.String("destination_type", destination.Type),
	)

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *DestinationHandlers) Disable(c *gin.Context) {
	h.setDisabilityHandler(c, true)
}

func (h *DestinationHandlers) Enable(c *gin.Context) {
	h.setDisabilityHandler(c, false)
}

func (h *DestinationHandlers) ListProviderMetadata(c *gin.Context) {
	types := h.registry.ListProviderMetadata()
	if mustRoleFromContext(c) != RoleAdmin {
		types = slices.DeleteFunc(slices.Clone(types), func(m *destmetadata.ProviderMetadata) bool {
			return m.Type == models.DestinationTypeMCP
		})
	}
	c.JSON(http.StatusOK, types)
}

func (h *DestinationHandlers) RetrieveProviderMetadata(c *gin.Context) {
	providerType := c.Param("type")
	metadata, err := h.registry.RetrieveProviderMetadata(providerType)
	hidden := providerType == models.DestinationTypeMCP && mustRoleFromContext(c) != RoleAdmin
	if err != nil || hidden {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination type"))
		return
	}
	c.JSON(http.StatusOK, metadata)
}

func (h *DestinationHandlers) setDisabilityHandler(c *gin.Context, disabled bool) {
	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)
	var destination *models.Destination
	written := false
	for attempt := 0; ; attempt++ {
		destination = mustRetrieveDestination(c, h.tenantStore, tenant.ID, c.Param("destination_id"))
		if destination == nil {
			return
		}
		if disabled && destination.DisabledAt == nil {
			now := time.Now()
			destination.DisabledAt = &now
		} else if !disabled && destination.DisabledAt != nil {
			destination.DisabledAt = nil
		} else {
			break
		}
		err := h.tenantStore.UpdateDestination(c.Request.Context(), *destination)
		if err == nil {
			written = true
			break
		}
		if errors.Is(err, tenantstore.ErrDestinationConflict) && attempt+1 < tenantstore.MaxDestinationWriteAttempts {
			continue
		}
		h.handleDestinationWriteError(c, err)
		return
	}
	if written {
		h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)
		action := "destination enabled"
		if disabled {
			action = "destination disabled"
		}
		h.logger.Ctx(c.Request.Context()).Audit(action,
			zap.String("tenant_id", tenant.ID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
		)
	}

	display, err := h.displayer.Display(destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

// mustRetrieveDestination returns the tenant's destination, or answers the
// request and returns nil: 404 when it is deleted or does not exist.
func mustRetrieveDestination(c *gin.Context, store tenantstore.TenantStore, tenantID, destinationID string) *models.Destination {
	destination, err := store.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		if errors.Is(err, tenantstore.ErrDestinationDeleted) {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
			return nil
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return nil
	}
	if destination == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
		return nil
	}
	return destination
}

func (h *DestinationHandlers) handleDestinationWriteError(c *gin.Context, err error) {
	if strings.Contains(err.Error(), "validation failed") {
		AbortWithValidationError(c, err)
		return
	}
	if errors.Is(err, tenantstore.ErrDestinationNotFound) {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
		return
	}
	if errors.Is(err, tenantstore.ErrTenantNotFound) {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("tenant"))
		return
	}
	if errors.Is(err, tenantstore.ErrDestinationConflict) {
		AbortWithError(c, http.StatusConflict, NewErrConflict("destination was modified by another request, retry"))
		return
	}
	if errors.Is(err, tenantstore.ErrDuplicateDestination) {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}
	if errors.Is(err, tenantstore.ErrMaxDestinationsPerTenantReached) {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}
	AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
}

// ===== Requests =====

type CreateDestinationRequest struct {
	ID               string                  `json:"id" binding:"-"`
	Type             string                  `json:"type" binding:"required"`
	Topics           models.Topics           `json:"topics" binding:"required"`
	Filter           models.Filter           `json:"filter,omitempty" binding:"-"`
	Config           models.Config           `json:"config" binding:"-"`
	Credentials      models.Credentials      `json:"credentials" binding:"-"`
	DeliveryMetadata models.DeliveryMetadata `json:"delivery_metadata,omitempty" binding:"-"`
	Metadata         models.Metadata         `json:"metadata,omitempty" binding:"-"`
	CreatedAt        *time.Time              `json:"created_at,omitempty" binding:"-"`
	UpdatedAt        *time.Time              `json:"updated_at,omitempty" binding:"-"`
	DisabledAt       *time.Time              `json:"disabled_at,omitempty" binding:"-"`
	ExpiresAt        *time.Time              `json:"expires_at,omitempty" binding:"-"`
}

func (r *CreateDestinationRequest) ToDestination(tenantID string) models.Destination {
	if r.ID == "" {
		r.ID = idgen.Destination()
	}
	if r.Config == nil {
		r.Config = make(map[string]string)
	}
	if r.Credentials == nil {
		r.Credentials = make(map[string]string)
	}

	now := time.Now()
	createdAt := now
	if r.CreatedAt != nil {
		createdAt = *r.CreatedAt
	}
	updatedAt := createdAt
	if r.UpdatedAt != nil {
		updatedAt = *r.UpdatedAt
	}
	return models.Destination{
		ID:               r.ID,
		Type:             r.Type,
		Topics:           r.Topics,
		Filter:           r.Filter,
		Config:           r.Config,
		Credentials:      r.Credentials,
		DeliveryMetadata: r.DeliveryMetadata,
		Metadata:         r.Metadata,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		DisabledAt:       r.DisabledAt,
		ExpiresAt:        r.ExpiresAt,
		TenantID:         tenantID,
	}
}

type UpdateDestinationRequest struct {
	Type             string          `json:"type" binding:"-"`
	Topics           models.Topics   `json:"topics" binding:"-"`
	Filter           json.RawMessage `json:"filter" binding:"-"`
	Config           json.RawMessage `json:"config" binding:"-"`
	Credentials      json.RawMessage `json:"credentials" binding:"-"`
	DeliveryMetadata json.RawMessage `json:"delivery_metadata" binding:"-"`
	Metadata         json.RawMessage `json:"metadata" binding:"-"`
	DisabledAt       json.RawMessage `json:"disabled_at" binding:"-"`
	ExpiresAt        json.RawMessage `json:"expires_at" binding:"-"`
}

// isJSONNull checks if raw JSON bytes represent a JSON null literal.
func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 4 && string(raw) == "null"
}

// applyMergePatchStringMap applies RFC 7396 merge-patch semantics for a map[string]string field.
// Returns (result, request, changed, error), where request is the patch as
// sent by the caller (string-coerced, nulls dropped):
//   - raw is nil (field omitted): returns original unchanged
//   - raw is "null": returns nil (clear field)
//   - raw is "{}": returns original unchanged (empty merge = no-op)
//   - raw is an object: merge-patch into original
func applyMergePatchStringMap(original map[string]string, raw json.RawMessage) (map[string]string, map[string]string, bool, error) {
	if raw == nil {
		return original, nil, false, nil
	}
	if isJSONNull(raw) {
		return nil, nil, true, nil
	}
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, nil, false, err
	}
	if len(patch) == 0 {
		return original, nil, false, nil
	}
	request := maputil.MergePatchStringMap(nil, patch)
	return maputil.MergePatchStringMap(original, patch), request, true, nil
}

// tenantSnapshot captures the tenant's derived state before a destination mutation.
type tenantSnapshot struct {
	topics            []string
	destinationsCount int
}

func (h *DestinationHandlers) snapshotTenant(tenant *models.Tenant) tenantSnapshot {
	return tenantSnapshot{
		topics:            tenant.Topics,
		destinationsCount: tenant.DestinationsCount,
	}
}

// emitSubscriptionUpdateIfChanged re-fetches the tenant after a mutation and
// emits tenant.subscription.updated if topics or destinations_count changed.
// Best-effort: errors are logged but do not affect the API response.
func (h *DestinationHandlers) emitSubscriptionUpdateIfChanged(ctx context.Context, tenantID string, prev tenantSnapshot) {
	if h.emitter == nil {
		return
	}

	tenant, err := h.tenantStore.RetrieveTenant(ctx, tenantID)
	if err != nil {
		h.logger.Ctx(ctx).Error("failed to retrieve tenant for subscription update", zap.Error(err))
		return
	}

	if slices.Equal(tenant.Topics, prev.topics) && tenant.DestinationsCount == prev.destinationsCount {
		return
	}

	if err := h.emitter.Emit(ctx, opevents.TenantSubscriptionUpdatedEvent(opevents.TenantSubscriptionUpdatedData{
		TenantID:                  tenantID,
		Topics:                    tenant.Topics,
		PreviousTopics:            prev.topics,
		DestinationsCount:         tenant.DestinationsCount,
		PreviousDestinationsCount: prev.destinationsCount,
	})); err != nil {
		h.logger.Ctx(ctx).Error("failed to emit subscription update", zap.Error(err))
	}
}

func mustRoleFromContext(c *gin.Context) string {
	if role, exists := c.Get(authRoleKey); exists {
		if roleStr, ok := role.(string); ok {
			return roleStr
		}
	}
	return ""
}
