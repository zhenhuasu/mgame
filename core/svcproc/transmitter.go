package svcproc

import (
	"io"
	"net"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/util"
)

// TCPMessageTransmitter LTV 帧收发（2字节长度头+消息ID+body，带读写超时）。
// 骨架：复制 cellnet proc/tcp，wire 不变。
// TODO: 自定义帧格式 / 加密 / 压缩
type TCPMessageTransmitter struct {
}

type socketOpt interface {
	MaxPacketSize() int
	ApplySocketReadTimeout(conn net.Conn, callback func())
	ApplySocketWriteTimeout(conn net.Conn, callback func())
}

func (TCPMessageTransmitter) OnRecvMessage(ses cellnet.Session) (msg interface{}, err error) {
	reader, ok := ses.Raw().(io.Reader)
	if !ok || reader == nil {
		return nil, nil
	}

	opt := ses.Peer().(socketOpt)

	if conn, ok := reader.(net.Conn); ok {
		opt.ApplySocketReadTimeout(conn, func() {
			msg, err = util.RecvLTVPacket(reader, opt.MaxPacketSize())
		})
	}

	return
}

func (TCPMessageTransmitter) OnSendMessage(ses cellnet.Session, msg interface{}) (err error) {
	writer, ok := ses.Raw().(io.Writer)
	if !ok || writer == nil {
		return nil
	}

	opt := ses.Peer().(socketOpt)

	opt.ApplySocketWriteTimeout(writer.(net.Conn), func() {
		err = util.SendLTVPacket(writer, ses.(cellnet.ContextSet), msg)
	})

	return
}
