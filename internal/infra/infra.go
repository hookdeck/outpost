package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	lockKey      = "outpost:lock"
	lockAttempts = 5
	lockDelay    = 5 * time.Second
	lockTTL      = 60 * time.Second // 1 minute to allow for slow cloud provider operations
)

var (
	// ErrInfraNotFound is returned when infrastructure does not exist and auto provisioning is disabled
	ErrInfraNotFound = errors.New("required message queues do not exist. Either create them manually or set MQS_AUTO_PROVISION=true to enable auto-provisioning")
)

type Infra struct {
	lock         redislock.Lock
	provider     InfraProvider
	shouldManage bool
	logger       *logging.Logger
}

// InfraProvider handles the actual infrastructure operations
type InfraProvider interface {
	Exist(ctx context.Context) (bool, error)
	Declare(ctx context.Context) error
	Teardown(ctx context.Context) error
}

type Config struct {
	DeliveryMQ    *mqinfra.MQInfraConfig
	LogMQ         *mqinfra.MQInfraConfig
	AutoProvision *bool
}

func (cfg *Config) SetSensiblePolicyDefaults() {
	cfg.DeliveryMQ.Policy.RetryLimit = 5
	cfg.LogMQ.Policy.RetryLimit = 5
}

// infraProvider implements InfraProvider using real MQ infrastructure
type infraProvider struct {
	deliveryMQ mqinfra.MQInfra
	logMQ      mqinfra.MQInfra
	logger     *logging.Logger
	mqType     string
}

func (p *infraProvider) Exist(ctx context.Context) (bool, error) {
	var deliveryExists, logExists bool

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		p.logger.Debug("checking if deliverymq infrastructure exists", zap.String("mq_type", p.mqType))
		exists, err := p.deliveryMQ.Exist(ctx)
		if err != nil {
			return err
		}
		deliveryExists = exists
		if exists {
			p.logger.Debug("deliverymq infrastructure exists", zap.String("mq_type", p.mqType))
		} else {
			p.logger.Debug("deliverymq infrastructure does not exist", zap.String("mq_type", p.mqType))
		}
		return nil
	})

	g.Go(func() error {
		p.logger.Debug("checking if logmq infrastructure exists", zap.String("mq_type", p.mqType))
		exists, err := p.logMQ.Exist(ctx)
		if err != nil {
			return err
		}
		logExists = exists
		if exists {
			p.logger.Debug("logmq infrastructure exists", zap.String("mq_type", p.mqType))
		} else {
			p.logger.Debug("logmq infrastructure does not exist", zap.String("mq_type", p.mqType))
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return false, err
	}

	return deliveryExists && logExists, nil
}

func (p *infraProvider) Declare(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		p.logger.Info("declaring deliverymq infrastructure", zap.String("mq_type", p.mqType))
		start := time.Now()
		if err := p.deliveryMQ.Declare(ctx); err != nil {
			p.logger.Error("failed to declare deliverymq infrastructure",
				zap.String("mq_type", p.mqType),
				zap.Duration("duration", time.Since(start)),
				zap.Error(err))
			return err
		}
		p.logger.Info("deliverymq infrastructure declared",
			zap.String("mq_type", p.mqType),
			zap.Duration("duration", time.Since(start)))
		return nil
	})

	g.Go(func() error {
		p.logger.Info("declaring logmq infrastructure", zap.String("mq_type", p.mqType))
		start := time.Now()
		if err := p.logMQ.Declare(ctx); err != nil {
			p.logger.Error("failed to declare logmq infrastructure",
				zap.String("mq_type", p.mqType),
				zap.Duration("duration", time.Since(start)),
				zap.Error(err))
			return err
		}
		p.logger.Info("logmq infrastructure declared",
			zap.String("mq_type", p.mqType),
			zap.Duration("duration", time.Since(start)))
		return nil
	})

	return g.Wait()
}

func (p *infraProvider) Teardown(ctx context.Context) error {
	if err := p.deliveryMQ.TearDown(ctx); err != nil {
		return err
	}

	if err := p.logMQ.TearDown(ctx); err != nil {
		return err
	}

	return nil
}

func NewInfra(cfg Config, redisClient redis.Cmdable, logger *logging.Logger, mqType string) Infra {
	cfg.SetSensiblePolicyDefaults()

	provider := &infraProvider{
		deliveryMQ: mqinfra.New(cfg.DeliveryMQ),
		logMQ:      mqinfra.New(cfg.LogMQ),
		logger:     logger,
		mqType:     mqType,
	}

	// Default shouldManage to true if not set (backward compatible)
	shouldManage := true
	if cfg.AutoProvision != nil {
		shouldManage = *cfg.AutoProvision
	}

	return Infra{
		lock:         redislock.New(redisClient, redislock.WithKey(lockKey), redislock.WithTTL(lockTTL)),
		provider:     provider,
		shouldManage: shouldManage,
		logger:       logger,
	}
}

// Init initializes and verifies infrastructure based on configuration.
// If AutoProvision is true (default), it will create infrastructure if needed.
// If AutoProvision is false, it will only verify infrastructure exists.
func Init(ctx context.Context, cfg Config, redisClient redis.Cmdable, logger *logging.Logger, mqType string) error {
	infra := NewInfra(cfg, redisClient, logger, mqType)

	logger.Info("initializing mq infrastructure",
		zap.String("mq_type", mqType),
		zap.Bool("auto_provision", infra.shouldManage))

	if infra.shouldManage {
		return infra.Declare(ctx)
	}

	// shouldManage is false, only verify existence
	logger.Debug("auto_provision disabled, verifying infrastructure exists", zap.String("mq_type", mqType))
	exists, err := infra.provider.Exist(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify infrastructure exists: %w", err)
	}
	if !exists {
		return ErrInfraNotFound
	}
	logger.Info("mq infrastructure verified", zap.String("mq_type", mqType))
	return nil
}

// NewInfraWithProvider creates an Infra instance with custom lock and provider (for testing)
func NewInfraWithProvider(lock redislock.Lock, provider InfraProvider, shouldManage bool, logger *logging.Logger) *Infra {
	return &Infra{
		lock:         lock,
		provider:     provider,
		shouldManage: shouldManage,
		logger:       logger,
	}
}

func (infra *Infra) Declare(ctx context.Context) error {
	for attempt := 0; attempt < lockAttempts; attempt++ {
		infra.logger.Debug("checking if infrastructure declaration needed",
			zap.Int("attempt", attempt+1),
			zap.Int("max_attempts", lockAttempts))

		shouldDeclare, hasLocked, err := infra.shouldDeclareAndAcquireLock(ctx)
		if err != nil {
			return err
		}
		if !shouldDeclare {
			infra.logger.Info("infrastructure already exists, skipping declaration")
			return nil
		}

		if hasLocked {
			infra.logger.Info("acquired infrastructure lock, declaring infrastructure",
				zap.Duration("lock_ttl", lockTTL))

			// We got the lock, declare infrastructure
			declareStart := time.Now()
			defer func() {
				// Best-effort unlock. If it fails (e.g., lock expired due to slow
				// infrastructure provisioning), that's acceptable - the lock will
				// expire on its own and the infrastructure was still declared successfully.
				unlocked, unlockErr := infra.lock.Unlock(ctx)
				if unlockErr != nil {
					infra.logger.Warn("failed to unlock infrastructure lock",
						zap.Error(unlockErr),
						zap.Duration("declaration_duration", time.Since(declareStart)))
				} else if !unlocked {
					infra.logger.Warn("infrastructure lock already expired before unlock",
						zap.Duration("declaration_duration", time.Since(declareStart)),
						zap.Duration("lock_ttl", lockTTL))
				} else {
					infra.logger.Debug("infrastructure lock released")
				}
			}()

			if err := infra.provider.Declare(ctx); err != nil {
				return err
			}

			infra.logger.Info("infrastructure declaration completed",
				zap.Duration("duration", time.Since(declareStart)))
			return nil
		}

		// We didn't get the lock, wait before retry
		infra.logger.Debug("infrastructure lock held by another instance, waiting",
			zap.Duration("delay", lockDelay),
			zap.Int("attempt", attempt+1))
		if attempt < lockAttempts-1 {
			time.Sleep(lockDelay)
		}
	}

	return fmt.Errorf("failed to acquire lock after %d attempts", lockAttempts)
}

// Verify checks if infrastructure exists and returns an error if it doesn't.
func (infra *Infra) Verify(ctx context.Context) error {
	exists, err := infra.provider.Exist(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify infrastructure exists: %w", err)
	}
	if !exists {
		return ErrInfraNotFound
	}
	return nil
}

func (infra *Infra) Teardown(ctx context.Context) error {
	return infra.provider.Teardown(ctx)
}

// shouldDeclareAndAcquireLock checks if
func (infra *Infra) shouldDeclareAndAcquireLock(ctx context.Context) (shouldDeclare bool, hasLocked bool, err error) {
	shouldDeclare = false
	hasLocked = false
	err = nil

	exists, err := infra.provider.Exist(ctx)
	if err != nil {
		err = fmt.Errorf("failed to check if infra exists: %w", err)
		return
	}
	if exists {
		// if infra exists, return early, no need to acquire lock
		shouldDeclare = false
		return
	}
	shouldDeclare = true

	hasLocked, err = infra.lock.AttemptLock(ctx)
	if err != nil {
		err = fmt.Errorf("failed to acquire lock: %w", err)
		return
	}

	return
}
