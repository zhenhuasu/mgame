package wproc

import (
	"encoding/binary"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/codec"
	"github.com/davyxu/cellnet/util"
	"github.com/gorilla/websocket"
)

const (
	MsgIDSize = 2 // uint16
)

// WSTransmitter ws 帧收发。骨架：复制 cellnet gorillaws 的 2字节LE msgID + body。
// TODO: 自定义帧格式 / 加密 / 压缩 / 心跳
type WSTransmitter struct {
}

func (WSTransmitter) OnRecvMessage(ses cellnet.Session) (msg interface{}, err error) {
	conn, ok := ses.Raw().(*websocket.Conn)
	if !ok || conn == nil {
		return nil, nil
	}

	var messageType int
	var raw []byte
	messageType, raw, err = conn.ReadMessage()
	if err != nil {
		return
	}

	if len(raw) < MsgIDSize {
		return nil, util.ErrMinPacket
	}

	switch messageType {
	case websocket.BinaryMessage:
		msgID := binary.LittleEndian.Uint16(raw)
		msgData := raw[MsgIDSize:]

		msg, _, err = codec.DecodeMessage(int(msgID), msgData)
	}

	return
}

func (WSTransmitter) OnSendMessage(ses cellnet.Session, msg interface{}) error {
	conn, ok := ses.Raw().(*websocket.Conn)
	if !ok || conn == nil {
		return nil
	}

	var (
		msgData []byte
		msgID   int
	)

	switch m := msg.(type) {
	case *cellnet.RawPacket: // 发裸包
		msgData = m.MsgData
		msgID = m.MsgID
	default: // 发普通编码包
		var err error
		var meta *cellnet.MessageMeta

		msgData, meta, err = codec.EncodeMessage(msg, nil)
		if err != nil {
			return err
		}

		msgID = meta.ID
	}

	pkt := make([]byte, MsgIDSize+len(msgData))
	binary.LittleEndian.PutUint16(pkt, uint16(msgID))
	copy(pkt[MsgIDSize:], msgData)

	conn.WriteMessage(websocket.BinaryMessage, pkt)

	return nil
}
