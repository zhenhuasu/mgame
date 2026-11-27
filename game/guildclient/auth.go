// Package guildclient game→guild 内网：连接池、实例发现、HMAC 握手、
// GuildS2C/GuildNotify 下行投递。
package guildclient

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/zhenhuasu/mgame/core/auth"

	pb "github.com/zhenhuasu/mgame/proto"
)

// guildSecret game↔guild 共享密钥（SetSecret 注入；空 = 不校验，仅联调）
var guildSecret string

// SetSecret 注入内网共享密钥，须在建连接前调用
func SetSecret(s string) {
	guildSecret = s
}

// signRegisterGame 构造带 HMAC 的握手消息；密钥为空则留空（联调）
func signRegisterGame(gameID string) *pb.RegisterGame {
	req := &pb.RegisterGame{GameId: gameID}
	if guildSecret == "" {
		return req
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return req
	}
	req.Ts = time.Now().Unix()
	req.Nonce = hex.EncodeToString(b[:])
	req.Hmac = auth.SignPeer(guildSecret, gameID, req.Ts, req.Nonce)
	return req
}
