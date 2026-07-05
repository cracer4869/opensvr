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
	s := New(v, a, signer, 0)
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

func TestSFTPChinesePathRoundTrip(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	signer, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk")
	s := New(v, a, signer, 0)
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
	s := New(v, a, signer, 0)
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
	s := New(v, a, signer, 0)
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
