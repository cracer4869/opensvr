package vfs

import (
	"os"
	"path/filepath"
	"testing"

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
