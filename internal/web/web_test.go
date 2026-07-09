package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensvr/internal/config"
	"opensvr/internal/server"
)

func TestStatusEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["nics"]; !ok {
		t.Fatal("status missing nics")
	}
	if _, ok := body["auth"]; !ok {
		t.Fatal("status missing auth")
	}
	if _, ok := body["ports"]; !ok {
		t.Fatal("status missing ports")
	}
}

func TestStartStopViaAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	cfg.FTP.Port = 0
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/proto/ftp/start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("start code=%d", rec.Code)
	}
	if !m.Statuses()["ftp"].Running {
		t.Fatal("ftp not started via api")
	}
	rec = httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/proto/ftp/stop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stop code=%d", rec.Code)
	}
	if m.Statuses()["ftp"].Running {
		t.Fatal("ftp not stopped via api")
	}
}

func TestSetRootViaAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	nd := t.TempDir()
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"dir":"` + strings.ReplaceAll(nd, `\`, `\\`) + `"}`)
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/root", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("root code=%d body=%s", rec.Code, rec.Body.String())
	}
	if m.Config().RootDir != nd {
		t.Fatalf("root not updated: %s", m.Config().RootDir)
	}
}

func TestSetPortViaAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"proto":"ftp","port":2121}`)
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/port", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("port code=%d", rec.Code)
	}
	if m.Config().FTP.Port != 2121 {
		t.Fatalf("port not updated: %d", m.Config().FTP.Port)
	}
}

func TestSetAuthViaAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"user":"root","pass":"secret","anonymous":true}`)
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/auth", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("auth code=%d", rec.Code)
	}
	u, p, anon := m.Auth().Creds()
	if u != "root" || p != "secret" || !anon {
		t.Fatalf("auth not updated: %s/%s/%v", u, p, anon)
	}
}

func TestIndexServed(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<html") && !strings.Contains(rec.Body.String(), "<!DOCTYPE") {
		t.Fatalf("index not html: %s", rec.Body.String()[:min(80, len(rec.Body.String()))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestSetPermsViaAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"read":true,"write":false,"list":true,"mkdir":false,"delete":false,"rename":true}`)
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/perms", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("perms code=%d", rec.Code)
	}
	p := m.Config().Perms
	if !p.Read || p.Write || !p.List || p.Mkdir || p.Delete || !p.Rename {
		t.Fatalf("perms not applied: %+v", p)
	}
}

func TestStatusIncludesPerms(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	perms, ok := body["perms"].(map[string]any)
	if !ok {
		t.Fatalf("status missing perms object: %v", body["perms"])
	}
	// 必须是小写键且默认全开（前端按小写读取）
	for _, k := range []string{"read", "write", "list", "mkdir", "delete", "rename"} {
		v, ok := perms[k].(bool)
		if !ok {
			t.Fatalf("perms.%s 缺失或非布尔（键须小写）: %+v", k, perms)
		}
		if !v {
			t.Fatalf("perms.%s 默认应为 true", k)
		}
	}
}

func TestStatusIncludesElevated(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["elevated"].(bool); !ok {
		t.Fatalf("status 应含布尔字段 elevated, 实得 %v", body["elevated"])
	}
}

func TestSessionsEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions code=%d", rec.Code)
	}
	var arr []any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("sessions not a json array: %v", err)
	}
}
