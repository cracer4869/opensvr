// Package sessions 维护当前活跃的传输连接，供 Web 页"当前连接"面板展示。
package sessions

import (
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Session 描述一个活跃连接或传输。
type Session struct {
	ID     string `json:"id"`
	Proto  string `json:"proto"`  // ftp/sftp/tftp
	Remote string `json:"remote"` // 客户端地址
	User   string `json:"user"`
	Since  string `json:"since"`  // 建立时间 HH:MM:SS
	Action string `json:"action"` // upload/download/connected
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
}

var (
	mu    sync.RWMutex
	items = map[string]*Session{}
	seq   int64
)

// NewID 返回全局唯一会话 ID。
func NewID() string { return strconv.FormatInt(atomic.AddInt64(&seq, 1), 10) }

// Add 注册一个会话。since 为空时用当前时间。
func Add(s *Session) {
	if s.Since == "" {
		s.Since = time.Now().Format("15:04:05")
	}
	mu.Lock()
	items[s.ID] = s
	mu.Unlock()
}

// Update 在锁内修改指定会话；会话不存在时忽略。
func Update(id string, fn func(*Session)) {
	mu.Lock()
	if s, ok := items[id]; ok {
		fn(s)
	}
	mu.Unlock()
}

// AddBytes 累加会话已传字节。
func AddBytes(id string, n int64) {
	mu.Lock()
	if s, ok := items[id]; ok {
		s.Bytes += n
	}
	mu.Unlock()
}

// Remove 注销会话。
func Remove(id string) {
	mu.Lock()
	delete(items, id)
	mu.Unlock()
}

// List 返回当前所有会话，按建立顺序（ID 升序）稳定排序。
func List() []Session {
	mu.RLock()
	out := make([]Session, 0, len(items))
	for _, s := range items {
		out = append(out, *s)
	}
	mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		ai, _ := strconv.ParseInt(out[i].ID, 10, 64)
		aj, _ := strconv.ParseInt(out[j].ID, 10, 64)
		return ai < aj
	})
	return out
}

// Reset 清空（测试用）。
func Reset() {
	mu.Lock()
	items = map[string]*Session{}
	seq = 0
	mu.Unlock()
}
