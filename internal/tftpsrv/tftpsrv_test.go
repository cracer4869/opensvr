package tftpsrv

import (
	"bytes"
	"testing"
	"time"

	"github.com/pin/tftp/v3"

	"github.com/cracer4869/opensvr/internal/vfs"
)

func TestTFTPWriteThenRead(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	s := New(v, 0)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	c, _ := tftp.NewClient(s.Addr())
	// 写
	wt, err := c.Send("boot.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	src := bytes.NewReader([]byte("bootimg"))
	if _, err := wt.ReadFrom(src); err != nil {
		t.Fatal(err)
	}
	// 读
	rt, err := c.Receive("boot.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	if _, err := rt.WriteTo(buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "bootimg" {
		t.Fatalf("tftp roundtrip mismatch: %q", buf.String())
	}
}

func TestTFTPChineseFilename(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	s := New(v, 0)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	c, _ := tftp.NewClient(s.Addr())
	wt, err := c.Send("版本补丁.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.ReadFrom(bytes.NewReader([]byte("固件内容"))); err != nil {
		t.Fatal(err)
	}
	rt, err := c.Receive("版本补丁.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	if _, err := rt.WriteTo(buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "固件内容" {
		t.Fatalf("中文文件名 roundtrip mismatch: %q", buf.String())
	}
}

// TestTFTPLargeBlockNegotiation 客户端协商大块（blksize 选项）时上传/下载均应完整无误。
func TestTFTPLargeBlockNegotiation(t *testing.T) {
	v, _ := vfs.New(t.TempDir())
	s := New(v, 0)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	payload := make([]byte, 1<<20) // 1MB
	for i := range payload {
		payload[i] = byte(i * 31)
	}

	c, _ := tftp.NewClient(s.Addr())
	c.SetBlockSize(1428) // 常见设备协商值（1500 MTU 内）
	wt, err := c.Send("big.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.ReadFrom(bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	rt, err := c.Receive("big.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	if _, err := rt.WriteTo(buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("大块协商 roundtrip 内容不一致: got %d bytes", buf.Len())
	}
}
