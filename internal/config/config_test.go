package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Web.Port != 31944 {
		t.Fatalf("web port = %d, want 31944", c.Web.Port)
	}
	if c.FTP.Port != 21 || c.SFTP.Port != 22 || c.TFTP.Port != 69 {
		t.Fatalf("proto ports wrong: %+v", c)
	}
	if c.Auth.User != "admin" || c.Auth.Pass != "admin" {
		t.Fatal("default auth wrong")
	}
	if c.PassiveRange != [2]int{50000, 50100} {
		t.Fatal("passive range wrong")
	}
	if !c.AutoOpenBrowser {
		t.Fatal("AutoOpenBrowser should default true")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	c := Default()
	c.FTP.Port = 2121
	c.RootDir = "X:/fw"
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.FTP.Port != 2121 || got.RootDir != "X:/fw" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Web.Port != 31944 {
		t.Fatal("missing file should yield defaults")
	}
	_ = os.Remove
}

func TestResolveRootEmptyUsesPortableDefault(t *testing.T) {
	base := t.TempDir()
	got, warn := ResolveRoot("", base)
	want := filepath.Join(base, "开局文件")
	if got != want {
		t.Fatalf("resolved = %q, want %q", got, want)
	}
	if warn != "" {
		t.Fatalf("unexpected warning: %q", warn)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("default dir not created: %v", err)
	}
}

func TestResolveRootRelativeToBase(t *testing.T) {
	base := t.TempDir()
	got, warn := ResolveRoot("固件库", base)
	want := filepath.Join(base, "固件库")
	if got != want || warn != "" {
		t.Fatalf("got=%q warn=%q, want %q/无警告", got, warn, want)
	}
}

func TestResolveRootAbsoluteOK(t *testing.T) {
	base := t.TempDir()
	abs := filepath.Join(t.TempDir(), "当前开局")
	got, warn := ResolveRoot(abs, base)
	if got != abs || warn != "" {
		t.Fatalf("got=%q warn=%q, want %q", got, warn, abs)
	}
}

func TestResolveRootInvalidFallsBackWithWarning(t *testing.T) {
	base := t.TempDir()
	// 不存在的盘符，模拟换机后配置目录不可用
	got, warn := ResolveRoot(`Z:\不存在的目录\x`, base)
	want := filepath.Join(base, "开局文件")
	if got != want {
		t.Fatalf("fallback = %q, want %q", got, want)
	}
	if warn == "" {
		t.Fatal("expected non-empty warning on fallback")
	}
}

func TestLoadCorruptFallsBackToDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("{{{ 这不是 yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("损坏配置不应让 Load 失败: %v", err)
	}
	if got.Web.Port != 31944 {
		t.Fatal("损坏配置应回退默认值")
	}
	if _, err := os.Stat(p + ".bad"); err != nil {
		t.Fatalf("损坏配置应被备份为 .bad: %v", err)
	}
}

func TestSaveAtomicLeavesNoTmp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Default().Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("config.yaml 未写出: %v", err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("原子写不应留下 .tmp 残留")
	}
}
