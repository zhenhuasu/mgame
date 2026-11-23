// Package frontend 与客户端通信侧：ws 装配（gorillaws.Acceptor / wss 证书），
// 队列用 net.Queue()、分发用 net.Bind(p,"gate.ws")，自注册与事件循环由 gate 入口负责。
package frontend

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/peer"

	_ "github.com/davyxu/cellnet/peer/gorillaws"

	"github.com/davyxu/golog"

	"github.com/zhenhuasu/mgame/core/net"
	_ "github.com/zhenhuasu/mgame/gate/frontend/wproc" // 注册自定义 proc "gate.ws"
	"github.com/zhenhuasu/mgame/gate/session"
)

var log = golog.New("gate.frontend")

// Start 注册客户端侧处理函数、创建 ws acceptor 并启动。
// 须在事件循环起动前调用；wss 提供证书后底层走 ServeTLS。
func (s *Server) Start() error {
	// 注册 ws 侧处理函数（含 SessionClosed 分流，须在起循环前）
	s.registerHandlers()

	// 地址需含 scheme/path；cellnet 按 path 注册 HTTP 升级路由
	p := peer.NewGenericPeer("gorillaws.Acceptor", "gate", s.cfg.Addr(), net.Queue())

	// wss: 提供证书后底层走 ServeTLS
	if s.cfg.Protocol == "wss" {
		p.(cellnet.WSAcceptor).SetHttps(s.cfg.CertFile, s.cfg.KeyFile)
	}

	// 注入会话查找，供登录态顶号定位旧会话与广播发送
	csa := p.(cellnet.SessionAccessor)
	session.SetLookup(csa.GetSession)

	net.Bind(p, "gate.ws")
	p.Start()

	log.Infof("ws listening %s (protocol=%s)", s.cfg.Addr(), s.cfg.Protocol)
	return nil
}
