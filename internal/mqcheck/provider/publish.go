package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mqs"
	"golang.org/x/sync/errgroup"
)

// OutpostPublisher publishes through Outpost's own queue client, configured
// from a target's WorkerEnv, so messages reach the broker exactly as Outpost
// publishes them. Providers embed it to implement Publish.
type OutpostPublisher struct {
	// Env returns the Outpost environment for a target (the provider's
	// WorkerEnv).
	Env func(t *Target) map[string]string
	// Concurrency is the number of Publish calls in flight; default 64.
	Concurrency int

	mu       sync.Mutex
	queues   map[string]mqs.Queue
	cleanups []func()
}

type rawMessage []byte

func (m rawMessage) ToMessage() (*mqs.Message, error) { return &mqs.Message{Body: m}, nil }
func (m rawMessage) FromMessage(msg *mqs.Message) error {
	return fmt.Errorf("rawMessage is publish-only")
}

// Publish sends bodies to t.
func (p *OutpostPublisher) Publish(ctx context.Context, t *Target, bodies [][]byte) error {
	q, err := p.queue(ctx, t)
	if err != nil {
		return err
	}
	conc := p.Concurrency
	if conc <= 0 {
		conc = 64
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(conc)
	for _, b := range bodies {
		g.Go(func() error { return q.Publish(gctx, rawMessage(b)) })
	}
	return g.Wait()
}

// Close shuts the publishing clients down.
func (p *OutpostPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.cleanups {
		c()
	}
	p.cleanups = nil
	p.queues = nil
}

func (p *OutpostPublisher) queue(ctx context.Context, t *Target) (mqs.Queue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if q, ok := p.queues[t.Spec.Name]; ok {
		return q, nil
	}
	qc, err := OutpostQueueConfig(ctx, p.Env(t))
	if err != nil {
		return nil, err
	}
	q := mqs.NewQueue(qc)
	cleanup, err := q.Init(ctx)
	if err != nil {
		return nil, fmt.Errorf("init publisher: %w", err)
	}
	if p.queues == nil {
		p.queues = map[string]mqs.Queue{}
	}
	p.queues[t.Spec.Name] = q
	p.cleanups = append(p.cleanups, cleanup)
	return q, nil
}

// OutpostQueueConfig returns the delivery queue's config from Outpost's
// configuration in env.
func OutpostQueueConfig(ctx context.Context, env map[string]string) (*mqs.QueueConfig, error) {
	cfg, err := ParseOutpostConfig(env)
	if err != nil {
		return nil, err
	}
	qc, err := cfg.MQs.ToQueueConfig(ctx, "deliverymq")
	if err != nil {
		return nil, err
	}
	if qc == nil {
		return nil, fmt.Errorf("no message queue configured by the provider's worker env")
	}
	return qc, nil
}

// ParseOutpostConfig parses Outpost's configuration from env alone: no config
// file, no process environment.
func ParseOutpostConfig(env map[string]string) (*config.Config, error) {
	cfg, err := config.ParseWithoutValidation(config.Flags{}, mapOS(env))
	if err != nil {
		return nil, fmt.Errorf("parse outpost config: %w", err)
	}
	return cfg, nil
}

type mapOS map[string]string

func (m mapOS) Getenv(k string) string                { return m[k] }
func (m mapOS) LookupEnv(k string) (string, bool)     { v, ok := m[k]; return v, ok }
func (m mapOS) Stat(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
func (m mapOS) ReadFile(name string) ([]byte, error)  { return nil, os.ErrNotExist }
func (m mapOS) Environ() []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

var _ config.OSInterface = mapOS(nil)

// EnvList renders env as KEY=VALUE pairs.
func EnvList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// SanitizeName lowercases s and replaces characters brokers commonly reject.
func SanitizeName(s string) string {
	s = strings.ToLower(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
}
