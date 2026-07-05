package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"opensvr/internal/logbus"
	"opensvr/internal/metrics"
	"opensvr/internal/netinfo"
	"opensvr/internal/server"
)

//go:embed assets/*
var assetsFS embed.FS

// Web 是本地管理页 HTTP 服务，聚合 server.Manager、netinfo、logbus、metrics。
type Web struct {
	m    *server.Manager
	srv  *http.Server
	port int
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
	mux.HandleFunc("/api/port", w.handlePort)
	mux.HandleFunc("/api/auth", w.handleAuth)
	mux.HandleFunc("/api/firewall", w.handleFirewall)
	mux.HandleFunc("/api/events", w.handleEvents)
	mux.HandleFunc("/api/metrics", w.handleMetrics)

	return mux
}

// Start 在指定端口启动 HTTP 服务（绑定 127.0.0.1）。
func (w *Web) Start(port int) error {
	w.port = port
	w.srv = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: w.handler(),
	}
	go func() { _ = w.srv.ListenAndServe() }()
	return nil
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
		"nics":         netinfo.List(),
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

// handleProto 处理 /api/proto/{ftp|sftp|tftp}/{start|stop}
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
	switch proto {
	case "ftp":
		if action == "start" {
			err = w.m.StartFTP()
		} else {
			err = w.m.StopFTP()
		}
	case "sftp":
		if action == "start" {
			err = w.m.StartSFTP()
		} else {
			err = w.m.StopSFTP()
		}
	case "tftp":
		if action == "start" {
			err = w.m.StartTFTP()
		} else {
			err = w.m.StopTFTP()
		}
	default:
		http.Error(rw, "unknown proto", http.StatusBadRequest)
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

// handleFirewall 调 netsh 加入站放行；失败返回错误文本。
func (w *Web) handleFirewall(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := w.m.Config()
	rules := []struct {
		name  string
		proto string
		port  int
	}{
		{"opensvr-ftp", "TCP", cfg.FTP.Port},
		{"opensvr-sftp", "TCP", cfg.SFTP.Port},
		{"opensvr-tftp", "UDP", cfg.TFTP.Port},
		{"opensvr-ftp-passive", "TCP", 0}, // 被动段单独处理
	}
	var msgs []string
	var failed bool
	for _, ru := range rules {
		if ru.port == 0 {
			continue
		}
		out, err := runNetsh(ru.name, ru.proto, strconv.Itoa(ru.port))
		if err != nil {
			failed = true
			msgs = append(msgs, fmt.Sprintf("%s: %v %s", ru.name, err, out))
		} else {
			msgs = append(msgs, fmt.Sprintf("%s: ok", ru.name))
		}
	}
	// 被动端口段
	if cfg.PassiveRange[1] > 0 {
		portRange := fmt.Sprintf("%d-%d", cfg.PassiveRange[0], cfg.PassiveRange[1])
		out, err := runNetsh("opensvr-ftp-passive", "TCP", portRange)
		if err != nil {
			failed = true
			msgs = append(msgs, fmt.Sprintf("passive: %v %s", err, out))
		} else {
			msgs = append(msgs, "passive: ok")
		}
	}
	resp := map[string]any{"ok": !failed, "detail": msgs}
	if failed {
		rw.WriteHeader(http.StatusInternalServerError)
	}
	writeJSON(rw, resp)
}

// runNetsh 添加一条 Windows 防火墙入站放行规则。
func runNetsh(name, proto, port string) (string, error) {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+name, "dir=in", "action=allow",
		"protocol="+proto, "localport="+port)
	out, err := cmd.CombinedOutput()
	return string(out), err
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
	var out []string
	cur := ""
	for _, c := range p {
		if c == '/' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
