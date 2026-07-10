package tftpsrv

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/cracer4869/opensvr/internal/vfs"
)

// TestTFTPAnticipateWindowSendsAhead 验证服务端启用提前连发窗口：
// 客户端只发 RRQ 不回 ACK，服务端也应在极短时间内连发多个 DATA 块，
// 而不是每块都等 ACK（lock-step 会让现网 RTT/丢包把速度拖到 KB/s）。
func TestTFTPAnticipateWindowSendsAhead(t *testing.T) {
	v, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 4KB 文件 = 8 个 512B 块，足够观察连发。
	if err := afero.WriteFile(v.Fs(), "/win.bin", make([]byte, 4096), 0644); err != nil {
		t.Fatal(err)
	}
	s := New(v, 0)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(100 * time.Millisecond)

	srvAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", s.Status().Port))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// RRQ: opcode 1 + "win.bin" 0 + "octet" 0（无选项，纯经典客户端行为）
	rrq := append([]byte{0, 1}, []byte("win.bin\x00octet\x00")...)
	if _, err := conn.WriteToUDP(rrq, srvAddr); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	// 第一个 DATA 块正常到达
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, dataAddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("未收到第一个 DATA: %v", err)
	}
	if op := binary.BigEndian.Uint16(buf[0:2]); op != 3 {
		t.Fatalf("期望 DATA(3)，收到 opcode=%d", op)
	}
	if blk := binary.BigEndian.Uint16(buf[2:4]); blk != 1 {
		t.Fatalf("期望块号 1，收到 %d", blk)
	}

	// 不回 ACK：1.5 秒内应收到块号 2（提前连发）。
	// lock-step 服务端此时只会干等 ACK（默认 5 秒后才重发块 1），导致本读超时。
	conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	n, _, err = conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("未提前连发：1.5s 内没有收到后续 DATA（lock-step 行为）: %v", err)
	}
	if op := binary.BigEndian.Uint16(buf[0:2]); op != 3 {
		t.Fatalf("期望 DATA(3)，收到 opcode=%d", op)
	}
	if blk := binary.BigEndian.Uint16(buf[2:4]); blk != 2 {
		t.Fatalf("期望提前收到块号 2，收到 %d", blk)
	}
	_ = n

	// 通知服务端终止本次传输，避免后台重传逗留。
	errPkt := append([]byte{0, 5, 0, 0}, []byte("test done\x00")...)
	conn.WriteToUDP(errPkt, dataAddr)
}
