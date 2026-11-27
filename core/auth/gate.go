// Package auth 提供内网握手的共享签名/校验逻辑（gate↔game、game↔guild）。
package auth

import "time"

// GateSkew RegisterGate 时间戳允许的最大偏差（保留旧语义，等同 PeerSkew）
const GateSkew = PeerSkew

// SignGate 计算 RegisterGate 的 HMAC：base64(HMAC-SHA256(secret, gate_id|ts|nonce))
func SignGate(secret, gateID string, ts int64, nonce string) string {
	return SignPeer(secret, gateID, ts, nonce)
}

// VerifyGate 校验 RegisterGate 的 HMAC 与时间窗。secret 为空表示不校验（联调）。
func VerifyGate(secret, gateID string, ts int64, nonce, sig string, now time.Time) error {
	return VerifyPeer(secret, gateID, ts, nonce, sig, now)
}
