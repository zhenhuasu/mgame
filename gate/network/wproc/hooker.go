package wproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/msglog"
	"github.com/davyxu/golog"
)

// logger 名含 "gate" 子串，被 gate 的 LogNames 正则覆盖，无需改配置
var log = golog.New("gateproc")

// MsgHooker 收发事件拦截。骨架：复制 cellnet gorillaws 的收发日志。
// TODO: 自定义统计 / 过滤 / 鉴权
type MsgHooker struct {
}

func (MsgHooker) OnInboundEvent(inputEvent cellnet.Event) (outputEvent cellnet.Event) {
	msglog.WriteRecvLogger(log, "ws", inputEvent.Session(), inputEvent.Message())
	return inputEvent
}

func (MsgHooker) OnOutboundEvent(inputEvent cellnet.Event) (outputEvent cellnet.Event) {
	msglog.WriteSendLogger(log, "ws", inputEvent.Session(), inputEvent.Message())
	return inputEvent
}
