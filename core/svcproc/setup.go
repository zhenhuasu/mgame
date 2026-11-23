// Package svcproc gate↔game 后端通信自定义 proc（骨架）：
// 注册名 "svc.backend"，替代 cellnet 默认 "tcp.ltv"，wire 兼容（LTV 帧）。
// 当前逻辑复制自 cellnet proc/tcp，后续按 TODO 替换。
package svcproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/proc"
)

func init() {
	proc.RegisterProcessor("svc.backend", func(bundle proc.ProcessorBundle, userCallback cellnet.EventCallback) {
		bundle.SetTransmitter(new(TCPMessageTransmitter))
		bundle.SetHooker(new(MsgHooker))
		// 必须入会话队列，保持与 net 单队列串行分发一致
		bundle.SetCallback(proc.NewQueuedEventCallback(userCallback))
	})
}
