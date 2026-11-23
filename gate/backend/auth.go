// Package backend 与 game 服务通信侧：连接池、实例发现、内网 HMAC 握手。
package backend

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/zhenhuasu/mgame/core/auth"

	pb "github.com/zhenhuasu/mgame/proto"
)

// gateSecret gate↔game 共享密钥（SetSecret 注入；空 = 不校验，仅联调）
var gateSecret string

// SetSecret 注入内网共享密钥，须在建连接前调用（gate 入口）
func SetSecret(s string) {
	gateSecret = s
}

// signRegisterGate 构造带 HMAC 的握手消息；gateSecret 为空则留空（联调）
func signRegisterGate(gateID string) *pb.RegisterGate {
	req := &pb.RegisterGate{GateId: gateID}
	if gateSecret == "" {
		return req
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return req
	}
	req.Ts = time.Now().Unix()
	req.Nonce = hex.EncodeToString(b[:])
	req.Hmac = auth.SignGate(gateSecret, gateID, req.Ts, req.Nonce)
	return req
}
