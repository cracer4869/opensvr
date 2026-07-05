package logbus

import (
	"sync"
	"time"
)

const maxEvents = 500

type Event struct {
	Time   string `json:"time"`
	Proto  string `json:"proto"`
	User   string `json:"user"`
	Action string `json:"action"`
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Msg    string `json:"msg"`
}

var (
	mu   sync.RWMutex
	ring []Event
	subs = map[int]chan Event{}
	seq  int
)

func Emit(e Event) {
	if e.Time == "" {
		e.Time = time.Now().Format("15:04:05")
	}
	mu.Lock()
	ring = append(ring, e)
	if len(ring) > maxEvents {
		ring = ring[len(ring)-maxEvents:]
	}
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
	mu.Unlock()
}

func Recent() []Event {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Event, len(ring))
	copy(out, ring)
	return out
}

func Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	mu.Lock()
	id := seq
	seq++
	subs[id] = ch
	mu.Unlock()
	return ch, func() {
		mu.Lock()
		delete(subs, id)
		close(ch)
		mu.Unlock()
	}
}

func Reset() {
	mu.Lock()
	ring = nil
	subs = map[int]chan Event{}
	seq = 0
	mu.Unlock()
}
