package destmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
)

const Type = models.DestinationTypeMCP

const (
	ConfigURL       = "url"
	ConfigPrincipal = "principal"
	ConfigArguments = "arguments"

	CredentialSecret                  = "secret"
	CredentialPreviousSecret          = "previous_secret"
	CredentialPreviousSecretInvalidAt = "previous_secret_invalid_at"
)

const (
	ErrTypeRateLimited             = "rate_limited"
	ErrTypeVerificationUnavailable = "verification_unavailable"
)

const (
	DefaultMaxResponseBodyBytes = 4096
	SecretRotationGrace         = 24 * time.Hour
	MaxPrincipalBytes           = 512
	MaxArgumentsBytes           = 4 << 10

	checkURLTimeout = 5 * time.Second
)

type Verifier interface {
	Verify(ctx context.Context, req mcpevents.VerifyRequest) error
	Verified(ctx context.Context, tenantID, principal, rawURL string) (bool, error)
}

type Config struct {
	Client               *http.Client
	Guard                *netguard.Guard
	Verifier             Verifier
	Catalog              *topicschema.Catalog
	MaxResponseBodyBytes int
	UserAgent            string
	// AllowInsecureHTTP lets http callbacks reach allowlisted non-loopback
	// addresses. Loopback addresses are always allowed over http when
	// allowlisted.
	AllowInsecureHTTP bool
	// RetryBackoff and RetryMaxRetries are the retry schedule for mcp
	// destinations. A nil RetryBackoff leaves the global policy in place.
	RetryBackoff    backoff.Backoff
	RetryMaxRetries int
}

type Provider struct {
	*destregistry.BaseProvider
	client               *http.Client
	guard                *netguard.Guard
	verifier             Verifier
	catalog              *topicschema.Catalog
	maxResponseBodyBytes int
	userAgent            string
	allowInsecureHTTP    bool
	retryBackoff         backoff.Backoff
	retryMaxRetries      int
	now                  func() time.Time
}

var (
	_ destregistry.Provider               = (*Provider)(nil)
	_ destregistry.PublisherCacheBypasser = (*Provider)(nil)
	_ destregistry.RetryPolicyProvider    = (*Provider)(nil)
)

func New(loader metadata.MetadataLoader, cfg Config) (*Provider, error) {
	base, err := destregistry.NewBaseProvider(loader, Type)
	if err != nil {
		return nil, err
	}
	p := &Provider{
		BaseProvider:         base,
		guard:                cfg.Guard,
		client:               cfg.Client,
		verifier:             cfg.Verifier,
		catalog:              cfg.Catalog,
		maxResponseBodyBytes: cfg.MaxResponseBodyBytes,
		userAgent:            cfg.UserAgent,
		allowInsecureHTTP:    cfg.AllowInsecureHTTP,
		retryBackoff:         cfg.RetryBackoff,
		retryMaxRetries:      cfg.RetryMaxRetries,
		now:                  time.Now,
	}
	if p.guard == nil {
		p.guard = &netguard.Guard{}
	}
	if p.client == nil {
		return nil, errors.New("mcp: Config.Client is required")
	}
	// A nil *Verifier in the interface passes the nil check and panics on use.
	if v, ok := p.verifier.(*mcpevents.Verifier); ok && v == nil {
		p.verifier = nil
	}
	if p.maxResponseBodyBytes == 0 {
		p.maxResponseBodyBytes = DefaultMaxResponseBodyBytes
	}
	return p, nil
}

func (p *Provider) BypassPublisherCache() bool {
	return true
}

func (p *Provider) RetryPolicy() (backoff.Backoff, int, bool) {
	return p.retryBackoff, p.retryMaxRetries, p.retryBackoff != nil
}

func (p *Provider) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	return destregistry.DestinationTarget{Target: destination.Config[ConfigURL]}
}

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

type subscription struct {
	url       *url.URL
	urlString string
	principal string
	secrets   []mcpevents.Secret
}

func (p *Provider) Validate(ctx context.Context, destination *models.Destination) error {
	if destination.Type != Type {
		return invalid("type", "invalid_type")
	}
	sub, err := p.parse(destination)
	if err != nil {
		return err
	}
	if p.verifier == nil {
		return &destregistry.ErrDestinationUnavailable{Code: ErrTypeVerificationUnavailable}
	}

	if err := p.checkScheme(ctx, sub.url); err != nil {
		return err
	}

	// The delivery-time dialer checks the address on every connection, so a
	// cached verification can skip the create-time address check.
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
		SubscriptionID: destination.ID,
		Secrets:        sub.secrets,
	})
	if err == nil {
		return nil
	}
	var mcpErr *mcpevents.Error
	if errors.As(err, &mcpErr) {
		if mcpErr.Kind == mcpevents.KindResourceExhausted {
			ms, _ := mcpErr.Data["retryAfterMs"].(int64)
			return &destregistry.ErrDestinationRateLimited{Code: ErrTypeRateLimited, RetryAfter: time.Duration(ms) * time.Millisecond}
		}
		return invalid(fieldOf(mcpErr), typeOf(mcpErr))
	}
	return &destregistry.ErrDestinationUnavailable{Code: ErrTypeVerificationUnavailable}
}

// ValidateCreate checks a new subscription against the event definitions
// before Validate verifies its callback. Updates are not checked again, so a
// later change to the definitions doesn't block a subscription's rotation.
func (p *Provider) ValidateCreate(destination *models.Destination) error {
	if p.catalog == nil {
		return nil
	}
	if _, err := p.parse(destination); err != nil {
		return err
	}
	if len(destination.Filter) > 0 {
		return invalid("filter", "invalid")
	}
	topic := destination.Topics[0]
	if _, ok := p.catalog.Event(topic); !ok {
		return invalid("topics", "not_found")
	}
	if errs := p.catalog.ValidateArguments(topic, []byte(destination.Config[ConfigArguments])); len(errs) > 0 {
		return argumentErrors(errs)
	}
	return nil
}

func argumentErrors(errs []string) error {
	var details []destregistry.ValidationErrorDetail
	seen := map[string]bool{}
	for _, e := range errs {
		path, _, ok := strings.Cut(e, ": ")
		if !ok {
			continue
		}
		field := "config." + path
		if seen[field] {
			continue
		}
		seen[field] = true
		details = append(details, destregistry.ValidationErrorDetail{Field: field, Type: "invalid"})
	}
	if len(details) == 0 {
		details = []destregistry.ValidationErrorDetail{{Field: "config." + ConfigArguments, Type: "invalid"}}
	}
	return destregistry.NewErrDestinationValidation(details)
}

func filterFromArguments(raw string) (models.Filter, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return mcpevents.ArgumentsToFilter(args), nil
}

func sameFilter(a, b models.Filter) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

func (p *Provider) parse(destination *models.Destination) (*subscription, error) {
	config := destination.Config
	if !mcpevents.ValidSubscriptionID(destination.ID) {
		return nil, invalid("id", "invalid")
	}
	if len(destination.Topics) != 1 || destination.Topics[0] == "*" {
		return nil, invalid("topics", "invalid")
	}

	principal := config[ConfigPrincipal]
	if len(principal) > MaxPrincipalBytes {
		return nil, invalid("config."+ConfigPrincipal, "maxlength")
	}
	if raw, ok := config[ConfigArguments]; ok && raw != "" {
		if err := checkArguments(raw); err != nil {
			return nil, err
		}
	}

	u, urlString, err := mcpevents.NormalizeCallbackURL(config[ConfigURL])
	if err != nil {
		var mcpErr *mcpevents.Error
		if errors.As(err, &mcpErr) {
			return nil, invalid(fieldOf(mcpErr), typeOf(mcpErr))
		}
		return nil, invalid("config."+ConfigURL, mcpevents.ReasonInvalidURL)
	}
	// Verification is keyed on the normalized URL; another spelling would split it.
	if urlString != config[ConfigURL] {
		return nil, invalid("config."+ConfigURL, mcpevents.ReasonInvalidURL)
	}

	secrets, field, err := parseSecrets(destination.Credentials)
	if err != nil {
		if field == "credentials."+CredentialSecret {
			return nil, invalid(field, mcpevents.ReasonInvalidSecret)
		}
		return nil, invalid(field, "invalid")
	}
	return &subscription{url: u, urlString: urlString, principal: principal, secrets: secrets}, nil
}

func checkArguments(raw string) error {
	field := "config." + ConfigArguments
	if len(raw) > MaxArgumentsBytes {
		return invalid(field, "maxlength")
	}
	trimmed := bytes.TrimSpace([]byte(raw))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return invalid(field, "invalid")
	}
	// Only canonical JSON is accepted, so equal arguments are equal strings.
	canonical, err := mcpevents.CanonicalizeJSON(trimmed)
	if err != nil || string(canonical) != raw {
		return invalid(field, "invalid")
	}
	return nil
}

// checkScheme accepts https, and http only when every address the host
// resolves to is allowlisted and either loopback or covered by
// AllowInsecureHTTP. Every failure, DNS errors included, gives the same
// answer, so the result doesn't reveal what a host resolves to.
func (p *Provider) checkScheme(ctx context.Context, u *url.URL) error {
	if u.Scheme == "https" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkURLTimeout)
	defer cancel()
	if !p.httpAllowed(ctx, u.Hostname()) {
		return invalid("config."+ConfigURL, mcpevents.ReasonHTTPSRequired)
	}
	return nil
}

func (p *Provider) httpAllowed(ctx context.Context, host string) bool {
	addrs, ok := []netip.Addr(nil), false
	if addr, err := netip.ParseAddr(host); err == nil {
		addrs, ok = []netip.Addr{addr}, true
	} else {
		var r netguard.Resolver = net.DefaultResolver
		if p.guard.Resolver != nil {
			r = p.guard.Resolver
		}
		resolved, err := r.LookupNetIP(ctx, "ip", host+".")
		addrs, ok = resolved, err == nil
	}
	if !ok || len(addrs) == 0 {
		return false
	}
	for _, addr := range addrs {
		addr = addr.Unmap()
		if !p.guard.Allowlist.Contains(addr) || !(p.allowInsecureHTTP || addr.IsLoopback()) {
			return false
		}
	}
	return true
}

// checkAddress refuses every address the guard disallows with one type, so the
// answer can't reveal what a host resolves to.
func (p *Provider) checkAddress(ctx context.Context, u *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, checkURLTimeout)
	defer cancel()
	if err := p.guard.CheckURL(ctx, u); err != nil {
		return invalid("config."+ConfigURL, mcpevents.ReasonAddressNotAllowed)
	}
	return nil
}

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

func (p *Provider) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	// With event definitions, the filter is derived from the arguments on create and can't change after.
	if p.catalog != nil {
		if originalDestination == nil || originalDestination.Type != Type {
			filter, err := filterFromArguments(newDestination.Config[ConfigArguments])
			if err != nil {
				return invalid("config."+ConfigArguments, "invalid")
			}
			newDestination.Filter = filter
		} else if !sameFilter(newDestination.Filter, originalDestination.Filter) {
			return invalid("filter", "immutable")
		}
	}
	secret := newDestination.Credentials[CredentialSecret]
	credentials := make(map[string]string, 3)
	if originalDestination == nil || originalDestination.Type != Type {
		// A secret of another destination type must never sign mcp deliveries.
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
		credentials[CredentialPreviousSecretInvalidAt] = now.Add(SecretRotationGrace).UTC().Format(time.RFC3339)
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

func (p *Provider) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	_, urlString, err := mcpevents.NormalizeCallbackURL(destination.Config[ConfigURL])
	if err != nil {
		return nil, invalidDestination("config."+ConfigURL, errors.New("destmcp: config.url is not a valid callback URL"))
	}
	if !mcpevents.ValidSubscriptionID(destination.ID) {
		return nil, invalidDestination("id", errors.New("destmcp: the destination ID is not a valid subscription ID"))
	}
	secrets, field, err := parseSecrets(destination.Credentials)
	if err != nil {
		return nil, invalidDestination(field, fmt.Errorf("destmcp: %s is invalid", field))
	}
	return &Publisher{
		client:               p.client,
		url:                  urlString,
		tenantID:             destination.TenantID,
		destinationID:        destination.ID,
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

func invalid(field, typ string) error {
	return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{Field: field, Type: typ}})
}

func fieldOf(e *mcpevents.Error) string {
	if f, _ := e.Data["field"].(string); f != "" {
		return f
	}
	return mcpevents.FieldURL
}

func typeOf(e *mcpevents.Error) string {
	if r := e.Reason(); r != "" {
		return r
	}
	return string(e.Kind)
}
