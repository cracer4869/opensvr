package tftpsrv

import (
	"fmt"
	"io"
	"net"
	"path"
	"sync"

	"github.com/pin/tftp"

	"opensvr/internal/logbus"
	"opensvr/internal/sessions"
	"opensvr/internal/vfs"
)

type Status struct {
	Running bool
	Port    int
	Err     string
}

type Server struct {
	v    *vfs.VFS
	port int

	mu        sync.Mutex
	srv       *tftp.Server
	conn      net.PacketConn
	running   bool
	err       string
	boundPort int
}

func New(v *vfs.VFS, port int) *Server { return &Server{v: v, port: port} }

func (s *Server) readHandler(filename string, rf io.ReaderFrom) error {
	f, err := s.v.Fs().Open(path.Clean("/" + filename))
	if err != nil {
		logbus.Emit(logbus.Event{Proto: "tftp", Action: "read", Path: filename, OK: false, Msg: err.Error()})
		return err
	}
	defer f.Close()

	sess := &sessions.Session{ID: sessions.NewID(), Proto: "tftp", Action: "download", File: filename}
	if ot, ok := rf.(tftp.OutgoingTransfer); ok {
		addr := ot.RemoteAddr()
		sess.Remote = addr.String()
		// 上报 tsize，便于设备显示下载进度并按大小校验完整性。
		if fi, err := f.Stat(); err == nil {
			ot.SetSize(fi.Size())
		}
	}
	sessions.Add(sess)
	defer sessions.Remove(sess.ID)

	_, err = rf.ReadFrom(&countingReader{f: f, sess: sess.ID})
	logbus.Emit(logbus.Event{Proto: "tftp", Action: "read", Path: filename, OK: err == nil})
	return err
}

func (s *Server) writeHandler(filename string, wt io.WriterTo) error {
	f, err := s.v.Fs().Create(path.Clean("/" + filename))
	if err != nil {
		logbus.Emit(logbus.Event{Proto: "tftp", Action: "write", Path: filename, OK: false, Msg: err.Error()})
		return err
	}
	defer f.Close()

	sess := &sessions.Session{ID: sessions.NewID(), Proto: "tftp", Action: "upload", File: filename}
	if it, ok := wt.(tftp.IncomingTransfer); ok {
		addr := it.RemoteAddr()
		sess.Remote = addr.String()
	}
	sessions.Add(sess)
	defer sessions.Remove(sess.ID)

	_, err = wt.WriteTo(&countingWriter{f: f, sess: sess.ID})
	logbus.Emit(logbus.Event{Proto: "tftp", Action: "write", Path: filename, OK: err == nil})
	return err
}

// countingReader/countingWriter 把 TFTP 传输字节回填到会话面板（metrics 已由 vfs 计数）。
type countingReader struct {
	f    io.Reader
	sess string
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.f.Read(p)
	if n > 0 {
		sessions.AddBytes(c.sess, int64(n))
	}
	return n, err
}

type countingWriter struct {
	f    io.Writer
	sess string
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.f.Write(p)
	if n > 0 {
		sessions.AddBytes(c.sess, int64(n))
	}
	return n, err
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		s.err = err.Error()
		return err
	}
	s.conn = conn
	s.boundPort = conn.LocalAddr().(*net.UDPAddr).Port
	s.srv = tftp.NewServer(s.readHandler, s.writeHandler)
	s.running = true
	s.err = ""
	go s.srv.Serve(conn.(*net.UDPConn))
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.srv.Shutdown()
	_ = s.conn.Close()
	s.running = false
	return nil
}

func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.running, Port: s.boundPort, Err: s.err}
}

func (s *Server) Addr() string { return fmt.Sprintf("127.0.0.1:%d", s.Status().Port) }
