package memtenantstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// The conditional writes mirror the Lua scripts of redistenantstore; here the
// store mutex makes each one atomic.

type fence struct {
	atMs     int64
	expireAt time.Time
}

// expiringSet is a set with a key-level expiry, like a Redis SET with a TTL.
type expiringSet struct {
	members  map[string]struct{}
	expireAt time.Time
}

func (e *expiringSet) live(now time.Time) bool {
	return e != nil && len(e.members) > 0 && now.Before(e.expireAt)
}

func (s *store) CreateDestination(_ context.Context, destination models.Destination, opts ...driver.WriteOption) error {
	o, err := driver.ResolveWriteOptions(opts)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenantID := destination.TenantID
	drec, exists := s.destinations[destKey(tenantID, destination.ID)]
	if exists && drec.deletedAt == nil {
		return driver.ErrDuplicateDestination
	}

	if !o.NotDeletedSince.IsZero() {
		since := o.NotDeletedSince.UnixMilli()
		if exists && drec.deletedReason != driver.DeleteReasonExpired && drec.deletedAt.UnixMilli() >= since {
			return driver.ErrDestinationRevoked
		}
		if s.fencedLocked(tenantID, o.Fences, since) {
			return driver.ErrDestinationRevoked
		}
	}

	var buckets []driver.Bucket
	typeMax, typeLimited := s.typeLimits[destination.Type]
	if typeLimited {
		buckets = append(buckets, driver.Bucket{Name: driver.TypeBucket(destination.Type), Max: typeMax})
	}
	for _, b := range o.Buckets {
		if typeLimited && b.Name == driver.TypeBucket(destination.Type) {
			continue
		}
		buckets = append(buckets, b)
	}

	live := s.destsByTenant[tenantID]
	for _, b := range buckets {
		members := s.buckets[tenantID][b.Name]
		if b.Max <= 0 {
			continue
		}
		if _, member := members[destination.ID]; member {
			continue
		}
		if len(members) >= b.Max {
			// Drop members no longer listed before refusing.
			for id := range members {
				if _, ok := live[id]; !ok {
					delete(members, id)
				}
			}
			if len(members) >= b.Max {
				return &driver.ErrLimitReached{Bucket: b.Name, Max: b.Max}
			}
		}
	}

	if !typeLimited {
		n := len(live)
		if _, ok := live[destination.ID]; ok {
			n--
		}
		for _, typ := range s.limitedTypes {
			n -= len(s.buckets[tenantID][driver.TypeBucket(typ)])
		}
		if n >= s.maxDestinationsPerTenant {
			return driver.ErrMaxDestinationsPerTenantReached
		}
	}

	names := make([]string, len(buckets))
	for i, b := range buckets {
		names[i] = b.Name
		if s.buckets[tenantID] == nil {
			s.buckets[tenantID] = make(map[string]map[string]struct{})
		}
		if s.buckets[tenantID][b.Name] == nil {
			s.buckets[tenantID][b.Name] = make(map[string]struct{})
		}
		s.buckets[tenantID][b.Name][destination.ID] = struct{}{}
	}
	s.upsertDestinationLocked(destination, names)
	return nil
}

// fencedLocked reports whether one of the tenant's fences is at or after
// since (Unix ms).
func (s *store) fencedLocked(tenantID string, names []string, since int64) bool {
	now := time.Now()
	for _, name := range names {
		f, ok := s.fences[destKey(tenantID, name)]
		if ok && now.Before(f.expireAt) && f.atMs >= since {
			return true
		}
	}
	return false
}

func (s *store) UpdateDestinationIfLive(_ context.Context, destination models.Destination, expectedCreatedAt time.Time, opts ...driver.WriteOption) (driver.UpdateResult, error) {
	o, err := driver.ResolveWriteOptions(opts)
	if err != nil {
		return driver.UpdateResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenantID := destination.TenantID
	key := destKey(tenantID, destination.ID)
	drec, ok := s.destinations[key]
	if !ok {
		return driver.UpdateResult{}, driver.ErrDestinationNotFound
	}
	if drec.deletedAt != nil {
		return driver.UpdateResult{}, driver.ErrDestinationDeleted
	}
	if drec.destination.CreatedAt.UnixMilli() != expectedCreatedAt.UnixMilli() {
		return driver.UpdateResult{}, driver.ErrDestinationConflict
	}
	if !o.NotDeletedSince.IsZero() && s.fencedLocked(tenantID, o.Fences, o.NotDeletedSince.UnixMilli()) {
		return driver.UpdateResult{}, driver.ErrDestinationRevoked
	}

	result := driver.UpdateResult{WasDisabled: drec.destination.DisabledAt != nil}
	now := time.Now()
	destination.CreatedAt = time.UnixMilli(expectedCreatedAt.UnixMilli()).UTC()
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}
	s.upsertDestinationLocked(destination, drec.buckets)

	if o.ResumeParked && destination.DisabledAt == nil {
		if parked := s.parked[key]; parked.live(now) {
			suffix, err := randomSuffix()
			if err != nil {
				return driver.UpdateResult{}, err
			}
			result.ResumeKey = parkedRetriesKey(tenantID, destination.ID) + resumeKeyInfix +
				strconv.FormatInt(now.UnixMilli(), 10) + ":" + suffix
			s.resumeSets[result.ResumeKey] = parked
		}
		delete(s.parked, key)
	}
	return result, nil
}

func (s *store) DisableDestination(_ context.Context, tenantID, destinationID string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	drec, ok := s.destinations[destKey(tenantID, destinationID)]
	if !ok {
		return false, driver.ErrDestinationNotFound
	}
	if drec.deletedAt != nil {
		return false, driver.ErrDestinationDeleted
	}
	if drec.destination.DisabledAt != nil {
		return false, nil
	}
	drec.destination.DisabledAt = &at
	return true, nil
}

func (s *store) DeleteDestinationIf(_ context.Context, tenantID, destinationID string, c driver.DeleteCondition) (driver.DeleteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteIfLocked(tenantID, destinationID, c), nil
}

func (s *store) deleteIfLocked(tenantID, destinationID string, c driver.DeleteCondition) driver.DeleteResult {
	key := destKey(tenantID, destinationID)
	drec, ok := s.destinations[key]
	if !ok {
		return driver.DeleteResult{Gone: true}
	}
	d := &drec.destination
	result := driver.DeleteResult{Type: d.Type, Topics: slices.Clone([]string(d.Topics))}
	if d.ExpiresAt != nil {
		result.ExpiresAtMs = d.ExpiresAt.UnixMilli()
	}
	if drec.deletedAt != nil {
		result.Gone = true
		return result
	}

	if (c.Type != "" && d.Type != c.Type) ||
		(c.ExpectedCreatedAt != nil && d.CreatedAt.UnixMilli() != c.ExpectedCreatedAt.UnixMilli()) ||
		(c.ExpiredBefore != nil && (d.ExpiresAt == nil || d.ExpiresAt.UnixMilli() > c.ExpiredBefore.UnixMilli())) {
		result.Live = true
		return result
	}

	now := time.Now()
	drec.deletedAt = &now
	drec.deletedReason = c.Reason
	delete(s.destsByTenant[tenantID], destinationID)
	for _, name := range drec.buckets {
		delete(s.buckets[tenantID][name], destinationID)
	}
	delete(s.parked, key)
	s.removeIndexedLocked(d)
	result.Deleted = true
	return result
}

func (s *store) WriteFence(_ context.Context, tenantID, name string, at time.Time, ttl time.Duration) error {
	if err := driver.ValidateName(name); err != nil {
		return fmt.Errorf("invalid fence: %w", err)
	}
	if ttl < time.Millisecond {
		return errors.New("invalid fence: ttl must be at least 1ms")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	key := destKey(tenantID, name)
	f, ok := s.fences[key]
	if !ok || !now.Before(f.expireAt) || at.UnixMilli() > f.atMs {
		f.atMs = at.UnixMilli()
	}
	f.expireAt = now.Add(ttl)
	s.fences[key] = f
	return nil
}

func (s *store) ParkRetry(_ context.Context, tenantID, destinationID, member string, max int, expireAt time.Time) (driver.ParkResult, error) {
	if member == "" {
		return "", errors.New("park retry: empty member")
	}
	if max <= 0 {
		return "", errors.New("park retry: max must be positive")
	}
	if expireAt.IsZero() {
		return "", errors.New("park retry: missing expiry")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := destKey(tenantID, destinationID)
	drec, ok := s.destinations[key]
	if !ok || drec.deletedAt != nil {
		return driver.ParkResultGone, nil
	}
	if drec.destination.DisabledAt == nil {
		return driver.ParkResultEnabled, nil
	}
	set := s.parked[key]
	if !set.live(time.Now()) {
		set = &expiringSet{members: make(map[string]struct{})}
		s.parked[key] = set
	}
	if _, ok := set.members[member]; !ok {
		if len(set.members) >= max {
			return driver.ParkResultFull, nil
		}
		set.members[member] = struct{}{}
	}
	set.expireAt = expireAt
	if !time.Now().Before(expireAt) {
		delete(s.parked, key)
	}
	return driver.ParkResultParked, nil
}

func (s *store) PopResumeMembers(_ context.Context, tenantID, key string, n int) ([]string, error) {
	if !validResumeKey(tenantID, key) {
		return nil, driver.ErrInvalidResumeKey
	}
	if n <= 0 {
		return nil, errors.New("pop resume members: n must be positive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	set := s.resumeSets[key]
	if !set.live(time.Now()) {
		delete(s.resumeSets, key)
		return nil, nil
	}
	var members []string
	for member := range set.members {
		if len(members) == n {
			break
		}
		members = append(members, member)
		delete(set.members, member)
	}
	if len(set.members) == 0 {
		delete(s.resumeSets, key)
	}
	return members, nil
}

func (s *store) DeleteResumeSet(_ context.Context, tenantID, key string) error {
	if !validResumeKey(tenantID, key) {
		return driver.ErrInvalidResumeKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.resumeSets, key)
	return nil
}

// Resume set keys follow the Redis layout (without deployment prefix).
const resumeKeyInfix = ":resume:"

func parkedRetriesKey(tenantID, destinationID string) string {
	return "tenant:{" + tenantID + "}:parked_retries:" + destinationID
}

func validResumeKey(tenantID, key string) bool {
	prefix := "tenant:{" + tenantID + "}:parked_retries:"
	return strings.HasPrefix(key, prefix) && strings.Contains(key[len(prefix):], resumeKeyInfix)
}

func randomSuffix() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
