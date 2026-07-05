package ftpsrv

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"sync"

	ftpserver "github.com/fclairamb/ftpserverlib"

	"opensvr/internal/auth"
	"opensvr/internal/logbus"
	"opensvr/internal/sessions"
	"opensvr/internal/vfs"
)

// Status 描述 FTP 服务端运行状态。
type Status struct {
	Running bool
	Port    int
	Err     string
}

// Server 封装 ftpserverlib，支持主动+被动模式。
type Server struct {
	v       *vfs.VFS
	a       *auth.Store
	port    int
	passive [2]int

	mu        sync.Mutex
	srv       *ftpserver.FtpServer
	running   bool
	err       string
	boundPort int
}

// New 构造 FTP 服务端。port 为 0 时随机监听；passive 为被动数据端口段（[0,0] 让库自选）。
func New(v *vfs.VFS, a *auth.Store, port int, passive [2]int) *Server {
	return &Server{v: v, a: a, port: port, passive: passive}
}

// ----- MainDriver 实现 -----
type driver struct {
	s   *Server
	mu  sync.Mutex
	ids map[uint32]string // ftpserverlib 客户端ID -> 会话ID
}

func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	st := &ftpserver.Settings{
		ListenAddr: fmt.Sprintf("0.0.0.0:%d", d.s.port),
	}
	if d.s.passive[1] > 0 {
		st.PassiveTransferPortRange = ftpserver.PortRange{Start: d.s.passive[0], End: d.s.passive[1]}
	}
	return st, nil
}

func (d *driver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	id := sessions.NewID()
	d.mu.Lock()
	d.ids[cc.ID()] = id
	d.mu.Unlock()
	sessions.Add(&sessions.Session{
		ID:     id,
		Proto:  "ftp",
		Remote: cc.RemoteAddr().String(),
		Action: "connected",
	})
	return "opensvr", nil
}

func (d *driver) ClientDisconnected(cc ftpserver.ClientContext) {
	d.mu.Lock()
	id := d.ids[cc.ID()]
	delete(d.ids, cc.ID())
	d.mu.Unlock()
	sessions.Remove(id)
}

func (d *driver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	ok, _ := d.s.a.Authenticate(user, pass)
	if !ok {
		logbus.Emit(logbus.Event{Proto: "ftp", User: user, Action: "login", OK: false, Msg: "auth failed"})
		return nil, fmt.Errorf("authentication failed")
	}
	logbus.Emit(logbus.Event{Proto: "ftp", User: user, Action: "login", OK: true})
	d.mu.Lock()
	id := d.ids[cc.ID()]
	d.mu.Unlock()
	sessions.Update(id, func(s *sessions.Session) { s.User = user })
	// 返回会话感知的 Fs：传输字节回填到该会话（底层仍是囚笼+metrics 计数的 vfs）。
	return newSessionFs(d.s.v.Fs(), id), nil
}

func (d *driver) GetTLSConfig() (*tls.Config, error) {
	return nil, fmt.Errorf("TLS not enabled")
}

// Start 启动监听并在后台服务。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.srv = ftpserver.NewFtpServer(&driver{s: s, ids: map[uint32]string{}})
	if err := s.srv.Listen(); err != nil {
		s.err = err.Error()
		return err
	}
	s.boundPort = resolvePort(s.srv, s.port)
	s.running = true
	s.err = ""
	go s.srv.Serve()
	return nil
}

// Stop 停止服务。
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	_ = s.srv.Stop()
	s.running = false
	return nil
}

// Status 返回当前状态。
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.running, Port: s.boundPort, Err: s.err}
}

// Addr 返回本机可连接地址。
func (s *Server) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.Status().Port)
}

// resolvePort 从服务端实际监听地址解析端口（支持 port=0 随机分配）。
func resolvePort(srv *ftpserver.FtpServer, want int) int {
	addr := srv.Addr()
	if _, portStr, err := net.SplitHostPort(addr); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			return p
		}
	}
	return want
}
