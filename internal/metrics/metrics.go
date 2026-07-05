package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

const maxPoints = 60

type Sample struct {
	UpBps, DownBps     float64
	UpTotal, DownTotal int64
}

var (
	upTotal, downTotal int64
	lastUp, lastDown   int64
	mu                 sync.Mutex
	hist               []Sample
)

func AddUp(n int64)   { atomic.AddInt64(&upTotal, n) }
func AddDown(n int64) { atomic.AddInt64(&downTotal, n) }

func Snapshot() Sample {
	mu.Lock()
	defer mu.Unlock()
	s := Sample{UpTotal: atomic.LoadInt64(&upTotal), DownTotal: atomic.LoadInt64(&downTotal)}
	if len(hist) > 0 {
		s.UpBps = hist[len(hist)-1].UpBps
		s.DownBps = hist[len(hist)-1].DownBps
	}
	return s
}

func History() []Sample {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Sample, len(hist))
	copy(out, hist)
	return out
}

// sampleOnce 用 dt 秒计算这一周期速率并入队（测试可注入 dt）。
func sampleOnce(dt float64) {
	u := atomic.LoadInt64(&upTotal)
	d := atomic.LoadInt64(&downTotal)
	mu.Lock()
	s := Sample{
		UpBps:     float64(u-lastUp) / dt,
		DownBps:   float64(d-lastDown) / dt,
		UpTotal:   u,
		DownTotal: d,
	}
	lastUp, lastDown = u, d
	hist = append(hist, s)
	if len(hist) > maxPoints {
		hist = hist[len(hist)-maxPoints:]
	}
	mu.Unlock()
}

func Reset() {
	mu.Lock()
	defer mu.Unlock()
	atomic.StoreInt64(&upTotal, 0)
	atomic.StoreInt64(&downTotal, 0)
	lastUp, lastDown = 0, 0
	hist = nil
}

func Start(stop <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			sampleOnce(1.0)
		}
	}
}
