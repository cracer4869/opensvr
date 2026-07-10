package web

import (
	"encoding/json"
	"net/http"

	"opensvr/internal/config"
	"opensvr/internal/sessions"
)

// handleSessions 返回当前活跃连接/传输。
func (w *Web) handleSessions(rw http.ResponseWriter, r *http.Request) {
	writeJSON(rw, sessions.List())
}

// handlePerms 处理 POST /api/perms，设置目录操作权限（读/写/列目录/新建/删除/改名）。
func (w *Web) handlePerms(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p config.Perms
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	w.m.SetPerms(p)
	writeJSON(rw, map[string]any{"ok": true, "perms": w.m.Config().Perms})
}

// handlePickDir 处理 POST /api/pickdir：在本机弹出 Windows 原生文件夹选择框，
// 用户选定后即设为共享根目录并返回结果；取消则不改动。
// 单飞：对话框未关闭前的重复请求直接返回 409，避免叠出多个 STA 对话框。
func (w *Web) handlePickDir(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !w.picking.CompareAndSwap(false, true) {
		http.Error(rw, "目录选择框已打开，请先完成或取消", http.StatusConflict)
		return
	}
	defer w.picking.Store(false)
	dir, err := pickFolder()
	if err != nil {
		http.Error(rw, "弹出目录选择框失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if dir == "" { // 用户取消
		writeJSON(rw, map[string]any{"cancelled": true})
		return
	}
	if err := w.m.SetRoot(dir); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(rw, map[string]any{
		"root":         w.m.ActualRoot(),
		"root_warning": w.m.RootWarning(),
	})
}
