package auth

import (
	"crypto/subtle"
	"sync"

	"github.com/cracer4869/opensvr/internal/config"
)

// Store 口令库，线程安全。
type Store struct {
	mu  sync.RWMutex
	cfg config.AuthCfg
}

// New 用给定认证配置构造 Store。
func New(cfg config.AuthCfg) *Store { return &Store{cfg: cfg} }

// Update 替换认证配置。
func (s *Store) Update(cfg config.AuthCfg) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

// Creds 返回当前用户名、口令与匿名开关（供 Web 明文展示）。
func (s *Store) Creds() (string, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.User, s.cfg.Pass, s.cfg.Anonymous
}

// Authenticate 校验口令，返回 (是否通过, 是否匿名)。
func (s *Store) Authenticate(user, pass string) (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Anonymous && (user == "anonymous" || user == "ftp") {
		return true, true
	}
	u := subtle.ConstantTimeCompare([]byte(user), []byte(s.cfg.User))
	p := subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.Pass))
	return u == 1 && p == 1, false
}
