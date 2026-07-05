package tftpsrv

import (
	"bytes"
	"testing"
	"time"

	"github.com/pin/tftp"

	"opensvr/internal/vfs"
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
