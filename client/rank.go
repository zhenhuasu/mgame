package main

import (
	"time"

	"github.com/davyxu/cellnet"

	pb "github.com/zhenhuasu/mgame/proto"
)

func runRank(conn cellnet.Session, op, board string, offset, limit int, role *pb.RoleInfo,
	waitFor func(string, time.Duration) (interface{}, bool), exit func(int)) {
	if op != "list" {
		log.Errorf("unknown -rank %q", op)
		exit(1)
	}
	b, ok := parseRankBoard(board)
	if !ok {
		log.Errorf("bad -rank-board %q (power|level)", board)
		exit(1)
	}
	var roleID int64
	if role != nil {
		roleID = role.RoleId
	}
	conn.Send(&pb.RankListReq{
		Board:  b,
		Offset: int32(offset),
		Limit:  int32(limit),
		RoleId: roleID,
	})
	raw, ok := waitFor("ranklistack", 3*time.Second)
	if !ok {
		log.Errorf("rank list ack timeout")
		exit(1)
	}
	ack := raw.(*pb.RankListAck)
	if ack.Code != 0 {
		log.Errorf("rank list failed code=%d reason=%q", ack.Code, ack.Reason)
		exit(1)
	}
	log.Infof("rank board=%d total=%d self_rank=%d self_score=%d", ack.Board, ack.Total, ack.SelfRank, ack.SelfScore)
	for _, e := range ack.Entries {
		log.Infof("  #%d role=%d name=%q level=%d score=%d avatar=%q", e.Rank, e.RoleId, e.Name, e.Level, e.Score, e.Avatar)
	}
}

func parseRankBoard(s string) (pb.RankBoard, bool) {
	switch s {
	case "power":
		return pb.RankBoard_RANK_BOARD_POWER, true
	case "level":
		return pb.RankBoard_RANK_BOARD_LEVEL, true
	}
	return pb.RankBoard_RANK_BOARD_NONE, false
}
