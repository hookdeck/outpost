package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/redis"
)

const (
	lockKey      = "outpost:lock"
	lockAttempts = 5
	lockDelay    = 5 * time.Second
	lockTTL      = 10 * time.Second
)

var (
	// ErrInfraNotFound is returned when infrastructure does not exist and auto provisioning is disabled
	ErrInfraNotFound = errors.New("infrastructure does not exist and auto provisioning is disabled (MQS_AUTO_PROVISION=false). Please create the required message queues manually or set MQS_AUTO_PROVISION=true to enable auto provisioning")
)

type Infra struct {
	lock         Lock
	provider     InfraProvider
	shouldManage bool
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

type Lock interface {
	AttemptLock(ctx context.Context) (bool, error)
	Unlock(ctx context.Context) (bool, error)
}

// infraProvider implements InfraProvider using real MQ infrastructure
type infraProvider struct {
	deliveryMQ mqinfra.MQInfra
	logMQ      mqinfra.MQInfra
}

func (p *infraProvider) Exist(ctx context.Context) (bool, error) {
	if exists, err := p.deliveryMQ.Exist(ctx); err != nil {
		return false, err
	} else if !exists {
		return false, nil
	}

	if exists, err := p.logMQ.Exist(ctx); err != nil {
		return false, err
	} else if !exists {
		return false, nil
	}

	return true, nil
}

func (p *infraProvider) Declare(ctx context.Context) error {
	if err := p.deliveryMQ.Declare(ctx); err != nil {
		return err
	}

	if err := p.logMQ.Declare(ctx); err != nil {
		return err
	}

	return nil
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

func NewInfra(cfg Config, redisClient redis.Cmdable) Infra {
	cfg.SetSensiblePolicyDefaults()

	provider := &infraProvider{
		deliveryMQ: mqinfra.New(cfg.DeliveryMQ),
		logMQ:      mqinfra.New(cfg.LogMQ),
	}

	// Default shouldManage to true if not set (backward compatible)
	shouldManage := true
	if cfg.AutoProvision != nil {
		shouldManage = *cfg.AutoProvision
	}

	return Infra{
		lock:         NewRedisLock(redisClient),
		provider:     provider,
		shouldManage: shouldManage,
	}
}

// Init initializes and verifies infrastructure based on configuration.
// If AutoProvision is true (default), it will create infrastructure if needed.
// If AutoProvision is false, it will only verify infrastructure exists.
func Init(ctx context.Context, cfg Config, redisClient redis.Cmdable) error {
	infra := NewInfra(cfg, redisClient)

	if infra.shouldManage {
		return infra.Declare(ctx)
	}

	// shouldManage is false, only verify existence
	exists, err := infra.provider.Exist(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify infrastructure exists: %w", err)
	}
	if !exists {
		return ErrInfraNotFound
	}
	return nil
}

// NewInfraWithProvider creates an Infra instance with custom lock and provider (for testing)
func NewInfraWithProvider(lock Lock, provider InfraProvider, shouldManage bool) *Infra {
	return &Infra{
		lock:         lock,
		provider:     provider,
		shouldManage: shouldManage,
	}
}

func (infra *Infra) Declare(ctx context.Context) error {
	for attempt := 0; attempt < lockAttempts; attempt++ {
		shouldDeclare, hasLocked, err := infra.shouldDeclareAndAcquireLock(ctx)
		if err != nil {
			return err
		}
		if !shouldDeclare {
			return nil
		}

		if hasLocked {
			// We got the lock, declare infrastructure
			defer func() {
				// TODO: improve error handling
				unlocked, err := infra.lock.Unlock(ctx)
				if err != nil {
					panic(err)
				}
				if !unlocked {
					panic("failed to unlock lock")
				}
			}()

			if err := infra.provider.Declare(ctx); err != nil {
				return err
			}

			return nil
		}

		// We didn't get the lock, wait before retry
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
