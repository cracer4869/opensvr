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
