package rankclient

import (
	"context"
	"sync"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/discover"
	"github.com/zhenhuasu/mgame/core/net"
	"github.com/zhenhuasu/mgame/game/model"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("game.rankclient")

type Forwarder interface {
	NotifyRankChange(roleID int64, board pb.RankBoard, oldRank, newRank int32, score int64)
}

type Pool struct {
	gameID string
	dis    discover.Discovery
	fwd    Forwarder

	mu    sync.Mutex
	conns map[string]cellnet.Peer
	up    map[string]bool
	rr    int
}

func New(gameID string, dis discover.Discovery) *Pool {
	return &Pool{
		gameID: gameID,
		dis:    dis,
		conns:  make(map[string]cellnet.Peer),
		up:     make(map[string]bool),
	}
}

func (g *Pool) SetForwarder(f Forwarder) {
	g.mu.Lock()
	g.fwd = f
	g.mu.Unlock()
}

func (g *Pool) Start(ctx context.Context) {
	registerHandlers(g)
	g.dis.Watch(ctx, g.reconcile)
}

func (g *Pool) reconcile(addrs []string) {
	want := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if a != "" {
			want[a] = true
		}
	}

	g.mu.Lock()
	var added, removed []string
	for a := range want {
		if _, ok := g.conns[a]; !ok {
			added = append(added, a)
		}
	}
	for a := range g.conns {
		if !want[a] {
			removed = append(removed, a)
		}
	}
	g.mu.Unlock()

	for _, addr := range added {
		g.connect(addr)
	}
	for _, addr := range removed {
		g.disconnect(addr)
	}
	if len(added) > 0 || len(removed) > 0 {
		log.Infof("rank discovery updated total=%d added=%v removed=%v", len(want), added, removed)
	}
}

func (g *Pool) connect(addr string) {
	p := net.Connect(net.Options{
		Name:      addr,
		Addr:      addr,
		Reconnect: time.Second,
	})

	g.mu.Lock()
	if _, dup := g.conns[addr]; dup {
		g.mu.Unlock()
		if gp, ok := p.(cellnet.GenericPeer); ok {
			gp.Stop()
		}
		return
	}
	g.conns[addr] = p
	g.mu.Unlock()

	log.Infof("rank connector start addr=%s", addr)
}

func (g *Pool) disconnect(addr string) {
	g.mu.Lock()
	p, ok := g.conns[addr]
	delete(g.conns, addr)
	delete(g.up, addr)
	g.mu.Unlock()
	if !ok {
		return
	}
	if gp, ok2 := p.(cellnet.GenericPeer); ok2 {
		gp.Stop()
	}
	log.Infof("rank connector stop addr=%s", addr)
}

func (g *Pool) owns(addr string) bool {
	g.mu.Lock()
	_, ok := g.conns[addr]
	g.mu.Unlock()
	if ok {
		return true
	}
	for _, a := range g.dis.Addrs() {
		if a == addr {
			return true
		}
	}
	return false
}

func (g *Pool) setUp(addr string, up bool) {
	g.mu.Lock()
	g.up[addr] = up
	g.mu.Unlock()
}

func (g *Pool) UpdateRole(uid int64, role *model.Role) bool {
	ok := true
	ok = g.send(uid, role, pb.RankBoard_RANK_BOARD_POWER, role.Power) && ok
	ok = g.send(uid, role, pb.RankBoard_RANK_BOARD_LEVEL, int64(role.Level)) && ok
	return ok
}

func (g *Pool) send(uid int64, role *model.Role, board pb.RankBoard, value int64) bool {
	update := &pb.RankUpdate{
		Profile: &pb.RankProfile{
			RoleId: role.ID,
			Name:   role.Name,
			Avatar: role.Avatar,
			Level:  role.Level,
		},
		Board: board,
		Mode:  pb.RankUpdateMode_RANK_UPDATE_OVERWRITE,
		Value: value,
	}
	data, meta, err := encode(update)
	if err != nil {
		log.Errorf("encode rank update role=%d board=%d: %v", role.ID, board, err)
		return false
	}
	t := &pb.Transmit{
		Uid:       uid,
		RoleId:    role.ID,
		RoleName:  role.Name,
		RoleLevel: role.Level,
		MsgId:     uint32(meta.ID),
		Payload:   data,
	}
	g.mu.Lock()
	ses, ok := g.pickLocked()
	g.mu.Unlock()
	if !ok {
		return false
	}
	ses.Send(t)
	return true
}

func (g *Pool) pickLocked() (cellnet.Session, bool) {
	addrs := g.dis.Addrs()
	if len(addrs) == 0 {
		return nil, false
	}
	for i := 0; i < len(addrs); i++ {
		idx := (g.rr + i) % len(addrs)
		addr := addrs[idx]
		p, ok := g.conns[addr]
		if !ok || !g.up[addr] {
			continue
		}
		ses := p.(cellnet.TCPConnector).Session()
		if ses == nil {
			continue
		}
		g.rr = (idx + 1) % len(addrs)
		return ses, true
	}
	return nil, false
}
