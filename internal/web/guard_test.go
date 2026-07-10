package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"opensvr/internal/config"
	"opensvr/internal/server"
)

// newTestWeb 构造一个带默认配置的 Web 实例（不启动监听）。
func newTestWeb(t *testing.T) *Web {
	t.Helper()
	cfg := config.Default()
	cfg.RootDir = t.TempDir()
	m, err := server.New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(m)
}

func TestGuardRejectsForeignHost(t *testing.T) {
	w := newTestWeb(t)
	h := guard(w.handler())
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Host = "evil.example.com:31944" // DNS rebinding：外部域名解析到 127.0.0.1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非本机 Host 应 403, 实得 %d", rec.Code)
	}
}

func TestGuardRejectsForeignOriginPost(t *testing.T) {
	w := newTestWeb(t)
	h := guard(w.handler())
	req := httptest.NewRequest("POST", "/api/proto/ftp/start", nil)
	req.Host = "127.0.0.1:31944"
	req.Header.Set("Origin", "http://evil.example.com") // 本机浏览器里恶意页面的跨站请求
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("跨站 Origin 的 POST 应 403, 实得 %d", rec.Code)
	}
}

func TestGuardAllowsLocal(t *testing.T) {
	w := newTestWeb(t)
	h := guard(w.handler())
	for _, tc := range []struct {
		method, origin string
	}{
		{"GET", ""},
		{"POST", ""},                       // curl/无 Origin
		{"POST", "http://127.0.0.1:31944"}, // 同源 fetch
		{"POST", "http://localhost:31944"}, // localhost 访问
	} {
		req := httptest.NewRequest(tc.method, "/api/status", nil)
		req.Host = "127.0.0.1:31944"
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("本机请求(%s Origin=%q)不应被拦截", tc.method, tc.origin)
		}
	}
}

func TestStartPortConflictErrors(t *testing.T) {
	w1 := newTestWeb(t)
	if err := w1.Start(0); err != nil { // 随机端口
		t.Fatal(err)
	}
	defer w1.Stop()
	port, err := strconv.Atoi(w1.URL()[strings.LastIndex(w1.URL(), ":")+1:])
	if err != nil {
		t.Fatal(err)
	}

	w2 := newTestWeb(t)
	if err := w2.Start(port); err == nil { // 同端口再启动必须显式报错，而非静默失败
		w2.Stop()
		t.Fatal("端口被占时 Start 应返回错误")
	}
}

func TestProtoUnknownActionRejected(t *testing.T) {
	w := newTestWeb(t)
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/proto/ftp/whatever", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知 action 应 400 而非当作 stop, 实得 %d", rec.Code)
	}
}
