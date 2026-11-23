// Package net 封装两服务共用的 cellnet 装配：
// Init 建队列、日志级别与注册中心，Listen/Connect 建对内连接并绑定统一 dispatch，
// Run/RunContext 起循环，RegisterSelf 在监听启动后注册自身。
package net

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/peer"
	"github.com/davyxu/cellnet/proc"
	"github.com/davyxu/golog"

	_ "github.com/davyxu/cellnet/codec/gogopb"
	_ "github.com/davyxu/cellnet/peer/tcp"

	"github.com/zhenhuasu/mgame/config"
	"github.com/zhenhuasu/mgame/core/registry"
	_ "github.com/zhenhuasu/mgame/core/svcproc" // 注册后端 proc "svc.backend"
	_ "github.com/zhenhuasu/mgame/proto"        // 导入即注册 protobuf 消息
)

var log = golog.New("net")

// queue 进程内唯一事件队列：gate 客户端连接与对内连接共用，串行处理
var queue cellnet.EventQueue

// reg 进程内唯一服务注册中心（Init 按配置创建；endpoints 空 = nil）
var reg registry.Registry

// InitOptions 初始化参数
type InitOptions struct {
	// 日志级别: debug | info | warn | error（空则不设置）
	LogLevel string
	// 需要应用级别的日志名（正则子串），如 gate: ["gorillaws","tcp","gate","svc"]
	LogNames []string
	// 服务注册与发现配置（endpoints 空 = 不启用）
	Registry config.Registry
}

// Init 初始化：创建事件队列、应用日志级别、按配置创建注册中心。
// 进程启动时调用一次，不起循环。
func Init(opt InitOptions) error {
	queue = cellnet.NewEventQueue()

	if opt.LogLevel != "" {
		// golog 按正则子串匹配日志名；"net" 为本包日志
		golog.SetLevelByString("net", opt.LogLevel)
		for _, name := range opt.LogNames {
			golog.SetLevelByString(name, opt.LogLevel)
		}
		// 注册中心日志名由本包统一管理，调用方无需再传
		golog.SetLevelByString("registry", opt.LogLevel)
	}

	if opt.Registry.Enabled() {
		r, err := registry.NewFromConfig(opt.Registry)
		if err != nil {
			return fmt.Errorf("init registry: %w", err)
		}
		reg = r
	}
	return nil
}

// SignalContext 返回随 SIGINT/SIGTERM 取消的 ctx（优雅下线入口）
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// Registry 返回 Init 创建的注册中心（未启用返回 nil）
func Registry() registry.Registry {
	return reg
}

// CloseRegistry 关闭注册中心（未启用返回 nil）
func CloseRegistry() error {
	if reg == nil {
		return nil
	}
	err := reg.Close()
	reg = nil
	return err
}

// RegisterSelf 向 Init 创建的注册中心注册自身；未启用时返回可安全 Close 的 no-op 句柄。
// 调用时机：监听启动之后。
func RegisterSelf(ctx context.Context, inst registry.Instance) (registry.Registration, error) {
	if reg == nil {
		return nopRegistration{}, nil
	}
	return reg.Register(ctx, inst)
}

// nopRegistration 未启用注册中心时的空实现
type nopRegistration struct{}

func (nopRegistration) Close() error { return nil }

// Queue 返回 Init 创建的事件队列，供 gate 客户端 acceptor 复用（单队列串行）
func Queue() cellnet.EventQueue {
	return queue
}

// Bind 将 peer 绑定到统一 dispatch（Listen/Connect 内部与 gate ws 装配共用）
func Bind(p cellnet.Peer, procName string) {
	proc.BindProcessorHandler(p, procName, dispatch)
}

// Options Listen/Connect 共用参数；两函数对空值的默认不同
type Options struct {
	// peer 类型，空：Listen → "tcp.Acceptor"，Connect → "tcp.Connector"
	PeerType string
	// peer 名，如 "game"（gate 连接时用 game 地址）
	Name string
	// 监听/连接地址
	Addr string
	// 处理器名，空 = "svc.backend"
	ProcName string
	// 自动重连间隔，仅 Connect 使用；0 = 不重连
	Reconnect time.Duration
}

// Listen 创建对内监听 peer 并启动（绑定统一 dispatch）。game 等服务使用。
func Listen(opt Options) {
	if opt.PeerType == "" {
		opt.PeerType = "tcp.Acceptor"
	}
	if opt.ProcName == "" {
		opt.ProcName = "svc.backend"
	}

	p := peer.NewGenericPeer(opt.PeerType, opt.Name, opt.Addr, queue)
	Bind(p, opt.ProcName)
	p.Start()

	log.Infof("listen peer=%s type=%s addr=%s", opt.Name, opt.PeerType, opt.Addr)
}

// Connect 创建对内连接 peer 并启动（绑定统一 dispatch），返回 peer 供调用方 Send。
func Connect(opt Options) cellnet.GenericPeer {
	if opt.PeerType == "" {
		opt.PeerType = "tcp.Connector"
	}
	if opt.ProcName == "" {
		opt.ProcName = "svc.backend"
	}

	p := peer.NewGenericPeer(opt.PeerType, opt.Name, opt.Addr, queue)
	if opt.Reconnect > 0 {
		p.(cellnet.TCPConnector).SetReconnectDuration(opt.Reconnect)
	}
	Bind(p, opt.ProcName)
	p.Start()

	log.Infof("connect peer=%s type=%s addr=%s reconnect=%v", opt.Name, opt.PeerType, opt.Addr, opt.Reconnect)
	return p
}

// Post 将函数投递到事件队列执行（保证 peer 增删等在事件循环线程进行）
func Post(fn func()) {
	queue.Post(fn)
}

// Run 启动事件循环并阻塞等待退出。所有 Register/Listen/Connect 之后调用。
func Run() {
	queue.StartLoop()
	queue.Wait()
}

// RunContext 启动事件循环并阻塞；ctx 取消时停止循环（优雅下线入口）。
// 所有 Register/Listen/Connect 之后调用。
func RunContext(ctx context.Context) {
	queue.StartLoop()
	go func() {
		<-ctx.Done()
		log.Infof("run context done: %v", ctx.Err())
		queue.StopLoop()
	}()
	queue.Wait()
}
