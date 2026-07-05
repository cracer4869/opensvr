package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
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

func TestFileManagerAPI(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	h := w.handler()

	// 1) 上传一个中文名文件
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "版本补丁.bin")
	fw.Write([]byte("固件内容"))
	mw.Close()
	req := httptest.NewRequest("POST", "/api/files/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2) 列目录应看到该文件
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/files", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list code=%d", rec.Code)
	}
	var list struct {
		Entries []fileEntry `json:"entries"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Entries) != 1 || list.Entries[0].Name != "版本补丁.bin" {
		t.Fatalf("list wrong: %+v", list.Entries)
	}

	// 3) 下载校验内容
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/files/download?path=版本补丁.bin", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "固件内容" {
		t.Fatalf("download wrong: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 4) 新建中文目录
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/files/mkdir", strings.NewReader(`{"path":"当前开局"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("mkdir code=%d", rec.Code)
	}

	// 5) 删除文件
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/files/delete", strings.NewReader(`{"path":"版本补丁.bin"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/files", nil))
	json.Unmarshal(rec.Body.Bytes(), &list)
	// 现在应只剩目录 当前开局
	for _, e := range list.Entries {
		if e.Name == "版本补丁.bin" {
			t.Fatal("文件应已删除")
		}
	}
}

func TestFileManagerJailBlocksTraversal(t *testing.T) {
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, _ := server.New(cfg, t.TempDir())
	w := New(m)
	// 试图越界下载上级文件，应被 cleanRel + afero 囚笼拦截（404/400，绝不 200）
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/files/download?path=../../secret.txt", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal should be blocked, got 200")
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
