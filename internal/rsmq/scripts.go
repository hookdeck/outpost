package rsmq

// The popMessage LUA Script
//
// Parameters:
//
// KEYS[1]: the zset key
// KEYS[2]: the hash key (zset key + ":Q")
// ARGV[1]: the current time in ms
//
// * Find a message id
// * Get the message
// * Increase the rc (receive count)
// * Use hset to set the fr (first receive) time
// * Return the message and the counters
//
// Returns:
//
// {id, message, rc, fr}
const scriptPopMessage = `local msg = redis.call("ZRANGEBYSCORE", KEYS[1], "-inf", ARGV[1], "LIMIT", "0", "1")
if #msg == 0 then
	return {}
end
redis.call("HINCRBY", KEYS[2], "totalrecv", 1)
local mbody = redis.call("HGET", KEYS[2], msg[1])
local rc = redis.call("HINCRBY", KEYS[2], msg[1] .. ":rc", 1)
local o = {msg[1], mbody, rc}
if rc==1 then
	table.insert(o, ARGV[1])
else
	local fr = redis.call("HGET", KEYS[2], msg[1] .. ":fr")
	table.insert(o, fr)
end
redis.call("ZREM", KEYS[1], msg[1])
redis.call("HDEL", KEYS[2], msg[1], msg[1] .. ":rc", msg[1] .. ":fr")
return o`

// The receiveMessage LUA Script
//
// Self-contained: reads the queue's vt from the :Q hash and reads the clock
// with TIME inside the script, so a receive is a single round trip. (Upstream
// RSMQ fetched both with a preceding MULTI/HMGET/TIME/EXEC because Redis < 3.2
// replicated scripts verbatim, which made calling TIME inside a script unsafe.
// Effect replication has been the default since Redis 5.)
//
// Parameters:
//
// KEYS[1]: the zset key
// KEYS[2]: the hash key (zset key + ":Q")
// ARGV[1]: the vt in seconds, or "" to use the queue's configured vt
//
// * Resolve vt and the current time
// * Find a message id
// * Get the message
// * Increase the rc (receive count)
// * Use hset to set the fr (first receive) time
// * Return the message and the counters
//
// Returns one of:
//
// {"m", id, message, rc, fr}  a message was received
// {"e", hasNext, msUntilNext} nothing is due; hasNext is 0 for an empty
// .                           queue, otherwise msUntilNext is the time until
// .                           the earliest score in the zset
// {"n"}                       the queue does not exist
//
// The zset holds both not-yet-due messages and in-flight messages hidden by vt
// (a receive re-ZADDs at now+vt rather than removing), so the earliest score is
// the next moment this queue can yield a message in either case.
const scriptReceiveMessage = `local vt = redis.call("HGET", KEYS[2], "vt")
if not vt then
	return {"n"}
end
if ARGV[1] ~= "" then
	vt = ARGV[1]
end
local t = redis.call("TIME")
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local nowStr = string.format("%d", now)
local msg = redis.call("ZRANGEBYSCORE", KEYS[1], "-inf", nowStr, "LIMIT", "0", "1")
if #msg == 0 then
	local nxt = redis.call("ZRANGE", KEYS[1], 0, 0, "WITHSCORES")
	if #nxt == 0 then
		return {"e", 0, 0}
	end
	local d = math.floor(tonumber(nxt[2]) - now)
	if d < 0 then
		d = 0
	end
	return {"e", 1, d}
end
redis.call("ZADD", KEYS[1], string.format("%d", now + tonumber(vt) * 1000), msg[1])
redis.call("HINCRBY", KEYS[2], "totalrecv", 1)
local mbody = redis.call("HGET", KEYS[2], msg[1])
local rc = redis.call("HINCRBY", KEYS[2], msg[1] .. ":rc", 1)
local o = {"m", msg[1], mbody, rc}
if rc==1 then
	redis.call("HSET", KEYS[2], msg[1] .. ":fr", nowStr)
	table.insert(o, nowStr)
else
	local fr = redis.call("HGET", KEYS[2], msg[1] .. ":fr")
	table.insert(o, fr)
end
return o`

// The changeMessageVisibility LUA Script
//
// Parameters:
//
// KEYS[1]: the zset key
// KEYS[2]: the message id
//
// * Find the message id
// * Set the new timer
//
// Returns:
//
// 0 or 1
const scriptChangeMessageVisibility = `local msg = redis.call("ZSCORE", KEYS[1], ARGV[1])
if not msg then
	return 0
end
redis.call("ZADD", KEYS[1], ARGV[2], ARGV[1])
return 1`
