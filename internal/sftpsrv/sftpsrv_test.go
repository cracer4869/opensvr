package sftpsrv

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"opensvr/internal/auth"
	"opensvr/internal/config"
	"opensvr/internal/hostkey"
	"opensvr/internal/vfs"
)

func TestSFTPUploadDownload(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	signer, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk")
	s := New(v, a, 0, signer)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	cfg := &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("admin")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	conn, err := ssh.Dial("tcp", s.Addr(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	w, _ := cl.Create("cfg.txt")
	w.Write([]byte("device-config"))
	w.Close()
	r, _ := cl.Open("cfg.txt")
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, []byte("device-config")) {
		t.Fatal("sftp roundtrip mismatch")
	}
}

// TestCompatAlgorithms 校验兼容算法集非空，且同时包含现代强算法与老设备回落算法。
func TestCompatAlgorithms(t *testing.T) {
	kex, ciphers, macs := CompatAlgorithms()
	if len(kex) == 0 || len(ciphers) == 0 || len(macs) == 0 {
		t.Fatalf("兼容算法集不应为空: kex=%d ciphers=%d macs=%d", len(kex), len(ciphers), len(macs))
	}
	has := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}
	// 现代强算法在前，老设备兼容算法兜底。
	if !has(kex, ssh.KeyExchangeCurve25519) {
		t.Error("KEX 应含 curve25519")
	}
	if !has(ciphers, ssh.CipherAES128GCM) || !has(ciphers, ssh.InsecureCipherAES128CBC) {
		t.Error("Ciphers 应同时含 aes128-gcm 与兜底的 aes128-cbc")
	}
	// 返回副本，改动不应影响内部状态。
	kex[0] = "tampered"
	kex2, _, _ := CompatAlgorithms()
	if kex2[0] == "tampered" {
		t.Error("CompatAlgorithms 应返回副本，不可被外部篡改")
	}
}

// TestSFTPLegacyClientHandshake 用只接受老算法(aes128-cbc)且只认 RSA 主机密钥的客户端握手，
// 验证双主机密钥(ed25519+rsa)与兼容算法确实让老旧网络设备也能连上。
func TestSFTPLegacyClientHandshake(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	ed, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk_ed")
	rsa, _ := hostkey.LoadOrCreateRSA(t.TempDir() + "/hk_rsa")
	s := New(v, a, 0, ed, rsa)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("admin")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		// 只认 RSA 主机密钥 + 只接受老式 aes128-cbc，模拟老设备。
		HostKeyAlgorithms: []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		Config:            ssh.Config{Ciphers: []string{ssh.InsecureCipherAES128CBC}},
	}
	conn, err := ssh.Dial("tcp", s.Addr(), cfg)
	if err != nil {
		t.Fatalf("老设备风格握手应成功: %v", err)
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if _, err := cl.Getwd(); err != nil {
		t.Fatalf("sftp 会话应可用: %v", err)
	}
}

func TestSFTPChinesePathRoundTrip(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	signer, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk")
	s := New(v, a, 0, signer)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	cfg := &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("admin")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	conn, err := ssh.Dial("tcp", s.Addr(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	if err := cl.Mkdir("当前开局"); err != nil {
		t.Fatalf("mkdir 中文目录: %v", err)
	}
	data := []byte("设备配置文件")
	w, err := cl.Create("当前开局/配置.cfg")
	if err != nil {
		t.Fatalf("create 中文文件: %v", err)
	}
	w.Write(data)
	w.Close()
	r, err := cl.Open("当前开局/配置.cfg")
	if err != nil {
		t.Fatalf("open 中文文件: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("中文路径 roundtrip mismatch: %q", string(got))
	}
}

func TestSFTPBadAuthRejected(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	signer, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk")
	s := New(v, a, 0, signer)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	cfg := &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("wrong")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	if _, err := ssh.Dial("tcp", s.Addr(), cfg); err == nil {
		t.Fatal("bad password should be rejected")
	}
}

func TestSFTPListAndRemove(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	signer, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk")
	s := New(v, a, 0, signer)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	cfg := &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("admin")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	conn, err := ssh.Dial("tcp", s.Addr(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	w, _ := cl.Create("boot.bin")
	w.Write([]byte("img"))
	w.Close()

	if err := cl.Mkdir("sub"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entries, err := cl.ReadDir("/")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 entries, got %v", names)
	}

	if err := cl.Remove("boot.bin"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := cl.Stat("boot.bin"); err == nil {
		t.Fatal("file should be removed")
	}
}
