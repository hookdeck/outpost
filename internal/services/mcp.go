package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
)

// mcpNetwork is the MCP callback network stack of one process: the address
// guard, the guarded HTTP client and the per-host in-flight limiter. The
// provider (deliveries), the Verifier (challenges) and the Notifier
// (terminated envelopes) share it, so MCP_MAX_INFLIGHT_PER_HOST and the
// connection pool hold per process whichever service sends.
type mcpNetwork struct {
	allowlist   *netguard.Allowlist
	guard       *netguard.Guard
	client      *http.Client
	hostLimiter *netguard.HostLimiter
}

// mcpNetwork returns the process's MCP network stack, building it on first
// use. Build runs on one goroutine, so no locking is needed.
func (b *ServiceBuilder) mcpNetwork() (*mcpNetwork, error) {
	if b.mcpNet != nil {
		return b.mcpNet, nil
	}
	n, err := newMCPNetwork(b.cfg)
	if err != nil {
		return nil, err
	}
	b.mcpNet = n
	return n, nil
}

func newMCPNetwork(cfg *config.Config) (*mcpNetwork, error) {
	allowlist, _, err := cfg.MCPAllowlist()
	if err != nil {
		return nil, err
	}
	proxyURL, err := cfg.MCPProxyURL()
	if err != nil {
		return nil, err
	}
	guard := &netguard.Guard{
		Allowlist:     allowlist,
		AllowInsecure: cfg.MCP.AllowInsecureCallbacks,
	}

	// Callbacks fan out over arbitrarily many hosts, like webhooks, but a
	// host never needs more idle connections than requests in flight.
	pool := destregistry.SizeFanOutPool(cfg.DeliveryMaxConcurrency)
	if perHost := cfg.MCP.MaxInFlightPerHost; perHost > 0 {
		pool.MaxIdleConnsPerHost = min(pool.MaxIdleConnsPerHost, perHost)
	}
	emeter, err := emetrics.New()
	if err != nil {
		return nil, err
	}
	client, err := netguard.NewHTTPClient(netguard.ClientConfig{
		Guard:               guard,
		UserAgent:           cfg.Destinations.ToConfig(cfg).UserAgent,
		ProxyURL:            proxyURL,
		MaxIdleConns:        pool.MaxIdleConns,
		MaxIdleConnsPerHost: pool.MaxIdleConnsPerHost,
		OnConnection: func(reused bool) {
			emeter.DeliveryConnection(context.Background(), reused, destmcp.Type)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp callback client: %w", err)
	}
	return &mcpNetwork{
		allowlist:   allowlist,
		guard:       guard,
		client:      client,
		hostLimiter: netguard.NewHostLimiter(cfg.MCP.MaxInFlightPerHost),
	}, nil
}

// destMCPConfig returns the mcp provider options. verifier is nil outside the
// API service: only subscribe validates mcp destinations.
func (n *mcpNetwork) destMCPConfig(cfg *config.Config, verifier destmcp.Verifier) *destregistrydefault.DestMCPConfig {
	return &destregistrydefault.DestMCPConfig{
		Catalog:             cfg.TopicCatalog(),
		Client:              n.client,
		Guard:               n.guard,
		HostLimiter:         n.hostLimiter,
		Verifier:            verifier,
		CodeProfile:         cfg.MCPCodeProfile(),
		SecretRotationGrace: cfg.MCP.SecretRotationGrace.Duration(),
	}
}

// allowlistExempt reports the callback hosts that skip the verification
// failure budget: IP literals in the allowlist, and localhost when the
// allowlist holds both loopback addresses it resolves to. Other host names
// are never exempt: exempting them would need a DNS lookup per check.
func allowlistExempt(allowlist *netguard.Allowlist) func(host string) bool {
	loopback := allowlist.Contains(netip.MustParseAddr("127.0.0.1")) &&
		allowlist.Contains(netip.IPv6Loopback())
	return func(host string) bool {
		if addr, err := netip.ParseAddr(host); err == nil {
			return allowlist.Contains(addr)
		}
		return loopback && strings.EqualFold(strings.TrimSuffix(host, "."), "localhost")
	}
}

// Parked retries.
const (
	// parkedRetryMax caps the retries parked on one disabled destination.
	parkedRetryMax = 1000
	// parkedRetryTTL bounds how long the retries of a destination without
	// an expiry stay parked.
	parkedRetryTTL = 24 * time.Hour
)

// parkStore is the tenant store surface retryParker uses.
type parkStore interface {
	ParkRetry(ctx context.Context, tenantID, destinationID, member string, max int, expireAt time.Time) (tenantstore.ParkResult, error)
}

// retryParker implements deliverymq.RetryParker over the tenant store: the
// retry task is parked as its serialized string, which the resumer
// schedules as is once a refresh or an enable re-enables the destination.
type retryParker struct {
	store  parkStore
	logger *logging.Logger
	now    func() time.Time
}

func newRetryParker(store parkStore, logger *logging.Logger) *retryParker {
	return &retryParker{store: store, logger: logger, now: time.Now}
}

var _ deliverymq.RetryParker = (*retryParker)(nil)

func (p *retryParker) ParkRetry(ctx context.Context, task deliverymq.RetryTask, destination *models.Destination) (bool, error) {
	member, err := task.ToString()
	if err != nil {
		return false, err
	}
	// Parked retries are useless past the subscription's expiry.
	expireAt := p.now().Add(parkedRetryTTL)
	if destination.ExpiresAt != nil {
		expireAt = *destination.ExpiresAt
	}
	res, err := p.store.ParkRetry(ctx, task.TenantID, task.DestinationID, member, parkedRetryMax, expireAt)
	if err != nil {
		return false, err
	}
	switch res {
	case tenantstore.ParkResultParked:
		return true, nil
	case tenantstore.ParkResultFull:
		// Dropped, like a retry of a disabled destination that doesn't
		// park: acked rather than redelivered until the destination is
		// re-enabled.
		p.logger.Ctx(ctx).Warn("parked retries full, dropping retry",
			zap.String("event_id", task.EventID),
			zap.String("tenant_id", task.TenantID),
			zap.String("destination_id", task.DestinationID),
			zap.Int("max", parkedRetryMax))
		return true, nil
	default:
		// enabled: deliver against the current state; gone: the re-read
		// finds the destination deleted and drops the retry.
		return false, nil
	}
}

// Parked retry resumes.
const (
	resumeQueueSize    = 1024
	resumeWorkers      = 4
	resumeBatchSize    = 100
	resumePerSecond    = 10
	resumeTimeout      = time.Minute
	resumeDrainTimeout = 5 * time.Second
)

// resumeStore is the tenant store surface parkedRetryResumer uses.
type resumeStore interface {
	PopResumeMembers(ctx context.Context, tenantID, key string, n int) ([]string, error)
	DeleteResumeSet(ctx context.Context, tenantID, key string) error
}

// retryScheduler is the retry scheduler surface parkedRetryResumer uses.
type retryScheduler interface {
	Schedule(ctx context.Context, task string, delay time.Duration, opts ...scheduler.ScheduleOption) error
}

type resumeJob struct {
	tenantID, destinationID, key string
}

// parkedRetryResumer implements apirouter.ParkedRetryResumer. Resume queues
// the resume set and returns; a few workers drain each set in batches on a
// context of their own, scheduling every parked retry on the retry
// scheduler, at most resumePerSecond per destination per second.
type parkedRetryResumer struct {
	store     resumeStore
	scheduler retryScheduler
	logger    *logging.Logger

	jobs   chan resumeJob
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.RWMutex
	closed bool

	// timeout bounds one resume; drainTimeout bounds Close.
	timeout      time.Duration
	drainTimeout time.Duration
}

func newParkedRetryResumer(store resumeStore, sched retryScheduler, logger *logging.Logger) *parkedRetryResumer {
	ctx, cancel := context.WithCancel(context.Background())
	r := &parkedRetryResumer{
		store:        store,
		scheduler:    sched,
		logger:       logger,
		jobs:         make(chan resumeJob, resumeQueueSize),
		ctx:          ctx,
		cancel:       cancel,
		timeout:      resumeTimeout,
		drainTimeout: resumeDrainTimeout,
	}
	for range resumeWorkers {
		r.wg.Add(1)
		go r.work()
	}
	return r
}

// Resume queues the resume set and returns at once. When the queue is full
// or closed, the set is left to expire with the parked retries in it.
func (r *parkedRetryResumer) Resume(_ context.Context, tenantID, destinationID, resumeKey string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fields := []zap.Field{
		zap.String("tenant_id", tenantID),
		zap.String("destination_id", destinationID),
	}
	if r.closed {
		r.logger.Warn("parked retries not resumed: shutting down", fields...)
		return
	}
	select {
	case r.jobs <- resumeJob{tenantID: tenantID, destinationID: destinationID, key: resumeKey}:
	default:
		r.logger.Error("parked retries not resumed: resume queue full", fields...)
	}
}

func (r *parkedRetryResumer) work() {
	defer r.wg.Done()
	for job := range r.jobs {
		r.resume(job)
	}
}

func (r *parkedRetryResumer) resume(job resumeJob) {
	defer func() {
		if p := recover(); p != nil {
			r.logger.Error("panic resuming parked retries", zap.Any("panic", p),
				zap.String("tenant_id", job.tenantID), zap.String("destination_id", job.destinationID))
		}
	}()
	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	logger := r.logger.Ctx(ctx)
	fields := []zap.Field{
		zap.String("tenant_id", job.tenantID),
		zap.String("destination_id", job.destinationID),
	}

	scheduled, failed := 0, 0
	for {
		members, err := r.store.PopResumeMembers(ctx, job.tenantID, job.key, resumeBatchSize)
		if err != nil {
			// The set keeps the expiry of the parked set it was renamed
			// from, so what is left of it goes away on its own.
			logger.Error("failed to read parked retries", append(fields, zap.Error(err))...)
			return
		}
		if len(members) == 0 {
			break
		}
		for _, member := range members {
			if err := r.schedule(ctx, job, member, scheduled); err != nil {
				failed++
				logger.Error("failed to resume parked retry", append(fields, zap.Error(err))...)
				continue
			}
			scheduled++
		}
	}
	if err := r.store.DeleteResumeSet(ctx, job.tenantID, job.key); err != nil {
		logger.Error("failed to delete resumed retries set", append(fields, zap.Error(err))...)
	}
	logger.Info("resumed parked retries",
		append(fields, zap.Int("scheduled", scheduled), zap.Int("failed", failed))...)
}

var errForeignRetry = errors.New("parked retry belongs to another destination")

// schedule schedules the n-th resumed retry of a destination, spread over
// whole seconds, under its ResumedRetryID: it never replaces an automatic
// retry scheduled since, and a manual retry cancels it.
func (r *parkedRetryResumer) schedule(ctx context.Context, job resumeJob, member string, n int) error {
	var task deliverymq.RetryTask
	if err := task.FromString(member); err != nil {
		return fmt.Errorf("invalid parked retry: %w", err)
	}
	if task.TenantID != job.tenantID || task.DestinationID != job.destinationID || task.EventID == "" {
		return errForeignRetry
	}
	delay := time.Duration(n/resumePerSecond) * time.Second
	id := deliverymq.ResumedRetryID(task.EventID, task.DestinationID)
	return r.scheduler.Schedule(ctx, member, delay, scheduler.WithTaskID(id))
}

// Close stops accepting resumes and waits up to drainTimeout for the queued
// ones, then cancels what is left.
func (r *parkedRetryResumer) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.jobs)
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(r.drainTimeout):
		r.cancel()
		<-done
	}
	r.cancel()
}

// destinationDisabler implements logmq.ConditionalDestinationDisabler with
// the tenant store's conditional disable: only a live destination is
// disabled, so an auto-disable never resurrects a deleted subscription or
// reverts a concurrent refresh.
type destinationDisabler struct {
	tenantStore disableStore
	now         func() time.Time
}

type disableStore interface {
	DisableDestination(ctx context.Context, tenantID, destinationID string, at time.Time) (bool, error)
}

func newDestinationDisabler(store disableStore) *destinationDisabler {
	return &destinationDisabler{tenantStore: store, now: time.Now}
}

func (d *destinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	_, err := d.DisableDestinationIfEnabled(ctx, tenantID, destinationID)
	return err
}

// DisableDestinationIfEnabled disables a live, enabled destination and
// reports whether it did. A deleted or missing destination is a no-op.
func (d *destinationDisabler) DisableDestinationIfEnabled(ctx context.Context, tenantID, destinationID string) (bool, error) {
	changed, err := d.tenantStore.DisableDestination(ctx, tenantID, destinationID, d.now())
	if errors.Is(err, tenantstore.ErrDestinationNotFound) || errors.Is(err, tenantstore.ErrDestinationDeleted) {
		return false, nil
	}
	return changed, err
}
