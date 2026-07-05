package ftpsrv

import (
	"bytes"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"

	"opensvr/internal/auth"
	"opensvr/internal/config"
	"opensvr/internal/vfs"
)

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	s := New(v, a, 0, [2]int{0, 0}) // 0=随机端口；被动段 0 让库自选
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	return s, s.Addr()
}

func TestFTPPassiveUploadDownload(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Stop()
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("admin", "admin"); err != nil {
		t.Fatal(err)
	}
	data := []byte("firmware-bytes")
	if err := c.Stor("fw.bin", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	r, err := c.Retr("fw.bin")
	if err != nil {
		t.Fatal(err)
	}
	got := new(bytes.Buffer)
	got.ReadFrom(r)
	r.Close()
	if !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestFTPActiveMode(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Stop()
	c, err := ftp.Dial(addr,
		ftp.DialWithTimeout(3*time.Second),
		ftp.DialWithDisabledEPSV(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("admin", "admin"); err != nil {
		t.Fatal(err)
	}
	data := []byte("active-mode-config")
	if err := c.Stor("cfg.bin", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	r, err := c.Retr("cfg.bin")
	if err != nil {
		t.Fatal(err)
	}
	got := new(bytes.Buffer)
	got.ReadFrom(r)
	r.Close()
	if !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("active-mode roundtrip mismatch")
	}
}

func TestFTPChinesePathRoundTrip(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Stop()
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("admin", "admin"); err != nil {
		t.Fatal(err)
	}
	// 中文子目录 + 中文文件名
	if err := c.MakeDir("当前开局"); err != nil {
		t.Fatal(err)
	}
	data := []byte("华为交换机固件")
	if err := c.Stor("当前开局/版本补丁.bin", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	r, err := c.Retr("当前开局/版本补丁.bin")
	if err != nil {
		t.Fatal(err)
	}
	got := new(bytes.Buffer)
	got.ReadFrom(r)
	r.Close()
	if !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("中文路径 roundtrip mismatch: %q", got.String())
	}
}

func TestStatusReflectsLifecycle(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	s := New(v, a, 0, [2]int{0, 0})
	if s.Status().Running {
		t.Fatal("should not be running before Start")
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Running || st.Port == 0 {
		t.Fatalf("status wrong after start: %+v", st)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if s.Status().Running {
		t.Fatal("should not be running after Stop")
	}
}
