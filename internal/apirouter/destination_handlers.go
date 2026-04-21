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
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/maputil"
	"go.uber.org/zap"
)

// SubscriptionEmitter emits operator events for subscription changes.
// Satisfied by opevents.Emitter.
type SubscriptionEmitter interface {
	Emit(ctx context.Context, topic string, tenantID string, data any) error
}

// TenantSubscriptionUpdatedData is the data payload for tenant.subscription.updated events.
type TenantSubscriptionUpdatedData struct {
	TenantID                  string   `json:"tenant_id"`
	Topics                    []string `json:"topics"`
	PreviousTopics            []string `json:"previous_topics"`
	DestinationsCount         int      `json:"destinations_count"`
	PreviousDestinationsCount int      `json:"previous_destinations_count"`
}

type DestinationHandlers struct {
	logger      *logging.Logger
	telemetry   telemetry.Telemetry
	tenantStore tenantstore.TenantStore
	emitter     SubscriptionEmitter
	topics      []string
	registry    destregistry.Registry
	displayer   *destinationDisplayer
}

func NewDestinationHandlers(logger *logging.Logger, telemetry telemetry.Telemetry, tenantStore tenantstore.TenantStore, emitter SubscriptionEmitter, topics []string, registry destregistry.Registry, displayer *destinationDisplayer) *DestinationHandlers {
	return &DestinationHandlers{
		logger:      logger,
		telemetry:   telemetry,
		tenantStore: tenantStore,
		emitter:     emitter,
		topics:      topics,
		registry:    registry,
		displayer:   displayer,
	}
}

func (h *DestinationHandlers) List(c *gin.Context) {
	tenant := mustTenantFromContext(c)

	destinations, err := h.tenantStore.ListDestination(c.Request.Context(), tenantstore.ListDestinationRequest{
		TenantID: tenant.ID,
		Type:     ParseArrayQueryParam(c, "type"),
		Topics:   ParseArrayQueryParam(c, "topics"),
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

	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)

	destination := input.ToDestination(tenant.ID)
	if err := destination.Validate(h.topics); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.registry.ValidateDestination(c.Request.Context(), &destination); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.registry.PreprocessDestination(&destination, nil, &destregistry.PreprocessDestinationOpts{
		Role: mustRoleFromContext(c),
	}); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.tenantStore.CreateDestination(c.Request.Context(), destination); err != nil {
		h.handleUpsertDestinationError(c, err)
		return
	}
	h.telemetry.DestinationCreated(c.Request.Context(), destination.Type)
	h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)

	display, err := h.displayer.Display(&destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusCreated, display)
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	destination := h.mustRetrieveDestination(c, tenant.ID, c.Param("destination_id"))
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

	// Retrieve destination.
	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)
	originalDestination := h.mustRetrieveDestination(c, tenant.ID, c.Param("destination_id"))
	if originalDestination == nil {
		return
	}

	updatedDestination := *originalDestination

	// Validate.
	if input.Topics != nil {
		updatedDestination.Topics = input.Topics
		if err := updatedDestination.Validate(h.topics); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}
	shouldRevalidate := false
	if input.Type != "" && input.Type != originalDestination.Type {
		AbortWithValidationError(c, errors.New("type cannot be updated"))
		return
	}

	// Config (merge-patch)
	configResult, configChanged, err := applyMergePatchStringMap(originalDestination.Config, input.Config)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid config: %w", err))
		return
	}
	if configChanged {
		shouldRevalidate = true
		updatedDestination.Config = configResult
	}

	// Credentials (merge-patch)
	credsResult, credsChanged, err := applyMergePatchStringMap(originalDestination.Credentials, input.Credentials)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid credentials: %w", err))
		return
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
				return
			}
			updatedDestination.Filter = filter
		}
	}

	// DeliveryMetadata (merge-patch)
	dmResult, dmChanged, err := applyMergePatchStringMap(originalDestination.DeliveryMetadata, input.DeliveryMetadata)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid delivery_metadata: %w", err))
		return
	}
	if dmChanged {
		updatedDestination.DeliveryMetadata = dmResult
	}

	// Metadata (merge-patch)
	metaResult, metaChanged, err := applyMergePatchStringMap(originalDestination.Metadata, input.Metadata)
	if err != nil {
		AbortWithValidationError(c, fmt.Errorf("invalid metadata: %w", err))
		return
	}
	if metaChanged {
		updatedDestination.Metadata = metaResult
	}

	// Always preprocess before updating
	if err := h.registry.PreprocessDestination(&updatedDestination, originalDestination, &destregistry.PreprocessDestinationOpts{
		Role: mustRoleFromContext(c),
	}); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	if shouldRevalidate {
		if err := h.registry.ValidateDestination(c.Request.Context(), &updatedDestination); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}

	// Update destination.
	updatedDestination.UpdatedAt = time.Now()
	if err := h.tenantStore.UpsertDestination(c.Request.Context(), updatedDestination); err != nil {
		h.handleUpsertDestinationError(c, err)
		return
	}
	h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)

	display, err := h.displayer.Display(&updatedDestination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)
	destination := h.mustRetrieveDestination(c, tenant.ID, c.Param("destination_id"))
	if destination == nil {
		return
	}
	if err := h.tenantStore.DeleteDestination(c.Request.Context(), destination.TenantID, destination.ID); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *DestinationHandlers) Disable(c *gin.Context) {
	h.setDisabilityHandler(c, true)
}

func (h *DestinationHandlers) Enable(c *gin.Context) {
	h.setDisabilityHandler(c, false)
}

func (h *DestinationHandlers) ListProviderMetadata(c *gin.Context) {
	metadata := h.registry.ListProviderMetadata()
	c.JSON(http.StatusOK, metadata)
}

func (h *DestinationHandlers) RetrieveProviderMetadata(c *gin.Context) {
	providerType := c.Param("type")
	metadata, err := h.registry.RetrieveProviderMetadata(providerType)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, metadata)
}

func (h *DestinationHandlers) setDisabilityHandler(c *gin.Context, disabled bool) {
	tenant := mustTenantFromContext(c)
	prev := h.snapshotTenant(tenant)
	destination := h.mustRetrieveDestination(c, tenant.ID, c.Param("destination_id"))
	if destination == nil {
		return
	}
	shouldUpdate := false
	if disabled && destination.DisabledAt == nil {
		shouldUpdate = true
		now := time.Now()
		destination.DisabledAt = &now
	}
	if !disabled && destination.DisabledAt != nil {
		shouldUpdate = true
		destination.DisabledAt = nil
	}
	if shouldUpdate {
		if err := h.tenantStore.UpsertDestination(c.Request.Context(), *destination); err != nil {
			h.handleUpsertDestinationError(c, err)
			return
		}
		h.emitSubscriptionUpdateIfChanged(c.Request.Context(), tenant.ID, prev)
	}

	display, err := h.displayer.Display(destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) mustRetrieveDestination(c *gin.Context, tenantID, destinationID string) *models.Destination {
	destination, err := h.tenantStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		if errors.Is(err, tenantstore.ErrDestinationDeleted) {
			c.Status(http.StatusNotFound)
			return nil
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return nil
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return nil
	}
	return destination
}

func (h *DestinationHandlers) handleUpsertDestinationError(c *gin.Context, err error) {
	if strings.Contains(err.Error(), "validation failed") {
		AbortWithValidationError(c, err)
		return
	}
	if errors.Is(err, tenantstore.ErrDuplicateDestination) {
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
	return models.Destination{
		ID:               r.ID,
		Type:             r.Type,
		Topics:           r.Topics,
		Filter:           r.Filter,
		Config:           r.Config,
		Credentials:      r.Credentials,
		DeliveryMetadata: r.DeliveryMetadata,
		Metadata:         r.Metadata,
		CreatedAt:        now,
		UpdatedAt:        now,
		DisabledAt:       nil,
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
}

// isJSONNull checks if raw JSON bytes represent a JSON null literal.
func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 4 && string(raw) == "null"
}

// applyMergePatchStringMap applies RFC 7396 merge-patch semantics for a map[string]string field.
// Returns (result, changed, error):
//   - raw is nil (field omitted): returns original unchanged
//   - raw is "null": returns nil (clear field)
//   - raw is "{}": returns original unchanged (empty merge = no-op)
//   - raw is an object: merge-patch into original
func applyMergePatchStringMap(original map[string]string, raw json.RawMessage) (map[string]string, bool, error) {
	if raw == nil {
		return original, false, nil
	}
	if isJSONNull(raw) {
		return nil, true, nil
	}
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, false, err
	}
	if len(patch) == 0 {
		return original, false, nil
	}
	return maputil.MergePatchStringMap(original, patch), true, nil
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

	data := TenantSubscriptionUpdatedData{
		TenantID:                  tenantID,
		Topics:                    tenant.Topics,
		PreviousTopics:            prev.topics,
		DestinationsCount:         tenant.DestinationsCount,
		PreviousDestinationsCount: prev.destinationsCount,
	}
	if err := h.emitter.Emit(ctx, "tenant.subscription.updated", tenantID, data); err != nil {
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
