package server

import (
	"strings"
	"testing"

	"opensvr/internal/config"
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

// TestSFTPInfo 校验 Manager 提供 ed25519 + RSA 双主机密钥的指纹与类型，
// 以及非空的兼容算法集，供 Web 页展示与设备核对 known_hosts。
func TestSFTPInfo(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info := m.SFTPInfo()
	if len(info.Fingerprints) != 2 || len(info.KeyTypes) != 2 {
		t.Fatalf("应有两把主机密钥(ed25519+rsa)，实得 fp=%d types=%d", len(info.Fingerprints), len(info.KeyTypes))
	}
	for _, fp := range info.Fingerprints {
		if !strings.HasPrefix(fp, "SHA256:") {
			t.Fatalf("指纹格式应为 SHA256:xxx，实得 %q", fp)
		}
	}
	joined := strings.Join(info.KeyTypes, ",")
	if !strings.Contains(joined, "ed25519") || !strings.Contains(joined, "rsa") {
		t.Fatalf("密钥类型应含 ed25519 与 rsa，实得 %v", info.KeyTypes)
	}
	if len(info.KEX) == 0 || len(info.Ciphers) == 0 || len(info.MACs) == 0 {
		t.Fatalf("兼容算法集不应为空: kex=%d ciphers=%d macs=%d", len(info.KEX), len(info.Ciphers), len(info.MACs))
	}
}
