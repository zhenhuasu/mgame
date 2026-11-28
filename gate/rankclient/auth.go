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

func signRegisterRank(gateID string) *pb.RegisterRank {
	req := &pb.RegisterRank{ClientId: gateID, Kind: "gate"}
	if rankSecret == "" {
		return req
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return req
	}
	req.Ts = time.Now().Unix()
	req.Nonce = hex.EncodeToString(b[:])
	req.Hmac = auth.SignPeer(rankSecret, gateID, req.Ts, req.Nonce)
	return req
}
