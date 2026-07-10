package hostkey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hostkey")
	s1, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(s1.PublicKey().Marshal()) != string(s2.PublicKey().Marshal()) {
		t.Fatal("host key not persisted (changed between loads)")
	}
}

func TestLoadOrCreateRSAPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hostkey_rsa")
	s1, err := LoadOrCreateRSA(p)
	if err != nil {
		t.Fatal(err)
	}
	if s1.PublicKey().Type() != "ssh-rsa" {
		t.Fatalf("应为 ssh-rsa 密钥, 实得 %s", s1.PublicKey().Type())
	}
	s2, err := LoadOrCreateRSA(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(s1.PublicKey().Marshal()) != string(s2.PublicKey().Marshal()) {
		t.Fatal("RSA host key not persisted (changed between loads)")
	}
}

func TestCorruptKeySelfHeals(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hostkey")
	if err := os.WriteFile(p, []byte("garbage not a pem key"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadOrCreate(p)
	if err != nil {
		t.Fatalf("损坏密钥应自愈而非启动失败: %v", err)
	}
	if s == nil {
		t.Fatal("应返回重新生成的 signer")
	}
	if _, err := os.Stat(p + ".bad"); err != nil {
		t.Fatalf("损坏密钥应备份为 .bad: %v", err)
	}
	// 再次加载应能解析新生成的密钥
	if _, err := LoadOrCreate(p); err != nil {
		t.Fatalf("重新生成的密钥应可正常加载: %v", err)
	}
}
