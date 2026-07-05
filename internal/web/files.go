package web

import (
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/spf13/afero"

	"opensvr/internal/logbus"
	"opensvr/internal/sessions"
)

// fileEntry 是文件管理器列表项。
type fileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
}

// cleanRel 归一化前端传入的相对路径，去掉盘符/前导分隔与 .. 越界（afero 囚笼再兜底）。
func cleanRel(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return strings.TrimPrefix(p, "/")
}

func (w *Web) fs() afero.Fs { return w.m.VFS().Fs() }

// handleSessions 返回当前活跃连接/传输。
func (w *Web) handleSessions(rw http.ResponseWriter, r *http.Request) {
	writeJSON(rw, sessions.List())
}

// handleFiles 处理 GET /api/files?path=<rel>，列出目录内容。
func (w *Web) handleFiles(rw http.ResponseWriter, r *http.Request) {
	dir := cleanRel(r.URL.Query().Get("path"))
	openPath := dir
	if openPath == "" {
		openPath = "."
	}
	f, err := w.fs().Open(openPath)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()
	infos, err := f.Readdir(-1)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	entries := make([]fileEntry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, fileEntry{
			Name:  fi.Name(),
			IsDir: fi.IsDir(),
			Size:  fi.Size(),
			MTime: fi.ModTime().Format("2006-01-02 15:04"),
		})
	}
	writeJSON(rw, map[string]any{"path": dir, "entries": entries})
}

// handleFilesMkdir 处理 POST /api/files/mkdir {path}。
func (w *Web) handleFilesMkdir(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	rel := cleanRel(body.Path)
	if rel == "" {
		http.Error(rw, "空路径", http.StatusBadRequest)
		return
	}
	if err := w.fs().MkdirAll(rel, 0755); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	logbus.Emit(logbus.Event{Proto: "web", Action: "mkdir", Path: rel, OK: true})
	writeJSON(rw, map[string]any{"ok": true})
}

// handleFilesDelete 处理 POST /api/files/delete {path}，删除文件或整个目录。
func (w *Web) handleFilesDelete(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	rel := cleanRel(body.Path)
	if rel == "" {
		http.Error(rw, "不能删除根目录", http.StatusBadRequest)
		return
	}
	if err := w.fs().RemoveAll(rel); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	logbus.Emit(logbus.Event{Proto: "web", Action: "delete", Path: rel, OK: true})
	writeJSON(rw, map[string]any{"ok": true})
}

// handleFilesUpload 处理 POST /api/files/upload?path=<rel>，接收 multipart 上传并存入根目录。
func (w *Web) handleFilesUpload(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := cleanRel(r.URL.Query().Get("path"))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(rw, "无上传文件", http.StatusBadRequest)
		return
	}
	var saved []string
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		dest := path.Join(dir, path.Base(fh.Filename))
		out, err := w.fs().Create(dest)
		if err != nil {
			src.Close()
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(out, src)
		out.Close()
		src.Close()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		saved = append(saved, dest)
		logbus.Emit(logbus.Event{Proto: "web", User: "admin", Action: "upload", Path: dest, OK: true})
	}
	writeJSON(rw, map[string]any{"ok": true, "saved": saved})
}

// handleFilesDownload 处理 GET /api/files/download?path=<rel>，回传文件。
func (w *Web) handleFilesDownload(rw http.ResponseWriter, r *http.Request) {
	rel := cleanRel(r.URL.Query().Get("path"))
	if rel == "" {
		http.Error(rw, "空路径", http.StatusBadRequest)
		return
	}
	f, err := w.fs().Open(rel)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		http.Error(rw, "不是文件", http.StatusBadRequest)
		return
	}
	name := path.Base(rel)
	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(name))
	logbus.Emit(logbus.Event{Proto: "web", User: "admin", Action: "download", Path: rel, OK: true})
	io.Copy(rw, f)
}

// urlEscape 对文件名做百分号编码（支持中文文件名的 Content-Disposition）。
func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		// 不编码的安全字符
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
