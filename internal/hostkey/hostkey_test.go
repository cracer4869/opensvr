package hostkey

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hostkey")
	s1, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(s1.PublicKey().Marshal()) != string(s2.PublicKey().Marshal()) {
		t.Fatal("host key not persisted (changed between loads)")
	}
}
