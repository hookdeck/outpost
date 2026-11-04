package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/worker"
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

type retryLaterError struct {
	delay time.Duration
	err   error
}

func (e *retryLaterError) Error() string { return e.err.Error() }
func (e *retryLaterError) Unwrap() error { return e.err }

// RetryLater marks an exec error as "not ready yet": the task comes back after
// delay and this receive does not count toward the max receive count.
func RetryLater(delay time.Duration, err error) error {
	if err == nil {
		return nil
	}
	return &retryLaterError{delay: delay, err: err}
}

type Scheduler interface {
	Init(context.Context) error
	Schedule(context.Context, string, time.Duration, ...ScheduleOption) error
	Monitor(context.Context) error
	Cancel(context.Context, string) error
	Shutdown() error
}

type schedulerImpl struct {
	rsmqClient rsmq.Client
	config     *config
	name       string
	exec       func(context.Context, string) error
	// slots bounds concurrent executions; nil means unbounded.
	slots chan struct{}
}

const (
	// errorBackoffBase is the first delay of the consecutive-error ladder, and
	// minIdleSleep the floor on an idle sleep. Both are internal so the error
	// tolerance documented in New stays fixed regardless of pollBackoff.
	errorBackoffBase = 100 * time.Millisecond
	minIdleSleep     = 10 * time.Millisecond
)

type config struct {
	visibilityTimeout    uint
	pollBackoff          time.Duration
	maxConsecutiveErrors int
	maxErrorBackoff      time.Duration
	maxReceiveCount      uint64
	maxExecBackoff       time.Duration
	maxConcurrency       int
	shutdownGrace        time.Duration
	rand                 func() float64
	logger               *logging.Logger
}

type Option func(*config)

func WithVisibilityTimeout(vt uint) Option {
	return func(c *config) {
		c.visibilityTimeout = vt
	}
}

// WithPollBackoff sets the maximum time the monitor sleeps when no message is
// due. When the queue holds a message that is not yet visible, the monitor
// instead sleeps until that message comes due, so the actual sleep is
// min(timeUntilNextMessage, backoff). Worst-case lateness is therefore bounded
// by backoff, and is zero when backoff is at most the shortest delay anything
// is scheduled with — the config auto mode (retry_poll_backoff_ms = 0)
// guarantees this; explicit values may trade retry latency for idle cost.
func WithPollBackoff(backoff time.Duration) Option {
	return func(c *config) {
		c.pollBackoff = backoff
	}
}

func WithMaxConsecutiveErrors(n int) Option {
	return func(c *config) {
		c.maxConsecutiveErrors = n
	}
}

func WithMaxReceiveCount(n uint64) Option {
	return func(c *config) {
		c.maxReceiveCount = n
	}
}

func WithMaxExecBackoff(d time.Duration) Option {
	return func(c *config) {
		c.maxExecBackoff = d
	}
}

// WithMaxConcurrency bounds the number of tasks executing at once. The monitor
// stops receiving while at the limit, so tasks wait in the queue instead of
// holding a visibility timeout. 0 means unbounded.
func WithMaxConcurrency(n int) Option {
	return func(c *config) {
		c.maxConcurrency = n
	}
}

// WithShutdownGrace sets how long Monitor waits for running tasks after its
// context is cancelled. Tasks still running then are cancelled and handled
// like any failed task.
func WithShutdownGrace(d time.Duration) Option {
	return func(c *config) {
		c.shutdownGrace = d
	}
}

func WithLogger(logger *logging.Logger) Option {
	return func(c *config) {
		c.logger = logger
	}
}

func New(name string, rsmqClient rsmq.Client, exec func(context.Context, string) error, opts ...Option) Scheduler {
	// Error retry schedule:
	//
	//   Error  Backoff    Cumulative
	//   1      100ms      0.1s
	//   2      200ms      0.3s
	//   3      400ms      0.7s       ← 3 retries within ~1s
	//   4      800ms      1.5s
	//   5      1.6s       3.1s
	//   6      3.2s       6.3s
	//   7      6.4s       12.7s
	//   8      12.8s      25.5s
	//   9      15s (cap)  40.5s
	//   10     15s (cap)  55.5s      ← worker dies (~1 min total)
	//
	// Backoff formula: errorBackoffBase * 2^(attempt-1), capped at
	// maxErrorBackoff. The ladder is independent of pollBackoff so that this
	// tolerance holds however the idle poll interval is configured.
	// After maxConsecutiveErrors Monitor returns. Unless the worker is
	// registered with a restart policy, the supervisor does not restart it,
	// so these values must tolerate transient infra outages
	// (e.g. managed Redis/Dragonfly restarts) without killing the worker.
	// ~1 min is sufficient for managed Redis/Dragonfly to recover from
	// routine restarts or brief network blips.
	config := &config{
		visibilityTimeout:    rsmq.UnsetVt,
		pollBackoff:          200 * time.Millisecond,
		maxConsecutiveErrors: 10,
		maxErrorBackoff:      15 * time.Second,
		maxExecBackoff:       15 * time.Minute,
		shutdownGrace:        5 * time.Second,
		rand:                 rand.Float64,
	}
	for _, opt := range opts {
		opt(config)
	}

	s := &schedulerImpl{
		rsmqClient: rsmqClient,
		config:     config,
		name:       name,
		exec:       exec,
	}
	if config.maxConcurrency > 0 {
		s.slots = make(chan struct{}, config.maxConcurrency)
	}
	return s
}

// acquire takes an execution slot, blocking while at the limit. It returns
// false if ctx is done first.
func (s *schedulerImpl) acquire(ctx context.Context) bool {
	if s.slots == nil {
		return true
	}
	select {
	case s.slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *schedulerImpl) release() {
	if s.slots != nil {
		<-s.slots
	}
}

func (s *schedulerImpl) Init(ctx context.Context) error {
	if err := s.rsmqClient.CreateQueue(s.name, s.config.visibilityTimeout, rsmq.UnsetDelay, rsmq.UnsetMaxsize); err != nil && err != rsmq.ErrQueueExists {
		return err
	}
	if s.config.maxReceiveCount > 0 {
		if err := s.rsmqClient.CreateQueue(s.dlqName(), rsmq.UnsetVt, rsmq.UnsetDelay, rsmq.UnsetMaxsize); err != nil && err != rsmq.ErrQueueExists {
			return err
		}
	}
	return nil
}

func (s *schedulerImpl) dlqName() string {
	return s.name + "-dlq"
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
	// Tasks run on a context that shutdown doesn't cancel, so a received task
	// finishes instead of being redelivered; drain bounds the wait.
	taskCtx, cancelTasks := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelTasks()
	var inflight sync.WaitGroup
	defer s.drain(&inflight, cancelTasks)
	consecutiveErrors := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// The slot is taken before receiving so a message is only received
			// when it can run now.
			if !s.acquire(ctx) {
				return nil
			}
			res, err := s.rsmqClient.ReceiveMessagePoll(s.name, rsmq.UnsetVt)
			if err != nil {
				s.release()
				consecutiveErrors++
				if consecutiveErrors >= s.config.maxConsecutiveErrors {
					return fmt.Errorf("max consecutive errors reached: %w", err)
				}
				backoff := min(errorBackoffBase*time.Duration(1<<(consecutiveErrors-1)), s.config.maxErrorBackoff)
				s.config.logger.Ctx(ctx).Warn("scheduler receive error, retrying",
					zap.Error(err),
					zap.Int("attempt", consecutiveErrors),
					zap.Duration("backoff", backoff))
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff):
				}
				continue
			}
			consecutiveErrors = 0
			worker.Ready(ctx)
			msg := res.Message
			if msg == nil {
				s.release()
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(s.idleSleep(res)):
				}
				continue
			}
			if s.config.maxReceiveCount > 0 && msg.Rc > s.config.maxReceiveCount {
				s.moveToDLQ(ctx, msg)
				s.release()
				continue
			}
			inflight.Add(1)
			go func() {
				defer inflight.Done()
				defer s.release()
				s.handle(taskCtx, msg)
			}()
		}
	}
}

// drain waits for running tasks, cancelling them once the shutdown grace has
// passed.
func (s *schedulerImpl) drain(inflight *sync.WaitGroup, cancelTasks context.CancelFunc) {
	done := make(chan struct{})
	go func() {
		inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(s.config.shutdownGrace):
		cancelTasks()
		<-done
	}
}

// handle runs a received task and then deletes it, or backs it off on error.
// The delete and the backoff are conditional on the message's receipt: if the
// task was rescheduled under the same ID while it ran, the new message stays.
func (s *schedulerImpl) handle(ctx context.Context, msg *rsmq.QueueMessage) {
	if err := s.exec(ctx, msg.Message); err != nil {
		var later *retryLaterError
		if errors.As(err, &later) {
			s.requeueMessage(ctx, msg, later.delay)
			return
		}
		if s.config.maxReceiveCount > 0 {
			s.backoffMessage(ctx, msg)
		}
		return
	}
	err := s.rsmqClient.DeleteMessageWithReceipt(s.name, msg.ID, msg.Receipt)
	switch {
	case err == nil:
	case errors.Is(err, rsmq.ErrReceiptMismatch):
		s.config.logger.Ctx(ctx).Debug("task changed during execution (rescheduled or received again), not deleted",
			zap.String("queue", s.name),
			zap.String("message_id", msg.ID))
	case errors.Is(err, rsmq.ErrMessageNotFound):
	default:
		s.config.logger.Ctx(ctx).Warn("failed to delete executed task",
			zap.Error(err),
			zap.String("queue", s.name),
			zap.String("message_id", msg.ID))
	}
}

// isSuperseded reports that the message is gone or was re-created or received
// again since the receipt was issued.
func isSuperseded(err error) bool {
	return errors.Is(err, rsmq.ErrReceiptMismatch) || errors.Is(err, rsmq.ErrMessageNotFound)
}

// idleSleep returns how long to wait before the next poll when nothing was
// due. An empty queue waits the full pollBackoff; otherwise the wait is capped
// at the time until the queue's earliest message becomes visible, so a message
// scheduled before the monitor started sleeping is picked up on time.
func (s *schedulerImpl) idleSleep(res rsmq.PollResult) time.Duration {
	sleep := s.config.pollBackoff
	if res.HasNext && res.NextDue < sleep {
		sleep = res.NextDue
	}
	// Floor the wait so a message that is due in a millisecond does not turn
	// the monitor into a spin loop; never above the configured maximum.
	return max(sleep, min(minIdleSleep, s.config.pollBackoff))
}

// moveToDLQ dead-letters a message that exceeded the max receive count. The
// DLQ send happens before the delete so a failure between the two steps can
// only duplicate the message, not lose it.
func (s *schedulerImpl) moveToDLQ(ctx context.Context, msg *rsmq.QueueMessage) {
	logger := s.config.logger.Ctx(ctx)
	if _, err := s.rsmqClient.SendMessage(s.dlqName(), msg.Message, 0); err != nil {
		logger.Error("failed to move task to dead-letter queue",
			zap.Error(err),
			zap.String("queue", s.name),
			zap.String("message_id", msg.ID),
			zap.Uint64("receive_count", msg.Rc))
		return
	}
	if err := s.rsmqClient.DeleteMessageWithReceipt(s.name, msg.ID, msg.Receipt); err != nil {
		if errors.Is(err, rsmq.ErrReceiptMismatch) {
			logger.Warn("task changed before delete; copy added to dead-letter queue, original kept",
				zap.String("queue", s.name),
				zap.String("message_id", msg.ID))
			return
		}
		if errors.Is(err, rsmq.ErrMessageNotFound) {
			return
		}
		logger.Error("failed to delete task after moving to dead-letter queue",
			zap.Error(err),
			zap.String("queue", s.name),
			zap.String("message_id", msg.ID))
		return
	}
	logger.Error("task exceeded max receive count, moved to dead-letter queue",
		zap.String("queue", s.name),
		zap.String("dlq", s.dlqName()),
		zap.String("message_id", msg.ID),
		zap.Uint64("receive_count", msg.Rc),
		zap.String("task", msg.Message))
}

// backoffMessage reschedules a failed message with exponential backoff
// (vt, 2*vt, 4*vt, ... capped at maxExecBackoff).
func (s *schedulerImpl) backoffMessage(ctx context.Context, msg *rsmq.QueueMessage) {
	base := s.config.visibilityTimeout
	if base == rsmq.UnsetVt {
		base = rsmq.DefaultVt
	}
	backoff := jitterBackoff(calculateExecBackoff(time.Duration(base)*time.Second, msg.Rc, s.config.maxExecBackoff), s.config.maxExecBackoff, s.config.rand())
	if err := s.rsmqClient.ChangeMessageVisibilityWithReceipt(s.name, msg.ID, msg.Receipt, uint(backoff/time.Second)); err != nil && !isSuperseded(err) {
		s.config.logger.Ctx(ctx).Warn("failed to extend backoff for failed task",
			zap.Error(err),
			zap.String("queue", s.name),
			zap.String("message_id", msg.ID),
			zap.Uint64("receive_count", msg.Rc))
	}
}

// requeueMessage makes a not-ready task visible after a jittered delay,
// rounded up to whole seconds, without counting this receive.
func (s *schedulerImpl) requeueMessage(ctx context.Context, msg *rsmq.QueueMessage, delay time.Duration) {
	delay = jitterBackoff(delay, 2*delay, s.config.rand())
	seconds := uint((delay + time.Second - 1) / time.Second)
	if err := s.rsmqClient.RequeueMessage(s.name, msg.ID, msg.Receipt, seconds); err != nil && !isSuperseded(err) {
		s.config.logger.Ctx(ctx).Warn("failed to requeue task",
			zap.Error(err),
			zap.String("queue", s.name),
			zap.String("message_id", msg.ID))
	}
}

// jitterBackoff scales d by a factor in [0.75, 1.25) chosen by r in [0, 1), so
// tasks that failed together do not all come back at the same moment. At the
// cap only the lower half applies, so capped tasks still spread. The result is
// whole seconds (the queue's resolution) and at least 1s.
func jitterBackoff(d, maxBackoff time.Duration, r float64) time.Duration {
	if d <= 0 {
		return 0
	}
	factor := 0.75 + 0.5*r
	if d >= maxBackoff {
		factor = 0.75 + 0.25*r
	}
	jittered := time.Duration(float64(d) * factor).Round(time.Second)
	return max(min(jittered, maxBackoff), time.Second)
}

func calculateExecBackoff(base time.Duration, receiveCount uint64, maxBackoff time.Duration) time.Duration {
	if base <= 0 || maxBackoff <= 0 {
		return 0
	}

	backoff := min(base, maxBackoff)
	for i := uint64(1); i < receiveCount && backoff < maxBackoff; i++ {
		if backoff >= maxBackoff-backoff {
			return maxBackoff
		}
		backoff *= 2
	}
	return backoff
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
