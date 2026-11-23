package network

import (
	"sync"
	"time"

	"github.com/davyxu/cellnet"
)

// LoginInfo 登录态记录（不保存原始 token）
type LoginInfo struct {
	SessionID int64
	Account   string
	UID       int64
	LoginAt   time.Time
	ExpireAt  time.Time // 零值 = 不过期
}

// Manager 网关登录态注册表 + 广播。
// 事件回调在单 EventQueue 内串行执行；加锁是为后续 game 等外部 goroutine 调用广播预留。
type Manager struct {
	mu        sync.RWMutex
	byUID     map[int64]*LoginInfo // uid → 登录态
	bySession map[int64]int64      // sessionID → uid
	ttl       time.Duration        // 会话有效期，0 = 不过期
}

func NewManager() *Manager {
	return &Manager{
		byUID:     make(map[int64]*LoginInfo),
		bySession: make(map[int64]int64),
	}
}

// SetTTL 设置会话有效期（0 = 不过期），由 ConfigureAuth 注入
func (m *Manager) SetTTL(d time.Duration) {
	m.mu.Lock()
	m.ttl = d
	m.mu.Unlock()
}

// Touch 刷新会话过期时间（业务消息活跃时调用）
func (m *Manager) Touch(sessionID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, ok := m.bySession[sessionID]
	if !ok || m.ttl <= 0 {
		return
	}
	if info, ok := m.byUID[uid]; ok && info.SessionID == sessionID {
		info.ExpireAt = time.Now().Add(m.ttl)
	}
}

// Expired 会话是否已过期（未登录/不过期返回 false）
func (m *Manager) Expired(sessionID int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uid, ok := m.bySession[sessionID]
	if !ok {
		return false
	}
	info, ok := m.byUID[uid]
	if !ok || info.SessionID != sessionID || info.ExpireAt.IsZero() {
		return false
	}
	return time.Now().After(info.ExpireAt)
}

// Login 登记登录态。顶号：同 uid 已在线时返回旧会话，
// 调用方负责向旧会话发 KickNtf 并 Close。返回 nil 表示无顶号。
func (m *Manager) Login(ses cellnet.Session, info *LoginInfo) cellnet.Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	var old cellnet.Session
	if prev, ok := m.byUID[info.UID]; ok {
		// 清旧映射；旧会话 Close 后的 SessionClosed 因 sessionID 已删而幂等跳过
		delete(m.bySession, prev.SessionID)
		delete(m.byUID, info.UID)
		if sessionLookup != nil {
			old = sessionLookup(prev.SessionID)
		}
	}

	info.SessionID = ses.ID()
	info.LoginAt = time.Now()
	if m.ttl > 0 {
		info.ExpireAt = info.LoginAt.Add(m.ttl)
	}
	m.byUID[info.UID] = info
	m.bySession[ses.ID()] = info.UID

	return old
}

// Logout 清除登录态，返回被清除的记录（未登录返回 nil）
func (m *Manager) Logout(ses cellnet.Session) *LoginInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, ok := m.bySession[ses.ID()]
	if !ok {
		return nil
	}
	delete(m.bySession, ses.ID())
	info := m.byUID[uid]
	delete(m.byUID, uid)
	return info
}

// RemoveBySession 断线清理（SessionClosed 时调用，幂等）。
// 仅当记录仍指向该会话时删除，避免误删顶号后的新登录态。
func (m *Manager) RemoveBySession(sessionID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uid, ok := m.bySession[sessionID]
	if !ok {
		return
	}
	delete(m.bySession, sessionID)
	if info, ok := m.byUID[uid]; ok && info.SessionID == sessionID {
		delete(m.byUID, uid)
	}
}

// SessionByUID 查询 uid 当前的客户端会话（无登录态返回 nil）
func (m *Manager) SessionByUID(uid int64) cellnet.Session {
	m.mu.RLock()
	info, ok := m.byUID[uid]
	m.mu.RUnlock()
	if !ok || sessionLookup == nil {
		return nil
	}
	return sessionLookup(info.SessionID)
}

// UIDBySession 查询会话对应的 uid（未登录返回 false）
func (m *Manager) UIDBySession(sessionID int64) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	uid, ok := m.bySession[sessionID]
	return uid, ok
}

// IsOnline 查询登录态
func (m *Manager) IsOnline(uid int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.byUID[uid]
	return ok
}

// OnlineCount 在线登录数
func (m *Manager) OnlineCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byUID)
}

// onlineSessionIDs 返回已登录会话 ID 快照（调用方在锁外 Send）
func (m *Manager) onlineSessionIDs() []int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]int64, 0, len(m.bySession))
	for sid := range m.bySession {
		ids = append(ids, sid)
	}
	return ids
}

// onlineSessionIDsFor 指定 uid 的已登录会话 ID 快照
func (m *Manager) onlineSessionIDsFor(uids []int64) []int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]int64, 0, len(uids))
	for _, uid := range uids {
		if info, ok := m.byUID[uid]; ok {
			ids = append(ids, info.SessionID)
		}
	}
	return ids
}

// --- 由 Start 注入的外部依赖，避免 Manager 直接依赖 peer 细节 ---

var (
	// sessionLookup sessionID → Session（acceptor.SessionAccessor.GetSession）
	sessionLookup func(id int64) cellnet.Session
)

// --- 广播 ---
// 发送在锁外进行（先取快照再 Send），避免持锁调用网络 IO。

// Broadcast 全局广播：发给所有已登录会话
func (m *Manager) Broadcast(msg interface{}) int {
	return m.send(m.onlineSessionIDs(), msg)
}

// BroadcastToUids 按 uid 列表广播。
// uids 为空时退化为全局广播 —— 预留给 game 服务的统一入口：
// 收到转发消息后，uid 列表空=全部，非空=多播/单播。
func (m *Manager) BroadcastToUids(uids []int64, msg interface{}) int {
	if len(uids) == 0 {
		return m.Broadcast(msg)
	}
	return m.send(m.onlineSessionIDsFor(uids), msg)
}

func (m *Manager) send(sessionIDs []int64, msg interface{}) int {
	count := 0
	for _, sid := range sessionIDs {
		if sessionLookup == nil {
			break
		}
		ses := sessionLookup(sid)
		if ses == nil {
			continue
		}
		ses.Send(msg)
		count++
	}
	return count
}
