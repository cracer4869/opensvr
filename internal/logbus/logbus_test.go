package logbus

import "testing"

func TestEmitAndRecent(t *testing.T) {
	Reset()
	Emit(Event{Proto: "ftp", Action: "upload", OK: true})
	if len(Recent()) != 1 {
		t.Fatalf("recent len = %d", len(Recent()))
	}
}

func TestSubscribeReceives(t *testing.T) {
	Reset()
	ch, cancel := Subscribe()
	defer cancel()
	Emit(Event{Proto: "sftp", Action: "download", OK: true})
	select {
	case e := <-ch:
		if e.Proto != "sftp" {
			t.Fatalf("got %+v", e)
		}
	default:
		t.Fatal("subscriber did not receive event")
	}
}

func TestRingCapped(t *testing.T) {
	Reset()
	for i := 0; i < maxEvents+50; i++ {
		Emit(Event{Proto: "tftp"})
	}
	if len(Recent()) > maxEvents {
		t.Fatalf("ring not capped: %d", len(Recent()))
	}
}
