package sessions

import "testing"

func TestAddListRemove(t *testing.T) {
	Reset()
	id := NewID()
	Add(&Session{ID: id, Proto: "sftp", Remote: "1.2.3.4:5", User: "admin", Action: "connected"})
	if got := List(); len(got) != 1 || got[0].Proto != "sftp" {
		t.Fatalf("list = %+v", got)
	}
	AddBytes(id, 100)
	AddBytes(id, 50)
	if List()[0].Bytes != 150 {
		t.Fatalf("bytes = %d, want 150", List()[0].Bytes)
	}
	Update(id, func(s *Session) { s.Action = "upload"; s.File = "fw.bin" })
	if List()[0].Action != "upload" || List()[0].File != "fw.bin" {
		t.Fatalf("update failed: %+v", List()[0])
	}
	Remove(id)
	if len(List()) != 0 {
		t.Fatal("session not removed")
	}
}

func TestListOrderedByID(t *testing.T) {
	Reset()
	a, b, c := NewID(), NewID(), NewID()
	Add(&Session{ID: c, Proto: "tftp"})
	Add(&Session{ID: a, Proto: "ftp"})
	Add(&Session{ID: b, Proto: "sftp"})
	got := List()
	if got[0].ID != a || got[1].ID != b || got[2].ID != c {
		t.Fatalf("order wrong: %v %v %v", got[0].ID, got[1].ID, got[2].ID)
	}
}
