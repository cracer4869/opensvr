package sftpsrv

import (
	"fmt"
	"net"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"opensvr/internal/auth"
	"opensvr/internal/logbus"
	"opensvr/internal/vfs"
)

// Status 表示 SFTP 服务端的运行状态。
type Status struct {
	Running bool
	Port    int
	Err     string
}

// Server SFTP 服务端：ssh 监听 + sftp RequestServer，文件系统囚笼在 vfs.root。
type Server struct {
	v      *vfs.VFS
	a      *auth.Store
	signer ssh.Signer
	port   int

	mu        sync.Mutex
	ln        net.Listener
	running   bool
	err       string
	boundPort int
}

// New 构造 SFTP 服务端。port 为 0 时使用随机端口（便于测试）。
func New(v *vfs.VFS, a *auth.Store, signer ssh.Signer, port int) *Server {
	return &Server{v: v, a: a, signer: signer, port: port}
}

// Start 开始监听并接受连接。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	sc := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			ok, _ := s.a.Authenticate(c.User(), string(pass))
			if !ok {
				logbus.Emit(logbus.Event{Proto: "sftp", User: c.User(), Action: "login", OK: false, Msg: "auth failed"})
				return nil, fmt.Errorf("auth failed")
			}
			logbus.Emit(logbus.Event{Proto: "sftp", User: c.User(), Action: "login", OK: true})
			return &ssh.Permissions{}, nil
		},
	}
	sc.AddHostKey(s.signer)

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		s.err = err.Error()
		return err
	}
	s.ln = ln
	s.boundPort = ln.Addr().(*net.TCPAddr).Port
	s.running = true
	s.err = ""
	go s.acceptLoop(sc)
	return nil
}

func (s *Server) acceptLoop(sc *ssh.ServerConfig) {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(c, sc)
	}
}

func (s *Server) handleConn(c net.Conn, sc *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(c, sc)
	if err != nil {
		c.Close()
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			continue
		}
		// 只接受 sftp subsystem 请求。
		go func(in <-chan *ssh.Request) {
			for r := range in {
				ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
				r.Reply(ok, nil)
			}
		}(requests)

		// Handlers 走 vfs.Fs()（afero，已囚笼），随机读写计入 metrics。
		h := newHandlers(s.v)
		srv := sftp.NewRequestServer(ch, h)
		go func(rs *sftp.RequestServer, channel ssh.Channel) {
			_ = rs.Serve()
			channel.Close()
		}(srv, ch)
	}
}

// Stop 停止监听。
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	_ = s.ln.Close()
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
