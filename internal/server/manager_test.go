package server

import (
	"testing"

	"github.com/cracer4869/opensvr/internal/config"
	"github.com/cracer4869/opensvr/internal/firewall"
)

func TestManagerStartStopFTP(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	cfg.FTP.Port = 0
	m, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StartFTP(); err != nil {
		t.Fatal(err)
	}
	if !m.Statuses()["ftp"].Running {
		t.Fatal("ftp should be running")
	}
	if err := m.StopFTP(); err != nil {
		t.Fatal(err)
	}
	if m.Statuses()["ftp"].Running {
		t.Fatal("ftp should be stopped")
	}
}

func TestSetRootRebuilds(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := New(cfg, t.TempDir())
	nd := t.TempDir()
	if err := m.SetRoot(nd); err != nil {
		t.Fatal(err)
	}
	if m.Config().RootDir != nd {
		t.Fatal("root not updated")
	}
}

// fakeFW 是记录调用的假防火墙控制器，供单测断言自动放行/清理。
type fakeFW struct {
	allowed [][]firewall.Rule
	removed [][]string
}

func (f *fakeFW) Allow(r []firewall.Rule) error { f.allowed = append(f.allowed, r); return nil }
func (f *fakeFW) Remove(n []string) error       { f.removed = append(f.removed, n); return nil }

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestFirewallAutoAllowOnStartStop：注入 fake 后，StartFTP 应放行 ftp+被动段，StopFTP 应清理。
func TestFirewallAutoAllowOnStartStop(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	cfg.FTP.Port = 0
	m, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeFW{}
	m.SetFirewall(fw)

	if err := m.StartFTP(); err != nil {
		t.Fatal(err)
	}
	if len(fw.allowed) == 0 {
		t.Fatal("StartFTP 应触发一次 Allow")
	}
	var names []string
	for _, r := range fw.allowed[len(fw.allowed)-1] {
		names = append(names, r.Name)
	}
	if !containsStr(names, "opensvr-ftp") || !containsStr(names, "opensvr-ftp-passive") {
		t.Fatalf("Allow 应含 opensvr-ftp 与 opensvr-ftp-passive, 实得 %v", names)
	}

	if err := m.StopFTP(); err != nil {
		t.Fatal(err)
	}
	if len(fw.removed) == 0 || !containsStr(fw.removed[len(fw.removed)-1], "opensvr-ftp") {
		t.Fatalf("StopFTP 应移除 opensvr-ftp 规则, removed=%v", fw.removed)
	}
}

// TestSetPortRefreshesFirewall：运行中的协议改端口后应重新 Allow（新端口生效）。
func TestSetPortRefreshesFirewall(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	cfg.FTP.Port = 0
	m, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeFW{}
	m.SetFirewall(fw)
	if err := m.StartFTP(); err != nil {
		t.Fatal(err)
	}
	before := len(fw.allowed)
	if err := m.SetPort("ftp", 0); err != nil { // 仍用随机端口，避免测试端口冲突
		t.Fatal(err)
	}
	if len(fw.allowed) <= before {
		t.Fatal("SetPort 后应重新放行防火墙规则")
	}
}
