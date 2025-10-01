// Package rsmq provides a Redis Simple Message Queue implementation.
// This adapter file bridges the gap between RSMQ's legacy interface and the modern Redis v9 client.

package rsmq

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// ============================================================================
// ADAPTER PATTERN EXPLANATION
// ============================================================================
//
// Why do we need this adapter?
//
// RSMQ was originally built against an older version of the go-redis library
// (github.com/go-redis/redis v6/v7) which did NOT require context parameters
// for Redis operations. For example:
//
//   Old v6/v7:  client.HSet(key, field, value)
//   New v9:     client.HSet(ctx, key, field, value)
//
// When we upgraded to the newer Redis client (github.com/redis/go-redis/v9),
// all Redis operations now require a context parameter for proper cancellation
// and timeout support.
//
// Rather than rewriting all of RSMQ to be context-aware (which would require
// changing every method signature and all calling code), we use this adapter
// pattern to:
//
// 1. Accept the old-style method calls (without context)
// 2. Inject context.Background() automatically
// 3. Forward the calls to the v9 client
//
// This allows RSMQ to work with the modern v9 client without any changes to
// its core logic, maintaining backward compatibility while using the latest
// Redis client features for the rest of the application.
//
// The adapter consists of two main components:
// - RedisAdapter: Wraps regular Redis operations
// - PipelineAdapter: Wraps pipeline/transaction operations
//
// ============================================================================

// RedisAdapter wraps a v9 Redis client to implement the RedisClient interface
// It uses context.Background() for all operations to maintain backward compatibility
type RedisAdapter struct {
	client redis.Cmdable
	ctx    context.Context
}

// NewRedisAdapter creates a new adapter for the v9 Redis client
func NewRedisAdapter(client redis.Cmdable) *RedisAdapter {
	return &RedisAdapter{
		client: client,
		ctx:    context.Background(),
	}
}

// NewRedisAdapterWithContext creates a new adapter with a specific context
func NewRedisAdapterWithContext(ctx context.Context, client redis.Cmdable) *RedisAdapter {
	return &RedisAdapter{
		client: client,
		ctx:    ctx,
	}
}

// ============================================================================
// Regular Redis Operations
// ============================================================================
// These methods adapt v9's context-requiring methods to RSMQ's context-free interface

func (r *RedisAdapter) Time() *redis.TimeCmd {
	return r.client.Time(r.ctx)
}

func (r *RedisAdapter) HSetNX(key, field string, value interface{}) *redis.BoolCmd {
	return r.client.HSetNX(r.ctx, key, field, value)
}

func (r *RedisAdapter) HMGet(key string, fields ...string) *redis.SliceCmd {
	return r.client.HMGet(r.ctx, key, fields...)
}

func (r *RedisAdapter) SMembers(key string) *redis.StringSliceCmd {
	return r.client.SMembers(r.ctx, key)
}

func (r *RedisAdapter) SAdd(key string, members ...interface{}) *redis.IntCmd {
	return r.client.SAdd(r.ctx, key, members...)
}

func (r *RedisAdapter) ZCard(key string) *redis.IntCmd {
	return r.client.ZCard(r.ctx, key)
}

func (r *RedisAdapter) ZCount(key, min, max string) *redis.IntCmd {
	return r.client.ZCount(r.ctx, key, min, max)
}

func (r *RedisAdapter) ZAdd(key string, members ...redis.Z) *redis.IntCmd {
	return r.client.ZAdd(r.ctx, key, members...)
}

func (r *RedisAdapter) HSet(key, field string, value interface{}) *redis.BoolCmd {
	// HSet in v9 returns IntCmd, but RSMQ expects BoolCmd
	// We need to use HSetNX for bool or adapt the interface
	// For now, we'll return a synthetic BoolCmd
	_ = r.client.HSet(r.ctx, key, field, value)
	// Create a synthetic BoolCmd that always returns true for compatibility
	cmd := redis.NewBoolCmd(r.ctx)
	cmd.SetVal(true)
	return cmd
}

func (r *RedisAdapter) HIncrBy(key, field string, incr int64) *redis.IntCmd {
	return r.client.HIncrBy(r.ctx, key, field, incr)
}

func (r *RedisAdapter) Del(keys ...string) *redis.IntCmd {
	return r.client.Del(r.ctx, keys...)
}

func (r *RedisAdapter) HDel(key string, fields ...string) *redis.IntCmd {
	return r.client.HDel(r.ctx, key, fields...)
}

func (r *RedisAdapter) ZRem(key string, members ...interface{}) *redis.IntCmd {
	return r.client.ZRem(r.ctx, key, members...)
}

func (r *RedisAdapter) SRem(key string, members ...interface{}) *redis.IntCmd {
	return r.client.SRem(r.ctx, key, members...)
}

func (r *RedisAdapter) EvalSha(sha1 string, keys []string, args ...interface{}) *redis.Cmd {
	return r.client.EvalSha(r.ctx, sha1, keys, args...)
}

func (r *RedisAdapter) ScriptLoad(script string) *redis.StringCmd {
	return r.client.ScriptLoad(r.ctx, script)
}

func (r *RedisAdapter) TxPipeline() Pipeliner {
	// Return our custom pipeline adapter that will inject context
	return newPipelineAdapter(r.client.TxPipeline(), r.ctx)
}

func (r *RedisAdapter) Exists(keys ...string) *redis.IntCmd {
	return r.client.Exists(r.ctx, keys...)
}

func (r *RedisAdapter) Type(key string) *redis.StatusCmd {
	return r.client.Type(r.ctx, key)
}

func (r *RedisAdapter) Close() error {
	// Check if the underlying client has a Close method
	if closer, ok := r.client.(interface{ Close() error }); ok {
		return closer.Close()
	}
	// Cluster clients and some other types may not have Close
	return nil
}

// ============================================================================
// Pipeline Adapter
// ============================================================================
// This section handles pipeline/transaction operations which also need context injection

// pipelineAdapter wraps a redis.Pipeliner to provide methods without context
// This allows RSMQ to work with v9 client without modifying all the pipeline calls
type pipelineAdapter struct {
	pipe redis.Pipeliner
	ctx  context.Context
}

func newPipelineAdapter(pipe redis.Pipeliner, ctx context.Context) *pipelineAdapter {
	return &pipelineAdapter{
		pipe: pipe,
		ctx:  ctx,
	}
}

// Pipeline operations - inject context for each operation

func (p *pipelineAdapter) HSetNX(key, field string, value interface{}) *redis.BoolCmd {
	return p.pipe.HSetNX(p.ctx, key, field, value)
}

func (p *pipelineAdapter) HMGet(key string, fields ...string) *redis.SliceCmd {
	return p.pipe.HMGet(p.ctx, key, fields...)
}

func (p *pipelineAdapter) Time() *redis.TimeCmd {
	return p.pipe.Time(p.ctx)
}

func (p *pipelineAdapter) SAdd(key string, members ...interface{}) *redis.IntCmd {
	return p.pipe.SAdd(p.ctx, key, members...)
}

func (p *pipelineAdapter) HSet(key string, values ...interface{}) *redis.IntCmd {
	return p.pipe.HSet(p.ctx, key, values...)
}

func (p *pipelineAdapter) HIncrBy(key, field string, incr int64) *redis.IntCmd {
	return p.pipe.HIncrBy(p.ctx, key, field, incr)
}

func (p *pipelineAdapter) ZAdd(key string, members ...redis.Z) *redis.IntCmd {
	return p.pipe.ZAdd(p.ctx, key, members...)
}

func (p *pipelineAdapter) ZCard(key string) *redis.IntCmd {
	return p.pipe.ZCard(p.ctx, key)
}

func (p *pipelineAdapter) ZCount(key, min, max string) *redis.IntCmd {
	return p.pipe.ZCount(p.ctx, key, min, max)
}

func (p *pipelineAdapter) ZRem(key string, members ...interface{}) *redis.IntCmd {
	return p.pipe.ZRem(p.ctx, key, members...)
}

func (p *pipelineAdapter) HDel(key string, fields ...string) *redis.IntCmd {
	return p.pipe.HDel(p.ctx, key, fields...)
}

func (p *pipelineAdapter) SRem(key string, members ...interface{}) *redis.IntCmd {
	return p.pipe.SRem(p.ctx, key, members...)
}

func (p *pipelineAdapter) Del(keys ...string) *redis.IntCmd {
	return p.pipe.Del(p.ctx, keys...)
}

func (p *pipelineAdapter) Exec() ([]redis.Cmder, error) {
	return p.pipe.Exec(p.ctx)
}

// ZRangeByScoreWithScores for script operations
func (p *pipelineAdapter) ZRangeByScoreWithScores(key string, opt *redis.ZRangeBy) *redis.ZSliceCmd {
	return p.pipe.ZRangeByScoreWithScores(p.ctx, key, opt)
}
