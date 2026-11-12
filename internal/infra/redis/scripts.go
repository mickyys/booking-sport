package redis

// luaPrelude is shared by every script. Metadata layout per occupancy:
//
//	start_ms|end_ms|state|token|owner_ref|expires_ms|booking_id|version
//
// All keys of a space share one hash tag, so each script touches a single
// slot. On Upstash the scripts are prefixed with allow-key-locking so they
// lock only those keys instead of the whole database.
const luaPrelude = `
local function split(s)
  local t, i = {}, 1
  while true do
    local j = string.find(s, '|', i, true)
    if not j then t[#t + 1] = string.sub(s, i); break end
    t[#t + 1] = string.sub(s, i, j - 1)
    i = j + 1
  end
  return t
end

local function nowMs()
  local t = redis.call('TIME')
  return tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
end

local function join(f)
  return table.concat(f, '|')
end

-- checkGen rejects a writer whose generation differs from the one published
-- for this space. A space without a published generation is generation 0.
local function checkGen(g, expected)
  local current = redis.call('GET', g)
  if not current then current = '0' end
  return current == expected
end

-- fetch validates existence, expiry and token. Returns code, fields.
local function fetch(z, m, id, token, now)
  local raw = redis.call('HGET', m, id)
  if not raw then return -4, nil end
  local f = split(raw)
  if f[3] == 'held' and tonumber(f[6]) > 0 and tonumber(f[6]) <= now then
    redis.call('ZREM', z, id)
    redis.call('HDEL', m, id)
    return -4, nil
  end
  if f[4] ~= token then return -2, nil end
  return 0, f
end
`

// KEYS: z, m, g
// ARGV: id, startMs, endMs, token, ownerRef, ttlMs, maxDurationMs, generation
const luaAcquire = luaPrelude + `
local z, m, g = KEYS[1], KEYS[2], KEYS[3]
if not checkGen(g, ARGV[8]) then return {-9} end
local id, startMs, endMs = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
local ttl, maxDur = tonumber(ARGV[6]), tonumber(ARGV[7])
local now = nowMs()

local candidates = redis.call('ZRANGEBYSCORE', z, '(' .. (startMs - maxDur), '(' .. endMs)
for _, cid in ipairs(candidates) do
  local raw = redis.call('HGET', m, cid)
  if not raw then
    redis.call('ZREM', z, cid)
  else
    local f = split(raw)
    if f[3] == 'held' and tonumber(f[6]) > 0 and tonumber(f[6]) <= now then
      redis.call('ZREM', z, cid)
      redis.call('HDEL', m, cid)
    elseif tonumber(f[2]) > startMs then
      return {-1}
    end
  end
end

local expires = now + ttl
redis.call('ZADD', z, startMs, id)
redis.call('HSET', m, id, join({ARGV[2], ARGV[3], 'held', ARGV[4], ARGV[5], tostring(expires), '', '1'}))
return {0, expires}
`

// ARGV: id, token, ttlMs
const luaRenew = luaPrelude + `
local z, m, g = KEYS[1], KEYS[2], KEYS[3]
local now = nowMs()
local code, f = fetch(z, m, ARGV[1], ARGV[2], now)
if code ~= 0 then return {code} end
if f[3] ~= 'held' then return {-3} end
f[6] = tostring(now + tonumber(ARGV[3]))
f[8] = tostring(tonumber(f[8]) + 1)
redis.call('HSET', m, ARGV[1], join(f))
return {0, tonumber(f[6])}
`

// ARGV: id, token
const luaBeginCommit = luaPrelude + `
local z, m, g = KEYS[1], KEYS[2], KEYS[3]
local now = nowMs()
local code, f = fetch(z, m, ARGV[1], ARGV[2], now)
if code ~= 0 then return {code} end
if f[3] ~= 'held' then return {-3} end
f[3] = 'committing'
f[6] = '0'
f[8] = tostring(tonumber(f[8]) + 1)
redis.call('HSET', m, ARGV[1], join(f))
return {0}
`

// ARGV: id, token, bookingId
const luaCommit = luaPrelude + `
local z, m, g = KEYS[1], KEYS[2], KEYS[3]
local now = nowMs()
local code, f = fetch(z, m, ARGV[1], ARGV[2], now)
if code ~= 0 then return {code} end
if f[3] ~= 'committing' then return {-3} end
f[3] = 'booked'
f[6] = '0'
f[7] = ARGV[3]
f[8] = tostring(tonumber(f[8]) + 1)
redis.call('HSET', m, ARGV[1], join(f))
return {0}
`

// ARGV: id, token
const luaRelease = luaPrelude + `
local z, m, g = KEYS[1], KEYS[2], KEYS[3]
local now = nowMs()
local code = fetch(z, m, ARGV[1], ARGV[2], now)
if code ~= 0 then return {code} end
redis.call('ZREM', z, ARGV[1])
redis.call('HDEL', m, ARGV[1])
return {0}
`

// ARGV: id, startMs, endMs, state, token, ownerRef, expiresMs, bookingId, maxDurationMs, generation
const luaImport = luaPrelude + `
local z, m, g = KEYS[1], KEYS[2], KEYS[3]
if not checkGen(g, ARGV[10]) then return {-9} end
local id, startMs, endMs = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
local maxDur = tonumber(ARGV[9])
if redis.call('HEXISTS', m, id) == 1 then return {1} end
local now = nowMs()

local candidates = redis.call('ZRANGEBYSCORE', z, '(' .. (startMs - maxDur), '(' .. endMs)
for _, cid in ipairs(candidates) do
  local raw = redis.call('HGET', m, cid)
  if not raw then
    redis.call('ZREM', z, cid)
  else
    local f = split(raw)
    if f[3] == 'held' and tonumber(f[6]) > 0 and tonumber(f[6]) <= now then
      redis.call('ZREM', z, cid)
      redis.call('HDEL', m, cid)
    elseif tonumber(f[2]) > startMs then
      return {-1}
    end
  end
end

redis.call('ZADD', z, startMs, id)
redis.call('HSET', m, id, join({ARGV[2], ARGV[3], ARGV[4], ARGV[5], ARGV[6], ARGV[7], ARGV[8], '1'}))
return {0}
`

const upstashFlags = "#!lua flags=allow-key-locking\n"
