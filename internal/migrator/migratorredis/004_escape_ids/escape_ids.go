// Package migration_004_escape_ids moves tenant store keys to the format that
// escapes tenant and destination IDs.
package migration_004_escape_ids

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rediskey"
	goredis "github.com/redis/go-redis/v9"
)

// EscapeIDsMigration renames the tenant store keys of tenants and destinations
// whose ID contains "%", ":", "{" or "}":
//
//	tenant:{<tenant>}:tenant             -> tenant:{<escaped tenant>}:tenant
//	tenant:{<tenant>}:destinations       -> tenant:{<escaped tenant>}:destinations
//	tenant:{<tenant>}:destination:<dest> -> tenant:{<escaped tenant>}:destination:<escaped dest>
//
// IDs come from the stored records (the tenant's "id" field and the fields of
// its destination list), never from parsing a key. Keys of IDs without those
// characters are the same in both formats and are left alone.
//
// A run stops at the first rename that fails and can be repeated: a rename is
// skipped when the old key is gone, and a tenant's record moves last, so a
// tenant is picked up again until all of its keys have moved.
//
// Deleted destinations are not in the destination list. Their records stay
// under the old key until they expire.
type EscapeIDsMigration struct {
	client    redis.Client
	logger    migratorredis.Logger
	keyPrefix string
}

var _ migratorredis.Migration = (*EscapeIDsMigration)(nil)

// move is one key rename.
type move struct {
	from string
	to   string
}

// New creates the migration. deploymentID is optional.
func New(client redis.Client, logger migratorredis.Logger, deploymentID string) *EscapeIDsMigration {
	keyPrefix := ""
	if deploymentID != "" {
		keyPrefix = deploymentID + ":"
	}
	return &EscapeIDsMigration{client: client, logger: logger, keyPrefix: keyPrefix}
}

func (m *EscapeIDsMigration) Name() string { return "004_escape_ids" }

func (m *EscapeIDsMigration) Version() int { return 4 }

func (m *EscapeIDsMigration) Description() string {
	return "Escape tenant and destination IDs in tenant store keys"
}

// AutoRunnable is false: the migration renames keys.
func (m *EscapeIDsMigration) AutoRunnable() bool { return false }

func (m *EscapeIDsMigration) IsApplicable(ctx context.Context) (bool, string) {
	return true, ""
}

func (m *EscapeIDsMigration) Plan(ctx context.Context) (*migratorredis.Plan, error) {
	m.logger.LogInfo("Scanning tenant records...")
	moves, err := m.collectMoves(ctx)
	if err != nil {
		return nil, err
	}

	if len(moves) == 0 {
		m.logger.LogInfo("No keys need to move")
	} else {
		m.logger.LogInfo(fmt.Sprintf("Found %d keys to move", len(moves)))
	}

	return &migratorredis.Plan{
		MigrationName:  m.Name(),
		Description:    m.Description(),
		Version:        "v4",
		Timestamp:      time.Now(),
		Scope:          map[string]int{"keys_to_move": len(moves)},
		EstimatedItems: len(moves),
		Data:           moves,
	}, nil
}

func (m *EscapeIDsMigration) Apply(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
	state := &migratorredis.State{
		MigrationName: m.Name(),
		Phase:         "applied",
		StartedAt:     time.Now(),
		Progress:      migratorredis.Progress{TotalItems: plan.EstimatedItems},
		Metadata:      make(map[string]interface{}),
	}

	moves, _ := plan.Data.([]move)
	for i, mv := range moves {
		moved, err := m.rename(ctx, mv)
		if err != nil {
			// Stop here: a tenant's record moves after its other keys, and a
			// tenant whose record has moved is not looked at again.
			m.logger.LogError(fmt.Sprintf("Failed to move %s", mv.from), err)
			state.Progress.FailedItems++
			state.Errors = append(state.Errors, fmt.Sprintf("%s: %v", mv.from, err))
			return state, fmt.Errorf("failed to move %s: %w", mv.from, err)
		}
		if moved {
			state.Progress.ProcessedItems++
		} else {
			state.Progress.SkippedItems++
		}
		m.logger.LogProgress(i+1, len(moves), "keys")
	}

	completed := time.Now()
	state.CompletedAt = &completed
	return state, nil
}

func (m *EscapeIDsMigration) Verify(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
	moves, err := m.collectMoves(ctx)
	if err != nil {
		return nil, err
	}

	result := &migratorredis.VerificationResult{
		Valid:     len(moves) == 0,
		ChecksRun: 1,
		Details:   map[string]string{"keys_pending": fmt.Sprintf("%d", len(moves))},
	}
	if result.Valid {
		result.ChecksPassed = 1
	} else {
		result.Issues = append(result.Issues, fmt.Sprintf("%d keys still in the old format", len(moves)))
	}
	return result, nil
}

func (m *EscapeIDsMigration) PlanCleanup(ctx context.Context) (int, error) { return 0, nil }

func (m *EscapeIDsMigration) Cleanup(ctx context.Context, state *migratorredis.State) error {
	m.logger.LogInfo("No cleanup needed: keys are renamed in place")
	return nil
}

func (m *EscapeIDsMigration) oldTenantKey(tenantID string) string {
	return m.keyPrefix + "tenant:{" + tenantID + "}:tenant"
}

func (m *EscapeIDsMigration) newTenantKey(tenantID string) string {
	return m.keyPrefix + "tenant:" + rediskey.Tag(tenantID) + ":tenant"
}

func (m *EscapeIDsMigration) oldSummaryKey(tenantID string) string {
	return m.keyPrefix + "tenant:{" + tenantID + "}:destinations"
}

func (m *EscapeIDsMigration) newSummaryKey(tenantID string) string {
	return m.keyPrefix + "tenant:" + rediskey.Tag(tenantID) + ":destinations"
}

func (m *EscapeIDsMigration) oldDestinationKey(tenantID, destinationID string) string {
	return m.keyPrefix + "tenant:{" + tenantID + "}:destination:" + destinationID
}

func (m *EscapeIDsMigration) newDestinationKey(tenantID, destinationID string) string {
	return m.keyPrefix + "tenant:" + rediskey.Tag(tenantID) + ":destination:" + rediskey.Escape(destinationID)
}

// collectMoves returns the renames still to do, in the order to do them.
func (m *EscapeIDsMigration) collectMoves(ctx context.Context) ([]move, error) {
	tenants, err := m.scanTenants(ctx)
	if err != nil {
		return nil, err
	}

	// Escaping only ever lengthens an ID. Longest first, so a tenant whose ID
	// is the escaped form of another's has moved away before the other takes
	// its keys.
	tenantIDs := make([]string, 0, len(tenants))
	for tenantID, moved := range tenants {
		// A record under its new key only: the tenant's other keys moved before it.
		if moved {
			continue
		}
		tenantIDs = append(tenantIDs, tenantID)
	}
	sortLongestFirst(tenantIDs)

	var moves []move
	const batchSize = 100
	for start := 0; start < len(tenantIDs); start += batchSize {
		batch := tenantIDs[start:min(start+batchSize, len(tenantIDs))]

		// A tenant's destination list is under the old or the new key,
		// depending on how far an earlier run got.
		pipe := m.client.Pipeline()
		oldLists := make([]*goredis.StringSliceCmd, len(batch))
		newLists := make([]*goredis.StringSliceCmd, len(batch))
		for i, tenantID := range batch {
			oldLists[i] = pipe.HKeys(ctx, m.oldSummaryKey(tenantID))
			newLists[i] = pipe.HKeys(ctx, m.newSummaryKey(tenantID))
		}
		if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("failed to read destination lists: %w", err)
		}

		for i, tenantID := range batch {
			destinationIDs := mergeIDs(oldLists[i].Val(), newLists[i].Val())
			pending, err := m.pendingDestinations(ctx, tenantID, destinationIDs)
			if err != nil {
				return nil, err
			}
			for _, destinationID := range pending {
				moves = append(moves, move{from: m.oldDestinationKey(tenantID, destinationID), to: m.newDestinationKey(tenantID, destinationID)})
			}

			if m.oldSummaryKey(tenantID) == m.newSummaryKey(tenantID) {
				continue
			}
			exists, err := m.client.Exists(ctx, m.oldSummaryKey(tenantID)).Result()
			if err != nil {
				return nil, fmt.Errorf("failed to check destination list: %w", err)
			}
			if exists == 1 {
				moves = append(moves, move{from: m.oldSummaryKey(tenantID), to: m.newSummaryKey(tenantID)})
			}
			moves = append(moves, move{from: m.oldTenantKey(tenantID), to: m.newTenantKey(tenantID)})
		}
	}
	return moves, nil
}

// pendingDestinations returns the destination IDs of a tenant whose record is
// still under the old key. The record's own "id" decides: the old key of one
// destination can be the new key of another.
func (m *EscapeIDsMigration) pendingDestinations(ctx context.Context, tenantID string, destinationIDs []string) ([]string, error) {
	var candidates []string
	for _, destinationID := range destinationIDs {
		if m.oldDestinationKey(tenantID, destinationID) != m.newDestinationKey(tenantID, destinationID) {
			candidates = append(candidates, destinationID)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	pipe := m.client.Pipeline()
	ids := make([]*redis.StringCmd, len(candidates))
	for i, destinationID := range candidates {
		ids[i] = pipe.HGet(ctx, m.oldDestinationKey(tenantID, destinationID), "id")
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("failed to read destination records: %w", err)
	}

	var pending []string
	for i, destinationID := range candidates {
		if id, err := ids[i].Result(); err == nil && id == destinationID {
			pending = append(pending, destinationID)
		}
	}
	return pending, nil
}

// scanTenants returns every tenant ID, read from the tenant's record, and
// whether that record is already under its new key.
func (m *EscapeIDsMigration) scanTenants(ctx context.Context) (map[string]bool, error) {
	pattern := m.keyPrefix + "tenant:*:tenant"
	tenants := make(map[string]bool)
	var cursor uint64
	for {
		keys, next, err := m.client.Scan(ctx, cursor, pattern, 1000).Result()
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		if len(keys) > 0 {
			pipe := m.client.Pipeline()
			cmds := make([]*redis.SliceCmd, len(keys))
			for i, key := range keys {
				cmds[i] = pipe.HMGet(ctx, key, "entity", "id")
			}
			// A key of another type fails its own command and is skipped;
			// any other error ends the scan.
			_, _ = pipe.Exec(ctx)

			for i, key := range keys {
				vals, err := cmds[i].Result()
				if err != nil {
					if strings.Contains(err.Error(), "WRONGTYPE") {
						continue
					}
					return nil, fmt.Errorf("failed to read %s: %w", key, err)
				}
				if len(vals) != 2 {
					continue
				}
				entity, _ := vals[0].(string)
				id, _ := vals[1].(string)
				if entity != "tenant" || id == "" {
					continue
				}
				// A record still under the old key means the tenant is not
				// done, whatever else exists. (An ID without special
				// characters has one key for both formats: only its
				// destinations can move.)
				switch key {
				case m.oldTenantKey(id):
					tenants[id] = false
				case m.newTenantKey(id):
					if _, seen := tenants[id]; !seen {
						tenants[id] = true
					}
				}
			}
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}
	return tenants, nil
}

// rename moves one key. It reports false when there was nothing to move.
func (m *EscapeIDsMigration) rename(ctx context.Context, mv move) (bool, error) {
	ok, err := m.client.RenameNX(ctx, mv.from, mv.to).Result()
	switch {
	case err == nil && ok:
		return true, nil
	case err == nil:
		return m.resolveExistingTarget(ctx, mv)
	case strings.Contains(err.Error(), "no such key"):
		return false, nil
	case strings.Contains(err.Error(), "CROSSSLOT"):
		// On Redis Cluster the escaped tenant ID can hash to another slot.
		return m.copyAndDelete(ctx, mv)
	default:
		return false, err
	}
}

// resolveExistingTarget handles a rename whose new key is taken:
//   - by the same record (an earlier run copied it and stopped before deleting
//     the old key): the old key is deleted;
//   - by a deleted record (left under an old-format key that equals this new
//     key): it is replaced;
//   - by anything else: an error, so nothing live is overwritten.
func (m *EscapeIDsMigration) resolveExistingTarget(ctx context.Context, mv move) (bool, error) {
	source, err := m.client.HGetAll(ctx, mv.from).Result()
	if err != nil {
		return false, err
	}
	if len(source) == 0 {
		return false, nil
	}
	target, err := m.client.HGetAll(ctx, mv.to).Result()
	if err != nil {
		return false, err
	}

	if sameFields(source, target) {
		return true, m.client.Del(ctx, mv.from).Err()
	}
	if _, deleted := target["deleted_at"]; deleted {
		if err := m.client.Del(ctx, mv.to).Err(); err != nil {
			return false, err
		}
		return m.rename(ctx, mv)
	}
	return false, fmt.Errorf("target key %s already holds another record", mv.to)
}

// copyAndDelete moves a hash between Redis Cluster slots, keeping its expiry.
func (m *EscapeIDsMigration) copyAndDelete(ctx context.Context, mv move) (bool, error) {
	fields, err := m.client.HGetAll(ctx, mv.from).Result()
	if err != nil {
		return false, err
	}
	if len(fields) == 0 {
		return false, nil
	}
	exists, err := m.client.Exists(ctx, mv.to).Result()
	if err != nil {
		return false, err
	}
	if exists == 1 {
		target, err := m.client.HGetAll(ctx, mv.to).Result()
		if err != nil {
			return false, err
		}
		if sameFields(fields, target) {
			return true, m.client.Del(ctx, mv.from).Err()
		}
		if _, deleted := target["deleted_at"]; !deleted {
			return false, fmt.Errorf("target key %s already holds another record", mv.to)
		}
		if err := m.client.Del(ctx, mv.to).Err(); err != nil {
			return false, err
		}
	}
	ttl, err := m.client.PTTL(ctx, mv.from).Result()
	if err != nil {
		return false, err
	}

	values := make([]interface{}, 0, len(fields)*2)
	for field, value := range fields {
		values = append(values, field, value)
	}
	pipe := m.client.TxPipeline()
	pipe.HSet(ctx, mv.to, values...)
	if ttl > 0 {
		pipe.PExpire(ctx, mv.to, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	if err := m.client.Del(ctx, mv.from).Err(); err != nil {
		return false, err
	}
	return true, nil
}

func sameFields(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for field, value := range a {
		if other, ok := b[field]; !ok || other != value {
			return false
		}
	}
	return true
}

// mergeIDs returns the IDs of both lists once each, longest first.
func mergeIDs(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var ids []string
	for _, id := range append(append([]string{}, a...), b...) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sortLongestFirst(ids)
	return ids
}

func sortLongestFirst(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) > len(ids[j])
		}
		return ids[i] < ids[j]
	})
}
