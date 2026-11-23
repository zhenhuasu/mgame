package net

import (
	"reflect"
	"strings"
	"sync"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/util"
)

// Handler 真实消息处理函数（由 gate/game 注册）
type Handler func(ev cellnet.Event)

var (
	handlersGuard sync.RWMutex
	// handlers 统一注册表：键为 int(消息ID) 或 string(系统事件类型名)，
	// 同一键可注册多个 handler，派发时按注册顺序依次执行。
	handlers = map[interface{}][]Handler{}

	// defaultHandler 业务消息未命中任何注册时的兜底（如 gate 转发）；nil 则记 debug
	defaultHandler Handler
)

// Register 注册消息处理函数。
// key 支持：
//   - int / uint16：消息 ID
//   - string 含 "."：协议名（如 "gate.HelloReq"），按 StringHash 解析并校验已注册
//   - string 不含 "."：系统事件类型名（如 "SessionClosed"）
//   - 消息实例（如 &pb.HelloReq{}）或系统事件实例（如 &cellnet.SessionClosed{}）
//
// 同一 key 可多次注册，派发时全部依次执行。须在 Run 之前调用。
func Register(key interface{}, h Handler) {
	k, ok := normalizeKey(key)
	if !ok {
		return
	}

	handlersGuard.Lock()
	handlers[k] = append(handlers[k], h)
	handlersGuard.Unlock()
}

// RegisterDefault 注册业务消息兜底处理（未命中注册表时调用）；系统事件不走兜底。
func RegisterDefault(h Handler) {
	handlersGuard.Lock()
	defaultHandler = h
	handlersGuard.Unlock()
}

// normalizeKey 将各种 key 归一为注册表键
func normalizeKey(key interface{}) (interface{}, bool) {
	switch k := key.(type) {
	case int:
		return k, true
	case uint16:
		return int(k), true
	case string:
		// 协议名 "gate.X" 走消息 ID；系统事件名原样
		if strings.Contains(k, ".") {
			meta := cellnet.MessageMetaByFullName(k)
			if meta == nil {
				// FullName 为 Go 包名（proto.X），协议名为 proto 包名（gate.X）时按 hash 找
				id := int(util.StringHash(k))
				meta = cellnet.MessageMetaByID(id)
				if meta == nil {
					log.Errorf("register: protocol not found %q", k)
					return nil, false
				}
			}
			return meta.ID, true
		}
		return k, true
	default:
		// 系统事件实例：按类型名
		if _, ok := key.(interface{ SystemMessage() }); ok {
			return typeNameOf(key), true
		}
		// 业务消息实例：取 meta.ID
		meta := cellnet.MessageMetaByMsg(key)
		if meta == nil {
			log.Errorf("register: message meta not found %T", key)
			return nil, false
		}
		return meta.ID, true
	}
}

// dispatch 统一事件入口：Listen/Connect 与 gate ws 装配共用。
// 系统事件按类型名查表，业务消息按消息 ID 查表；未注册记 debug。
func dispatch(ev cellnet.Event) {
	msg := ev.Message()
	if msg == nil {
		return
	}

	var key interface{}
	isSystem := false
	if _, ok := msg.(interface{ SystemMessage() }); ok {
		key = typeNameOf(msg)
		isSystem = true
	} else {
		meta := cellnet.MessageMetaByMsg(msg)
		if meta == nil {
			log.Debugf("dispatch: meta not found %T", msg)
			return
		}
		key = meta.ID
	}

	handlersGuard.RLock()
	hs := handlers[key]
	def := defaultHandler
	handlersGuard.RUnlock()

	if len(hs) == 0 {
		if isSystem {
			// 系统事件未注册不记日志（多数连接事件无业务意义）
			return
		}
		if def != nil {
			def(ev)
			return
		}
		log.Debugf("dispatch: no handler key=%v msg=%T", key, msg)
		return
	}

	for _, h := range hs {
		h(ev)
	}
}

// typeNameOf 取消息具体类型名（*cellnet.SessionClosed → "SessionClosed"）
func typeNameOf(msg interface{}) string {
	t := reflect.TypeOf(msg)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
