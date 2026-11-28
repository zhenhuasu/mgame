package handler

import (
	"context"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/rpc"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/auth"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/core/svcproc"
	"github.com/zhenhuasu/mgame/rank/model"
	"github.com/zhenhuasu/mgame/rank/service"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("rank.handler")

type Handler struct {
	mgr    *service.RankManager
	secret string

	kinds    map[int64]string
	sessions map[int64]cellnet.Session
}

func New(mgr *service.RankManager, rankSecret string) *Handler {
	return &Handler{
		mgr:      mgr,
		secret:   rankSecret,
		kinds:    make(map[int64]string),
		sessions: make(map[int64]cellnet.Session),
	}
}

func (h *Handler) Register() {
	net.Register("SessionAccepted", h.onAccepted)
	net.Register("SessionClosed", h.onClosed)
	net.Register(&pb.RegisterRank{}, h.onRegisterRank)
	net.Register(&pb.RankUpdate{}, h.onRankUpdate)
	net.Register(&pb.RankListReq{}, h.onRankList)
}

func (h *Handler) onAccepted(ev cellnet.Event) {
	log.Infof("peer connected id=%d", ev.Session().ID())
}

func (h *Handler) onClosed(ev cellnet.Event) {
	msg := ev.Message().(*cellnet.SessionClosed)
	sid := ev.Session().ID()
	delete(h.kinds, sid)
	delete(h.sessions, sid)
	log.Infof("peer disconnected id=%d reason=%v", sid, msg.Reason)
}

func (h *Handler) onRegisterRank(ev cellnet.Event) {
	msg := ev.Message().(*pb.RegisterRank)
	ses := ev.Session()

	if msg.Kind != "game" && msg.Kind != "gate" {
		log.Warnf("register rank rejected kind=%q session=%d", msg.Kind, ses.ID())
		ses.Send(&pb.RegisterRankAck{Code: 1})
		ses.Close()
		return
	}
	if err := auth.VerifyPeer(h.secret, msg.ClientId, msg.Ts, msg.Nonce, msg.Hmac, time.Now()); err != nil {
		log.Warnf("register rank rejected id=%q kind=%q session=%d: %v", msg.ClientId, msg.Kind, ses.ID(), err)
		ses.Send(&pb.RegisterRankAck{Code: 1})
		ses.Close()
		return
	}

	h.kinds[ses.ID()] = msg.Kind
	h.sessions[ses.ID()] = ses
	log.Infof("register rank id=%q kind=%q session=%d", msg.ClientId, msg.Kind, ses.ID())
	ses.Send(&pb.RegisterRankAck{Code: 0})
}

func (h *Handler) onRankUpdate(ev cellnet.Event) {
	ses := ev.Session()
	if h.kinds[ses.ID()] != "game" {
		log.Warnf("drop rank update from unauthenticated session=%d", ses.ID())
		return
	}
	msg := ev.Message().(*pb.RankUpdate)
	board, ok := toBoard(msg.Board)
	if !ok {
		log.Warnf("drop rank update: unsupported board=%d", msg.Board)
		return
	}

	profile := model.Profile{}
	if msg.Profile != nil {
		profile = model.Profile{
			RoleID: msg.Profile.RoleId,
			Name:   msg.Profile.Name,
			Avatar: msg.Profile.Avatar,
			Level:  msg.Profile.Level,
		}
	}
	if rc, ok := svcproc.ContextOf(ev); ok {
		if profile.RoleID == 0 {
			profile.RoleID = rc.RoleID
		}
		if profile.Name == "" {
			profile.Name = rc.RoleName
		}
		if profile.Level == 0 {
			profile.Level = rc.RoleLevel
		}
	}
	var mode model.UpdateMode
	switch msg.Mode {
	case pb.RankUpdateMode_RANK_UPDATE_DELTA:
		mode = model.UpdateDelta
	case pb.RankUpdateMode_RANK_UPDATE_OVERWRITE:
		mode = model.UpdateOverwrite
	default:
		log.Warnf("drop rank update: unsupported mode=%d", msg.Mode)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := h.mgr.Update(ctx, board, profile, mode, msg.Value)
	if err != nil {
		log.Warnf("rank update role=%d board=%d: %v", profile.RoleID, board, err)
		return
	}
	log.Infof("rank update role=%d board=%d score=%d old=%d new=%d",
		profile.RoleID, board, res.Score, res.OldRank, res.NewRank)
}

func (h *Handler) onRankList(ev cellnet.Event) {
	rpcEv, ok := ev.(*rpc.RecvMsgEvent)
	if !ok {
		return
	}
	ses := ev.Session()
	if h.kinds[ses.ID()] == "" {
		rpcEv.Reply(&pb.RankListAck{Code: 1, Reason: "not registered"})
		return
	}
	req := ev.Message().(*pb.RankListReq)
	board, ok := toBoard(req.Board)
	if !ok {
		rpcEv.Reply(&pb.RankListAck{Code: 1, Reason: "unsupported rank board"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := h.mgr.List(ctx, board, int(req.Offset), int(req.Limit), req.RoleId)
	if err != nil {
		log.Warnf("rank list board=%d: %v", req.Board, err)
		rpcEv.Reply(&pb.RankListAck{Code: 1, Reason: err.Error()})
		return
	}

	entries := make([]*pb.RankEntry, 0, len(res.Entries))
	for _, e := range res.Entries {
		entries = append(entries, &pb.RankEntry{
			Rank:   e.Rank,
			RoleId: e.RoleID,
			Name:   e.Name,
			Avatar: e.Avatar,
			Level:  e.Level,
			Score:  e.Score,
		})
	}
	rpcEv.Reply(&pb.RankListAck{
		Code:      0,
		Board:     req.Board,
		Entries:   entries,
		SelfRank:  res.SelfRank,
		SelfScore: res.SelfScore,
		Total:     res.Total,
	})
}

func (h *Handler) RankChanged(roleID int64, board model.Board, oldRank, newRank int32, score int64) {
	pbBoard, ok := toPBBoard(board)
	if !ok {
		return
	}
	for sid, ses := range h.sessions {
		if h.kinds[sid] != "game" {
			continue
		}
		ses.Send(&pb.RankChangeNotify{
			RoleId:  roleID,
			Board:   pbBoard,
			OldRank: oldRank,
			NewRank: newRank,
			Score:   score,
		})
	}
}

func toBoard(b pb.RankBoard) (model.Board, bool) {
	switch b {
	case pb.RankBoard_RANK_BOARD_POWER:
		return model.BoardPower, true
	case pb.RankBoard_RANK_BOARD_LEVEL:
		return model.BoardLevel, true
	default:
		return 0, false
	}
}

func toPBBoard(b model.Board) (pb.RankBoard, bool) {
	switch b {
	case model.BoardPower:
		return pb.RankBoard_RANK_BOARD_POWER, true
	case model.BoardLevel:
		return pb.RankBoard_RANK_BOARD_LEVEL, true
	default:
		return pb.RankBoard_RANK_BOARD_NONE, false
	}
}
