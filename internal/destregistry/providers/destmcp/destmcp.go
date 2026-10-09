// Package destmcp is the mcp destination type: one MCP Events subscription,
// created and refreshed through the MCP subscription endpoints only.
//
// Validate checks a subscription before it is stored: its fields, the event
// against the topic catalog, the arguments against the event's inputSchema,
// the callback address (netguard) and the callback's verification challenge
// (mcpevents.Verifier). Preprocess handles secret rotation. Publishers wrap
// each event in the MCP event envelope, sign it with Standard Webhooks and
// POST it through the SSRF-guarded client; they hold no resources, so the
// registry builds one per attempt (BypassPublisherCache).
package destmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"text/template"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
)

// Type is the destination type the provider registers as.
const Type = models.DestinationTypeMCP

// Destination config and credential keys.
const (
	ConfigURL            = "url"
	ConfigSubscriptionID = "subscription_id"
	ConfigPrincipal      = "principal"
	ConfigEvent          = "event"
	ConfigArguments      = "arguments"
	ConfigSchemaHash     = "schema_hash"

	CredentialSecret                  = "secret"
	CredentialPreviousSecret          = "previous_secret"
	CredentialPreviousSecretInvalidAt = "previous_secret_invalid_at"
)

// Defaults for zero Config values.
const (
	// DefaultDeliveryTimeout bounds one attempt, whatever
	// DELIVERY_TIMEOUT_SECONDS is.
	DefaultDeliveryTimeout = 10 * time.Second
	// DefaultMaxResponseBodyBytes is how much of a callback's response body
	// is stored on the attempt.
	DefaultMaxResponseBodyBytes = 4096
	// DefaultSecretRotationGrace is how long the previous secret keeps
	// signing after a rotation (MCP_SECRET_ROTATION_GRACE).
	DefaultSecretRotationGrace = 24 * time.Hour
	// DefaultMaxInflightPerHost caps attempts in flight per callback
	// host:port when no HostLimiter is given (MCP_MAX_INFLIGHT_PER_HOST).
	DefaultMaxInflightPerHost = 8
	// MaxPrincipalBytes caps config.principal, as the subscribe endpoint does.
	MaxPrincipalBytes = 512

	// FieldPrincipal is the invalid_params field for principal failures.
	FieldPrincipal = "principal"

	// checkURLTimeout bounds the subscribe-time address check (DNS).
	checkURLTimeout = 5 * time.Second
)

// ErrVerificationUnavailable is the Cause (wrapped) of a validation error that
// isn't the client's fault: no Verifier is configured, or the verifier
// couldn't run (store outage, cancelled request). Callers map it to a server
// error, not to an mcp_error.
var ErrVerificationUnavailable = errors.New("destmcp: callback verification unavailable")

// Verifier verifies callback URLs. *mcpevents.Verifier implements it.
type Verifier interface {
	// Verify passes when (tenant, principal, url) is verified, from the
	// cache or with a challenge signed with the given secrets.
	Verify(ctx context.Context, req mcpevents.VerifyRequest) error
	// Verified reports a cached verification without sending anything.
	Verified(ctx context.Context, tenantID, principal, rawURL string) (bool, error)
}

// Config configures the provider. It holds runtime objects only; every zero
// value takes a safe default.
type Config struct {
	// Catalog decides which events can be subscribed to. Nil means no topic
	// is MCP-enabled, so Validate refuses every subscription.
	Catalog *topicschema.Catalog
	// Client sends deliveries. It must be a netguard client (address checks
	// at dial time, no redirects, no environment proxy). Nil builds one from
	// Guard, sized by Pool.
	Client *http.Client
	// Guard checks callback addresses at subscribe time. It should be the
	// guard behind Client. Nil means a strict guard: no allowlist, system
	// resolver.
	Guard *netguard.Guard
	// HostLimiter caps attempts in flight per callback host:port; share it
	// with the Verifier and the Notifier. Nil means a limiter of
	// DefaultMaxInflightPerHost for this provider alone.
	HostLimiter *netguard.HostLimiter
	// Verifier verifies callbacks at subscribe time. Nil makes Validate fail
	// closed with ErrVerificationUnavailable: services that only deliver
	// never validate.
	Verifier Verifier
	// CodeProfile numbers the mcp_error causes of validation errors.
	CodeProfile mcpevents.CodeProfile
	// SecretRotationGrace is how long a rotated-away secret keeps signing;
	// <= 0 means DefaultSecretRotationGrace.
	SecretRotationGrace time.Duration
	// MaxResponseBodyBytes caps the response body stored on an attempt: 0
	// means DefaultMaxResponseBodyBytes, < 0 stores none.
	MaxResponseBodyBytes int
	// UserAgent is sent on every delivery (a netguard client configured with
	// its own UserAgent overrides it).
	UserAgent string
	// Pool sizes the idle pool of the client built when Client is nil.
	Pool destregistry.PoolSizing
	// OnConnection observes connection reuse on the client built when Client
	// is nil.
	OnConnection func(reused bool)
}

// Provider is the mcp destination provider.
type Provider struct {
	*destregistry.BaseProvider
	catalog *topicschema.Catalog
	// schemaHashes holds the topic hash of every MCP-enabled topic,
	// computed once (the catalog is immutable).
	schemaHashes         map[string]string
	client               *http.Client
	guard                *netguard.Guard
	hostLimiter          *netguard.HostLimiter
	verifier             Verifier
	profile              mcpevents.CodeProfile
	rotationGrace        time.Duration
	maxResponseBodyBytes int
	userAgent            string
	deliveryTimeout      time.Duration
	instructions         *template.Template
	now                  func() time.Time
}

var (
	_ destregistry.Provider               = (*Provider)(nil)
	_ destregistry.DeliveryTimeouter      = (*Provider)(nil)
	_ destregistry.PublisherCacheBypasser = (*Provider)(nil)
)

// New returns the mcp provider, with metadata from loader. It fails when the
// instructions (possibly overridden through DESTINATIONS_METADATA_PATH) are
// not a valid template, or the client can't be built.
func New(loader metadata.MetadataLoader, cfg Config) (*Provider, error) {
	base, err := destregistry.NewBaseProvider(loader, Type)
	if err != nil {
		return nil, err
	}
	instructions, err := parseInstructions(base.Metadata().Instructions)
	if err != nil {
		return nil, err
	}
	profile, err := mcpevents.ParseCodeProfile(string(cfg.CodeProfile))
	if err != nil {
		return nil, err
	}

	p := &Provider{
		BaseProvider:         base,
		catalog:              cfg.Catalog,
		guard:                cfg.Guard,
		client:               cfg.Client,
		hostLimiter:          cfg.HostLimiter,
		verifier:             cfg.Verifier,
		profile:              profile,
		rotationGrace:        cfg.SecretRotationGrace,
		maxResponseBodyBytes: cfg.MaxResponseBodyBytes,
		userAgent:            cfg.UserAgent,
		deliveryTimeout:      DefaultDeliveryTimeout,
		instructions:         instructions,
		now:                  time.Now,
	}
	if p.catalog == nil {
		p.catalog = topicschema.EmptyCatalog(nil)
	}
	snapshot := p.catalog.Snapshot()
	p.schemaHashes = make(map[string]string)
	for _, topic := range p.catalog.MCPTopics() {
		p.schemaHashes[topic] = snapshot.TopicHash(topic)
	}
	if p.guard == nil {
		p.guard = &netguard.Guard{}
	}
	if p.client == nil {
		p.client, err = netguard.NewHTTPClient(netguard.ClientConfig{
			Guard:               p.guard,
			UserAgent:           cfg.UserAgent,
			MaxIdleConns:        cfg.Pool.MaxIdleConns,
			MaxIdleConnsPerHost: cfg.Pool.MaxIdleConnsPerHost,
			OnConnection:        cfg.OnConnection,
		})
		if err != nil {
			return nil, fmt.Errorf("mcp client: %w", err)
		}
	}
	if p.hostLimiter == nil {
		p.hostLimiter = netguard.NewHostLimiter(DefaultMaxInflightPerHost)
	}
	// A nil *mcpevents.Verifier in the interface would pass the nil check
	// and panic on first use; treat it as absent.
	if v, ok := p.verifier.(*mcpevents.Verifier); ok && v == nil {
		p.verifier = nil
	}
	if p.rotationGrace <= 0 {
		p.rotationGrace = DefaultSecretRotationGrace
	}
	if p.maxResponseBodyBytes == 0 {
		p.maxResponseBodyBytes = DefaultMaxResponseBodyBytes
	}
	return p, nil
}

// DeliveryTimeout implements destregistry.DeliveryTimeouter.
func (p *Provider) DeliveryTimeout() time.Duration {
	return p.deliveryTimeout
}

// BypassPublisherCache implements destregistry.PublisherCacheBypasser:
// publishers hold no resources and cost two base64 decodes to build, while
// one cache entry per subscription would churn the shared LRU.
func (p *Provider) BypassPublisherCache() bool {
	return true
}

// SchemaHash returns the hash recorded as config.schema_hash for a
// subscription to topic (topicschema Snapshot().TopicHash), or "" when the
// topic isn't MCP-enabled.
func (p *Provider) SchemaHash(topic string) string {
	return p.schemaHashes[topic]
}

// Instructions renders the type's instructions with serverURL
// (MCP_SERVER_URL) and the catalog's MCP-enabled topics.
func (p *Provider) Instructions(serverURL string) (string, error) {
	return executeInstructions(p.instructions, InstructionsData{
		ServerURL: serverURL,
		Topics:    p.catalog.MCPTopics(),
	})
}

func (p *Provider) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	return destregistry.DestinationTarget{Target: destination.Config[ConfigURL]}
}

// ObfuscateDestination masks secret and previous_secret, and leaves out a
// previous secret that no longer signs.
func (p *Provider) ObfuscateDestination(destination *models.Destination) *models.Destination {
	result := p.BaseProvider.ObfuscateDestination(destination)
	if invalidAt, ok := destination.Credentials[CredentialPreviousSecretInvalidAt]; ok {
		if t, err := time.Parse(time.RFC3339, invalidAt); err != nil || !p.now().Before(t) {
			delete(result.Credentials, CredentialPreviousSecret)
			delete(result.Credentials, CredentialPreviousSecretInvalidAt)
		}
	}
	return result
}

// subscription is a destination's parsed config and credentials.
type subscription struct {
	url            *url.URL
	urlString      string
	principal      string
	subscriptionID string
	secrets        []mcpevents.Secret
}

// Validate checks an mcp destination before it is stored, cheapest first:
// fields and the event, the arguments against the event's inputSchema, the
// URL and secrets, then (unless the callback is already verified for the
// tenant and principal) the callback address, and finally the verification
// challenge.
//
// Failures are *destregistry.ErrDestinationValidation. Those the MCP client
// can cause carry a *mcpevents.Error Cause (invalid_params, not_found,
// resource_exhausted, callback_endpoint_error) to render as mcp_error; an
// address check failure is always invalid_params {field: "delivery.url",
// reason: "address_not_allowed"} (or "https_required"), never naming the
// host or address. Inconsistencies only a caller bug can produce
// (subscription_id, schema_hash, topics) have no Cause, and verification that
// couldn't run wraps ErrVerificationUnavailable.
func (p *Provider) Validate(ctx context.Context, destination *models.Destination) error {
	if destination.Type != Type {
		return invalid("type", "invalid_type", nil)
	}
	sub, err := p.parse(destination)
	if err != nil {
		return err
	}
	if p.verifier == nil {
		return unavailable(errors.New("destmcp: no verifier configured"))
	}

	// A cached verification means the address passed when the challenge
	// did; the delivery-time dialer checks it on every connection anyway.
	cached, err := p.verifier.Verified(ctx, destination.TenantID, sub.principal, sub.urlString)
	if err == nil && cached {
		return nil
	}
	if err := p.checkAddress(ctx, sub.url); err != nil {
		return err
	}
	err = p.verifier.Verify(ctx, mcpevents.VerifyRequest{
		TenantID:       destination.TenantID,
		Principal:      sub.principal,
		URL:            sub.urlString,
		SubscriptionID: sub.subscriptionID,
		Secrets:        sub.secrets,
	})
	if err == nil {
		return nil
	}
	var mcpErr *mcpevents.Error
	if errors.As(err, &mcpErr) {
		return p.invalidFromCause(verifyDetail(mcpErr), mcpErr)
	}
	return unavailable(err)
}

// parse runs the static checks of Validate.
func (p *Provider) parse(destination *models.Destination) (*subscription, error) {
	config := destination.Config
	principal := config[ConfigPrincipal]
	if principal == "" {
		return nil, p.invalidParams("config."+ConfigPrincipal, mcpevents.ReasonRequired, mcpevents.InvalidParams(FieldPrincipal, mcpevents.ReasonRequired))
	}
	if len(principal) > MaxPrincipalBytes {
		return nil, p.invalidParams("config."+ConfigPrincipal, mcpevents.ReasonTooLarge, mcpevents.InvalidParams(FieldPrincipal, mcpevents.ReasonTooLarge))
	}

	event := config[ConfigEvent]
	if event == "" {
		return nil, p.invalidParams("config."+ConfigEvent, mcpevents.ReasonRequired, mcpevents.InvalidParams(mcpevents.FieldName, mcpevents.ReasonRequired))
	}
	schemaHash, ok := p.schemaHashes[event]
	if !ok {
		return nil, p.invalidFromCause(destregistry.ValidationErrorDetail{Field: "config." + ConfigEvent, Type: "not_found"}, mcpevents.NotFound(mcpevents.NotFoundEvent))
	}
	if len(destination.Topics) != 1 || destination.Topics[0] != event {
		return nil, invalid("topics", "invalid", nil)
	}

	arguments, err := p.parseArguments(event, config[ConfigArguments])
	if err != nil {
		return nil, err
	}

	u, urlString, err := mcpevents.NormalizeCallbackURL(config[ConfigURL])
	if err != nil {
		var mcpErr *mcpevents.Error
		if errors.As(err, &mcpErr) {
			return nil, p.invalidFromCause(verifyDetail(mcpErr), mcpErr)
		}
		return nil, p.invalidParams("config."+ConfigURL, mcpevents.ReasonInvalidURL, mcpevents.InvalidParams(mcpevents.FieldDeliveryURL, mcpevents.ReasonInvalidURL))
	}
	if urlString != config[ConfigURL] {
		// Subscription IDs and verification are keyed on the normalized
		// URL; storing another spelling would split them.
		return nil, p.invalidParams("config."+ConfigURL, mcpevents.ReasonInvalidURL, mcpevents.InvalidParams(mcpevents.FieldDeliveryURL, mcpevents.ReasonInvalidURL))
	}

	secrets, field, err := parseSecrets(destination.Credentials)
	if err != nil {
		if field == "credentials."+CredentialSecret {
			return nil, p.invalidParams(field, mcpevents.ReasonInvalidSecret, mcpevents.InvalidParams(mcpevents.FieldDeliverySecret, mcpevents.ReasonInvalidSecret))
		}
		// The previous secret only ever comes from Preprocess.
		return nil, invalid(field, "invalid", nil)
	}

	subscriptionID := mcpevents.DeriveSubscriptionID(principal, urlString, event, arguments)
	if config[ConfigSubscriptionID] != subscriptionID || destination.ID != subscriptionID {
		return nil, invalid("config."+ConfigSubscriptionID, "invalid", nil)
	}
	if config[ConfigSchemaHash] != schemaHash {
		return nil, invalid("config."+ConfigSchemaHash, "invalid", nil)
	}

	return &subscription{
		url:            u,
		urlString:      urlString,
		principal:      principal,
		subscriptionID: subscriptionID,
		secrets:        secrets,
	}, nil
}

// parseArguments checks that raw is the canonical JSON object form of
// arguments valid for event's inputSchema.
func (p *Provider) parseArguments(event, raw string) (json.RawMessage, error) {
	field := "config." + ConfigArguments
	if len(raw) > mcpevents.MaxArgumentsBytes {
		return nil, p.invalidParams(field, mcpevents.ReasonTooLarge, mcpevents.InvalidParams(mcpevents.FieldArguments, mcpevents.ReasonTooLarge))
	}
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, p.invalidParams(field, mcpevents.ReasonInvalid, mcpevents.InvalidParams(mcpevents.FieldArguments, mcpevents.ReasonInvalid))
	}
	canonical, err := mcpevents.CanonicalizeJSON(trimmed)
	if err != nil || string(canonical) != raw {
		// The subscription ID hashes the canonical form, so it must be the
		// form stored.
		return nil, p.invalidParams(field, mcpevents.ReasonInvalid, mcpevents.InvalidParams(mcpevents.FieldArguments, mcpevents.ReasonInvalid))
	}
	if errs := p.catalog.ValidateArguments(event, canonical); len(errs) > 0 {
		return nil, p.invalidFromCause(destregistry.ValidationErrorDetail{Field: field, Type: mcpevents.ReasonInvalid},
			mcpevents.InvalidParamsWithErrors(mcpevents.FieldArguments, mcpevents.ReasonInvalid, errs))
	}
	return canonical, nil
}

// checkAddress applies the guard to the callback URL. The client-visible
// result depends on the URL's scheme only, never on what the host resolves
// to (or whether it resolves), so the check can't be used to probe internal
// DNS: a refused http URL is https_required (it would need to be allowlisted
// loopback), a refused https URL address_not_allowed.
func (p *Provider) checkAddress(ctx context.Context, u *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, checkURLTimeout)
	defer cancel()
	err := p.guard.CheckURL(ctx, u)
	if err == nil {
		return nil
	}
	reason := mcpevents.ReasonAddressNotAllowed
	if u.Scheme != "https" || netguard.IsHTTPSRequired(err) {
		reason = mcpevents.ReasonHTTPSRequired
	}
	return p.invalidParams("config."+ConfigURL, reason, mcpevents.InvalidParams(mcpevents.FieldDeliveryURL, reason))
}

// parseSecrets decodes the current secret and, when present, the previous
// one with its expiry. On error it returns the offending field.
func parseSecrets(credentials map[string]string) ([]mcpevents.Secret, string, error) {
	key, err := mcpevents.DecodeSecret(credentials[CredentialSecret])
	if err != nil {
		return nil, "credentials." + CredentialSecret, err
	}
	secrets := []mcpevents.Secret{{Key: key}}

	previous, invalidAtStr := credentials[CredentialPreviousSecret], credentials[CredentialPreviousSecretInvalidAt]
	switch {
	case previous == "" && invalidAtStr == "":
		return secrets, "", nil
	case previous == "":
		return nil, "credentials." + CredentialPreviousSecret, errors.New("destmcp: previous_secret_invalid_at without previous_secret")
	}
	previousKey, err := mcpevents.DecodeSecret(previous)
	if err != nil {
		return nil, "credentials." + CredentialPreviousSecret, err
	}
	invalidAt, err := time.Parse(time.RFC3339, invalidAtStr)
	if err != nil {
		return nil, "credentials." + CredentialPreviousSecretInvalidAt, errors.New("destmcp: previous_secret_invalid_at must be RFC 3339")
	}
	return append(secrets, mcpevents.Secret{Key: previousKey, InvalidAt: &invalidAt}), "", nil
}

// Preprocess rotates secrets. On create the destination keeps only its
// secret. On refresh, a changed secret moves the stored one to
// previous_secret, signing until now + SecretRotationGrace; an unchanged one
// keeps the stored previous pair until it expires. previous_* values sent by
// the caller are ignored, and credentials are reduced to the three keys. An
// original of another type is ignored: its secret must never sign MCP
// deliveries to a URL an agent chose.
func (p *Provider) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	secret := newDestination.Credentials[CredentialSecret]
	credentials := make(map[string]string, 3)
	if originalDestination == nil || originalDestination.Type != Type {
		if secret != "" {
			credentials[CredentialSecret] = secret
		}
		newDestination.Credentials = credentials
		return nil
	}

	now := p.now()
	stored := originalDestination.Credentials
	if secret == "" {
		secret = stored[CredentialSecret]
	}
	if secret != "" {
		credentials[CredentialSecret] = secret
	}
	switch {
	case stored[CredentialSecret] != "" && secret != stored[CredentialSecret]:
		credentials[CredentialPreviousSecret] = stored[CredentialSecret]
		credentials[CredentialPreviousSecretInvalidAt] = now.Add(p.rotationGrace).UTC().Format(time.RFC3339)
	case stored[CredentialPreviousSecret] != "":
		invalidAt, err := time.Parse(time.RFC3339, stored[CredentialPreviousSecretInvalidAt])
		if err == nil && now.Before(invalidAt) {
			credentials[CredentialPreviousSecret] = stored[CredentialPreviousSecret]
			credentials[CredentialPreviousSecretInvalidAt] = stored[CredentialPreviousSecretInvalidAt]
		}
	}
	newDestination.Credentials = credentials
	return nil
}

// CreatePublisher builds a publisher from the stored destination without any
// I/O. A destination that can't be delivered to (bad URL or secret) fails
// as a non-retryable publish attempt.
func (p *Provider) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	u, urlString, err := mcpevents.NormalizeCallbackURL(destination.Config[ConfigURL])
	if err != nil {
		return nil, invalidDestination("config."+ConfigURL, errors.New("destmcp: config.url is not a valid callback URL"))
	}
	subscriptionID := destination.Config[ConfigSubscriptionID]
	// Same rule as event IDs: it goes out as a header value.
	if !mcpevents.ValidEventID(subscriptionID) {
		return nil, invalidDestination("config."+ConfigSubscriptionID, errors.New("destmcp: config.subscription_id is not a valid header value"))
	}
	secrets, field, err := parseSecrets(destination.Credentials)
	if err != nil {
		return nil, invalidDestination(field, fmt.Errorf("destmcp: %s is invalid", field))
	}
	return &Publisher{
		client:               p.client,
		hostLimiter:          p.hostLimiter,
		url:                  urlString,
		hostPort:             mcpevents.HostPort(u),
		subscriptionID:       subscriptionID,
		secrets:              secrets,
		userAgent:            p.userAgent,
		maxResponseBodyBytes: p.maxResponseBodyBytes,
		now:                  p.now,
	}, nil
}

func invalidDestination(field string, err error) error {
	return &destregistry.ErrDestinationPublishAttempt{
		Err:          err,
		Provider:     Type,
		Data:         map[string]interface{}{"error": "invalid_destination", "field": field},
		NonRetryable: true,
	}
}

// invalid is a validation error with one detail and an optional cause.
func invalid(field, typ string, cause error) error {
	return &destregistry.ErrDestinationValidation{
		Errors: []destregistry.ValidationErrorDetail{{Field: field, Type: typ}},
		Cause:  cause,
	}
}

// invalidParams is a validation error caused by the MCP request.
func (p *Provider) invalidParams(field, typ string, cause *mcpevents.Error) error {
	return p.invalidFromCause(destregistry.ValidationErrorDetail{Field: field, Type: typ}, cause)
}

func (p *Provider) invalidFromCause(detail destregistry.ValidationErrorDetail, cause *mcpevents.Error) error {
	return &destregistry.ErrDestinationValidation{
		Errors: []destregistry.ValidationErrorDetail{detail},
		Cause:  cause.WithProfile(p.profile),
	}
}

func unavailable(err error) error {
	return invalid("config."+ConfigURL, "verification_unavailable", fmt.Errorf("%w: %w", ErrVerificationUnavailable, err))
}

// verifyDetail maps an mcp_error to the destination field it concerns, with
// its reason (or kind) as the type.
func verifyDetail(e *mcpevents.Error) destregistry.ValidationErrorDetail {
	field := "config." + ConfigURL
	switch f, _ := e.Data["field"].(string); f {
	case mcpevents.FieldDeliverySecret:
		field = "credentials." + CredentialSecret
	case mcpevents.FieldArguments:
		field = "config." + ConfigArguments
	case mcpevents.FieldName:
		field = "config." + ConfigEvent
	case FieldPrincipal:
		field = "config." + ConfigPrincipal
	}
	typ := string(e.Kind)
	if reason, ok := e.Data["reason"].(string); ok && reason != "" {
		typ = reason
	}
	return destregistry.ValidationErrorDetail{Field: field, Type: typ}
}
