package redistenantstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// maxScriptAttempts bounds the optimistic retries of the scripts that check
// a value the caller read beforehand.
const maxScriptAttempts = 8

var errScriptContention = errors.New("tenantstore: destination changed concurrently too many times")

// destinationWrite is the field-level form of a destination, shared by the
// UpsertDestination transaction and the conditional-write scripts.
type destinationWrite struct {
	set     []any    // field/value pairs to HSET
	del     []string // unset optional fields to HDEL
	summary []byte   // summary entry
}

// newDestinationWrite encrypts and encodes d. CreatedAt and UpdatedAt must be
// set.
func (s *store) newDestinationWrite(d *models.Destination) (*destinationWrite, error) {
	credentialsBytes, err := d.Credentials.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("invalid destination credentials: %w", err)
	}
	encryptedCredentials, err := s.cipher.encrypt(credentialsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt destination credentials: %w", err)
	}
	topics, err := d.Topics.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("invalid destination topics: %w", err)
	}
	config, err := d.Config.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("invalid destination config: %w", err)
	}

	w := &destinationWrite{
		set: []any{
			"id", d.ID,
			"entity", "destination",
			"type", d.Type,
			"topics", topics,
			"config", config,
			"credentials", encryptedCredentials,
			"created_at", d.CreatedAt.UnixMilli(),
			"updated_at", d.UpdatedAt.UnixMilli(),
		},
	}

	if d.DisabledAt != nil {
		w.set = append(w.set, "disabled_at", d.DisabledAt.UnixMilli())
	} else {
		w.del = append(w.del, "disabled_at")
	}

	if d.DeliveryMetadata != nil {
		deliveryMetadataBytes, err := d.DeliveryMetadata.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("invalid destination delivery_metadata: %w", err)
		}
		encryptedDeliveryMetadata, err := s.cipher.encrypt(deliveryMetadataBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt destination delivery_metadata: %w", err)
		}
		w.set = append(w.set, "delivery_metadata", encryptedDeliveryMetadata)
	} else {
		w.del = append(w.del, "delivery_metadata")
	}

	if d.Metadata != nil {
		metadata, err := d.Metadata.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("invalid destination metadata: %w", err)
		}
		w.set = append(w.set, "metadata", metadata)
	} else {
		w.del = append(w.del, "metadata")
	}

	if len(d.Filter) > 0 {
		filter, err := d.Filter.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("invalid destination filter: %w", err)
		}
		w.set = append(w.set, "filter", filter)
	} else {
		w.del = append(w.del, "filter")
	}

	// Always HDEL when unset: a destination recreated over a tombstone must
	// not inherit the old expiry.
	if d.ExpiresAt != nil {
		w.set = append(w.set, "expires_at", d.ExpiresAt.UnixMilli())
	} else {
		w.del = append(w.del, "expires_at")
	}

	summary, err := newDestinationSummary(d)
	if err != nil {
		return nil, err
	}
	if w.summary, err = summary.MarshalBinary(); err != nil {
		return nil, err
	}
	return w, nil
}

// args appends the field arguments of the scripts: the HSET pairs then the
// HDEL fields.
func (w *destinationWrite) args(args []any) []any {
	args = append(args, w.set...)
	for _, field := range w.del {
		args = append(args, field)
	}
	return args
}

// msArg formats an optional time as script argument: "" when zero.
func msArg(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return strconv.FormatInt(t.UnixMilli(), 10)
}

func (s *store) fenceKeys(tenantID string, names []string) []string {
	keys := make([]string, len(names))
	for i, name := range names {
		keys[i] = s.redisFenceKey(tenantID, name)
	}
	return keys
}

func (s *store) CreateDestination(ctx context.Context, destination models.Destination, opts ...driver.WriteOption) error {
	o, err := driver.ResolveWriteOptions(opts)
	if err != nil {
		return err
	}

	now := time.Now()
	if destination.CreatedAt.IsZero() {
		destination.CreatedAt = now
	}
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}
	w, err := s.newDestinationWrite(&destination)
	if err != nil {
		return err
	}

	// Buckets: the type's own, then the caller's.
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
	recorded := ""
	if len(buckets) > 0 {
		names := make([]string, len(buckets))
		for i, b := range buckets {
			names[i] = b.Name
		}
		b, err := json.Marshal(names)
		if err != nil {
			return err
		}
		recorded = string(b)
	}

	tenantID := destination.TenantID
	keys := []string{
		s.redisDestinationID(destination.ID, tenantID),
		s.redisTenantDestinationSummaryKey(tenantID),
		s.redisBucketRegistryKey(tenantID),
	}
	for _, b := range buckets {
		keys = append(keys, s.redisBucketKey(tenantID, b.Name))
	}
	// Types with their own limit don't count toward the general one.
	generalMax := ""
	var generalKeys []string
	if !typeLimited {
		generalMax = strconv.Itoa(s.maxDestinationsPerTenant)
		for _, typ := range s.limitedTypes {
			generalKeys = append(generalKeys, s.redisBucketKey(tenantID, driver.TypeBucket(typ)))
		}
	}
	keys = append(keys, generalKeys...)
	keys = append(keys, s.fenceKeys(tenantID, o.Fences)...)

	args := []any{
		destination.ID, w.summary, recorded, msArg(o.NotDeletedSince), generalMax,
		len(buckets), len(generalKeys), len(w.set), len(w.del),
	}
	for _, b := range buckets {
		args = append(args, b.Max)
	}
	for _, b := range buckets {
		args = append(args, b.Name)
	}
	args = w.args(args)

	indexed := s.isIndexed(destination.Type)
	if indexed {
		if err := s.addIndexed(ctx, &destination); err != nil {
			return err
		}
	}

	res, err := scriptReply(createDestinationScript.Run(ctx, s.redisClient, keys, args...))
	if err != nil {
		return err
	}
	if res[0] == "ok" {
		return nil
	}

	// The write was refused: undo this call's index entry, or restore the
	// live destination's score.
	if indexed {
		var actual *int64
		if res[0] == "duplicate" && len(res) > 1 {
			score := scoreFromHash(res[1])
			actual = &score
		}
		s.repairIndexed(ctx, &destination, actual)
	}
	switch res[0] {
	case "duplicate":
		return driver.ErrDuplicateDestination
	case "revoked":
		return driver.ErrDestinationRevoked
	case "max":
		return driver.ErrMaxDestinationsPerTenantReached
	case "limit":
		if len(res) > 1 {
			if i, err := strconv.Atoi(res[1]); err == nil && i >= 1 && i <= len(buckets) {
				return &driver.ErrLimitReached{Bucket: buckets[i-1].Name, Max: buckets[i-1].Max}
			}
		}
	}
	return fmt.Errorf("unexpected create script reply %q", res)
}

func (s *store) UpdateDestinationIfLive(ctx context.Context, destination models.Destination, expectedCreatedAt time.Time, opts ...driver.WriteOption) (driver.UpdateResult, error) {
	o, err := driver.ResolveWriteOptions(opts)
	if err != nil {
		return driver.UpdateResult{}, err
	}

	now := time.Now()
	// The stored generation keeps its created_at.
	destination.CreatedAt = time.UnixMilli(expectedCreatedAt.UnixMilli()).UTC()
	if destination.UpdatedAt.IsZero() {
		destination.UpdatedAt = now
	}
	w, err := s.newDestinationWrite(&destination)
	if err != nil {
		return driver.UpdateResult{}, err
	}

	tenantID := destination.TenantID
	parkedKey := s.redisParkedRetriesKey(tenantID, destination.ID)
	resume := ""
	resumeKey := parkedKey + resumeKeyInfix + "none"
	if o.ResumeParked && destination.DisabledAt == nil {
		resume = "1"
		suffix, err := randomSuffix()
		if err != nil {
			return driver.UpdateResult{}, err
		}
		resumeKey = parkedKey + resumeKeyInfix + strconv.FormatInt(now.UnixMilli(), 10) + ":" + suffix
	}
	keys := []string{
		s.redisDestinationID(destination.ID, tenantID),
		s.redisTenantDestinationSummaryKey(tenantID),
		parkedKey,
		resumeKey,
	}
	keys = append(keys, s.fenceKeys(tenantID, o.Fences)...)
	args := w.args([]any{
		destination.ID, w.summary, expectedCreatedAt.UnixMilli(), msArg(o.NotDeletedSince), resume,
		len(w.set), len(w.del),
	})

	indexed := s.isIndexed(destination.Type)
	if indexed {
		if err := s.addIndexed(ctx, &destination); err != nil {
			return driver.UpdateResult{}, err
		}
	}

	res, err := scriptReply(updateDestinationIfLiveScript.Run(ctx, s.redisClient, keys, args...))
	if err != nil {
		return driver.UpdateResult{}, err
	}
	if res[0] == "ok" && len(res) == 3 {
		return driver.UpdateResult{WasDisabled: res[1] == "1", ResumeKey: res[2]}, nil
	}

	if indexed {
		var actual *int64
		if (res[0] == "conflict" || res[0] == "revoked") && len(res) > 1 {
			score := scoreFromHash(res[1])
			actual = &score
		}
		s.repairIndexed(ctx, &destination, actual)
	}
	switch res[0] {
	case "not_found":
		return driver.UpdateResult{}, driver.ErrDestinationNotFound
	case "deleted":
		return driver.UpdateResult{}, driver.ErrDestinationDeleted
	case "conflict":
		return driver.UpdateResult{}, driver.ErrDestinationConflict
	case "revoked":
		return driver.UpdateResult{}, driver.ErrDestinationRevoked
	}
	return driver.UpdateResult{}, fmt.Errorf("unexpected update script reply %q", res)
}

func (s *store) DisableDestination(ctx context.Context, tenantID, destinationID string, at time.Time) (bool, error) {
	keys := []string{
		s.redisDestinationID(destinationID, tenantID),
		s.redisTenantDestinationSummaryKey(tenantID),
	}
	for range maxScriptAttempts {
		// The script rewrites the summary entry only if it still reads as
		// seen here, so a concurrent update is never reverted.
		seen, err := s.redisClient.HGet(ctx, keys[1], destinationID).Result()
		if err != nil && err != redis.Nil {
			return false, err
		}
		next := ""
		if err == nil {
			var ds destinationSummary
			if err := ds.UnmarshalBinary([]byte(seen)); err != nil {
				return false, err
			}
			ds.Disabled = true
			b, err := ds.MarshalBinary()
			if err != nil {
				return false, err
			}
			next = string(b)
		}

		status, err := disableDestinationScript.Run(ctx, s.redisClient, keys, destinationID, at.UnixMilli(), seen, next).Text()
		if err != nil {
			return false, err
		}
		switch status {
		case "disabled":
			return true, nil
		case "unchanged":
			return false, nil
		case "not_found":
			return false, driver.ErrDestinationNotFound
		case "deleted":
			return false, driver.ErrDestinationDeleted
		case "retry":
			continue
		default:
			return false, fmt.Errorf("unexpected disable script reply %q", status)
		}
	}
	return false, errScriptContention
}

// Statuses of scriptDeleteDestinationIf.
const (
	deleteStatusDeleted = "deleted"
	deleteStatusLive    = "live"
	deleteStatusGone    = "gone"
	deleteStatusMissing = "missing"
)

func (s *store) DeleteDestinationIf(ctx context.Context, tenantID, destinationID string, c driver.DeleteCondition) (driver.DeleteResult, error) {
	_, result, err := s.deleteDestinationIf(ctx, tenantID, destinationID, c)
	return result, err
}

func (s *store) deleteDestinationIf(ctx context.Context, tenantID, destinationID string, c driver.DeleteCondition) (string, driver.DeleteResult, error) {
	destKey := s.redisDestinationID(destinationID, tenantID)
	expectedCreatedAt, expiredBefore := "", ""
	if c.ExpectedCreatedAt != nil {
		expectedCreatedAt = strconv.FormatInt(c.ExpectedCreatedAt.UnixMilli(), 10)
	}
	if c.ExpiredBefore != nil {
		expiredBefore = strconv.FormatInt(c.ExpiredBefore.UnixMilli(), 10)
	}

	for range maxScriptAttempts {
		// The bucket keys must be declared to the script, so read them
		// first; the script retries if they changed meanwhile.
		recorded, err := s.redisClient.HGet(ctx, destKey, "buckets").Result()
		if err != nil && err != redis.Nil {
			return "", driver.DeleteResult{}, err
		}
		keys := []string{
			destKey,
			s.redisTenantDestinationSummaryKey(tenantID),
			s.redisParkedRetriesKey(tenantID, destinationID),
		}
		var names []string
		if recorded != "" && json.Unmarshal([]byte(recorded), &names) == nil {
			for _, name := range names {
				keys = append(keys, s.redisBucketKey(tenantID, name))
			}
		}
		args := []any{
			destinationID,
			time.Now().UnixMilli(),
			int64(destinationTombstoneTTL / time.Second),
			c.Reason,
			c.Type,
			expectedCreatedAt,
			expiredBefore,
			recorded,
		}

		res, err := scriptReply(deleteDestinationIfScript.Run(ctx, s.redisClient, keys, args...))
		if err != nil {
			return "", driver.DeleteResult{}, err
		}
		if res[0] == "retry" {
			continue
		}
		if len(res) != 4 {
			return "", driver.DeleteResult{}, fmt.Errorf("unexpected delete script reply %q", res)
		}

		result := driver.DeleteResult{Type: res[1]}
		if res[2] != "" {
			result.Topics = models.TopicsFromString(res[2])
		}
		if res[3] != "" {
			if t, err := parseTimestamp(res[3]); err == nil {
				result.ExpiresAtMs = t.UnixMilli()
			}
		}
		switch res[0] {
		case deleteStatusDeleted:
			result.Deleted = true
			if s.isIndexed(result.Type) {
				ref := driver.IndexedDestination{TenantID: tenantID, DestinationID: destinationID, Score: result.Score()}
				// Best effort: a stale entry is removed by whoever next finds
				// the destination gone.
				_ = s.RemoveIndexedDestination(ctx, result.Type, result.Topics, ref)
			}
		case deleteStatusLive:
			result.Live = true
		case deleteStatusGone, deleteStatusMissing:
			result.Gone = true
		default:
			return "", driver.DeleteResult{}, fmt.Errorf("unexpected delete script reply %q", res)
		}
		return res[0], result, nil
	}
	return "", driver.DeleteResult{}, errScriptContention
}

func (s *store) WriteFence(ctx context.Context, tenantID, name string, at time.Time, ttl time.Duration) error {
	if err := driver.ValidateName(name); err != nil {
		return fmt.Errorf("invalid fence: %w", err)
	}
	if ttl < time.Millisecond {
		return errors.New("invalid fence: ttl must be at least 1ms")
	}
	keys := []string{s.redisFenceKey(tenantID, name)}
	return writeFenceScript.Run(ctx, s.redisClient, keys, at.UnixMilli(), ttl.Milliseconds()).Err()
}

// scoreFromHash converts an expires_at hash value read by a script into an
// index score.
func scoreFromHash(value string) int64 {
	if value == "" {
		return driver.NoExpiryScore
	}
	t, err := parseTimestamp(value)
	if err != nil {
		return driver.NoExpiryScore
	}
	return driver.IndexScore(&t)
}

// resumeKeyInfix separates a parked-retries key from its resume sets.
const resumeKeyInfix = ":resume:"

func randomSuffix() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validResumeKey reports whether key is a resume set of the tenant.
func (s *store) validResumeKey(tenantID, key string) bool {
	prefix := s.tenantScopedPrefix(tenantID) + "parked_retries:"
	return strings.HasPrefix(key, prefix) && strings.Contains(key[len(prefix):], resumeKeyInfix)
}
