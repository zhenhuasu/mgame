package svcproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/msglog"
	"github.com/davyxu/cellnet/relay"
	"github.com/davyxu/cellnet/rpc"
	"github.com/davyxu/golog"
)

// logger 名含 "svc" 子串，被两服务 LogNames 的 "svc" 正则覆盖
var log = golog.New("svcproc")

// MsgHooker 收发事件拦截（rpc/relay 解析 + 收发日志）。
// 骨架：复制 cellnet proc/tcp。
// TODO: 自定义统计 / 过滤
type MsgHooker struct {
}

func (MsgHooker) OnInboundEvent(inputEvent cellnet.Event) (outputEvent cellnet.Event) {
	var handled bool
	var err error

	inputEvent, handled, err = rpc.ResolveInboundEvent(inputEvent)
	if err != nil {
		log.Errorln("rpc.ResolveInboundEvent:", err)
		return
	}

	if !handled {
		inputEvent, handled, err = relay.ResoleveInboundEvent(inputEvent)
		if err != nil {
			log.Errorln("relay.ResoleveInboundEvent:", err)
			return
		}

		if !handled {
			msglog.WriteRecvLogger(log, "tcp", inputEvent.Session(), inputEvent.Message())
		}
	}

	return inputEvent
}

func (MsgHooker) OnOutboundEvent(inputEvent cellnet.Event) (outputEvent cellnet.Event) {
	handled, err := rpc.ResolveOutboundEvent(inputEvent)
	if err != nil {
		log.Errorln("rpc.ResolveOutboundEvent:", err)
		return nil
	}

	if !handled {
		handled, err = relay.ResolveOutboundEvent(inputEvent)
		if err != nil {
			log.Errorln("relay.ResolveOutboundEvent:", err)
			return nil
		}

		if !handled {
			msglog.WriteSendLogger(log, "tcp", inputEvent.Session(), inputEvent.Message())
		}
	}

	return inputEvent
}
