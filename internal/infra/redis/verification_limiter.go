package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/app"
)

const (
	verifyIPMax     = 10
	verifyDocMax    = 5
	verifyWindow    = 15 * time.Minute
	verifyCooldown  = 60 * time.Second
	verifyKeyPrefix = "verify"
)

// luaVerifyLimit evalúa de forma atómica el cooldown, el límite por IP y el
// límite por RUT+IP. Devuelve 0 (ok), 1 (IP), 2 (cooldown) o 3 (RUT).
const luaVerifyLimit = `
local cd = KEYS[1]
local ipk = KEYS[2]
local dk = KEYS[3]
local ipMax = tonumber(ARGV[1])
local docMax = tonumber(ARGV[2])
local window = tonumber(ARGV[3])
local cooldown = tonumber(ARGV[4])

if redis.call('EXISTS', cd) == 1 then return 2 end

local ipc = redis.call('INCR', ipk)
if ipc == 1 then redis.call('PEXPIRE', ipk, window) end
if ipc > ipMax then return 1 end

local dc = redis.call('INCR', dk)
if dc == 1 then redis.call('PEXPIRE', dk, window) end
if dc > docMax then return 3 end

redis.call('SET', cd, '1', 'PX', cooldown)
return 0
`

// AllowVerificationRequest implementa app.MemberVerificationLimiter con
// contadores compartidos entre instancias.
func (c *Coordinator) AllowVerificationRequest(ctx context.Context, ip, document string) (app.VerificationLimit, error) {
	tag := fmt.Sprintf("%s:%s:{v}", c.prefix, verifyKeyPrefix)
	pair := sha256.Sum256([]byte(strings.ToLower(document) + "|" + ip))
	keys := []string{
		tag + ":cd:" + hex.EncodeToString(pair[:16]),
		tag + ":ip:" + ip,
		tag + ":doc:" + hex.EncodeToString(pair[:16]),
	}
	res, err := c.verifyLimit.Run(ctx, c.client, keys,
		verifyIPMax, verifyDocMax, verifyWindow.Milliseconds(), verifyCooldown.Milliseconds(),
	).Int()
	if err != nil {
		return app.VerificationLimitOK, err
	}
	switch res {
	case 1:
		return app.VerificationLimitIP, nil
	case 2:
		return app.VerificationLimitCooldown, nil
	case 3:
		return app.VerificationLimitDocument, nil
	}
	return app.VerificationLimitOK, nil
}
