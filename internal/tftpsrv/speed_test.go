package tftpsrv

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"github.com/pin/tftp/v3"

	"opensvr/internal/vfs"
)

// TestTFTPThroughputBaseline 测量 loopback 下载/上传吞吐，防止性能回退。
// 默认 512B 块逐包应答，本机回环也应达到 MB/s 量级；若跌到 KB/s 说明服务端有逐包延迟问题。
func TestTFTPThroughputBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("perf smoke test, skip in -short")
	}
	v, _ := vfs.New(t.TempDir())
	s := New(v, 0)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	const size = 4 << 20 // 4MB
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}

	c, _ := tftp.NewClient(s.Addr())

	// 上传（设备→服务端）
	start := time.Now()
	wt, err := c.Send("speed.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.ReadFrom(bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	upDur := time.Since(start)

	// 下载（服务端→设备）
	start = time.Now()
	rt, err := c.Receive("speed.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	if _, err := rt.WriteTo(buf); err != nil {
		t.Fatal(err)
	}
	downDur := time.Since(start)

	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatal("内容不一致")
	}
	upMBps := float64(size) / (1 << 20) / upDur.Seconds()
	downMBps := float64(size) / (1 << 20) / downDur.Seconds()
	t.Logf("上传 4MB 耗时 %v (%.2f MB/s)，下载耗时 %v (%.2f MB/s)", upDur, upMBps, downDur, downMBps)

	_ = io.Discard
	// loopback 512B 逐包也应远高于 0.5MB/s；低于此值视为服务端逐包处理异常。
	if upMBps < 0.5 || downMBps < 0.5 {
		t.Fatalf("loopback 吞吐异常偏低: 上传 %.2f MB/s, 下载 %.2f MB/s", upMBps, downMBps)
	}
}
