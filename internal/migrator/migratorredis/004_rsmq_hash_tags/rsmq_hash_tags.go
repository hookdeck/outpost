package migration_004_rsmq_hash_tags

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
	goredis "github.com/redis/go-redis/v9"
)

// queues are the rsmq queues used by the delivery retry scheduler.
var queues = []string{"deliverymq-retry", "deliverymq-retry-dlq"}

// moveBatchSize bounds the messages moved per script call, so a large queue
// doesn't block Redis for long. Each call is atomic.
const moveBatchSize = 1000

// moveScript moves up to ARGV[1] messages of one rsmq queue from the
// untagged keys to the hash-tagged keys, keeping each message's score (due
// time, also for messages currently being processed) and its receive count
// and first-receive time. A message whose ID already exists in the tagged
// queue was scheduled again by a newer instance and is dropped instead.
// Queue settings are copied to the tagged hash only where it doesn't have
// them; the untagged hash keeps them, so instances still running the
// previous version see a valid, empty queue.
// With ARGV[2] == "1", the untagged keys are deleted once the zset is empty.
//
// KEYS[1]: untagged zset, KEYS[2]: untagged hash, KEYS[3]: tagged zset,
// KEYS[4]: tagged hash.
// Returns {moved, dropped, remaining}.
var moveScript = goredis.NewScript(`
for _, f in ipairs({"vt", "delay", "maxsize", "created", "modified"}) do
	local v = redis.call("HGET", KEYS[2], f)
	if v then
		redis.call("HSETNX", KEYS[4], f, v)
	end
end
local moved, dropped = 0, 0
local batch = redis.call("ZRANGE", KEYS[1], 0, tonumber(ARGV[1]) - 1, "WITHSCORES")
for i = 1, #batch, 2 do
	local id, score = batch[i], batch[i + 1]
	local body = redis.call("HGET", KEYS[2], id)
	if body and not redis.call("ZSCORE", KEYS[3], id) then
		redis.call("ZADD", KEYS[3], score, id)
		redis.call("HSET", KEYS[4], id, body)
		for _, suffix in ipairs({":rc", ":fr"}) do
			local v = redis.call("HGET", KEYS[2], id .. suffix)
			if v then
				redis.call("HSET", KEYS[4], id .. suffix, v)
			else
				redis.call("HDEL", KEYS[4], id .. suffix)
			end
		end
		moved = moved + 1
	else
		dropped = dropped + 1
	end
	redis.call("ZREM", KEYS[1], id)
	redis.call("HDEL", KEYS[2], id, id .. ":rc", id .. ":fr")
end
local remaining = redis.call("ZCARD", KEYS[1])
if remaining == 0 and ARGV[2] == "1" then
	redis.call("DEL", KEYS[1], KEYS[2])
end
return {moved, dropped, remaining}
`)

// RSMQHashTagsMigration moves the delivery retry queues from the untagged rsmq
// keys (<ns>:<queue>, <ns>:<queue>:Q) to the hash-tagged keys
// (<ns>:{<prefix><queue>}, <ns>:{<prefix><queue>}:Q).
type RSMQHashTagsMigration struct {
	client    redis.Client
	logger    migratorredis.Logger
	ns        string // "rsmq:" or "<deployment_id>:rsmq:"
	tagPrefix string // "" or "<deployment_id>:"
}

var _ migratorredis.Migration = (*RSMQHashTagsMigration)(nil)

// New creates the migration. deploymentID is optional.
func New(client redis.Client, logger migratorredis.Logger, deploymentID string) *RSMQHashTagsMigration {
	m := &RSMQHashTagsMigration{client: client, logger: logger, ns: "rsmq:"}
	if deploymentID != "" {
		m.ns = deploymentID + ":rsmq:"
		m.tagPrefix = deploymentID + ":"
	}
	return m
}

type queueKeys struct {
	name                   string
	oldZset, oldHash       string
	taggedZset, taggedHash string
}

func (m *RSMQHashTagsMigration) keys(queue string) queueKeys {
	tagged := m.ns + "{" + m.tagPrefix + queue + "}"
	return queueKeys{
		name:       queue,
		oldZset:    m.ns + queue,
		oldHash:    m.ns + queue + ":Q",
		taggedZset: tagged,
		taggedHash: tagged + ":Q",
	}
}

func (m *RSMQHashTagsMigration) Name() string { return "004_rsmq_hash_tags" }

func (m *RSMQHashTagsMigration) Version() int { return 4 }

func (m *RSMQHashTagsMigration) Description() string {
	return "Move scheduled retries to hash-tagged rsmq keys (rsmq:{deliverymq-retry}) for Redis Cluster support"
}

// AutoRunnable: safe to run at any time; it only moves messages and leaves
// the old queue usable.
func (m *RSMQHashTagsMigration) AutoRunnable() bool { return true }

func (m *RSMQHashTagsMigration) IsApplicable(ctx context.Context) (bool, string) {
	if _, ok := m.client.(*goredis.ClusterClient); ok {
		return false, "Not needed - Redis Cluster (retry queues under the old keys never worked there)"
	}
	return true, ""
}

func (m *RSMQHashTagsMigration) Plan(ctx context.Context) (*migratorredis.Plan, error) {
	scope := make(map[string]int, len(queues))
	total := 0
	for _, queue := range queues {
		n, err := m.client.ZCard(ctx, m.keys(queue).oldZset).Result()
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", queue, err)
		}
		scope[queue] = int(n)
		total += int(n)
	}
	return &migratorredis.Plan{
		MigrationName:  m.Name(),
		Description:    m.Description(),
		Version:        "v4",
		Timestamp:      time.Now(),
		Scope:          scope,
		EstimatedItems: total,
	}, nil
}

func (m *RSMQHashTagsMigration) Apply(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
	state := &migratorredis.State{
		MigrationName: m.Name(),
		Phase:         "applied",
		StartedAt:     time.Now(),
		Progress:      migratorredis.Progress{TotalItems: plan.EstimatedItems},
	}
	for _, queue := range queues {
		moved, dropped, err := m.move(ctx, m.keys(queue), false)
		if err != nil {
			return nil, err
		}
		state.Progress.ProcessedItems += moved
		state.Progress.SkippedItems += dropped
	}
	completed := time.Now()
	state.CompletedAt = &completed
	return state, nil
}

// move runs moveScript until the untagged zset is empty.
func (m *RSMQHashTagsMigration) move(ctx context.Context, k queueKeys, deleteOld bool) (moved, dropped int, err error) {
	del := "0"
	if deleteOld {
		del = "1"
	}
	for {
		res, err := moveScript.Run(ctx, m.client,
			[]string{k.oldZset, k.oldHash, k.taggedZset, k.taggedHash},
			moveBatchSize, del,
		).Int64Slice()
		if err != nil {
			return moved, dropped, fmt.Errorf("move %s: %w", k.name, err)
		}
		moved += int(res[0])
		dropped += int(res[1])
		if res[2] == 0 || res[0]+res[1] == 0 {
			break
		}
	}
	if moved > 0 || dropped > 0 {
		m.logger.LogInfo(fmt.Sprintf("%s: moved %d messages, dropped %d already in the new queue", k.name, moved, dropped))
	}
	return moved, dropped, nil
}

// Verify reports messages still under the untagged keys: retries scheduled by
// instances running the previous version after Apply. Cleanup moves them.
func (m *RSMQHashTagsMigration) Verify(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
	result := &migratorredis.VerificationResult{Valid: true, Details: map[string]string{}}
	for _, queue := range queues {
		result.ChecksRun++
		k := m.keys(queue)
		n, err := m.client.ZCard(ctx, k.oldZset).Result()
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", queue, err)
		}
		result.Details[queue] = fmt.Sprintf("%d messages under the old keys", n)
		if n > 0 {
			result.Valid = false
			result.Issues = append(result.Issues, fmt.Sprintf(
				"%d messages under %s, scheduled by instances of the previous version; run 'outpost migrate cleanup %s'",
				n, k.oldZset, m.Name()))
			continue
		}
		result.ChecksPassed++
	}
	return result, nil
}

// PlanCleanup returns the number of untagged keys left.
func (m *RSMQHashTagsMigration) PlanCleanup(ctx context.Context) (int, error) {
	total := 0
	for _, queue := range queues {
		k := m.keys(queue)
		n, err := m.client.Exists(ctx, k.oldZset, k.oldHash).Result()
		if err != nil {
			return 0, fmt.Errorf("check %s: %w", queue, err)
		}
		total += int(n)
	}
	return total, nil
}

// Cleanup moves messages scheduled under the untagged keys since Apply
// (newest wins on the same ID) and deletes the untagged keys. Run it once no
// instance of the previous version is left.
func (m *RSMQHashTagsMigration) Cleanup(ctx context.Context, state *migratorredis.State) error {
	for _, queue := range queues {
		if _, _, err := m.move(ctx, m.keys(queue), true); err != nil {
			return err
		}
	}
	m.logger.LogInfo("Removed the old retry queue keys.")
	return nil
}
