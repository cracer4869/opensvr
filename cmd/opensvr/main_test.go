package main

import (
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"opensvr/internal/config"
)

// freePort 申请一个空闲 TCP 端口用于测试（避免与真实服务冲突）。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRunServesStatus 断言 run() 能起 Web，/api/status 返回 200，随后 stop() 收尾。
func TestRunServesStatus(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.RootDir = filepath.Join(dir, "data")
	cfg.Web.Port = freePort(t)
	cfg.AutoOpenBrowser = false // 测试不弹浏览器
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	stop, url, err := run(cfgPath, dir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stop == nil {
		t.Fatal("stop func is nil")
	}
	defer stop()

	if url == "" {
		t.Fatal("url is empty")
	}

	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = http.Get(url + "/api/status")
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want 200", resp.StatusCode)
	}
}

// TestRunMissingConfigUsesDefault 断言配置文件缺失时 run() 仍能用默认配置启动。
func TestRunMissingConfigUsesDefault(t *testing.T) {
	dir := t.TempDir()
	// 不写 config.yaml，Load 返回默认；但默认 Web.Port=31944 可能冲突，
	// 这里仅验证 run 不因缺文件而报错，随后立即停止。
	// 为避免端口冲突导致的连接测试不稳定，此用例只校验返回值。
	cfgPath := filepath.Join(dir, "config.yaml")

	stop, url, err := run(cfgPath, dir)
	if err != nil {
		t.Fatalf("run with missing config: %v", err)
	}
	defer stop()
	if url == "" {
		t.Fatal("url is empty")
	}
}
