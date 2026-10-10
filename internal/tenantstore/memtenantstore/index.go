package memtenantstore

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// Destination indexes mirroring the Redis ZSETs: a global index per type and
// one per type and topic, member driver.IndexMember, score the expiry.

func (s *store) isIndexed(typ string) bool {
	_, ok := s.indexedTypes[typ]
	return ok
}

func indexKey(typ, topic string) string {
	return typ + "\x00" + topic
}

// indexKeys returns the global index key then one key per non-empty topic.
func indexKeys(typ string, topics []string) []string {
	keys := []string{indexKey(typ, "")}
	for _, topic := range topics {
		if topic != "" {
			keys = append(keys, indexKey(typ, topic))
		}
	}
	return keys
}

func (s *store) addIndexedLocked(d *models.Destination) {
	member := driver.IndexMember(d.TenantID, d.ID)
	score := driver.IndexScore(d.ExpiresAt)
	for _, key := range indexKeys(d.Type, d.Topics) {
		if s.index[key] == nil {
			s.index[key] = make(map[string]int64)
		}
		s.index[key][member] = score
	}
	for _, topic := range d.Topics {
		if topic == "" {
			continue
		}
		if s.indexTopics[d.Type] == nil {
			s.indexTopics[d.Type] = make(map[string]struct{})
		}
		s.indexTopics[d.Type][topic] = struct{}{}
	}
}

// removeIndexedLocked removes a destination being deleted from its indexes.
func (s *store) removeIndexedLocked(d *models.Destination) {
	if !s.isIndexed(d.Type) {
		return
	}
	member := driver.IndexMember(d.TenantID, d.ID)
	score := driver.IndexScore(d.ExpiresAt)
	for _, key := range indexKeys(d.Type, d.Topics) {
		if cur, ok := s.index[key][member]; ok && cur == score {
			delete(s.index[key], member)
		}
	}
}

// sortedIndex returns the entries of an index ordered like a ZSET: by score,
// then member.
func (s *store) sortedIndex(typ, topic string) []driver.IndexedDestination {
	entries := s.index[indexKey(typ, topic)]
	members := make([]string, 0, len(entries))
	for member := range entries {
		members = append(members, member)
	}
	slices.SortFunc(members, func(a, b string) int {
		if c := cmp.Compare(entries[a], entries[b]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	out := make([]driver.IndexedDestination, 0, len(members))
	for _, member := range members {
		tenantID, destinationID, ok := driver.ParseIndexMember(member)
		if !ok {
			continue
		}
		out = append(out, driver.IndexedDestination{TenantID: tenantID, DestinationID: destinationID, Score: entries[member]})
	}
	return out
}

func (s *store) ListIndexedDestinations(_ context.Context, typ, topic string, maxScore int64, limit int) ([]driver.IndexedDestination, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []driver.IndexedDestination
	for _, entry := range s.sortedIndex(typ, topic) {
		if entry.Score > maxScore || len(out) == limit {
			break
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *store) ScanIndexedDestinations(_ context.Context, typ, topic string, cursor uint64, count int) ([]driver.IndexedDestination, uint64, error) {
	if count <= 0 {
		return nil, 0, errors.New("count must be positive")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// The cursor is an offset into the sorted index.
	entries := s.sortedIndex(typ, topic)
	if cursor >= uint64(len(entries)) {
		return nil, 0, nil
	}
	end := min(cursor+uint64(count), uint64(len(entries)))
	next := end
	if end == uint64(len(entries)) {
		next = 0
	}
	return slices.Clone(entries[cursor:end]), next, nil
}

func (s *store) RescoreIndexedDestination(_ context.Context, typ string, topics []string, ref driver.IndexedDestination, newScore int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	member := driver.IndexMember(ref.TenantID, ref.DestinationID)
	for _, key := range indexKeys(typ, topics) {
		if cur, ok := s.index[key][member]; ok && cur == ref.Score {
			s.index[key][member] = newScore
		}
	}
	return nil
}

func (s *store) RemoveIndexedDestination(_ context.Context, typ string, topics []string, ref driver.IndexedDestination) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	member := driver.IndexMember(ref.TenantID, ref.DestinationID)
	removed := false
	for _, key := range indexKeys(typ, topics) {
		if cur, ok := s.index[key][member]; ok && cur == ref.Score {
			delete(s.index[key], member)
			removed = true
		}
	}
	// As in redistenantstore: a live destination of typ keeps its entries.
	if drec, ok := s.destinations[destKey(ref.TenantID, ref.DestinationID)]; removed && ok &&
		drec.deletedAt == nil && drec.destination.Type == typ {
		s.addIndexedLocked(&drec.destination)
	}
	return nil
}

func (s *store) CountIndexed(_ context.Context, typ, topic string, minScore int64) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var n int64
	for _, score := range s.index[indexKey(typ, topic)] {
		if score >= minScore {
			n++
		}
	}
	return n, nil
}

func (s *store) ListIndexedTopics(_ context.Context, typ string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topics := make([]string, 0, len(s.indexTopics[typ]))
	for topic := range s.indexTopics[typ] {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics, nil
}
