package redistenantstore

import (
	"context"
	"errors"
	"sort"
	"strconv"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	goredis "github.com/redis/go-redis/v9"
)

// Cross-tenant destination indexes, kept for the types of WithIndexedTypes:
// a global ZSET per type and a ZSET per type and topic (plus the SET of those
// topics), scored by expiry. They live outside any tenant's hash slot, so
// they are written around the tenant writes rather than atomically with
// them: added before a write, removed (comparing scores, then checking the
// destination) after a delete. An entry may thus outlive its destination,
// but a live destination is never missing; the destination hash stays the
// source of truth.

func (s *store) isIndexed(typ string) bool {
	_, ok := s.indexedTypes[typ]
	return ok
}

// indexKey is the global index of typ when topic is "", else its topic index.
func (s *store) indexKey(typ, topic string) string {
	if topic == "" {
		return s.deploymentPrefix() + "destination_index:" + typ
	}
	return s.deploymentPrefix() + "destination_index:" + typ + ":topic:" + topic
}

func (s *store) indexTopicsKey(typ string) string {
	return s.deploymentPrefix() + "destination_index:" + typ + ":topics"
}

// indexKeys returns the global index key then one key per non-empty topic.
func (s *store) indexKeys(typ string, topics []string) []string {
	keys := []string{s.indexKey(typ, "")}
	for _, topic := range topics {
		if topic != "" {
			keys = append(keys, s.indexKey(typ, topic))
		}
	}
	return keys
}

// indexEntry is the index entry of a destination.
func indexEntry(d *models.Destination) goredis.Z {
	return goredis.Z{
		Score:  float64(driver.IndexScore(d.ExpiresAt)),
		Member: driver.IndexMember(d.TenantID, d.ID),
	}
}

// addIndexed adds (or rescores) the destination in its indexes.
func (s *store) addIndexed(ctx context.Context, d *models.Destination) error {
	return s.indexAdd(ctx, d.Type, d.Topics, indexEntry(d), false)
}

// indexAdd writes z to the global index of typ and the index of each topic,
// only where it is missing when onlyMissing is set.
func (s *store) indexAdd(ctx context.Context, typ string, topics []string, z goredis.Z, onlyMissing bool) error {
	pipe := s.redisClient.Pipeline()
	add := pipe.ZAdd
	if onlyMissing {
		add = pipe.ZAddNX
	}
	add(ctx, s.indexKey(typ, ""), z)
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		pipe.SAdd(ctx, s.indexTopicsKey(typ), topic)
		add(ctx, s.indexKey(typ, topic), z)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// restoreIndexed adds a live destination of typ back to its indexes where
// it is missing, with its stored expiry.
func (s *store) restoreIndexed(ctx context.Context, typ, tenantID, destinationID string) error {
	f, err := s.redisClient.HMGet(ctx, s.redisDestinationID(destinationID, tenantID), "deleted_at", "type", "topics", "expires_at").Result()
	if err != nil {
		return err
	}
	if storedType, _ := f[1].(string); f[0] != nil || storedType != typ {
		return nil
	}
	topics, _ := f[2].(string)
	expiresAt, _ := f[3].(string)
	z := goredis.Z{
		Score:  float64(scoreFromHash(expiresAt)),
		Member: driver.IndexMember(tenantID, destinationID),
	}
	return s.indexAdd(ctx, typ, models.TopicsFromString(topics), z, true)
}

// repairIndexed undoes addIndexed after a refused write: it restores the
// stored destination's score when actual is set, else removes the entry
// (both only while the entry still has the score this call wrote). Best
// effort.
func (s *store) repairIndexed(ctx context.Context, d *models.Destination, actual *int64) {
	ref := driver.IndexedDestination{
		TenantID:      d.TenantID,
		DestinationID: d.ID,
		Score:         driver.IndexScore(d.ExpiresAt),
	}
	if actual != nil {
		_ = s.RescoreIndexedDestination(ctx, d.Type, d.Topics, ref, *actual)
		return
	}
	_ = s.RemoveIndexedDestination(ctx, d.Type, d.Topics, ref)
}

// parseIndexEntries converts ZSET entries, dropping (and removing) members
// that don't decode.
func (s *store) parseIndexEntries(ctx context.Context, key string, zs []goredis.Z) []driver.IndexedDestination {
	out := make([]driver.IndexedDestination, 0, len(zs))
	var bad []any
	for _, z := range zs {
		member, _ := z.Member.(string)
		tenantID, destinationID, ok := driver.ParseIndexMember(member)
		if !ok {
			bad = append(bad, z.Member)
			continue
		}
		out = append(out, driver.IndexedDestination{
			TenantID:      tenantID,
			DestinationID: destinationID,
			Score:         int64(z.Score),
		})
	}
	if len(bad) > 0 {
		_ = s.redisClient.ZRem(ctx, key, bad...).Err()
	}
	return out
}

func (s *store) ListIndexedDestinations(ctx context.Context, typ, topic string, maxScore int64, limit int) ([]driver.IndexedDestination, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}
	key := s.indexKey(typ, topic)
	zs, err := s.redisClient.ZRangeByScoreWithScores(ctx, key, &goredis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(maxScore, 10),
		Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, err
	}
	return s.parseIndexEntries(ctx, key, zs), nil
}

func (s *store) ScanIndexedDestinations(ctx context.Context, typ, topic string, cursor uint64, count int) ([]driver.IndexedDestination, uint64, error) {
	if count <= 0 {
		return nil, 0, errors.New("count must be positive")
	}
	key := s.indexKey(typ, topic)
	pairs, next, err := s.redisClient.ZScan(ctx, key, cursor, "", int64(count)).Result()
	if err != nil {
		return nil, 0, err
	}
	zs := make([]goredis.Z, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		score, err := strconv.ParseFloat(pairs[i+1], 64)
		if err != nil {
			continue
		}
		zs = append(zs, goredis.Z{Member: pairs[i], Score: score})
	}
	return s.parseIndexEntries(ctx, key, zs), next, nil
}

func (s *store) RescoreIndexedDestination(ctx context.Context, typ string, topics []string, ref driver.IndexedDestination, newScore int64) error {
	member := driver.IndexMember(ref.TenantID, ref.DestinationID)
	var firstErr error
	for _, key := range s.indexKeys(typ, topics) {
		err := rescoreIndexedScript.Run(ctx, s.redisClient, []string{key}, member, ref.Score, newScore).Err()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *store) RemoveIndexedDestination(ctx context.Context, typ string, topics []string, ref driver.IndexedDestination) error {
	member := driver.IndexMember(ref.TenantID, ref.DestinationID)
	var firstErr error
	removed := false
	for _, key := range s.indexKeys(typ, topics) {
		n, err := removeIndexedScript.Run(ctx, s.redisClient, []string{key}, member, ref.Score).Int()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = removed || n > 0
	}
	// Every generation of a destination without expiry has the same member
	// and score, so the entry removed may be that of a destination created
	// again meanwhile: put it back while the destination is live.
	if removed {
		if err := s.restoreIndexed(ctx, typ, ref.TenantID, ref.DestinationID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *store) CountIndexed(ctx context.Context, typ, topic string, minScore int64) (int64, error) {
	return s.redisClient.ZCount(ctx, s.indexKey(typ, topic), strconv.FormatInt(minScore, 10), "+inf").Result()
}

func (s *store) ListIndexedTopics(ctx context.Context, typ string) ([]string, error) {
	topics, err := s.redisClient.SMembers(ctx, s.indexTopicsKey(typ)).Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(topics)
	return topics, nil
}
