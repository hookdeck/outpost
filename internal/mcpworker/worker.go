// Package mcpworker runs the API service's mcp-subscriptions worker. Every
// sweep interval, one instance at a time (under a Redis lock) runs a pass
// that:
//   - deletes MCP subscriptions expired for longer than a grace period and
//     emits mcp.subscription.expired for each;
//   - ends, with a terminated envelope, subscriptions to topics that are no
//     longer MCP-enabled and those a breaking change left behind, but only
//     on an instance running the applied topic configuration, so instances
//     on an older or rolled-back configuration never end any. Once no
//     instance runs the applied configuration, an instance applies its own;
//   - emits one tenant.subscription.updated per tenant it changed.
//
// Every instance also reports the topic configuration it runs
// (topicschema.Heartbeat), whether or not it holds the lock.
//
// The worker never returns an error while running and recovers panics:
// failures are logged and retried on the next pass, so it never fails
// /healthz. Deletions are conditional (DeleteDestinationIf), so overlapping
// passes never report or terminate a subscription twice.
package mcpworker

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// Name is the worker's name in /healthz.
const Name = "mcp-subscriptions"

// Defaults.
const (
	// DefaultInterval is MCP_EXPIRY_SWEEP_INTERVAL's default.
	DefaultInterval = 30 * time.Second
	// MinGrace and MaxGrace bound the grace (GraceFor).
	MinGrace = 5 * time.Second
	MaxGrace = 60 * time.Second
	// DefaultTTLMax is MCP_TTL_MAX's default.
	DefaultTTLMax      = 24 * time.Hour
	DefaultConcurrency = 16
	DefaultBatchSize   = 100
	// DefaultPassBudget bounds the subscriptions a pass handles in the
	// expiry sweep, and separately in the terminations.
	DefaultPassBudget = 10_000
	// DefaultNotifyWait bounds how long a terminated envelope waits for room
	// in the Notifier's queue (no longer than the interval).
	DefaultNotifyWait = 10 * time.Second

	unlockTimeout = 2 * time.Second
	reportTimeout = 10 * time.Second
	jitterRatio   = 0.1
)

// Store is the subset of tenantstore.TenantStore the worker uses.
type Store interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error)
	RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error)
	DeleteDestinationIf(ctx context.Context, tenantID, destinationID string, c tenantstore.DeleteCondition) (tenantstore.DeleteResult, error)
	ListIndexedDestinations(ctx context.Context, typ, topic string, maxScore int64, limit int) ([]tenantstore.IndexedDestination, error)
	ScanIndexedDestinations(ctx context.Context, typ, topic string, cursor uint64, count int) ([]tenantstore.IndexedDestination, uint64, error)
	RescoreIndexedDestination(ctx context.Context, typ string, topics []string, ref tenantstore.IndexedDestination, newScore int64) error
	RemoveIndexedDestination(ctx context.Context, typ string, topics []string, ref tenantstore.IndexedDestination) error
	ListIndexedTopics(ctx context.Context, typ string) ([]string, error)
}

// Notifier queues terminated envelopes (*mcpevents.Notifier). EnqueueWait
// waits for room, so a mass termination is paced by the Notifier.
type Notifier interface {
	EnqueueWait(ctx context.Context, t mcpevents.Termination) error
}

var (
	_ Store    = tenantstore.TenantStore(nil)
	_ Notifier = (*mcpevents.Notifier)(nil)
)

// CountIndexer counts index entries (tenantstore.TenantStore).
type CountIndexer interface {
	CountIndexed(ctx context.Context, typ, topic string, minScore int64) (int64, error)
}

// LiveTopics is the topicschema.ApplyOptions.LiveTopics check: whether the
// topic's index holds a subscription that hasn't expired. Entries that
// outlived their subscription count too, which errs on the side of
// checking the topic.
func LiveTopics(store CountIndexer, now func() time.Time) func(ctx context.Context, topic string) (bool, error) {
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context, topic string) (bool, error) {
		n, err := store.CountIndexed(ctx, models.DestinationTypeMCP, topic, now().UnixMilli())
		return n > 0, err
	}
}

// Config configures a Worker. Zero durations and sizes take the defaults.
type Config struct {
	// Redis holds the worker lock, the heartbeats, the applied topic
	// configuration and the termination scan state.
	Redis        redis.Cmdable
	DeploymentID string
	Store        Store
	// Snapshot is this instance's topic configuration (Catalog.Snapshot()).
	Snapshot topicschema.Snapshot
	// Notifier sends terminated envelopes; required (a disabled
	// *mcpevents.Notifier sends nothing).
	Notifier Notifier
	// Emitter emits mcp.subscription.expired and, through the default
	// TenantUpdates, tenant.subscription.updated. Optional. It should not
	// block (mcpevents.AsyncEmitter).
	Emitter opevents.Emitter
	// TenantUpdates reports tenant.subscription.updated for the tenants a
	// pass changed. Nil uses NewTenantUpdates(Store, Emitter, Logger) when
	// Emitter is set.
	TenantUpdates TenantUpdates
	// Secrets returns the signing secrets of a subscription for its
	// terminated envelope. Nil uses DestinationSecrets.
	Secrets func(d *models.Destination) []mcpevents.Secret

	Interval          time.Duration // MCP_EXPIRY_SWEEP_INTERVAL; ±10% jitter
	Grace             time.Duration // GraceFor(Interval) when zero
	TTLMax            time.Duration // MCP_TTL_MAX
	HeartbeatInterval time.Duration // topicschema.DefaultHeartbeatInterval
	HeartbeatTTL      time.Duration // topicschema.DefaultHeartbeatTTL
	Concurrency       int
	BatchSize         int
	PassBudget        int
	NotifyWait        time.Duration

	Logger *logging.Logger
	// Now is the clock (tests).
	Now func() time.Time
}

// Worker is the mcp-subscriptions worker. It implements worker.Worker.
type Worker struct {
	cfg        Config
	localHash  string
	lock       redislock.Lock
	logger     *logging.Logger
	notifyWait time.Duration
	jitter     func(time.Duration) time.Duration
}

var _ worker.Worker = (*Worker)(nil)

// New validates cfg and builds a Worker.
func New(cfg Config) (*Worker, error) {
	if cfg.Redis == nil || cfg.Store == nil || cfg.Notifier == nil {
		return nil, errors.New("mcpworker: Redis, Store and Notifier are required")
	}
	cfg.Interval = positiveOr(cfg.Interval, DefaultInterval)
	cfg.Grace = positiveOr(cfg.Grace, GraceFor(cfg.Interval))
	cfg.TTLMax = positiveOr(cfg.TTLMax, DefaultTTLMax)
	cfg.HeartbeatInterval = positiveOr(cfg.HeartbeatInterval, topicschema.DefaultHeartbeatInterval)
	cfg.HeartbeatTTL = positiveOr(cfg.HeartbeatTTL, topicschema.DefaultHeartbeatTTL)
	cfg.Concurrency = positiveOr(cfg.Concurrency, DefaultConcurrency)
	cfg.BatchSize = positiveOr(cfg.BatchSize, DefaultBatchSize)
	cfg.PassBudget = positiveOr(cfg.PassBudget, DefaultPassBudget)
	cfg.NotifyWait = positiveOr(cfg.NotifyWait, DefaultNotifyWait)
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Secrets == nil {
		cfg.Secrets = DestinationSecrets
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logging.NewTestLogger(zap.NewNop())
	}
	if cfg.TenantUpdates == nil && cfg.Emitter != nil {
		cfg.TenantUpdates = NewTenantUpdates(cfg.Store, cfg.Emitter, logger)
	}
	return &Worker{
		cfg:       cfg,
		localHash: cfg.Snapshot.Hash(),
		// A pass stops at one interval; the lock outlives it so a slow
		// pass is not joined by another instance.
		lock: redislock.New(cfg.Redis,
			redislock.WithKey(LockKey(cfg.DeploymentID)),
			redislock.WithTTL(2*cfg.Interval)),
		logger:     logger,
		notifyWait: min(cfg.NotifyWait, cfg.Interval),
		jitter:     jitter,
	}, nil
}

// GraceFor returns how long past expires_at the sweep waits before deleting
// a subscription, for a sweep interval: twice the interval, within MinGrace
// and MaxGrace (60s at the default 30s interval). The grace keeps an
// instance whose clock runs ahead from deleting a subscription its client is
// still refreshing on time. Delivery stops at expires_at regardless.
func GraceFor(interval time.Duration) time.Duration {
	return min(MaxGrace, max(MinGrace, 2*interval))
}

func positiveOr[T int | time.Duration](v, def T) T {
	if v > 0 {
		return v
	}
	return def
}

func keyPrefix(deploymentID string) string {
	if deploymentID == "" {
		return ""
	}
	return deploymentID + ":"
}

// LockKey is the key of the lock held during a pass.
func LockKey(deploymentID string) string {
	return keyPrefix(deploymentID) + "outpost:lock:mcp_worker"
}

// StateKey is the key of the termination scan state (HASH): a cursor per
// topic with broken schema hashes, and when an older configuration was last
// seen running. It shares the applied configuration's hash tag.
func StateKey(deploymentID string) string {
	return keyPrefix(deploymentID) + topicschema.KeyHashTag + ":termination"
}

// Name implements worker.Worker.
func (w *Worker) Name() string { return Name }

// Run heartbeats and runs a pass every interval (±10%) until ctx is done.
// It always returns nil.
func (w *Worker) Run(ctx context.Context) error {
	worker.Ready(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { w.heartbeatLoop(ctx) })
	defer wg.Wait()

	timer := time.NewTimer(w.jitter(w.cfg.Interval))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		w.runPass(ctx)
		timer.Reset(w.jitter(w.cfg.Interval))
	}
}

func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (1 - jitterRatio + 2*jitterRatio*rand.Float64()))
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		w.heartbeat(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) heartbeat(ctx context.Context) {
	defer w.recoverPanic("heartbeat")
	hctx, cancel := context.WithTimeout(ctx, w.cfg.HeartbeatInterval)
	defer cancel()
	if err := topicschema.Heartbeat(hctx, w.cfg.Redis, w.cfg.DeploymentID, w.localHash, w.cfg.HeartbeatTTL); err != nil && ctx.Err() == nil {
		w.logger.Warn("mcp subscriptions worker: failed to report the topic configuration as live", zap.Error(err))
	}
}

func (w *Worker) recoverPanic(what string) {
	if r := recover(); r != nil {
		w.logger.Error("mcp subscriptions worker: recovered from a panic",
			zap.String("in", what), zap.Any("panic", r), zap.StackSkip("stack", 2))
	}
}

// PassStats counts what a pass did.
type PassStats struct {
	// Expired subscriptions deleted, and index entries moved to a refreshed
	// expiry or removed (destination gone).
	Expired, Rescored, Removed int64
	// Terminated subscriptions: topic ended, or schema hash broken.
	Ended, SchemaChanged int64
	// TerminationPaused: this instance doesn't run the applied topic
	// configuration, so it ended nothing.
	TerminationPaused bool
	// Errors counts failed store calls and recovered panics; EmitFailed
	// events the emitter refused; NotifyFailed envelopes not queued.
	Errors, EmitFailed, NotifyFailed int64
}

func (s PassStats) busy() bool {
	return s.Expired+s.Ended+s.SchemaChanged+s.Errors+s.EmitFailed+s.NotifyFailed > 0
}

// runPass runs a pass unless another instance holds the lock.
func (w *Worker) runPass(ctx context.Context) (stats PassStats, ran bool) {
	defer w.recoverPanic("pass")
	ok, err := w.lock.AttemptLock(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Warn("mcp subscriptions worker: failed to take the lock", zap.Error(err))
		}
		return stats, false
	}
	if !ok {
		return stats, false
	}
	defer func() {
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
		defer cancel()
		_, _ = w.lock.Unlock(uctx)
	}()

	start := time.Now()
	stats = w.pass(ctx)
	fields := []zap.Field{
		zap.Int64("expired", stats.Expired),
		zap.Int64("rescored", stats.Rescored),
		zap.Int64("removed", stats.Removed),
		zap.Int64("ended", stats.Ended),
		zap.Int64("schema_changed", stats.SchemaChanged),
		zap.Int64("errors", stats.Errors),
		zap.Int64("emit_failed", stats.EmitFailed),
		zap.Int64("notify_failed", stats.NotifyFailed),
		zap.Bool("termination_paused", stats.TerminationPaused),
		zap.Duration("duration", time.Since(start)),
	}
	if stats.busy() {
		w.logger.Info("mcp subscriptions worker pass", fields...)
	} else {
		w.logger.Debug("mcp subscriptions worker pass", fields...)
	}
	return stats, true
}

// pass runs the terminations, then the expiry sweep, within one interval.
// Terminations get at most half of it: they are rare, the sweep is steady.
func (w *Worker) pass(ctx context.Context) PassStats {
	passCtx, cancel := context.WithTimeout(ctx, w.cfg.Interval)
	defer cancel()
	p := &pass{
		w:       w,
		runCtx:  ctx,
		now:     w.cfg.Now(),
		tenants: make(map[string]*tenantEntry),
	}
	if w.cfg.Emitter != nil && w.cfg.Emitter.Enabled(opevents.TopicMCPSubscriptionExpired) {
		p.emitExpired = true
	}

	tctx, tcancel := context.WithTimeout(passCtx, w.cfg.Interval/2)
	p.reconcile(tctx)
	tcancel()
	p.sweep(passCtx)
	p.reportTenants(ctx)
	return p.stats.snapshot()
}

// pass is the state of one pass.
type pass struct {
	w *Worker
	// runCtx is the worker's context: envelopes for subscriptions already
	// deleted may wait past the pass deadline, not past shutdown.
	runCtx      context.Context
	now         time.Time
	emitExpired bool
	stats       passCounters

	mu      sync.Mutex
	tenants map[string]*tenantEntry
}

type passCounters struct {
	expired, rescored, removed, ended, schemaChanged atomic.Int64
	errors, emitFailed, notifyFailed                 atomic.Int64
	terminationPaused                                atomic.Bool
}

func (c *passCounters) snapshot() PassStats {
	return PassStats{
		Expired:           c.expired.Load(),
		Rescored:          c.rescored.Load(),
		Removed:           c.removed.Load(),
		Ended:             c.ended.Load(),
		SchemaChanged:     c.schemaChanged.Load(),
		TerminationPaused: c.terminationPaused.Load(),
		Errors:            c.errors.Load(),
		EmitFailed:        c.emitFailed.Load(),
		NotifyFailed:      c.notifyFailed.Load(),
	}
}

// tenantEntry tracks a tenant whose subscriptions the pass may delete.
type tenantEntry struct {
	once    sync.Once
	snap    TenantSnapshot
	ok      bool
	changed atomic.Bool
}

func (e *tenantEntry) markChanged() {
	if e != nil {
		e.changed.Store(true)
	}
}

// beforeDelete snapshots the tenant once per pass, before its first
// deletion; concurrent callers wait for it. Nil without TenantUpdates.
func (p *pass) beforeDelete(ctx context.Context, tenantID string) *tenantEntry {
	updates := p.w.cfg.TenantUpdates
	if updates == nil {
		return nil
	}
	p.mu.Lock()
	e := p.tenants[tenantID]
	if e == nil {
		e = &tenantEntry{}
		p.tenants[tenantID] = e
	}
	p.mu.Unlock()
	e.once.Do(func() { e.snap, e.ok = updates.Snapshot(ctx, tenantID) })
	return e
}

// reportTenants reports every tenant the pass changed.
func (p *pass) reportTenants(ctx context.Context) {
	updates := p.w.cfg.TenantUpdates
	if updates == nil || ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	changed := make(map[string]TenantSnapshot)
	for id, e := range p.tenants {
		if e.ok && e.changed.Load() {
			changed[id] = e.snap
		}
	}
	p.mu.Unlock()

	sem := make(chan struct{}, p.w.cfg.Concurrency)
	var wg sync.WaitGroup
	for id, snap := range changed {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			defer p.recoverItem("tenant update", id, "")
			rctx, cancel := context.WithTimeout(ctx, reportTimeout)
			defer cancel()
			updates.Report(rctx, id, snap)
		})
	}
	wg.Wait()
}

// each runs fn on refs, at most Concurrency at once, and returns how many
// returned true. It starts nothing once ctx is done.
func (p *pass) each(ctx context.Context, refs []tenantstore.IndexedDestination, fn func(context.Context, tenantstore.IndexedDestination) bool) int {
	sem := make(chan struct{}, p.w.cfg.Concurrency)
	var wg sync.WaitGroup
	var n atomic.Int64
	for _, ref := range refs {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			defer p.recoverItem("subscription", ref.TenantID, ref.DestinationID)
			if fn(ctx, ref) {
				n.Add(1)
			}
		})
	}
	wg.Wait()
	return int(n.Load())
}

func (p *pass) recoverItem(what, tenantID, destinationID string) {
	if r := recover(); r != nil {
		p.stats.errors.Add(1)
		p.w.logger.Error("mcp subscriptions worker: recovered from a panic",
			zap.String("in", what),
			zap.String("tenant_id", tenantID),
			zap.String("destination_id", destinationID),
			zap.Any("panic", r), zap.StackSkip("stack", 2))
	}
}

// fail counts and logs a failed store call, unless the pass is stopping.
func (p *pass) fail(ctx context.Context, op string, ref tenantstore.IndexedDestination, err error) {
	if ctx.Err() != nil {
		return
	}
	p.stats.errors.Add(1)
	p.w.logger.Warn("mcp subscriptions worker: "+op+" failed",
		zap.String("tenant_id", ref.TenantID),
		zap.String("destination_id", ref.DestinationID),
		zap.Error(err))
}

// removeIndexed removes ref from the global index and the given topic
// indexes, where its score is still ref.Score.
func (p *pass) removeIndexed(ctx context.Context, topics []string, ref tenantstore.IndexedDestination) bool {
	if err := p.w.cfg.Store.RemoveIndexedDestination(ctx, models.DestinationTypeMCP, topics, ref); err != nil {
		p.fail(ctx, "index removal", ref, err)
		return false
	}
	p.stats.removed.Add(1)
	return true
}
