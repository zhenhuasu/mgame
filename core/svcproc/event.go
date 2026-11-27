package svcproc

import (
	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"

	pb "github.com/zhenhuasu/mgame/proto"
)

// TransmitContext 服务间上行信封携带的已校验上下文
type TransmitContext struct {
	UID       int64
	RoleID    int64
	RoleName  string
	RoleLevel int32
}

// TransmitEvent 解包后的入站事件：Message 为最终业务协议
type TransmitEvent struct {
	ses cellnet.Session
	msg interface{}
	ctx TransmitContext
}

func (e *TransmitEvent) Session() cellnet.Session { return e.ses }
func (e *TransmitEvent) Message() interface{}     { return e.msg }
func (e *TransmitEvent) Context() TransmitContext { return e.ctx }

// ContextOf 取 TransmitEvent 携带的上下文（非 Transmit 事件返回 false）
func ContextOf(ev cellnet.Event) (TransmitContext, bool) {
	e, ok := ev.(*TransmitEvent)
	if !ok {
		return TransmitContext{}, false
	}
	return e.ctx, true
}

// unwrapTransmit 解包 *pb.Transmit 为最终业务消息；解码失败返回 nil
func unwrapTransmit(ev cellnet.Event) cellnet.Event {
	t, ok := ev.Message().(*pb.Transmit)
	if !ok {
		return ev
	}

	msg, _, err := codec.DecodeMessage(int(t.MsgId), t.Payload)
	if err != nil {
		log.Errorf("decode transmit uid=%d role=%d msgid=%d: %v", t.Uid, t.RoleId, t.MsgId, err)
		return nil
	}

	log.Debugf("transmit recv uid=%d role=%d name=%q level=%d msgid=%d msg=%T",
		t.Uid, t.RoleId, t.RoleName, t.RoleLevel, t.MsgId, msg)

	return &TransmitEvent{
		ses: ev.Session(),
		msg: msg,
		ctx: TransmitContext{
			UID:       t.Uid,
			RoleID:    t.RoleId,
			RoleName:  t.RoleName,
			RoleLevel: t.RoleLevel,
		},
	}
}
