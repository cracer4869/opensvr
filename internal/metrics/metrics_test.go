package metrics

import "testing"

func TestCountersAndSample(t *testing.T) {
	Reset()
	AddUp(1000)
	AddDown(500)
	s := Snapshot()
	if s.UpTotal != 1000 || s.DownTotal != 500 {
		t.Fatalf("totals wrong: %+v", s)
	}
}

func TestSampleRateComputation(t *testing.T) {
	Reset()
	AddUp(2000)
	// 手动推进一个采样周期（1s 视为 dt=1）
	sampleOnce(1.0)
	h := History()
	if len(h) == 0 {
		t.Fatal("history empty")
	}
	if h[len(h)-1].UpBps != 2000 {
		t.Fatalf("up bps = %v, want 2000", h[len(h)-1].UpBps)
	}
}

func TestHistoryCapped(t *testing.T) {
	Reset()
	for i := 0; i < 200; i++ {
		sampleOnce(1.0)
	}
	if len(History()) > maxPoints {
		t.Fatalf("history not capped: %d", len(History()))
	}
}
