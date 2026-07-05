package web

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

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
func (w *Web) handlePickDir(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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

// pickFolder 调用 PowerShell 的 FolderBrowserDialog 弹出原生文件夹选择框，
// 返回所选绝对路径（UTF-8）；用户取消时返回空字符串。
func pickFolder() (string, error) {
	const ps = `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;` +
		`Add-Type -AssemblyName System.Windows.Forms;` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog;` +
		`$d.Description='选择开局文件根目录';$d.ShowNewFolderButton=$true;` +
		`if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Out.Write($d.SelectedPath)}`
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
