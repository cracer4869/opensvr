package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"opensvr/internal/firewall"
	"opensvr/internal/logbus"
	"opensvr/internal/metrics"
	"opensvr/internal/netinfo"
	"opensvr/internal/server"
)

//go:embed assets/*
var assetsFS embed.FS

// Web 是本地管理页 HTTP 服务，聚合 server.Manager、netinfo、logbus、metrics。
type Web struct {
	m       *server.Manager
	srv     *http.Server
	port    int
	picking atomic.Bool // 目录选择框单飞标志：同一时刻只允许弹一个
}

// New 构造 Web。
func New(m *server.Manager) *Web { return &Web{m: m} }

// handler 组装并返回路由 mux。
func (w *Web) handler() *http.ServeMux {
	mux := http.NewServeMux()

	// 静态前端（内嵌 assets）
	sub, _ := fs.Sub(assetsFS, "assets")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/status", w.handleStatus)
	mux.HandleFunc("/api/proto/", w.handleProto)
	mux.HandleFunc("/api/root", w.handleRoot)
	mux.HandleFunc("/api/pickdir", w.handlePickDir)
	mux.HandleFunc("/api/port", w.handlePort)
	mux.HandleFunc("/api/auth", w.handleAuth)
	mux.HandleFunc("/api/perms", w.handlePerms)
	mux.HandleFunc("/api/firewall", w.handleFirewall)
	mux.HandleFunc("/api/events", w.handleEvents)
	mux.HandleFunc("/api/metrics", w.handleMetrics)
	mux.HandleFunc("/api/sessions", w.handleSessions)

	return mux
}

// localHost 判定主机名是否为本机回环。
func localHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// guard 是安全中间件：
//   - 拒绝 Host 非本机回环的请求，阻断 DNS rebinding（恶意域名解析到 127.0.0.1 后
//     读取 /api/status 中的明文口令）；
//   - POST 要求 Origin 缺省（curl/同源 fetch）或同为本机回环，阻断本机浏览器里
//     恶意网页的跨站表单/fetch 驱动管理接口（改根目录/改密码/开服务）。
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		if !localHost(strings.Trim(host, "[]")) {
			http.Error(rw, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && o != "null" {
				u, err := url.Parse(o)
				if err != nil || !localHost(u.Hostname()) {
					http.Error(rw, "forbidden origin", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(rw, r)
	})
}

// Start 在指定端口启动 HTTP 服务（绑定 127.0.0.1）。
// 先同步 Listen 拿到绑定结果：端口被占（如已开着另一个 opensvr 实例）立即报错，
// 而不是静默失败让托盘/浏览器指向别的进程。
func (w *Web) Start(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("管理页端口 %d 监听失败: %w", port, err)
	}
	w.port = ln.Addr().(*net.TCPAddr).Port
	w.srv = &http.Server{Handler: guard(w.handler())}
	go func() { _ = w.srv.Serve(ln) }()
	return nil
}

// Stop 优雅关闭 HTTP 服务（限时，超时后强制返回）。
func (w *Web) Stop() error {
	if w.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return w.srv.Shutdown(ctx)
}

// URL 返回管理页地址。
func (w *Web) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", w.port)
}

// ----- REST handlers -----

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(rw).Encode(v)
}

func (w *Web) handleStatus(rw http.ResponseWriter, r *http.Request) {
	st := w.m.Statuses()
	cfg := w.m.Config()
	user, pass, anon := w.m.Auth().Creds()

	resp := map[string]any{
		"ftp":          st["ftp"],
		"sftp":         st["sftp"],
		"tftp":         st["tftp"],
		"root":         w.m.ActualRoot(),
		"root_config":  cfg.RootDir,
		"root_warning": w.m.RootWarning(),
		"perms":        cfg.Perms,
		"nics":         netinfo.List(),
		"elevated":     firewall.IsElevated(),
		"auth": map[string]any{
			"user":      user,
			"pass":      pass,
			"anonymous": anon,
		},
		"ports": map[string]int{
			"ftp":  cfg.FTP.Port,
			"sftp": cfg.SFTP.Port,
			"tftp": cfg.TFTP.Port,
			"web":  cfg.Web.Port,
		},
	}
	writeJSON(rw, resp)
}

func (w *Web) handleProto(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 解析路径：/api/proto/<proto>/<action>
	rest := r.URL.Path[len("/api/proto/"):]
	parts := splitPath(rest)
	if len(parts) != 2 {
		http.Error(rw, "bad path", http.StatusBadRequest)
		return
	}
	proto, action := parts[0], parts[1]

	var err error
	switch proto + "/" + action {
	case "ftp/start":
		err = w.m.StartFTP()
	case "ftp/stop":
		err = w.m.StopFTP()
	case "sftp/start":
		err = w.m.StartSFTP()
	case "sftp/stop":
		err = w.m.StopSFTP()
	case "tftp/start":
		err = w.m.StartTFTP()
	case "tftp/stop":
		err = w.m.StopTFTP()
	default:
		http.Error(rw, "unknown proto/action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(rw, w.m.Statuses())
}

func (w *Web) handleRoot(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := w.m.SetRoot(body.Dir); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(rw, map[string]string{"root": w.m.Config().RootDir})
}

func (w *Web) handlePort(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Proto string `json:"proto"`
		Port  int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := w.m.SetPort(body.Proto, body.Port); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(rw, map[string]any{"ok": true})
}

func (w *Web) handleAuth(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		User      string `json:"user"`
		Pass      string `json:"pass"`
		Anonymous bool   `json:"anonymous"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := w.m.Config().Auth
	cfg.User = body.User
	cfg.Pass = body.Pass
	cfg.Anonymous = body.Anonymous
	w.m.SetAuth(cfg)
	writeJSON(rw, map[string]any{"ok": true})
}

// handleFirewall 手动放行三协议端口：复用 firewall.Netsh 的幂等实现（先删后加，
// 多次点击不累积重复规则），规则名与自动放行一致，退出清理时可一并删除。
func (w *Web) handleFirewall(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !firewall.IsElevated() {
		writeJSON(rw, map[string]any{"ok": false, "detail": []string{"需要以管理员身份运行才能修改防火墙规则"}})
		return
	}
	rules := w.m.FirewallRules()
	var msgs []string
	if err := (firewall.Netsh{}).Allow(rules); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		writeJSON(rw, map[string]any{"ok": false, "detail": []string{err.Error()}})
		return
	}
	for _, ru := range rules {
		msgs = append(msgs, fmt.Sprintf("%s(%s %s): ok", ru.Name, ru.Proto, ru.Port))
	}
	writeJSON(rw, map[string]any{"ok": true, "detail": msgs})
}

// handleEvents SSE：推送 logbus 事件。
func (w *Web) handleEvents(rw http.ResponseWriter, r *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")

	// 先回放最近事件
	for _, e := range logbus.Recent() {
		writeSSE(rw, e)
	}
	flusher.Flush()

	ch, cancel := logbus.Subscribe()
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(rw, e)
			flusher.Flush()
		}
	}
}

// handleMetrics SSE：每秒推送 metrics 快照 + 历史。
func (w *Web) handleMetrics(rw http.ResponseWriter, r *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")

	send := func() {
		payload := map[string]any{
			"snapshot": metrics.Snapshot(),
			"history":  metrics.History(),
		}
		writeSSE(rw, payload)
		flusher.Flush()
	}
	send()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

func writeSSE(rw http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(rw, "data: %s\n\n", b)
}

// splitPath 按 '/' 分割并去除空段。
func splitPath(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}
