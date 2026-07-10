package auth

import (
	"testing"

	"github.com/cracer4869/opensvr/internal/config"
)

func TestAuthenticate(t *testing.T) {
	s := New(config.AuthCfg{User: "admin", Pass: "admin", Anonymous: false})
	if ok, _ := s.Authenticate("admin", "admin"); !ok {
		t.Fatal("valid creds rejected")
	}
	if ok, _ := s.Authenticate("admin", "wrong"); ok {
		t.Fatal("bad pass accepted")
	}
	if ok, _ := s.Authenticate("anonymous", "x"); ok {
		t.Fatal("anon should be off")
	}
}

func TestAnonymous(t *testing.T) {
	s := New(config.AuthCfg{User: "admin", Pass: "admin", Anonymous: true})
	ok, anon := s.Authenticate("anonymous", "whatever")
	if !ok || !anon {
		t.Fatal("anonymous login should pass when enabled")
	}
}
