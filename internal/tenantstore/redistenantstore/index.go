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
// them: added before a write, removed (comparing scores) after a delete. An
// entry may thus outlive its destination, but a live destination is never
// missing; the destination hash stays the source of truth.

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

// addIndexed adds (or rescores) the destination in its indexes.
func (s *store) addIndexed(ctx context.Context, d *models.Destination) error {
	z := goredis.Z{
		Score:  float64(driver.IndexScore(d.ExpiresAt)),
		Member: driver.IndexMember(d.TenantID, d.ID),
	}
	pipe := s.redisClient.Pipeline()
	pipe.ZAdd(ctx, s.indexKey(d.Type, ""), z)
	for _, topic := range d.Topics {
		if topic == "" {
			continue
		}
		pipe.SAdd(ctx, s.indexTopicsKey(d.Type), topic)
		pipe.ZAdd(ctx, s.indexKey(d.Type, topic), z)
	}
	_, err := pipe.Exec(ctx)
	return err
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
	for _, key := range s.indexKeys(typ, topics) {
		err := removeIndexedScript.Run(ctx, s.redisClient, []string{key}, member, ref.Score).Err()
		if err != nil && firstErr == nil {
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
