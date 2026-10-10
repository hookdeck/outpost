package redistenantstore

import (
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

// Lua scripts for the conditional writes. Portability rules (Redis 6+ on Lua
// 5.1, Dragonfly on Lua 5.4, miniredis):
//   - every key a script touches is passed in KEYS (Dragonfly rejects
//     undeclared keys), and all KEYS of one script share the tenant's hash tag;
//   - numbers read back from Redis are compared with tonumber() on both sides
//     (Dragonfly hands ZSCORE to Lua as a number, Redis as a string);
//   - numbers are never formatted in Lua (Lua 5.1 prints 15-digit values in
//     exponent form): scripts return the raw strings they read;
//   - unpack is table.unpack on Lua 5.4.

// scriptCreateDestination creates a destination unless a live one has the
// same ID, a revocation guard, a deleted tenant or a limit rejects it. A
// missing tenant hash doesn't: destinations may be written without one.
//
// KEYS[1] destination hash, KEYS[2] summary hash, KEYS[3] bucket registry,
// KEYS[4] tenant hash, KEYS[5 .. 4+nb] buckets the destination joins,
// KEYS[5+nb .. 4+2nb] their sweep markers,
// KEYS[5+2nb .. 4+2nb+ng] buckets of the separately-limited types,
// KEYS[5+2nb+ng ..] fences.
//
// ARGV[1] destination ID, ARGV[2] summary JSON, ARGV[3] recorded buckets JSON
// ("" for none), ARGV[4] not-deleted-since ms ("" when unchecked), ARGV[5]
// general limit ("" when the type has its own limit), ARGV[6] nb, ARGV[7] ng,
// ARGV[8] nset, ARGV[9] ndel, ARGV[10] sweep interval ms, then nb bucket
// limits, nb bucket names, nset field/value arguments and ndel field names.
//
// Returns {"ok"}, {"duplicate", expires_at}, {"revoked"},
// {"tenant_deleted"}, {"limit", i} (i-th bucket) or {"max"}.
const scriptCreateDestination = `
local unpack = unpack or table.unpack
local dest, summary, registry, tenant = KEYS[1], KEYS[2], KEYS[3], KEYS[4]
local id = ARGV[1]
local nb, ng = tonumber(ARGV[6]), tonumber(ARGV[7])
local nset, ndel = tonumber(ARGV[8]), tonumber(ARGV[9])

local cur = redis.call('HMGET', dest, 'deleted_at', 'deleted_reason', 'expires_at')
if redis.call('EXISTS', dest) == 1 and not cur[1] then
	return {'duplicate', cur[3] or ''}
end

local since = tonumber(ARGV[4])
if since then
	if cur[1] and cur[2] ~= 'expired' then
		local deletedAt = tonumber(cur[1])
		if deletedAt == nil or deletedAt >= since then
			return {'revoked'}
		end
	end
	for i = 5 + 2 * nb + ng, #KEYS do
		local fence = redis.call('GET', KEYS[i])
		if fence and (tonumber(fence) == nil or tonumber(fence) >= since) then
			return {'revoked'}
		end
	end
end

if redis.call('HEXISTS', tenant, 'deleted_at') == 1 then
	return {'tenant_deleted'}
end

for i = 1, nb do
	local max = tonumber(ARGV[10 + i])
	local bucket = KEYS[4 + i]
	if max and max > 0 and redis.call('SISMEMBER', bucket, id) == 0 then
		local n = redis.call('SCARD', bucket)
		-- Drop members no longer listed (left by an interrupted cleanup)
		-- before refusing, at most once per interval: the sweep reads the
		-- whole bucket and almost never finds any.
		if n >= max and redis.call('SET', KEYS[4 + nb + i], '1', 'PX', ARGV[10], 'NX') then
			for _, member in ipairs(redis.call('SMEMBERS', bucket)) do
				if redis.call('HEXISTS', summary, member) == 0 then
					redis.call('SREM', bucket, member)
					n = n - 1
				end
			end
		end
		if n >= max then
			return {'limit', tostring(i)}
		end
	end
end

local generalMax = tonumber(ARGV[5])
if generalMax then
	local n = redis.call('HLEN', summary) - redis.call('HEXISTS', summary, id)
	for i = 1, ng do
		n = n - redis.call('SCARD', KEYS[4 + 2 * nb + i])
	end
	if n >= generalMax then
		return {'max'}
	end
end

local base = 11 + 2 * nb
redis.call('PERSIST', dest)
redis.call('HDEL', dest, 'deleted_at', 'deleted_reason', 'buckets')
if nset > 0 then
	redis.call('HSET', dest, unpack(ARGV, base, base + nset - 1))
end
if ndel > 0 then
	redis.call('HDEL', dest, unpack(ARGV, base + nset, base + nset + ndel - 1))
end
if ARGV[3] ~= '' then
	redis.call('HSET', dest, 'buckets', ARGV[3])
end
redis.call('HSET', summary, id, ARGV[2])
for i = 1, nb do
	redis.call('SADD', KEYS[4 + i], id)
	redis.call('SADD', registry, ARGV[10 + nb + i])
end
return {'ok'}
`

// scriptUpdateDestinationIfLive overwrites a live destination of the expected
// generation.
//
// KEYS[1] destination hash, KEYS[2] summary hash, KEYS[3] parked retries,
// KEYS[4] resume set, KEYS[5 ..] fences.
//
// ARGV[1] destination ID, ARGV[2] summary JSON, ARGV[3] expected created_at
// ms, ARGV[4] not-deleted-since ms ("" when unchecked), ARGV[5] "1" to move
// the parked retries to KEYS[4], ARGV[6] nset, ARGV[7] ndel, then nset
// field/value arguments and ndel field names.
//
// Returns {"ok", was_disabled ("1" or ""), resume key or ""},
// {"not_found"}, {"deleted"}, {"conflict", expires_at} or
// {"revoked", expires_at}.
const scriptUpdateDestinationIfLive = `
local unpack = unpack or table.unpack
local dest = KEYS[1]
local nset, ndel = tonumber(ARGV[6]), tonumber(ARGV[7])

local cur = redis.call('HMGET', dest, 'deleted_at', 'created_at', 'disabled_at', 'expires_at')
if redis.call('EXISTS', dest) == 0 then
	return {'not_found'}
end
if cur[1] then
	return {'deleted'}
end
local createdAt = tonumber(cur[2])
if createdAt == nil or createdAt ~= tonumber(ARGV[3]) then
	return {'conflict', cur[4] or ''}
end
local since = tonumber(ARGV[4])
if since then
	for i = 5, #KEYS do
		local fence = redis.call('GET', KEYS[i])
		if fence and (tonumber(fence) == nil or tonumber(fence) >= since) then
			return {'revoked', cur[4] or ''}
		end
	end
end

local base = 8
if nset > 0 then
	redis.call('HSET', dest, unpack(ARGV, base, base + nset - 1))
end
if ndel > 0 then
	redis.call('HDEL', dest, unpack(ARGV, base + nset, base + nset + ndel - 1))
end
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])

local resumed = ''
if ARGV[5] == '1' and redis.call('EXISTS', KEYS[3]) == 1 then
	redis.call('RENAME', KEYS[3], KEYS[4])
	resumed = KEYS[4]
end
local wasDisabled = ''
if cur[3] then
	wasDisabled = '1'
end
return {'ok', wasDisabled, resumed}
`

// scriptDisableDestination sets disabled_at on a live destination and flips
// its summary entry, provided the entry still reads as the caller saw it.
//
// KEYS[1] destination hash, KEYS[2] summary hash.
//
// ARGV[1] destination ID, ARGV[2] disabled_at ms, ARGV[3] summary entry read
// by the caller ("" when absent), ARGV[4] the entry to write ("" to leave an
// absent entry absent).
//
// Returns "disabled", "unchanged", "not_found", "deleted" or "retry".
const scriptDisableDestination = `
local cur = redis.call('HMGET', KEYS[1], 'deleted_at', 'disabled_at')
if redis.call('EXISTS', KEYS[1]) == 0 then
	return 'not_found'
end
if cur[1] then
	return 'deleted'
end
if cur[2] then
	return 'unchanged'
end
local entry = redis.call('HGET', KEYS[2], ARGV[1]) or ''
if entry ~= ARGV[3] then
	return 'retry'
end
redis.call('HSET', KEYS[1], 'disabled_at', ARGV[2])
if ARGV[4] ~= '' then
	redis.call('HSET', KEYS[2], ARGV[1], ARGV[4])
end
return 'disabled'
`

// scriptEnableDestination clears disabled_at on a live destination and flips
// its summary entry, provided the entry still reads as the caller saw it, and
// optionally moves its parked retries to a resume set.
//
// KEYS[1] destination hash, KEYS[2] summary hash, KEYS[3] parked retries,
// KEYS[4] resume set.
//
// ARGV[1] destination ID, ARGV[2] summary entry read by the caller ("" when
// absent), ARGV[3] the entry to write ("" to leave an absent entry absent),
// ARGV[4] "1" to move the parked retries to KEYS[4].
//
// Returns {"ok", was_disabled ("1" or ""), resume key or ""}, {"not_found"},
// {"deleted"} or {"retry"}.
const scriptEnableDestination = `
local cur = redis.call('HMGET', KEYS[1], 'deleted_at', 'disabled_at')
if redis.call('EXISTS', KEYS[1]) == 0 then
	return {'not_found'}
end
if cur[1] then
	return {'deleted'}
end
local wasDisabled = ''
if cur[2] then
	local entry = redis.call('HGET', KEYS[2], ARGV[1]) or ''
	if entry ~= ARGV[2] then
		return {'retry'}
	end
	redis.call('HDEL', KEYS[1], 'disabled_at')
	if ARGV[3] ~= '' then
		redis.call('HSET', KEYS[2], ARGV[1], ARGV[3])
	end
	wasDisabled = '1'
end
local resumed = ''
if ARGV[4] == '1' and redis.call('EXISTS', KEYS[3]) == 1 then
	redis.call('RENAME', KEYS[3], KEYS[4])
	resumed = KEYS[4]
end
return {'ok', wasDisabled, resumed}
`

// scriptDeleteDestinationIf tombstones a live destination matching the
// condition, removes it from the summary and its buckets (and the names of
// the buckets it empties from the registry) and drops its parked retries.
// The tombstone keeps what the deleted checks read, not the configuration,
// secrets, filter or metadata.
//
// KEYS[1] destination hash, KEYS[2] summary hash, KEYS[3] parked retries,
// KEYS[4] bucket registry, KEYS[5 ..] the buckets recorded on the
// destination as read by the caller.
//
// ARGV[1] destination ID, ARGV[2] deleted_at ms, ARGV[3] tombstone TTL in
// seconds, ARGV[4] reason, ARGV[5] required type, ARGV[6] expected created_at
// ms, ARGV[7] expired-before ms (each "" when unchecked), ARGV[8] the recorded
// buckets as read by the caller ("" when absent), then the names of the
// buckets of KEYS[5 ..].
//
// Returns {status, type, topics, expires_at} with status "deleted", "live",
// "gone" (tombstone) or "missing", or {"retry"} when the recorded buckets
// changed since the caller read them.
const scriptDeleteDestinationIf = `
local dest = KEYS[1]
local f = redis.call('HMGET', dest, 'deleted_at', 'type', 'topics', 'created_at', 'expires_at', 'buckets')
if redis.call('EXISTS', dest) == 0 then
	return {'missing', '', '', ''}
end
local typ, topics, expiresAt = f[2] or '', f[3] or '', f[5] or ''
if f[1] then
	return {'gone', typ, topics, expiresAt}
end

local live = {'live', typ, topics, expiresAt}
if ARGV[5] ~= '' and typ ~= ARGV[5] then
	return live
end
if ARGV[6] ~= '' then
	local createdAt = tonumber(f[4])
	if createdAt == nil or createdAt ~= tonumber(ARGV[6]) then
		return live
	end
end
if ARGV[7] ~= '' then
	if not f[5] then
		return live
	end
	-- An unreadable expiry counts as expired (fail closed).
	local e = tonumber(f[5])
	if e ~= nil and e > tonumber(ARGV[7]) then
		return live
	end
end
if (f[6] or '') ~= ARGV[8] then
	return {'retry'}
end

redis.call('HDEL', KEYS[2], ARGV[1])
redis.call('HSET', dest, 'deleted_at', ARGV[2])
if ARGV[4] ~= '' then
	redis.call('HSET', dest, 'deleted_reason', ARGV[4])
else
	redis.call('HDEL', dest, 'deleted_reason')
end
redis.call('HDEL', dest, 'config', 'credentials', 'delivery_metadata', 'metadata', 'filter')
redis.call('EXPIRE', dest, ARGV[3])
for i = 5, #KEYS do
	redis.call('SREM', KEYS[i], ARGV[1])
	if redis.call('SCARD', KEYS[i]) == 0 then
		redis.call('SREM', KEYS[4], ARGV[4 + i])
	end
end
redis.call('DEL', KEYS[3])
return {'deleted', typ, topics, expiresAt}
`

// scriptDeleteTenant tombstones a tenant and the destinations of its summary
// as read by the caller (keeping what scriptDeleteDestinationIf keeps), and
// drops the summary, the buckets, the registry and the parked retries.
//
// KEYS[1] tenant hash, KEYS[2] summary hash, KEYS[3] bucket registry,
// KEYS[4 .. 3+nd] destination hashes, KEYS[4+nd .. 3+2nd] their parked
// retries, KEYS[4+2nd ..] the buckets of the registry.
//
// ARGV[1] deleted_at ms, ARGV[2] tombstone TTL in seconds, ARGV[3]
// destinations' deleted_reason, ARGV[4] nd, then nd destination IDs and the
// bucket names of KEYS[4+2nd ..].
//
// Returns "deleted", "not_found", or "retry" when the summary or the registry
// changed since the caller read them.
const scriptDeleteTenant = `
local tenant, summary, registry = KEYS[1], KEYS[2], KEYS[3]
local nd = tonumber(ARGV[4])
local nb = #KEYS - 3 - 2 * nd

if redis.call('EXISTS', tenant) == 0 then
	return 'not_found'
end
if redis.call('HLEN', summary) ~= nd or redis.call('SCARD', registry) ~= nb then
	return 'retry'
end
for i = 1, nd do
	if redis.call('HEXISTS', summary, ARGV[4 + i]) == 0 then
		return 'retry'
	end
end
for i = 1, nb do
	if redis.call('SISMEMBER', registry, ARGV[4 + nd + i]) == 0 then
		return 'retry'
	end
end

for i = 1, nd do
	local dest = KEYS[3 + i]
	redis.call('HSET', dest, 'deleted_at', ARGV[1], 'deleted_reason', ARGV[3])
	redis.call('HDEL', dest, 'config', 'credentials', 'delivery_metadata', 'metadata', 'filter')
	redis.call('EXPIRE', dest, ARGV[2])
	redis.call('DEL', KEYS[3 + nd + i])
end
for i = 4 + 2 * nd, #KEYS do
	redis.call('DEL', KEYS[i])
end
redis.call('DEL', registry, summary)
redis.call('HSET', tenant, 'deleted_at', ARGV[1])
redis.call('EXPIRE', tenant, ARGV[2])
return 'deleted'
`

// scriptWriteFence moves a fence forward.
//
// KEYS[1] fence. ARGV[1] fence time ms, ARGV[2] TTL ms.
const scriptWriteFence = `
local cur = tonumber(redis.call('GET', KEYS[1]) or '')
if cur == nil or tonumber(ARGV[1]) > cur then
	redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
else
	redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 1
`

// scriptParkRetry parks a retry while the destination is live and disabled.
//
// KEYS[1] destination hash, KEYS[2] parked retries.
// ARGV[1] member, ARGV[2] max members, ARGV[3] expire-at ms.
//
// Returns "parked", "enabled", "gone" or "full".
const scriptParkRetry = `
local f = redis.call('HMGET', KEYS[1], 'deleted_at', 'disabled_at')
if redis.call('EXISTS', KEYS[1]) == 0 or f[1] then
	return 'gone'
end
if not f[2] then
	return 'enabled'
end
if redis.call('SISMEMBER', KEYS[2], ARGV[1]) == 0 then
	if redis.call('SCARD', KEYS[2]) >= tonumber(ARGV[2]) then
		return 'full'
	end
	redis.call('SADD', KEYS[2], ARGV[1])
end
redis.call('PEXPIREAT', KEYS[2], ARGV[3])
return 'parked'
`

// scriptRemoveIndexed removes an index member whose score still equals
// ARGV[2]. KEYS[1] index, ARGV[1] member, ARGV[2] expected score.
const scriptRemoveIndexed = `
local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
if score and tonumber(score) == tonumber(ARGV[2]) then
	return redis.call('ZREM', KEYS[1], ARGV[1])
end
return 0
`

// scriptRescoreIndexed moves an index member from score ARGV[2] to ARGV[3].
// KEYS[1] index, ARGV[1] member.
const scriptRescoreIndexed = `
local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
if score and tonumber(score) == tonumber(ARGV[2]) then
	redis.call('ZADD', KEYS[1], ARGV[3], ARGV[1])
	return 1
end
return 0
`

var (
	createDestinationScript       = goredis.NewScript(scriptCreateDestination)
	updateDestinationIfLiveScript = goredis.NewScript(scriptUpdateDestinationIfLive)
	disableDestinationScript      = goredis.NewScript(scriptDisableDestination)
	enableDestinationScript       = goredis.NewScript(scriptEnableDestination)
	deleteDestinationIfScript     = goredis.NewScript(scriptDeleteDestinationIf)
	deleteTenantScript            = goredis.NewScript(scriptDeleteTenant)
	writeFenceScript              = goredis.NewScript(scriptWriteFence)
	parkRetryScript               = goredis.NewScript(scriptParkRetry)
	removeIndexedScript           = goredis.NewScript(scriptRemoveIndexed)
	rescoreIndexedScript          = goredis.NewScript(scriptRescoreIndexed)
)

// scriptReply converts a script's array reply.
func scriptReply(cmd *goredis.Cmd) ([]string, error) {
	res, err := cmd.StringSlice()
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("empty script reply")
	}
	return res, nil
}
