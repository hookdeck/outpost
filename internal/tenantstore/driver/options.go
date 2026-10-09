package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Bucket is a named set of a tenant's destinations holding at most Max
// members (no limit when Max <= 0). CreateDestination adds the destination to
// its buckets and DeleteDestinationIf/DeleteDestination remove it.
type Bucket struct {
	Name string
	Max  int
}

// TypeBucket names the bucket of a type with its own limit (TypeLimits).
// Such types don't count toward MaxDestinationsPerTenant.
func TypeBucket(typ string) string {
	return "type:" + typ
}

// WriteOptions holds the options of CreateDestination and
// UpdateDestinationIfLive. Drivers read them with ResolveWriteOptions.
type WriteOptions struct {
	Buckets         []Bucket
	NotDeletedSince time.Time
	Fences          []string
	ResumeParked    bool
}

// WriteOption configures CreateDestination and UpdateDestinationIfLive.
type WriteOption func(*WriteOptions)

// WithBuckets adds the created destination to buckets, failing with
// *ErrLimitReached when one is full. CreateDestination only.
func WithBuckets(buckets ...Bucket) WriteOption {
	return func(o *WriteOptions) {
		o.Buckets = append(o.Buckets, buckets...)
	}
}

// WithNotDeletedSince rejects the write with ErrDestinationRevoked when the
// destination was deleted at or after t for any reason but
// DeleteReasonExpired (CreateDestination over a tombstone), or when a fence
// of WithFence is at or after t.
func WithNotDeletedSince(t time.Time) WriteOption {
	return func(o *WriteOptions) {
		o.NotDeletedSince = t
	}
}

// WithFence checks the fence name of the tenant (see WriteFence) against
// WithNotDeletedSince, which it requires.
func WithFence(name string) WriteOption {
	return func(o *WriteOptions) {
		o.Fences = append(o.Fences, name)
	}
}

// WithResumeParkedRetries makes UpdateDestinationIfLive move the parked
// retries of a destination it leaves enabled to a resume set, atomically with
// the write.
func WithResumeParkedRetries() WriteOption {
	return func(o *WriteOptions) {
		o.ResumeParked = true
	}
}

// ResolveWriteOptions applies opts and validates the result.
func ResolveWriteOptions(opts []WriteOption) (WriteOptions, error) {
	var o WriteOptions
	for _, opt := range opts {
		opt(&o)
	}
	seen := make(map[string]struct{}, len(o.Buckets))
	for _, b := range o.Buckets {
		if err := ValidateName(b.Name); err != nil {
			return o, fmt.Errorf("invalid bucket: %w", err)
		}
		if _, dup := seen[b.Name]; dup {
			return o, fmt.Errorf("invalid bucket: duplicate %q", b.Name)
		}
		seen[b.Name] = struct{}{}
	}
	for _, f := range o.Fences {
		if err := ValidateName(f); err != nil {
			return o, fmt.Errorf("invalid fence: %w", err)
		}
	}
	if len(o.Fences) > 0 && o.NotDeletedSince.IsZero() {
		return o, errors.New("fences require WithNotDeletedSince")
	}
	return o, nil
}

// maxNameLength bounds bucket and fence names, which become key suffixes.
const maxNameLength = 256

// ValidateName checks a bucket or fence name: 1 to 256 visible ASCII
// characters.
func ValidateName(name string) error {
	if name == "" || len(name) > maxNameLength {
		return fmt.Errorf("name must be 1 to %d bytes", maxNameLength)
	}
	for i := 0; i < len(name); i++ {
		if name[i] <= ' ' || name[i] > '~' {
			return fmt.Errorf("name %q must be visible ASCII", name)
		}
	}
	return nil
}

// UpdateResult reports what UpdateDestinationIfLive did.
type UpdateResult struct {
	// WasDisabled reports whether the destination was disabled just before
	// the write.
	WasDisabled bool
	// ResumeKey names the resume set holding the parked retries moved by
	// WithResumeParkedRetries, or is empty when nothing was parked.
	ResumeKey string
}

// Delete reasons recorded on tombstones. Only DeleteReasonExpired has a
// meaning to the store (see WithNotDeletedSince).
const (
	DeleteReasonExpired       = "expired"
	DeleteReasonUnsubscribed  = "unsubscribed"
	DeleteReasonRevoked       = "revoked"
	DeleteReasonTerminated    = "terminated"
	DeleteReasonTenantDeleted = "tenant_deleted"
)

// DeleteCondition restricts DeleteDestinationIf. Zero fields are unchecked.
type DeleteCondition struct {
	// Type requires the destination to have this type.
	Type string
	// ExpectedCreatedAt requires the destination generation created at this
	// time (millisecond precision).
	ExpectedCreatedAt *time.Time
	// ExpiredBefore requires an expiry at or before this time. Destinations
	// without an expiry never match.
	ExpiredBefore *time.Time
	// Reason is recorded on the tombstone.
	Reason string
}

// DeleteResult reports what DeleteDestinationIf found. Exactly one of
// Deleted, Live and Gone is true.
type DeleteResult struct {
	// Deleted: this call deleted the destination.
	Deleted bool
	// Live: the destination exists but did not match the condition.
	Live bool
	// Gone: the destination does not exist or was already deleted.
	Gone bool
	// ExpiresAtMs is the destination's expiry in Unix milliseconds, 0 when it
	// has none (or is unknown).
	ExpiresAtMs int64
	// Type and Topics of the destination, when known.
	Type   string
	Topics []string
}

// Score is the index score matching ExpiresAtMs.
func (r DeleteResult) Score() int64 {
	if r.ExpiresAtMs == 0 {
		return NoExpiryScore
	}
	return r.ExpiresAtMs
}

// NoExpiryScore is the index score of destinations without an expiry
// (9999-12-31T23:59:59.999Z), used instead of +inf, which not every backend
// compares reliably.
const NoExpiryScore int64 = 253402300799999

// IndexScore returns the index score of a destination expiring at expiresAt:
// its Unix milliseconds, or NoExpiryScore when nil or later.
func IndexScore(expiresAt *time.Time) int64 {
	if expiresAt == nil {
		return NoExpiryScore
	}
	ms := expiresAt.UnixMilli()
	if ms > NoExpiryScore {
		return NoExpiryScore
	}
	return ms
}

// IndexedDestination is a member of a destination index.
type IndexedDestination struct {
	TenantID      string
	DestinationID string
	// Score is the expiry in Unix milliseconds or NoExpiryScore.
	Score int64
}

// IndexMember encodes the index member of a destination. Tenant and
// destination IDs are free-form, so the encoding is a JSON array.
func IndexMember(tenantID, destinationID string) string {
	b, _ := json.Marshal([2]string{tenantID, destinationID})
	return string(b)
}

// ParseIndexMember decodes an IndexMember.
func ParseIndexMember(member string) (tenantID, destinationID string, ok bool) {
	var ids []string
	if err := json.Unmarshal([]byte(member), &ids); err != nil || len(ids) != 2 {
		return "", "", false
	}
	return ids[0], ids[1], true
}

// ParkResult is the outcome of ParkRetry.
type ParkResult string

const (
	// ParkResultParked: the member is in the parked set.
	ParkResultParked ParkResult = "parked"
	// ParkResultEnabled: the destination is enabled; deliver now.
	ParkResultEnabled ParkResult = "enabled"
	// ParkResultGone: the destination does not exist or was deleted.
	ParkResultGone ParkResult = "gone"
	// ParkResultFull: the parked set holds max members already.
	ParkResultFull ParkResult = "full"
)
