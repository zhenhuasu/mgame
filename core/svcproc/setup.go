// Package svcproc 后端服务间通信自定义 proc（gate→game、game→guild）：
// 注册名 "svc.backend"，替代 cellnet 默认 "tcp.ltv"，wire 兼容（LTV 帧）。
// OnInboundEvent 解包通用 Transmit 信封，业务层只注册最终协议。
package svcproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/proc"
	"github.com/davyxu/cellnet/proc/tcp"
)

func init() {
	proc.RegisterProcessor("svc.backend", func(bundle proc.ProcessorBundle, userCallback cellnet.EventCallback) {
		bundle.SetTransmitter(new(TCPMessageTransmitter))
		bundle.SetHooker(proc.NewMultiHooker(
			new(MsgHooker),
			new(tcp.MsgHooker))) // tcp基础消息处理，带有RPC和relay功能
		// 必须入会话队列，保持与 net 单队列串行分发一致
		bundle.SetCallback(proc.NewQueuedEventCallback(userCallback))
	})
}
