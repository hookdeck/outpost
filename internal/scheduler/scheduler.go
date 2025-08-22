package scheduler

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-redis/redis"
	"github.com/hookdeck/outpost/internal/logging"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"go.uber.org/zap"
)

type ScheduleOption func(*ScheduleOptions)

type ScheduleOptions struct {
	ID string
}

func WithTaskID(id string) ScheduleOption {
	return func(o *ScheduleOptions) {
		o.ID = id
	}
}

type Scheduler interface {
	Init(context.Context) error
	Schedule(context.Context, string, time.Duration, ...ScheduleOption) error
	Monitor(context.Context) error
	Cancel(context.Context, string) error
	Shutdown() error
}

type schedulerImpl struct {
	rsmqClient *rsmq.RedisSMQ
	config     *config
	name       string
	exec       func(context.Context, string) error
}

type config struct {
	visibilityTimeout uint
	logger            *logging.Logger
}

func WithVisibilityTimeout(vt uint) func(*config) {
	return func(c *config) {
		c.visibilityTimeout = vt
	}
}

func WithLogger(logger *logging.Logger) func(*config) {
	return func(c *config) {
		c.logger = logger
	}
}

func New(name string, redisConfig *iredis.RedisConfig, exec func(context.Context, string) error, opts ...func(*config)) Scheduler {
	// Extract configuration including logger first
	config := &config{
		visibilityTimeout: rsmq.UnsetVt,
	}
	for _, opt := range opts {
		opt(config)
	}
	
	var rsmqClient *rsmq.RedisSMQ
	
	if redisConfig.ClusterEnabled {
		// Use cluster client for clustered Redis deployments
		clusterOptions := &redis.ClusterOptions{
			Addrs:    []string{fmt.Sprintf("%s:%d", redisConfig.Host, redisConfig.Port)},
			Password: redisConfig.Password,
			// Note: Database is ignored in cluster mode
		}
		
		if redisConfig.TLSEnabled {
			clusterOptions.TLSConfig = &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, // Azure Managed Redis uses self-signed certificates
			}
		}
		
		clusterClient := redis.NewClusterClient(clusterOptions)
		
		// Test cluster connectivity
		if err := clusterClient.Ping().Err(); err != nil {
			if config.logger != nil {
				config.logger.Error("Redis cluster client connection failed", 
					zap.Error(err),
					zap.String("host", redisConfig.Host),
					zap.Int("port", redisConfig.Port),
					zap.Bool("tls", redisConfig.TLSEnabled))
			}
			panic(fmt.Sprintf("Redis cluster client connection failed: %v", err))
		}
		
		if config.logger != nil {
			config.logger.Info("Redis cluster client initialized successfully",
				zap.String("host", redisConfig.Host),
				zap.Int("port", redisConfig.Port),
				zap.Bool("tls", redisConfig.TLSEnabled))
		}
		
		// Create RSMQ client with cluster client directly
		if config.logger != nil {
			rsmqClient = rsmq.NewRedisSMQ(clusterClient, "rsmq", config.logger)
		} else {
			rsmqClient = rsmq.NewRedisSMQ(clusterClient, "rsmq")
		}
	} else {
		// Use regular client for non-clustered Redis
		redisOptions := &redis.Options{
			Addr:     fmt.Sprintf("%s:%d", redisConfig.Host, redisConfig.Port),
			Password: redisConfig.Password,
			DB:       redisConfig.Database,
		}
		
		if redisConfig.TLSEnabled {
			redisOptions.TLSConfig = &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, // Azure Managed Redis uses self-signed certificates
			}
		}
		
		redisClient := redis.NewClient(redisOptions)
		
		// Test regular client connectivity
		if err := redisClient.Ping().Err(); err != nil {
			if config.logger != nil {
				config.logger.Error("Redis regular client connection failed", 
					zap.Error(err),
					zap.String("host", redisConfig.Host),
					zap.Int("port", redisConfig.Port),
					zap.Bool("tls", redisConfig.TLSEnabled))
			}
			panic(fmt.Sprintf("Redis regular client connection failed: %v", err))
		}
		
		if config.logger != nil {
			config.logger.Info("Redis regular client initialized successfully",
				zap.String("host", redisConfig.Host),
				zap.Int("port", redisConfig.Port),
				zap.Bool("tls", redisConfig.TLSEnabled),
				zap.Int("database", redisConfig.Database))
		}
		
		// Create RSMQ client with regular client
		if config.logger != nil {
			rsmqClient = rsmq.NewRedisSMQ(redisClient, "rsmq", config.logger)
		} else {
			rsmqClient = rsmq.NewRedisSMQ(redisClient, "rsmq")
		}
	}
	
	return &schedulerImpl{
		rsmqClient: rsmqClient,
		config:     config,
		name:       name,
		exec:       exec,
	}
}

func (s *schedulerImpl) Init(ctx context.Context) error {
	if err := s.rsmqClient.CreateQueue(s.name, s.config.visibilityTimeout, rsmq.UnsetDelay, rsmq.UnsetMaxsize); err != nil && err != rsmq.ErrQueueExists {
		return err
	}
	return nil
}

func (s *schedulerImpl) Schedule(ctx context.Context, task string, delay time.Duration, opts ...ScheduleOption) error {
	// Parse options
	options := &ScheduleOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Convert delay to seconds and round up
	delaySeconds := uint(delay.Seconds() + 0.5)

	// Generate RSMQ ID if not provided
	var rsmqOpts []rsmq.SendMessageOption
	if options.ID != "" {
		rsmqID := generateRSMQID(options.ID)
		rsmqOpts = append(rsmqOpts, rsmq.WithMessageID(rsmqID))
	}

	// Send message
	_, err := s.rsmqClient.SendMessage(s.name, task, delaySeconds, rsmqOpts...)
	return err
}

func (s *schedulerImpl) Monitor(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, err := s.rsmqClient.ReceiveMessage(s.name, rsmq.UnsetVt)
			if err != nil {
				return err
			}
			if msg == nil {
				time.Sleep(time.Second / 10)
				continue
			}
			// TODO: consider using a worker pool to limit the number of concurrent executions
			go func() {
				if err := s.exec(ctx, msg.Message); err != nil {
					return
				}
				if err := s.rsmqClient.DeleteMessage(s.name, msg.ID); err != nil {
					return
				}
			}()
		}
	}
}

func (s *schedulerImpl) Cancel(ctx context.Context, taskID string) error {
	// Generate the RSMQ ID for this task
	rsmqID := generateRSMQID(taskID)

	// Delete the message - RSMQ returns ErrMessageNotFound if it doesn't exist
	err := s.rsmqClient.DeleteMessage(s.name, rsmqID)
	if err == rsmq.ErrMessageNotFound {
		return nil // Task already gone is not an error
	}
	return err
}

func (s *schedulerImpl) Shutdown() error {
	return s.rsmqClient.Quit()
}
