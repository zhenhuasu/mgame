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

// PeerSkew 内网握手时间戳允许的最大偏差
const PeerSkew = 30 * time.Second

// SignPeer 计算内网握手的 HMAC：base64(HMAC-SHA256(secret, peer_id|ts|nonce))
// 适用于 gate↔game（RegisterGate）与 game↔guild（RegisterGame）等场景。
func SignPeer(secret, peerID string, ts int64, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(peerID + "|" + strconv.FormatInt(ts, 10) + "|" + nonce))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyPeer 校验内网握手 HMAC 与时间窗。secret 为空表示不校验（联调）。
func VerifyPeer(secret, peerID string, ts int64, nonce, sig string, now time.Time) error {
	if secret == "" {
		return nil
	}
	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	skew := int64(PeerSkew / time.Second)
	if delta := now.Unix() - ts; delta < -skew || delta > skew {
		return fmt.Errorf("timestamp out of window")
	}
	want := SignPeer(secret, peerID, ts, nonce)
	if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) != 1 {
		return fmt.Errorf("bad hmac")
	}
	return nil
}
