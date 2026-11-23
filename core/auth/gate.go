// Package auth 提供 gate↔game 内网握手的共享签名/校验逻辑。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// GateSkew RegisterGate 时间戳允许的最大偏差
const GateSkew = 30 * time.Second

// SignGate 计算 RegisterGate 的 HMAC：base64(HMAC-SHA256(secret, gate_id|ts|nonce))
func SignGate(secret, gateID string, ts int64, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(gateID + "|" + strconv.FormatInt(ts, 10) + "|" + nonce))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyGate 校验 RegisterGate 的 HMAC 与时间窗。secret 为空表示不校验（联调）。
func VerifyGate(secret, gateID string, ts int64, nonce, sig string, now time.Time) error {
	if secret == "" {
		return nil
	}
	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	skew := int64(GateSkew / time.Second)
	if delta := now.Unix() - ts; delta < -skew || delta > skew {
		return fmt.Errorf("timestamp out of window")
	}
	want := SignGate(secret, gateID, ts, nonce)
	if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) != 1 {
		return fmt.Errorf("bad hmac")
	}
	return nil
}
