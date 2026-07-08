package sftpsrv

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"opensvr/internal/auth"
	"opensvr/internal/config"
	"opensvr/internal/hostkey"
	"opensvr/internal/vfs"
)

// startSCPServer 起一个 SFTP/SCP 测试服务端，返回已建立的 ssh 连接与 vfs 根目录。
func startSCPServer(t *testing.T) (*ssh.Client, string) {
	t.Helper()
	root := t.TempDir()
	v, _ := vfs.New(root)
	a := auth.New(config.AuthCfg{User: "admin", Pass: "admin"})
	signer, _ := hostkey.LoadOrCreate(t.TempDir() + "/hk")
	s := New(v, a, 0, signer)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	time.Sleep(100 * time.Millisecond)

	cfg := &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("admin")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	conn, err := ssh.Dial("tcp", s.Addr(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, root
}

// mustReadZero 读取一个应答字节并断言为 0x00（OK）。
func mustReadZero(t *testing.T, br *bufio.Reader) {
	t.Helper()
	b, err := br.ReadByte()
	if err != nil {
		t.Fatalf("读应答字节失败: %v", err)
	}
	if b != 0 {
		t.Fatalf("期望 0x00 应答, 实得 0x%02x", b)
	}
}

// TestSCPUpload 驱动服务端 sink（scp -t，设备上传），含中文文件名。
func TestSCPUpload(t *testing.T) {
	conn, root := startSCPServer(t)
	content := []byte("firmware-binary-配置内容")

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	w, _ := sess.StdinPipe()
	r, _ := sess.StdoutPipe()
	if err := sess.Start("scp -t /固件.bin"); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(r)

	mustReadZero(t, br)                               // 服务端就绪
	fmt.Fprintf(w, "C0644 %d 固件.bin\n", len(content)) // 文件头
	mustReadZero(t, br)                               // 头 ack
	w.Write(content)                                  // 文件数据
	w.Write([]byte{0})                                // 文件结束标记
	mustReadZero(t, br)                               // 完成 ack
	w.Close()
	sess.Wait()

	got, err := os.ReadFile(filepath.Join(root, "固件.bin"))
	if err != nil {
		t.Fatalf("上传文件未落盘: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("scp 上传内容不一致: %q", string(got))
	}
}

// readCRecord 从 scp source 流读取并解析 "C0644 <size> <name>" 文件头（测试辅助）。
func readCRecord(t *testing.T, br *bufio.Reader) (int64, string) {
	t.Helper()
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("读文件头失败: %v", err)
	}
	parts := strings.Fields(strings.TrimRight(line, "\n"))
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "C") {
		t.Fatalf("非法文件头: %q", line)
	}
	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("非法文件头长度: %q", line)
	}
	return size, parts[2]
}
