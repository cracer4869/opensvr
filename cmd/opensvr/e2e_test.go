package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/pin/tftp/v3"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/cracer4869/opensvr/internal/config"
)

// statusResp 用于解析 /api/status 中我们关心的字段。
type statusResp struct {
	FTP struct {
		Running bool `json:"running"`
		Port    int  `json:"port"`
	} `json:"ftp"`
	SFTP struct {
		Running bool `json:"running"`
		Port    int  `json:"port"`
	} `json:"sftp"`
	TFTP struct {
		Running bool `json:"running"`
		Port    int  `json:"port"`
	} `json:"tftp"`
	Root string `json:"root"`
}

// TestEndToEndAllProtocols 端到端验证：通过 Web API 启动三协议，
// 用真实客户端各传一个中文名文件，校验落盘到根目录。覆盖 web→manager→协议→vfs 全链路。
func TestEndToEndAllProtocols(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "开局文件")
	cfg := config.Default()
	cfg.RootDir = root
	cfg.Web.Port = freePort(t)
	cfg.FTP.Port = 0 // 0=随机端口，避免与真实/特权端口冲突
	cfg.SFTP.Port = 0
	cfg.TFTP.Port = 0
	cfg.AutoOpenBrowser = false
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	stop, url, err := run(cfgPath, dir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	defer stop()

	waitStatus(t, url) // 等 web 起来

	// 通过 API 启动三协议
	for _, p := range []string{"ftp", "sftp", "tftp"} {
		r, err := http.Post(url+"/api/proto/"+p+"/start", "application/json", nil)
		if err != nil {
			t.Fatalf("start %s: %v", p, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("start %s status=%d", p, r.StatusCode)
		}
	}
	time.Sleep(200 * time.Millisecond)

	st := getStatus(t, url)
	if !st.FTP.Running || !st.SFTP.Running || !st.TFTP.Running {
		t.Fatalf("protocols not all running: %+v", st)
	}

	// ---- FTP：传中文名文件 ----
	fc, err := ftp.Dial(sprintfAddr(st.FTP.Port), ftp.DialWithTimeout(3*time.Second))
	if err != nil {
		t.Fatalf("ftp dial: %v", err)
	}
	if err := fc.Login("admin", "admin"); err != nil {
		t.Fatalf("ftp login: %v", err)
	}
	if err := fc.Stor("版本补丁_ftp.bin", bytes.NewReader([]byte("ftp-固件"))); err != nil {
		t.Fatalf("ftp stor: %v", err)
	}
	fc.Quit()

	// ---- SFTP：传中文名文件 ----
	sc := &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("admin")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	conn, err := ssh.Dial("tcp", sprintfAddr(st.SFTP.Port), sc)
	if err != nil {
		t.Fatalf("sftp dial: %v", err)
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	w, err := cl.Create("配置_sftp.cfg")
	if err != nil {
		t.Fatalf("sftp create: %v", err)
	}
	w.Write([]byte("sftp-配置"))
	w.Close()
	cl.Close()

	// ---- TFTP：传中文名文件 ----
	tc, err := tftp.NewClient(sprintfAddr(st.TFTP.Port))
	if err != nil {
		t.Fatalf("tftp client: %v", err)
	}
	wt, err := tc.Send("版本补丁_tftp.bin", "octet")
	if err != nil {
		t.Fatalf("tftp send: %v", err)
	}
	if _, err := wt.ReadFrom(bytes.NewReader([]byte("tftp-固件"))); err != nil {
		t.Fatalf("tftp write: %v", err)
	}

	// ---- 校验三个中文文件都落盘到根目录 ----
	for name, want := range map[string]string{
		"版本补丁_ftp.bin":  "ftp-固件",
		"配置_sftp.cfg":   "sftp-配置",
		"版本补丁_tftp.bin": "tftp-固件",
	} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("读取落盘文件 %s 失败: %v", name, err)
		}
		if string(b) != want {
			t.Fatalf("文件 %s 内容 = %q, want %q", name, string(b), want)
		}
	}
}

func sprintfAddr(port int) string { return "127.0.0.1:" + itoa(port) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func waitStatus(t *testing.T, url string) {
	t.Helper()
	for i := 0; i < 60; i++ {
		r, err := http.Get(url + "/api/status")
		if err == nil {
			r.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("web 未在超时内就绪")
}

func getStatus(t *testing.T, url string) statusResp {
	t.Helper()
	r, err := http.Get(url + "/api/status")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	var st statusResp
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("parse status: %v; body=%s", err, b)
	}
	return st
}
