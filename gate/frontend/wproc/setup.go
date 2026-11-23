// Package wproc gate 客户端 ws 侧自定义 proc（骨架）：
// 注册名 "gate.ws"，传输层仍为 gorillaws.Acceptor，仅替换 Transmitter/Hooker。
// 当前逻辑复制自 cellnet proc/gorillaws（wire 兼容），后续按 TODO 替换。
package wproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/proc"
)

func init() {
	proc.RegisterProcessor("gate.ws", func(bundle proc.ProcessorBundle, userCallback cellnet.EventCallback) {
		bundle.SetTransmitter(new(WSTransmitter))
		bundle.SetHooker(new(MsgHooker))
		// 必须入会话队列，保持与 net 单队列串行分发一致
		bundle.SetCallback(proc.NewQueuedEventCallback(userCallback))
	})
}
