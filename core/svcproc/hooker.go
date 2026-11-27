package svcproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/golog"
)

// logger 名含 "svc" 子串，被各服务 LogNames 的 "svc" 正则覆盖
var log = golog.New("svcproc")

// MsgHooker 入站解包 Transmit 为最终业务协议；出站原样透传。
type MsgHooker struct {
}

func (MsgHooker) OnInboundEvent(inputEvent cellnet.Event) cellnet.Event {
	return unwrapTransmit(inputEvent)
}

func (MsgHooker) OnOutboundEvent(inputEvent cellnet.Event) cellnet.Event {
	return inputEvent
}
