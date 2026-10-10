package mcpworker

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
)

// sweep deletes the subscriptions expired before now minus the grace
// period, reading the global index from its lowest scores: every entry it
// handles leaves that range (deleted, removed or rescored to its real
// expiry), so it always reads from the start.
func (p *pass) sweep(ctx context.Context) {
	cutoff := p.now.Add(-p.w.cfg.Grace)
	p.drain(ctx, "", cutoff.UnixMilli(), p.w.cfg.PassBudget, func(ctx context.Context, ref tenantstore.IndexedDestination) bool {
		return p.expire(ctx, ref, cutoff)
	})
}

// drain lists the lowest-scored entries of an index (topic "" for the
// global one) up to maxScore again and again, and hands those it hasn't
// seen yet to fn, which reports whether the entry left the range. Entries
// that stay, after a failure, are listed past rather than retried. It
// stops when budget is spent, ctx is done or nothing new comes up, and
// returns the budget left.
func (p *pass) drain(ctx context.Context, topic string, maxScore int64, budget int, fn func(context.Context, tenantstore.IndexedDestination) bool) int {
	seen := make(map[tenantstore.IndexedDestination]struct{})
	stuck := 0
	for budget > 0 && ctx.Err() == nil {
		batch := min(p.w.cfg.BatchSize, budget)
		refs, err := p.w.cfg.Store.ListIndexedDestinations(ctx, models.DestinationTypeMCP, topic, maxScore, batch+stuck)
		if err != nil {
			if ctx.Err() == nil {
				p.stats.errors.Add(1)
				p.w.logger.Warn("mcp subscriptions worker: index listing failed", zap.String("topic", topic), zap.Error(err))
			}
			return budget
		}
		todo := make([]tenantstore.IndexedDestination, 0, batch)
		for _, ref := range refs {
			key := tenantstore.IndexedDestination{TenantID: ref.TenantID, DestinationID: ref.DestinationID}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			todo = append(todo, ref)
			if len(todo) == batch {
				break
			}
		}
		if len(todo) == 0 {
			return budget
		}
		left := p.each(ctx, todo, fn)
		stuck += len(todo) - left
		budget -= len(todo)
	}
	return budget
}

// expire handles one entry of the expiry range and reports whether it left
// the range.
func (p *pass) expire(ctx context.Context, ref tenantstore.IndexedDestination, cutoff time.Time) bool {
	store := p.w.cfg.Store
	cutoffMs := cutoff.UnixMilli()

	// The expired event names the subscription, so read it first; the
	// conditional delete below then only deletes that generation.
	var d *models.Destination
	gone := false
	if p.emitExpired {
		got, err := store.RetrieveDestination(ctx, ref.TenantID, ref.DestinationID)
		switch {
		case err == nil:
			// nil when missing: the delete below reports it gone, with
			// the topics of a tombstone.
			d, gone = got, got == nil
		case errors.Is(err, tenantstore.ErrDestinationDeleted):
			gone = true
		default:
			// Delete anyway, with a less detailed event.
			p.fail(ctx, "retrieve", ref, err)
		}
	}
	if d != nil {
		if d.Type != models.DestinationTypeMCP {
			// The entry outlived an mcp destination whose ID was reused.
			// Its topic entries are likely under the same topics; removal
			// compares scores, so other entries are safe.
			return p.removeIndexed(ctx, d.Topics, ref)
		}
		if !d.IsExpired(cutoff) {
			// Refreshed: its entry moves to the new expiry.
			return p.rescore(ctx, d.Topics, ref, tenantstore.IndexScore(d.ExpiresAt), cutoffMs)
		}
	}

	cond := tenantstore.DeleteCondition{
		Type:          models.DestinationTypeMCP,
		ExpiredBefore: &cutoff,
		Reason:        tenantstore.DeleteReasonExpired,
	}
	var tenant *tenantEntry
	if d != nil {
		createdAt := d.CreatedAt
		cond.ExpectedCreatedAt = &createdAt
	}
	if !gone {
		tenant = p.beforeDelete(ctx, ref.TenantID)
	}
	res, err := store.DeleteDestinationIf(ctx, ref.TenantID, ref.DestinationID, cond)
	if err != nil {
		p.fail(ctx, "delete", ref, err)
		return false
	}
	switch {
	case res.Deleted:
		p.stats.expired.Add(1)
		tenant.markChanged()
		// The store removed the entries scored like the destination; this
		// one may be scored otherwise.
		if res.Score() != ref.Score {
			p.removeIndexed(ctx, res.Topics, ref)
		}
		p.w.logger.Audit("mcp subscription expired",
			zap.String("tenant_id", ref.TenantID),
			zap.String("destination_id", ref.DestinationID))
		p.emitExpiredEvent(ctx, ref, d, res)
		return true
	case res.Live:
		if res.Type != models.DestinationTypeMCP {
			return p.removeIndexed(ctx, res.Topics, ref)
		}
		if res.Score() == ref.Score {
			// A newer generation with the same expiry: the next pass reads
			// it.
			return false
		}
		return p.rescore(ctx, res.Topics, ref, res.Score(), cutoffMs)
	default: // gone
		return p.removeIndexed(ctx, res.Topics, ref)
	}
}

// rescore moves ref to score and reports whether it left the expiry range.
func (p *pass) rescore(ctx context.Context, topics []string, ref tenantstore.IndexedDestination, score, cutoffMs int64) bool {
	if err := p.w.cfg.Store.RescoreIndexedDestination(ctx, models.DestinationTypeMCP, topics, ref, score); err != nil {
		p.fail(ctx, "index rescore", ref, err)
		return false
	}
	p.stats.rescored.Add(1)
	return score > cutoffMs
}

func (p *pass) emitExpiredEvent(ctx context.Context, ref tenantstore.IndexedDestination, d *models.Destination, res tenantstore.DeleteResult) {
	if !p.emitExpired {
		return
	}
	var data opevents.MCPSubscriptionExpiredData
	if d != nil {
		data = opevents.NewMCPSubscriptionExpiredData(d)
	} else {
		data = opevents.MCPSubscriptionExpiredData{TenantID: ref.TenantID, SubscriptionID: ref.DestinationID}
		if len(res.Topics) > 0 {
			data.Topic = res.Topics[0]
		}
		if res.ExpiresAtMs > 0 {
			data.ExpiresAt = time.UnixMilli(res.ExpiresAtMs)
		}
	}
	if err := p.w.cfg.Emitter.Emit(ctx, opevents.MCPSubscriptionExpiredEvent(data)); err != nil {
		p.stats.emitFailed.Add(1)
	}
}
