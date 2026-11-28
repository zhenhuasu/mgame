package rankclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/rpc"
	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/discover"
	"github.com/zhenhuasu/mgame/core/net"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("gate.rankclient")

type Pool struct {
	gateID string
	dis    discover.Discovery

	mu    sync.Mutex
	conns map[string]cellnet.Peer
	up    map[string]bool
	rr    int
}

func New(gateID string, dis discover.Discovery) *Pool {
	return &Pool{
		gateID: gateID,
		dis:    dis,
		conns:  make(map[string]cellnet.Peer),
		up:     make(map[string]bool),
	}
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

func (g *Pool) Query(req *pb.RankListReq, timeout time.Duration, cb func(*pb.RankListAck, error)) bool {
	g.mu.Lock()
	ses, ok := g.pickLocked()
	g.mu.Unlock()
	if !ok {
		return false
	}
	rpc.Call(ses, req, timeout, func(raw interface{}) {
		if err, ok := raw.(error); ok {
			cb(nil, err)
			return
		}
		ack, ok := raw.(*pb.RankListAck)
		if !ok {
			cb(nil, fmt.Errorf("unexpected rank ack %T", raw))
			return
		}
		cb(ack, nil)
	})
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
