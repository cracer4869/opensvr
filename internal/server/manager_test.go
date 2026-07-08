package server

import (
	"testing"

	"opensvr/internal/config"
)

func TestManagerStartStopFTP(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	cfg.FTP.Port = 0
	m, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StartFTP(); err != nil {
		t.Fatal(err)
	}
	if !m.Statuses()["ftp"].Running {
		t.Fatal("ftp should be running")
	}
	if err := m.StopFTP(); err != nil {
		t.Fatal(err)
	}
	if m.Statuses()["ftp"].Running {
		t.Fatal("ftp should be stopped")
	}
}

func TestSetRootRebuilds(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := New(cfg, t.TempDir())
	nd := t.TempDir()
	if err := m.SetRoot(nd); err != nil {
		t.Fatal(err)
	}
	if m.Config().RootDir != nd {
		t.Fatal("root not updated")
	}
}
