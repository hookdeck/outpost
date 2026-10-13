package sut

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// EnvSpec carries the worker's Spec (JSON) from the driver.
const EnvSpec = "MQCHECK_WORKER_SPEC"

// EventFD is the file descriptor the worker writes its event stream to.
const EventFD = 3

// Spec tells a worker process what to run.
type Spec struct {
	Provider string      `json:"provider"`
	Worker   string      `json:"worker"`
	Handler  HandlerSpec `json:"handler"`
	// VisibilityTimeout of the queue; the exceed-once directive runs 1.5 x
	// this long.
	VisibilityTimeout time.Duration `json:"visibility_timeout"`
	SampleEvery       time.Duration `json:"sample_every"`
}

// HandlerSpec is the synthetic handler's default behavior.
type HandlerSpec struct {
	// Latency is the mean time a handler runs; Jitter spreads it uniformly
	// over Latency ± Jitter.
	Latency time.Duration `json:"latency"`
	Jitter  time.Duration `json:"jitter"`
}

// Main runs a worker process and returns its exit code: 0 after a graceful
// shutdown, 1 when the consumer failed or could not start.
func Main() int {
	var spec Spec
	if err := json.Unmarshal([]byte(os.Getenv(EnvSpec)), &spec); err != nil {
		fmt.Fprintf(os.Stderr, "mqcheck worker: bad %s: %v\n", EnvSpec, err)
		return 2
	}
	if spec.SampleEvery <= 0 {
		spec.SampleEvery = 100 * time.Millisecond
	}
	em := NewEmitter(os.NewFile(EventFD, "events"))
	defer em.Close()

	w := &workerProc{spec: spec, em: em, rec: newRecorder()}
	if reg, ok := provider.Lookup(spec.Provider); ok {
		w.attempt = reg.Attempt
	}
	err := w.run()
	exit := Event{K: KindExit, T: time.Now().UnixNano(), PeakRSS: peakRSS()}
	exit.Received, exit.Started, exit.Unstarted = w.rec.summary()
	if err != nil {
		exit.Err = err.Error()
	}
	em.Emit(exit)
	if err != nil {
		return 1
	}
	return 0
}

type workerProc struct {
	spec      Spec
	em        *Emitter
	rec       *recorder
	attempt   func(any) int
	errorLogs atomic.Int64
}

func (w *workerProc) run() error {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	cfg, err := provider.ParseOutpostConfig(env)
	if err != nil {
		return err
	}
	level := cfg.LogLevel
	if level == "" {
		level = "warn"
	}
	logger, err := logging.NewLogger(logging.WithLogLevel(level))
	if err != nil {
		return err
	}
	logger.Logger = logger.Logger.WithOptions(zap.Hooks(func(e zapcore.Entry) error {
		if e.Level >= zapcore.ErrorLevel {
			w.errorLogs.Add(1)
		}
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Same steps as the service builder's delivery queue setup.
	qc, err := cfg.MQs.ToQueueConfig(ctx, "deliverymq")
	if err != nil {
		return fmt.Errorf("delivery queue config: %w", err)
	}
	if qc == nil {
		return errors.New("no message queue configured")
	}
	dmq := deliverymq.New(deliverymq.WithQueue(qc))
	cleanup, err := dmq.Init(ctx)
	if err != nil {
		logger.Error("delivery MQ initialization failed", zap.Error(err))
		return fmt.Errorf("delivery queue init: %w", err)
	}
	defer cleanup()

	subscribe := func(ctx context.Context, opts ...mqs.SubscribeOption) (mqs.Subscription, error) {
		sub, err := dmq.Subscribe(ctx, opts...)
		if err != nil {
			return nil, err
		}
		w.em.Emit(Event{K: KindReady, T: time.Now().UnixNano()})
		return wrapSubscription(sub, w.rec), nil
	}
	h := &handler{w: w, seen: map[string]int{}, rnd: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))}
	cw, regOpts := services.NewDeliveryConsumerWorker(cfg, subscribe, h, logger)
	sup := worker.NewWorkerSupervisor(logger)
	sup.Register(cw, regOpts...)

	// First sample before anything is received: the idle baseline.
	w.emitSample(sup)
	stopSampler := w.sample(sup)
	defer stopSampler()

	// Same shape as the server's run loop: SIGTERM cancels the context and
	// waits for the supervisor.
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGINT, syscall.SIGTERM)
	errc := make(chan error, 1)
	go func() { errc <- sup.Run(ctx) }()
	select {
	case <-term:
		cancel()
		if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	case err := <-errc:
		if err == nil {
			err = errors.New("workers exited")
		}
		return err
	}
}

func (w *workerProc) sample(sup *worker.WorkerSupervisor) func() {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(w.spec.SampleEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				w.emitSample(sup)
				return
			case <-t.C:
				w.emitSample(sup)
			}
		}
	}()
	return func() { close(done); <-finished }
}

func (w *workerProc) emitSample(sup *worker.WorkerSupervisor) {
	ev := w.rec.sample()
	ev.K = KindSample
	ev.T = time.Now().UnixNano()
	ev.RSS = rss()
	ev.CPUNanos = cpuNanos()
	ev.ErrorLogs = w.errorLogs.Load()
	ev.WorkerFailed = !sup.GetHealthTracker().IsHealthy()
	w.em.Emit(ev)
}

// handler is the synthetic delivery logic: decode the task, run for the
// configured time, ack or nack as the message's directive says.
type handler struct {
	w    *workerProc
	mu   sync.Mutex
	seen map[string]int
	rnd  *rand.Rand
}

func (h *handler) Handle(ctx context.Context, msg *mqs.Message) error {
	recv := h.w.rec.start(msg)
	size := len(msg.Body)
	start := time.Now()

	var task models.DeliveryTask
	if err := json.Unmarshal(msg.Body, &task); err != nil {
		msg.Nack()
		h.w.rec.end(size)
		return fmt.Errorf("decode delivery task: %w", err)
	}
	id := task.Event.ID
	pub, _ := strconv.ParseInt(task.Event.Metadata[payload.MetaPublished], 10, 64)
	att := 0
	if h.w.attempt != nil {
		att = h.w.attempt(msg.QueueMessage)
	}
	h.w.em.Emit(Event{K: KindStart, T: start.UnixNano(), ID: id, MID: msg.ID, Attempt: att, Size: size, Recv: recv, Pub: pub})

	h.mu.Lock()
	h.seen[id]++
	first := h.seen[id] == 1
	d := h.w.spec.Handler.Latency
	if j := h.w.spec.Handler.Jitter; j > 0 {
		d += time.Duration(h.rnd.Int64N(int64(2*j))) - j
	}
	h.mu.Unlock()

	outcome := OutcomeAck
	switch task.Event.Metadata[payload.MetaDirective] {
	case payload.DoNackOnce:
		if first {
			outcome = OutcomeNack
		}
	case payload.DoNackAlways:
		outcome = OutcomeNack
	case payload.DoExceedOnce:
		if first {
			d = h.w.spec.VisibilityTimeout * 3 / 2
		}
	}
	if d > 0 {
		time.Sleep(d)
	}
	runtime.KeepAlive(msg.Body)
	if outcome == OutcomeAck {
		msg.Ack()
	} else {
		msg.Nack()
	}
	h.w.rec.end(size)
	h.w.em.Emit(Event{K: KindEnd, T: time.Now().UnixNano(), ID: id, Outcome: outcome})
	return nil
}
