package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"opensvr/internal/config"
	"opensvr/internal/metrics"
)

func TestJailBlocksTraversal(t *testing.T) {
	root := t.TempDir()
	v, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	// 试图越界读取上级文件应失败
	if _, err := v.Fs().Open("../secret.txt"); err == nil {
		t.Fatal("traversal should be blocked")
	}
}

func TestWriteReadCounts(t *testing.T) {
	metrics.Reset()
	root := t.TempDir()
	v, _ := New(root)
	f, err := v.Fs().Create("a.bin")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("hello"))
	f.Close()
	if metrics.Snapshot().UpTotal != 5 {
		t.Fatalf("up total = %d, want 5", metrics.Snapshot().UpTotal)
	}

	rf, _ := v.Fs().Open("a.bin")
	buf := make([]byte, 5)
	rf.Read(buf)
	rf.Close()
	if metrics.Snapshot().DownTotal != 5 {
		t.Fatalf("down total = %d, want 5", metrics.Snapshot().DownTotal)
	}
	_ = os.WriteFile
	_ = filepath.Join
}

func TestChinesePathRoundTrip(t *testing.T) {
	root := t.TempDir()
	v, _ := New(root)
	fs := v.Fs()
	if err := fs.MkdirAll("当前开局/固件", 0755); err != nil {
		t.Fatalf("mkdir 中文目录: %v", err)
	}
	data := []byte("版本补丁内容")
	f, err := fs.Create("当前开局/固件/版本补丁.bin")
	if err != nil {
		t.Fatalf("create 中文文件: %v", err)
	}
	f.Write(data)
	f.Close()

	rf, err := fs.Open("当前开局/固件/版本补丁.bin")
	if err != nil {
		t.Fatalf("open 中文文件: %v", err)
	}
	buf := make([]byte, len(data))
	rf.Read(buf)
	rf.Close()
	if string(buf) != string(data) {
		t.Fatalf("中文路径 roundtrip mismatch: %q", string(buf))
	}
	// 确认落盘到真实的中文目录
	if _, err := os.Stat(filepath.Join(root, "当前开局", "固件", "版本补丁.bin")); err != nil {
		t.Fatalf("中文文件未落盘: %v", err)
	}
}

func TestPermsEnforced(t *testing.T) {
	v, _ := New(t.TempDir())
	fs := v.Fs()
	// 先用默认全权限建一个文件与目录
	f, _ := fs.Create("a.bin")
	f.Write([]byte("x"))
	f.Close()
	if err := fs.Mkdir("d", 0755); err != nil {
		t.Fatalf("默认应可建目录: %v", err)
	}

	// 关闭 写/新建/删除/改名 权限，只留 读/列目录
	v.SetPerms(config.Perms{Read: true, List: true})

	if _, err := fs.Create("b.bin"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("禁写后 Create 应权限拒绝, got %v", err)
	}
	if _, err := fs.OpenFile("c.bin", os.O_CREATE|os.O_WRONLY, 0644); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("禁写后 OpenFile(写) 应权限拒绝, got %v", err)
	}
	if err := fs.Mkdir("d2", 0755); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("禁新建后 Mkdir 应权限拒绝, got %v", err)
	}
	if err := fs.Remove("a.bin"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("禁删除后 Remove 应权限拒绝, got %v", err)
	}
	if err := fs.Rename("a.bin", "a2.bin"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("禁改名后 Rename 应权限拒绝, got %v", err)
	}
	// 读仍应允许
	if rf, err := fs.Open("a.bin"); err != nil {
		t.Fatalf("读权限仍在, Open 应成功: %v", err)
	} else {
		rf.Close()
	}

	// 关闭读权限后，打开文件读应被拒
	v.SetPerms(config.Perms{List: true})
	if _, err := fs.Open("a.bin"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("禁读后 Open 文件应权限拒绝, got %v", err)
	}
}
