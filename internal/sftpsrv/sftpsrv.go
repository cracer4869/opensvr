package sftpsrv

import (
	"fmt"
	"net"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"opensvr/internal/auth"
	"opensvr/internal/logbus"
	"opensvr/internal/sessions"
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
	v       *vfs.VFS
	a       *auth.Store
	signers []ssh.Signer // 主机密钥，可多把（如 ed25519 + RSA）以适配不同设备
	port    int

	mu        sync.Mutex
	ln        net.Listener
	running   bool
	err       string
	boundPort int
}

// New 构造 SFTP 服务端。port 为 0 时使用随机端口（便于测试）。
// signers 为主机密钥列表，通常传入 ed25519 与 RSA 各一把以最大化设备兼容性。
func New(v *vfs.VFS, a *auth.Store, port int, signers ...ssh.Signer) *Server {
	return &Server{v: v, a: a, signers: signers, port: port}
}

// 兼容算法集：在 x/crypto 默认的安全算法之外，额外启用一批老旧但网络设备
// (华为/H3C/思科/锐捷等) SSH 客户端仍在用的算法，以最大化 SFTP 握手兼容性。
// 现代客户端仍会优先协商列表前部的强算法，老设备则可回落到后部的兼容算法。
var (
	compatKEX = []string{
		ssh.KeyExchangeCurve25519,
		ssh.KeyExchangeECDHP256, ssh.KeyExchangeECDHP384, ssh.KeyExchangeECDHP521,
		ssh.KeyExchangeDH14SHA256, ssh.KeyExchangeDH16SHA512, ssh.KeyExchangeDHGEXSHA256,
		ssh.InsecureKeyExchangeDH14SHA1, ssh.InsecureKeyExchangeDHGEXSHA1, ssh.InsecureKeyExchangeDH1SHA1,
	}
	compatCiphers = []string{
		ssh.CipherAES128GCM, ssh.CipherAES256GCM, ssh.CipherChaCha20Poly1305,
		ssh.CipherAES128CTR, ssh.CipherAES192CTR, ssh.CipherAES256CTR,
		ssh.InsecureCipherAES128CBC, ssh.InsecureCipherTripleDESCBC,
	}
	compatMACs = []string{
		ssh.HMACSHA256ETM, ssh.HMACSHA512ETM,
		ssh.HMACSHA256, ssh.HMACSHA512, ssh.HMACSHA1, ssh.InsecureHMACSHA196,
	}
)

// CompatAlgorithms 返回启用的兼容算法集（各返回副本），供 Web 页展示与核对。
func CompatAlgorithms() (kex, ciphers, macs []string) {
	return append([]string(nil), compatKEX...),
		append([]string(nil), compatCiphers...),
		append([]string(nil), compatMACs...)
}

// Start 开始监听并接受连接。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	sc := &ssh.ServerConfig{
		// 显式放宽算法集，兼容老旧网络设备的 SSH 客户端。
		Config: ssh.Config{
			KeyExchanges: compatKEX,
			Ciphers:      compatCiphers,
			MACs:         compatMACs,
		},
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
	// 加入全部主机密钥（ed25519 + RSA）：设备按自身支持的类型自行选择。
	for _, sg := range s.signers {
		sc.AddHostKey(sg)
	}

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

	// 登记会话，供 Web "当前连接" 面板展示。
	sessID := sessions.NewID()
	sessions.Add(&sessions.Session{
		ID:     sessID,
		Proto:  "sftp",
		Remote: conn.RemoteAddr().String(),
		User:   conn.User(),
		Action: "connected",
	})
	defer sessions.Remove(sessID)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.serveChannel(ch, requests, sessID)
	}
}

// serveChannel 分发单个会话 channel 的请求：
//   - subsystem "sftp" -> 起 sftp.RequestServer（原有 SFTP 行为）
//   - exec "scp ..."   -> 起 SCP handler（覆盖只走 SCP 的思科/华为/H3C 老设备）
//   - 其余             -> 拒绝
//
// 请求循环持续 drain 直到对端关闭 channel；实际传输在各自 goroutine 内进行。
func (s *Server) serveChannel(ch ssh.Channel, in <-chan *ssh.Request, sessID string) {
	for r := range in {
		switch r.Type {
		case "subsystem":
			if len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp" {
				r.Reply(true, nil)
				// Handlers 走 vfs.Fs()（afero，已囚笼），读写计入 metrics 与会话。
				h := newHandlers(s.v, sessID)
				go func() { _ = sftp.NewRequestServer(ch, h).Serve(); ch.Close() }()
				continue
			}
			r.Reply(false, nil)
		case "exec":
			if cmd := scpCommand(r.Payload); cmd != "" {
				r.Reply(true, nil)
				go s.handleSCP(ch, cmd, sessID)
				continue
			}
			r.Reply(false, nil)
		default:
			r.Reply(false, nil)
		}
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
