package rankclient

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/zhenhuasu/mgame/core/auth"

	pb "github.com/zhenhuasu/mgame/proto"
)

var rankSecret string

func SetSecret(s string) {
	rankSecret = s
}

func signRegisterRank(gameID string) *pb.RegisterRank {
	req := &pb.RegisterRank{ClientId: gameID, Kind: "game"}
	if rankSecret == "" {
		return req
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return req
	}
	req.Ts = time.Now().Unix()
	req.Nonce = hex.EncodeToString(b[:])
	req.Hmac = auth.SignPeer(rankSecret, gameID, req.Ts, req.Nonce)
	return req
}
