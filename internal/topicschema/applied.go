package topicschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Schema evolution enforcement. The API service records the topic
// configuration it applied (its Snapshot) in Redis and, at the next startup
// with a different configuration, checks the change against it: breaking
// changes to topics with live MCP subscriptions are refused unless forced,
// and forced ones record the old topic hashes whose subscriptions the MCP
// subscriptions worker then ends. Running instances mark their
// configuration live with Heartbeat, so a rolling deploy can restart
// instances on the previous configuration without a check.

// KeyHashTag is the Redis hash tag of the applied configuration, its
// history and the MCP subscriptions worker's state, so they share a cluster
// slot and can be written together.
const KeyHashTag = "{outpost:topic_schemas}"

const (
	// HistoryLimit is the number of applied hashes the history keeps.
	HistoryLimit = 50
	// DefaultHeartbeatInterval is how often a running instance marks its
	// configuration live, and DefaultHeartbeatTTL how long a mark lasts.
	DefaultHeartbeatInterval = 20 * time.Second
	DefaultHeartbeatTTL      = 60 * time.Second

	applyLockTTL         = 10 * time.Second
	defaultApplyLockWait = 15 * time.Second
	applyLockPoll        = 250 * time.Millisecond
	unlockTimeout        = 2 * time.Second
	// maxSupersededHashes bounds the earlier hashes kept per topic.
	maxSupersededHashes = 16
	maxApplyAttempts    = 3
)

// ChangeTopicEnded is the kind of the changes a *BreakingChangeError lists
// when a configuration ends every MCP-enabled topic.
const ChangeTopicEnded = "topic_ended"

const (
	msgBreakingChanges = "topic schema changes break live MCP subscriptions: publish the new schema under a new topic, " +
		"or set TOPICS_ALLOW_BREAKING_CHANGES=true for one deploy to apply it and end those subscriptions"
	msgAllTopicsEnded = "the topic schemas end every MCP-enabled topic, which would end every MCP subscription: " +
		"check that they are configured, or set TOPICS_ALLOW_BREAKING_CHANGES=true for one deploy to apply them"
)

func keyPrefix(deploymentID string) string {
	if deploymentID == "" {
		return ""
	}
	return deploymentID + ":"
}

// AppliedKey is the key of the applied configuration (JSON Applied).
func AppliedKey(deploymentID string) string {
	return keyPrefix(deploymentID) + KeyHashTag + ":applied"
}

// HistoryKey is the key of the applied hashes (LIST of JSON HistoryEntry,
// newest first).
func HistoryKey(deploymentID string) string {
	return keyPrefix(deploymentID) + KeyHashTag + ":history"
}

// ApplyLockKey is the key of the lock Apply holds while writing.
func ApplyLockKey(deploymentID string) string {
	return keyPrefix(deploymentID) + "outpost:lock:topic_schemas"
}

// LiveKey is the heartbeat key of a configuration hash.
func LiveKey(deploymentID, hash string) string {
	return keyPrefix(deploymentID) + "outpost:topic_schemas:live:" + hash
}

// Applied is the last applied topic configuration.
type Applied struct {
	Snapshot  Snapshot  `json:"snapshot"`
	Hash      string    `json:"hash"`
	AppliedAt time.Time `json:"applied_at"`
	// Broken lists, per topic, the topic hashes (Snapshot.TopicHash) a
	// forced breaking change left behind: MCP subscriptions whose
	// config.schema_hash is one of them must end. Entries are retired by
	// the MCP subscriptions worker.
	Broken map[string][]BrokenHash `json:"broken,omitempty"`
	// Superseded lists, per topic of Snapshot, its earlier topic hashes,
	// most recent first (at most 16), so a forced change also ends
	// subscriptions created against schemas older than the last applied
	// one.
	Superseded map[string][]string `json:"superseded,omitempty"`

	// raw is the stored value, for compare-and-set writes.
	raw string
}

// BrokenHash is one old topic hash of Applied.Broken.
type BrokenHash struct {
	Hash       string    `json:"hash"`
	RecordedAt time.Time `json:"recorded_at"`
}

// rawValue is the stored value a was read from, "" for none.
func (a *Applied) rawValue() string {
	if a == nil {
		return ""
	}
	return a.raw
}

// BrokenHashes returns the old topic hashes of topic whose subscriptions
// must end.
func (a *Applied) BrokenHashes(topic string) []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.Broken[topic]))
	for _, b := range a.Broken[topic] {
		out = append(out, b.Hash)
	}
	return out
}

// IsBroken reports whether a subscription to topic created against
// schemaHash must end.
func (a *Applied) IsBroken(topic, schemaHash string) bool {
	if a == nil || schemaHash == "" {
		return false
	}
	for _, b := range a.Broken[topic] {
		if b.Hash == schemaHash {
			return true
		}
	}
	return false
}

// BrokenSet holds the broken topic hashes of an Applied for lookups, without
// the rest of it. Immutable once built; the nil set has none.
type BrokenSet map[string]map[string]struct{}

// BrokenSet returns the broken topic hashes of a, nil when there are none.
func (a *Applied) BrokenSet() BrokenSet {
	if a == nil || len(a.Broken) == 0 {
		return nil
	}
	set := make(BrokenSet, len(a.Broken))
	for topic, hashes := range a.Broken {
		set[topic] = make(map[string]struct{}, len(hashes))
		for _, b := range hashes {
			set[topic][b.Hash] = struct{}{}
		}
	}
	return set
}

// IsBroken is Applied.IsBroken.
func (s BrokenSet) IsBroken(topic, schemaHash string) bool {
	if schemaHash == "" {
		return false
	}
	_, ok := s[topic][schemaHash]
	return ok
}

// HistoryEntry is one applied configuration hash.
type HistoryEntry struct {
	Hash      string    `json:"hash"`
	AppliedAt time.Time `json:"applied_at"`
}

// MCPServed reports whether topic is MCP-enabled, with a payload schema, in
// s.
func (s Snapshot) MCPServed(topic string) bool {
	t, ok := s.Topics[topic]
	return ok && mcpServed(t)
}

// ApplyOptions configures Apply and Plan.
type ApplyOptions struct {
	// AllowBreaking applies breaking changes, and a configuration ending
	// every MCP-enabled topic, instead of refusing them
	// (TOPICS_ALLOW_BREAKING_CHANGES).
	AllowBreaking bool
	// LiveTopics reports whether topic has live MCP subscriptions. Only
	// those topics are checked for breaking changes. Nil treats every topic
	// as live.
	LiveTopics func(ctx context.Context, topic string) (bool, error)
	// Logger is optional. Plan never logs.
	Logger *logging.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// LockWait bounds the wait for the apply lock; 0 means 15 seconds, which
	// outlasts the 10 second lock of an instance that died holding it.
	LockWait time.Duration
}

// ApplyResult reports what Apply did, or Plan would do.
type ApplyResult struct {
	// Hash is the hash of the configuration given.
	Hash string
	// Unchanged: it is the applied configuration; nothing to do.
	Unchanged bool
	// Initial: nothing was applied yet; it is recorded without a check.
	Initial bool
	// KnownConfig: a running instance reported it (see Heartbeat), so it is
	// accepted without a check and the applied configuration is kept.
	// Changes and Ended then compare it with the applied configuration, for
	// logging only.
	KnownConfig bool
	// Changes lists the breaking changes to topics MCP-enabled in both
	// configurations with live subscriptions.
	Changes []Change
	// Forced: Changes, or the end of every MCP-enabled topic, are applied
	// because AllowBreaking is set.
	Forced bool
	// Ended lists the topics MCP-enabled in the applied configuration and
	// not in this one. The MCP subscriptions worker ends their
	// subscriptions.
	Ended []string
}

// BreakingChangeError refuses a configuration: breaking changes to topics
// with live MCP subscriptions, or the end of every MCP-enabled topic (kind
// ChangeTopicEnded). It marshals to {"message", "data": [changes]}.
type BreakingChangeError struct {
	Message string
	Changes []Change
}

func (e *BreakingChangeError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	for _, c := range e.Changes {
		b.WriteString("\n  - ")
		b.WriteString(changeLine(c))
	}
	return b.String()
}

// changeLine renders a change as "topic path: kind (detail)".
func changeLine(c Change) string {
	var b strings.Builder
	b.WriteString(c.Topic)
	if c.Path != "" {
		b.WriteByte(' ')
		b.WriteString(c.Path)
	}
	b.WriteString(": ")
	b.WriteString(c.Kind)
	if c.Detail != "" {
		b.WriteString(" (")
		b.WriteString(c.Detail)
		b.WriteByte(')')
	}
	return b.String()
}

// MarshalJSON renders the error for an API response.
func (e BreakingChangeError) MarshalJSON() ([]byte, error) {
	changes := e.Changes
	if changes == nil {
		changes = []Change{}
	}
	return marshalNoEscape(struct {
		Message string   `json:"message"`
		Data    []Change `json:"data"`
	}{e.Message, changes})
}

// Apply checks current against the applied configuration and records it,
// on the API service at startup. In order:
//   - the applied hash equals current's: nothing to do (no lock taken);
//   - nothing applied yet: current is recorded;
//   - a running instance reports current live: it is accepted without a
//     check, and the applied configuration is kept;
//   - otherwise breaking changes to topics MCP-enabled in both with live
//     subscriptions (LiveTopics), and a current that ends every
//     MCP-enabled topic, fail with *BreakingChangeError unless
//     AllowBreaking; forced changes record the topics' old hashes in
//     Applied.Broken.
//
// Writes happen under a lock and as one compare-and-set of the applied
// configuration and its history.
func Apply(ctx context.Context, rdb redis.Cmdable, deploymentID string, current Snapshot, opts ApplyOptions) (ApplyResult, error) {
	a := newApplier(rdb, deploymentID, current, opts)
	// Nothing to write in the common cases (unchanged, a running
	// configuration, a refusal), so decide before taking the lock.
	res, next, prev, err := a.planFromStore(ctx)
	if err != nil || next == nil {
		a.log(res, err)
		return res, err
	}

	unlock, err := a.lock(ctx)
	if err != nil {
		return ApplyResult{Hash: a.hash}, err
	}
	defer unlock()

	for range maxApplyAttempts {
		// Plan again only if the applied configuration changed meanwhile.
		cur, err := ReadApplied(ctx, a.rdb, a.dep)
		if err != nil {
			return ApplyResult{Hash: a.hash}, err
		}
		if cur.rawValue() != prev.rawValue() {
			prev = cur
			if res, next, err = a.plan(ctx, prev); err != nil || next == nil {
				a.log(res, err)
				return res, err
			}
		}
		ok, err := writeApplied(ctx, a.rdb, a.dep, prev, next, true)
		if err != nil {
			return ApplyResult{Hash: a.hash}, err
		}
		if ok {
			a.log(res, nil)
			return res, nil
		}
	}
	return ApplyResult{Hash: a.hash}, errors.New("topic schemas: the applied configuration kept changing while applying; retry")
}

// Plan returns what Apply would do with current, without taking the lock
// or writing anything: a dry run for a configuration API or CLI.
func Plan(ctx context.Context, rdb redis.Cmdable, deploymentID string, current Snapshot, opts ApplyOptions) (ApplyResult, error) {
	res, _, _, err := newApplier(rdb, deploymentID, current, opts).planFromStore(ctx)
	return res, err
}

// ReadApplied returns the applied configuration, or nil when there is none.
func ReadApplied(ctx context.Context, rdb redis.Cmdable, deploymentID string) (*Applied, error) {
	key := AppliedKey(deploymentID)
	raw, err := rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("topic schemas: read %s: %w", key, err)
	}
	var a Applied
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, fmt.Errorf("topic schemas: %s is unreadable; delete it to start over from the current schemas: %w", key, err)
	}
	if a.Hash == "" {
		return nil, fmt.Errorf("topic schemas: %s has no hash; delete it to start over from the current schemas", key)
	}
	a.raw = raw
	return &a, nil
}

// ReadHistory returns the applied hashes, newest first. Unreadable entries
// are skipped.
func ReadHistory(ctx context.Context, rdb redis.Cmdable, deploymentID string) ([]HistoryEntry, error) {
	raws, err := rdb.LRange(ctx, HistoryKey(deploymentID), 0, HistoryLimit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("topic schemas: read history: %w", err)
	}
	out := make([]HistoryEntry, 0, len(raws))
	for _, raw := range raws {
		var e HistoryEntry
		if json.Unmarshal([]byte(raw), &e) == nil && e.Hash != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// Heartbeat marks the configuration hash as run by this instance for ttl
// (DefaultHeartbeatTTL when <= 0). Running instances call it every
// DefaultHeartbeatInterval.
func Heartbeat(ctx context.Context, rdb redis.Cmdable, deploymentID, hash string, ttl time.Duration) error {
	if hash == "" {
		return errors.New("topic schemas: heartbeat needs a hash")
	}
	if ttl <= 0 {
		ttl = DefaultHeartbeatTTL
	}
	return rdb.Set(ctx, LiveKey(deploymentID, hash), time.Now().UnixMilli(), ttl).Err()
}

// IsLive reports whether a running instance reported hash recently.
func IsLive(ctx context.Context, rdb redis.Cmdable, deploymentID, hash string) (bool, error) {
	n, err := rdb.Exists(ctx, LiveKey(deploymentID, hash)).Result()
	if err != nil {
		return false, fmt.Errorf("topic schemas: check live configuration: %w", err)
	}
	return n > 0, nil
}

// LiveHashes returns the hashes, of those given, that running instances
// reported recently. Each key is checked on its own, so it works in
// cluster mode.
func LiveHashes(ctx context.Context, rdb redis.Cmdable, deploymentID string, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	pipe := rdb.Pipeline()
	cmds := make([]*redis.IntCmd, len(hashes))
	for i, h := range hashes {
		cmds[i] = pipe.Exists(ctx, LiveKey(deploymentID, h))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("topic schemas: check live configurations: %w", err)
	}
	var live []string
	for i, cmd := range cmds {
		if cmd.Val() > 0 {
			live = append(live, hashes[i])
		}
	}
	return live, nil
}

// RetireBroken removes hashes from prev.Broken (per topic) and writes the
// result, only while the applied configuration is still exactly prev, as
// read by ReadApplied. It reports whether it wrote.
func RetireBroken(ctx context.Context, rdb redis.Cmdable, deploymentID string, prev *Applied, retire map[string][]string) (bool, error) {
	if prev == nil || prev.raw == "" {
		return false, errors.New("topic schemas: RetireBroken needs a configuration read by ReadApplied")
	}
	next := *prev
	next.Broken = make(map[string][]BrokenHash, len(prev.Broken))
	for topic, entries := range prev.Broken {
		kept := slices.DeleteFunc(slices.Clone(entries), func(b BrokenHash) bool {
			return slices.Contains(retire[topic], b.Hash)
		})
		if len(kept) > 0 {
			next.Broken[topic] = kept
		}
	}
	if len(next.Broken) == 0 {
		next.Broken = nil
	}
	return writeApplied(ctx, rdb, deploymentID, prev, &next, false)
}

// casAppliedScript replaces the applied configuration only while it is
// still ARGV[1] ("" for none) and, with ARGV[3] set, pushes it to the
// trimmed history in the same step.
var casAppliedScript = goredis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if not cur then cur = '' end
if cur ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2])
if ARGV[3] ~= '' then
	redis.call('LPUSH', KEYS[2], ARGV[3])
	redis.call('LTRIM', KEYS[2], 0, tonumber(ARGV[4]) - 1)
end
return 1
`)

func writeApplied(ctx context.Context, rdb redis.Cmdable, deploymentID string, prev, next *Applied, withHistory bool) (bool, error) {
	value, err := marshalNoEscape(next)
	if err != nil {
		return false, fmt.Errorf("topic schemas: encode applied configuration: %w", err)
	}
	var history []byte
	if withHistory {
		if history, err = marshalNoEscape(HistoryEntry{Hash: next.Hash, AppliedAt: next.AppliedAt}); err != nil {
			return false, fmt.Errorf("topic schemas: encode history entry: %w", err)
		}
	}
	keys := []string{AppliedKey(deploymentID), HistoryKey(deploymentID)}
	n, err := casAppliedScript.Run(ctx, rdb, keys, prev.rawValue(), value, history, HistoryLimit).Int()
	if err != nil {
		return false, fmt.Errorf("topic schemas: write applied configuration: %w", err)
	}
	if n != 1 {
		return false, nil
	}
	next.raw = string(value)
	return true, nil
}

type applier struct {
	rdb         redis.Cmdable
	dep         string
	current     Snapshot
	hash        string
	topicHashes map[string]string
	opts        ApplyOptions
}

func newApplier(rdb redis.Cmdable, deploymentID string, current Snapshot, opts ApplyOptions) *applier {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.LockWait <= 0 {
		opts.LockWait = defaultApplyLockWait
	}
	a := &applier{
		rdb:         rdb,
		dep:         deploymentID,
		current:     current,
		hash:        current.Hash(),
		topicHashes: make(map[string]string, len(current.Topics)),
		opts:        opts,
	}
	for name := range current.Topics {
		a.topicHashes[name] = current.TopicHash(name)
	}
	return a
}

// planFromStore reads the applied configuration and plans against it. next
// is the configuration to write, nil when there is nothing to write.
func (a *applier) planFromStore(ctx context.Context) (res ApplyResult, next, prev *Applied, err error) {
	prev, err = ReadApplied(ctx, a.rdb, a.dep)
	if err != nil {
		return ApplyResult{Hash: a.hash}, nil, nil, err
	}
	res, next, err = a.plan(ctx, prev)
	return res, next, prev, err
}

func (a *applier) plan(ctx context.Context, prev *Applied) (ApplyResult, *Applied, error) {
	res := ApplyResult{Hash: a.hash}
	now := a.opts.Now().UTC()
	if prev == nil {
		res.Initial = true
		return res, a.next(nil, now, nil), nil
	}
	if prev.Hash == a.hash {
		res.Unchanged = true
		return res, nil, nil
	}
	res.Ended = EndedTopics(prev.Snapshot, a.current)

	live, err := IsLive(ctx, a.rdb, a.dep, a.hash)
	if err != nil {
		return res, nil, err
	}
	changes, err := a.breakingChanges(ctx, prev.Snapshot)
	if live {
		// Another instance runs it: accepted as is. The diff is only
		// informative, so failing to compute it doesn't matter.
		res.KnownConfig = true
		if err == nil {
			res.Changes = changes
		}
		return res, nil, nil
	}
	if err != nil {
		return res, nil, err
	}
	res.Changes = changes

	allEnded := endsEveryTopic(prev.Snapshot, res.Ended)
	if !a.opts.AllowBreaking {
		if len(changes) > 0 {
			return res, nil, &BreakingChangeError{Message: msgBreakingChanges, Changes: changes}
		}
		if allEnded {
			return res, nil, &BreakingChangeError{Message: msgAllTopicsEnded, Changes: endedChanges(res.Ended)}
		}
	}
	res.Forced = len(changes) > 0 || allEnded
	var forced []string
	for _, c := range changes {
		forced = append(forced, c.Topic)
	}
	return res, a.next(prev, now, forced), nil
}

// breakingChanges diffs the topics MCP-enabled in both prev and current
// whose schema changed and that have live subscriptions.
func (a *applier) breakingChanges(ctx context.Context, prev Snapshot) ([]Change, error) {
	var topics []string
	for _, name := range slices.Sorted(maps.Keys(prev.Topics)) {
		if !prev.MCPServed(name) || !a.current.MCPServed(name) {
			continue
		}
		if prev.TopicHash(name) == a.topicHashes[name] {
			continue
		}
		if a.opts.LiveTopics != nil {
			live, err := a.opts.LiveTopics(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("topic schemas: check live subscriptions of %q: %w", name, err)
			}
			if !live {
				continue
			}
		}
		topics = append(topics, name)
	}
	if len(topics) == 0 {
		return nil, nil
	}
	return BreakingChanges(prev, a.current, topics), nil
}

// endsEveryTopic reports whether ended covers every MCP-enabled topic of
// prev, of which there is at least one.
func endsEveryTopic(prev Snapshot, ended []string) bool {
	served := 0
	for name := range prev.Topics {
		if prev.MCPServed(name) {
			served++
		}
	}
	return served > 0 && len(ended) == served
}

func endedChanges(ended []string) []Change {
	out := make([]Change, 0, len(ended))
	for _, topic := range ended {
		out = append(out, Change{Topic: topic, Kind: ChangeTopicEnded, Detail: "no longer MCP-enabled"})
	}
	return out
}

// next builds the configuration to record: current, the broken hashes of
// prev plus those of the forced topics, and the topic hash lineage.
func (a *applier) next(prev *Applied, now time.Time, forced []string) *Applied {
	n := &Applied{Snapshot: a.current, Hash: a.hash, AppliedAt: now}
	if prev == nil {
		return n
	}

	superseded := make(map[string][]string)
	for name, hashes := range prev.Superseded {
		if _, ok := a.current.Topics[name]; ok {
			superseded[name] = slices.Clone(hashes)
		}
	}
	for name := range prev.Snapshot.Topics {
		if _, ok := a.current.Topics[name]; !ok {
			continue
		}
		if old := prev.Snapshot.TopicHash(name); old != a.topicHashes[name] {
			list := append([]string{old}, slices.DeleteFunc(superseded[name], func(h string) bool { return h == old })...)
			superseded[name] = list[:min(len(list), maxSupersededHashes)]
		}
	}

	broken := make(map[string][]BrokenHash, len(prev.Broken))
	for topic, entries := range prev.Broken {
		broken[topic] = slices.Clone(entries)
	}
	for _, topic := range slices.Compact(slices.Sorted(slices.Values(forced))) {
		// The applied hash and every earlier one: subscriptions not
		// refreshed since an earlier additive change still carry those.
		hashes := append([]string{prev.Snapshot.TopicHash(topic)}, prev.Superseded[topic]...)
		for _, h := range hashes {
			if h == "" {
				continue
			}
			i := slices.IndexFunc(broken[topic], func(b BrokenHash) bool { return b.Hash == h })
			if i >= 0 {
				broken[topic][i].RecordedAt = now
			} else {
				broken[topic] = append(broken[topic], BrokenHash{Hash: h, RecordedAt: now})
			}
		}
	}

	// The schema being applied is never broken nor superseded, which
	// matters when a configuration comes back.
	for topic, entries := range broken {
		cur := a.topicHashes[topic]
		if entries = slices.DeleteFunc(entries, func(b BrokenHash) bool { return b.Hash == cur }); len(entries) > 0 {
			broken[topic] = entries
		} else {
			delete(broken, topic)
		}
	}
	for topic, hashes := range superseded {
		cur := a.topicHashes[topic]
		if hashes = slices.DeleteFunc(hashes, func(h string) bool { return h == cur }); len(hashes) > 0 {
			superseded[topic] = hashes
		} else {
			delete(superseded, topic)
		}
	}
	if len(broken) > 0 {
		n.Broken = broken
	}
	if len(superseded) > 0 {
		n.Superseded = superseded
	}
	return n
}

// lock takes the apply lock, waiting up to LockWait for it.
func (a *applier) lock(ctx context.Context) (func(), error) {
	key := ApplyLockKey(a.dep)
	l := redislock.New(a.rdb, redislock.WithKey(key), redislock.WithTTL(applyLockTTL))
	deadline := time.Now().Add(a.opts.LockWait)
	for {
		ok, err := l.AttemptLock(ctx)
		if err != nil {
			return nil, fmt.Errorf("topic schemas: take lock %s: %w", key, err)
		}
		if ok {
			return func() {
				uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
				defer cancel()
				_, _ = l.Unlock(uctx)
			}, nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("topic schemas: lock %s still held after %s", key, a.opts.LockWait)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(applyLockPoll):
		}
	}
}

func (a *applier) log(res ApplyResult, err error) {
	if a.opts.Logger == nil || err != nil {
		return
	}
	fields := []zap.Field{zap.String("hash", res.Hash)}
	if len(res.Ended) > 0 {
		fields = append(fields, zap.Strings("ended_topics", res.Ended))
	}
	if len(res.Changes) > 0 {
		fields = append(fields, zap.Strings("breaking_changes", changeLines(res.Changes)))
	}
	switch {
	case res.Unchanged:
		a.opts.Logger.Debug("topic schemas unchanged", fields...)
	case res.Initial:
		a.opts.Logger.Info("topic schemas applied (first time)", fields...)
	case res.KnownConfig:
		a.opts.Logger.Info("topic schemas accepted without a check: a running instance uses them; the applied ones are kept", fields...)
	case res.Forced:
		a.opts.Logger.Warn("topic schemas applied with TOPICS_ALLOW_BREAKING_CHANGES: subscriptions to the changed and ended topics will be ended; remove the setting", fields...)
	default:
		a.opts.Logger.Info("topic schemas applied", fields...)
	}
}

func changeLines(changes []Change) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, changeLine(c))
	}
	return out
}
