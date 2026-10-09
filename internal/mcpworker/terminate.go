package mcpworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"go.uber.org/zap"
)

// Termination reconciliation. It runs only on an instance whose topic
// configuration is the applied one, so instances on an older or
// rolled-back configuration never end subscriptions:
//   - topics indexed for mcp that are MCP-enabled neither here nor in the
//     applied configuration have ended: every subscription to them is
//     deleted and sent a terminated envelope (NotFound {kind: "event"});
//   - topics with broken schema hashes (forced breaking changes) are scanned
//     with ZSCAN, the cursor kept in StateKey across passes, and
//     subscriptions whose config.schema_hash is broken are ended the same
//     way (Unsupported {feature: "payloadSchema", reason:
//     "schema_changed"}). A topic is scanned again whenever a scan may have
//     missed some: while instances on an older configuration run. Broken
//     hashes retire MCP_TTL_MAX after they were recorded and after an older
//     configuration was last seen running, once a full scan started after
//     both.

const (
	stateOldConfigSeenAt = "old_config_seen_at"
	stateScanPrefix      = "scan:"
)

// scanState is the progress of the broken-hash scan of one topic.
type scanState struct {
	// Fingerprint of the broken hashes being looked for; a change restarts
	// the scan.
	FP     string `json:"fp"`
	Cursor uint64 `json:"cursor"`
	// StartedAt is when the scan in progress started (Unix ms), 0 when none
	// is.
	StartedAt int64 `json:"started_at"`
	// Dirty: the scan in progress removed entries. A store whose cursor is
	// an offset (miniredis, the in-memory store) may then skip some, so
	// only a scan that removed nothing counts as complete.
	Dirty bool `json:"dirty,omitempty"`
	// CompletedAt is when the last complete scan started (Unix ms).
	CompletedAt int64 `json:"completed_at"`
}

type terminationState struct {
	oldConfigSeenAt int64
	scans           map[string]scanState
}

func (p *pass) reconcile(ctx context.Context) {
	cfg := p.w.cfg
	applied, err := topicschema.ReadApplied(ctx, cfg.Redis, cfg.DeploymentID)
	if err != nil {
		p.warn(ctx, "reading the applied topic configuration failed", err)
		return
	}
	if applied == nil {
		return
	}
	if applied.Hash != p.w.localHash {
		p.stats.terminationPaused.Store(true)
		return
	}

	budget := cfg.PassBudget
	topics, err := cfg.Store.ListIndexedTopics(ctx, models.DestinationTypeMCP)
	if err != nil {
		p.warn(ctx, "listing indexed topics failed", err)
	}
	slices.Sort(topics)
	for _, topic := range topics {
		if budget <= 0 || ctx.Err() != nil {
			return
		}
		if cfg.Snapshot.MCPServed(topic) || applied.Snapshot.MCPServed(topic) {
			continue
		}
		budget = p.drain(ctx, topic, tenantstore.NoExpiryScore, budget, func(ctx context.Context, ref tenantstore.IndexedDestination) bool {
			return p.terminate(ctx, ref, topic, nil)
		})
	}
	p.terminateBroken(ctx, applied, budget)
}

// terminate ends the subscription of ref, an entry of topic's index: every
// one when brokenHashes is nil (the topic ended), else those whose
// schema_hash is broken. It reports whether the entry is gone.
func (p *pass) terminate(ctx context.Context, ref tenantstore.IndexedDestination, topic string, brokenHashes []string) bool {
	store := p.w.cfg.Store
	d, err := store.RetrieveDestination(ctx, ref.TenantID, ref.DestinationID)
	if errors.Is(err, tenantstore.ErrDestinationDeleted) || (err == nil && d == nil) {
		return p.removeIndexed(ctx, []string{topic}, ref)
	}
	if err != nil {
		p.fail(ctx, "retrieve", ref, err)
		return false
	}
	if d.Type != models.DestinationTypeMCP {
		return p.removeIndexed(ctx, []string{topic}, ref)
	}
	if !slices.Contains(d.Topics, topic) {
		// Not a subscription to this topic (its ID is derived from it, so
		// this shouldn't happen). Removing the entry would also drop the
		// destination's global entry, so keep it.
		return false
	}
	if d.IsExpired(p.now) {
		// Already over for its client: the expiry sweep deletes it and
		// reports it as expired.
		return false
	}
	reason := mcpevents.EventEnded
	if brokenHashes != nil {
		if !slices.Contains(brokenHashes, d.Config["schema_hash"]) {
			return false
		}
		reason = mcpevents.SchemaChanged
	}

	tenant := p.beforeDelete(ctx, ref.TenantID)
	createdAt := d.CreatedAt
	res, err := store.DeleteDestinationIf(ctx, ref.TenantID, ref.DestinationID, tenantstore.DeleteCondition{
		Type:              models.DestinationTypeMCP,
		ExpectedCreatedAt: &createdAt,
		Reason:            tenantstore.DeleteReasonTerminated,
	})
	if err != nil {
		p.fail(ctx, "delete", ref, err)
		return false
	}
	switch {
	case res.Deleted:
		tenant.markChanged()
		if brokenHashes != nil {
			p.stats.schemaChanged.Add(1)
		} else {
			p.stats.ended.Add(1)
		}
		if res.Score() != ref.Score {
			p.removeIndexed(ctx, append(res.Topics, topic), ref)
		}
		e := reason()
		p.w.logger.Audit("mcp subscription terminated",
			zap.String("tenant_id", ref.TenantID),
			zap.String("destination_id", ref.DestinationID),
			zap.String("topic", topic),
			zap.String("reason", string(e.Kind)))
		p.notify(d, e)
		return true
	case res.Live:
		// Replaced by a newer generation meanwhile: the next pass reads it.
		return false
	default:
		return p.removeIndexed(ctx, append(res.Topics, topic), ref)
	}
}

// notify queues the terminated envelope of d, which was just deleted. It
// may wait for room past the pass deadline, but not past shutdown.
func (p *pass) notify(d *models.Destination, e *mcpevents.Error) {
	ctx, cancel := context.WithTimeout(p.runCtx, p.w.notifyWait)
	defer cancel()
	err := p.w.cfg.Notifier.EnqueueWait(ctx, mcpevents.Termination{
		TenantID:       d.TenantID,
		SubscriptionID: d.ID,
		URL:            d.Config["url"],
		Secrets:        p.w.cfg.Secrets(d),
		CreatedAt:      d.CreatedAt,
		Error:          e,
	})
	if err != nil {
		p.stats.notifyFailed.Add(1)
		p.w.logger.Warn("mcp subscriptions worker: terminated envelope not sent",
			zap.String("tenant_id", d.TenantID),
			zap.String("destination_id", d.ID),
			zap.Error(err))
	}
}

// terminateBroken scans the topics with broken schema hashes and retires
// the hashes that can't match anything anymore.
func (p *pass) terminateBroken(ctx context.Context, applied *topicschema.Applied, budget int) {
	cfg := p.w.cfg
	key := StateKey(cfg.DeploymentID)
	state, err := p.loadState(ctx)
	if err != nil {
		p.warn(ctx, "reading the termination state failed", err)
		return
	}
	// Drop the progress of topics without broken hashes anymore.
	var stale []string
	for topic := range state.scans {
		if len(applied.Broken[topic]) == 0 {
			stale = append(stale, stateScanPrefix+topic)
		}
	}
	if len(stale) > 0 {
		if err := cfg.Redis.HDel(ctx, key, stale...).Err(); err != nil {
			p.warn(ctx, "cleaning the termination state failed", err)
		}
	}
	if len(applied.Broken) == 0 {
		return
	}

	nowMs := p.now.UnixMilli()
	olderLive, err := p.olderConfigurationsLive(ctx, applied)
	if err != nil {
		p.warn(ctx, "checking for older running configurations failed", err)
		olderLive = true // retire nothing this pass
	} else if olderLive {
		state.oldConfigSeenAt = nowMs
		if err := cfg.Redis.HSet(ctx, key, stateOldConfigSeenAt, nowMs).Err(); err != nil {
			p.warn(ctx, "saving the termination state failed", err)
		}
	}

	for _, topic := range slices.Sorted(maps.Keys(applied.Broken)) {
		hashes := applied.BrokenHashes(topic)
		st := state.scans[topic]
		if fp := fingerprint(hashes); st.FP != fp {
			st = scanState{FP: fp}
		}
		since := max(latestRecordedAt(applied.Broken[topic]), state.oldConfigSeenAt)
		if st.StartedAt == 0 && st.CompletedAt <= since && budget > 0 && ctx.Err() == nil {
			st.StartedAt, st.Cursor, st.Dirty = nowMs, 0, false
		}
		for st.StartedAt != 0 && budget > 0 && ctx.Err() == nil {
			refs, next, err := cfg.Store.ScanIndexedDestinations(ctx, models.DestinationTypeMCP, topic, st.Cursor, min(cfg.BatchSize, budget))
			if err != nil {
				p.warn(ctx, "scanning the index of "+topic+" failed", err)
				break
			}
			removed := p.each(ctx, refs, func(ctx context.Context, ref tenantstore.IndexedDestination) bool {
				return p.terminate(ctx, ref, topic, hashes)
			})
			if ctx.Err() != nil {
				break // the batch may be unfinished: redo it next pass
			}
			budget -= max(len(refs), 1)
			st.Cursor = next
			st.Dirty = st.Dirty || removed > 0
			if next == 0 {
				if !st.Dirty {
					st.CompletedAt = st.StartedAt
				}
				st.StartedAt, st.Dirty = 0, false
			}
			p.saveScan(ctx, topic, st)
		}
		state.scans[topic] = st
	}

	if !olderLive && ctx.Err() == nil {
		p.retireBroken(ctx, applied, state)
	}
}

// retireBroken retires the broken hashes recorded, and last possibly used
// by a running instance, more than MCP_TTL_MAX ago, and covered by a
// complete scan started after both.
func (p *pass) retireBroken(ctx context.Context, applied *topicschema.Applied, state terminationState) {
	cfg := p.w.cfg
	nowMs := p.now.UnixMilli()
	retire := make(map[string][]string)
	emptied := []string{}
	for topic, entries := range applied.Broken {
		st := state.scans[topic]
		if st.FP != fingerprint(applied.BrokenHashes(topic)) {
			continue
		}
		for _, e := range entries {
			since := max(e.RecordedAt.UnixMilli(), state.oldConfigSeenAt)
			if nowMs-since >= cfg.TTLMax.Milliseconds() && st.CompletedAt > since {
				retire[topic] = append(retire[topic], e.Hash)
			}
		}
		if len(retire[topic]) == len(entries) {
			emptied = append(emptied, stateScanPrefix+topic)
		}
	}
	if len(retire) == 0 {
		return
	}
	ok, err := topicschema.RetireBroken(ctx, cfg.Redis, cfg.DeploymentID, applied, retire)
	if err != nil {
		p.warn(ctx, "retiring broken schema hashes failed", err)
		return
	}
	if !ok {
		return // the applied configuration changed: next pass
	}
	p.w.logger.Info("mcp subscriptions worker: retired broken schema hashes", zap.Any("hashes", retire))
	if len(emptied) > 0 {
		if err := cfg.Redis.HDel(ctx, StateKey(cfg.DeploymentID), emptied...).Err(); err != nil {
			p.warn(ctx, "cleaning the termination state failed", err)
		}
	}
}

// olderConfigurationsLive reports whether an instance runs a configuration
// other than the applied one: one of the recently applied hashes still
// reported live.
func (p *pass) olderConfigurationsLive(ctx context.Context, applied *topicschema.Applied) (bool, error) {
	cfg := p.w.cfg
	history, err := topicschema.ReadHistory(ctx, cfg.Redis, cfg.DeploymentID)
	if err != nil {
		return false, err
	}
	var hashes []string
	for _, h := range history {
		if h.Hash != applied.Hash && !slices.Contains(hashes, h.Hash) {
			hashes = append(hashes, h.Hash)
		}
	}
	live, err := topicschema.LiveHashes(ctx, cfg.Redis, cfg.DeploymentID, hashes)
	return len(live) > 0, err
}

func (p *pass) loadState(ctx context.Context) (terminationState, error) {
	fields, err := p.w.cfg.Redis.HGetAll(ctx, StateKey(p.w.cfg.DeploymentID)).Result()
	if err != nil {
		return terminationState{}, err
	}
	state := terminationState{scans: make(map[string]scanState)}
	for field, value := range fields {
		if field == stateOldConfigSeenAt {
			state.oldConfigSeenAt, _ = strconv.ParseInt(value, 10, 64)
			continue
		}
		if topic, ok := strings.CutPrefix(field, stateScanPrefix); ok {
			var st scanState
			if json.Unmarshal([]byte(value), &st) == nil {
				state.scans[topic] = st
			} else {
				state.scans[topic] = scanState{} // restart it
			}
		}
	}
	return state, nil
}

func (p *pass) saveScan(ctx context.Context, topic string, st scanState) {
	b, _ := json.Marshal(st)
	if err := p.w.cfg.Redis.HSet(ctx, StateKey(p.w.cfg.DeploymentID), stateScanPrefix+topic, b).Err(); err != nil {
		p.warn(ctx, "saving the termination state failed", err)
	}
}

func (p *pass) warn(ctx context.Context, msg string, err error) {
	if ctx.Err() != nil {
		return
	}
	p.stats.errors.Add(1)
	p.w.logger.Warn("mcp subscriptions worker: "+msg, zap.Error(err))
}

// fingerprint identifies a set of hashes.
func fingerprint(hashes []string) string {
	sorted := slices.Sorted(slices.Values(hashes))
	sum := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(sum[:8])
}

func latestRecordedAt(entries []topicschema.BrokenHash) int64 {
	var latest int64
	for _, e := range entries {
		latest = max(latest, e.RecordedAt.UnixMilli())
	}
	return latest
}

// DestinationSecrets returns the signing secrets of an mcp destination:
// credentials.secret and, until previous_secret_invalid_at (RFC 3339),
// previous_secret. Invalid values are left out.
func DestinationSecrets(d *models.Destination) []mcpevents.Secret {
	var out []mcpevents.Secret
	if key, err := mcpevents.DecodeSecret(d.Credentials["secret"]); err == nil {
		out = append(out, mcpevents.Secret{Key: key})
	}
	if prev := d.Credentials["previous_secret"]; prev != "" {
		invalidAt, err := time.Parse(time.RFC3339Nano, d.Credentials["previous_secret_invalid_at"])
		key, keyErr := mcpevents.DecodeSecret(prev)
		if err == nil && keyErr == nil {
			out = append(out, mcpevents.Secret{Key: key, InvalidAt: &invalidAt})
		}
	}
	return out
}
