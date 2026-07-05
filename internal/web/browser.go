package web

import "os/exec"

// OpenBrowser 用系统默认浏览器打开 url（Windows）。
func OpenBrowser(url string) error {
	// cmd /c start "" <url>：第一个空引号是 start 的窗口标题占位，避免 url 被当作标题。
	return exec.Command("cmd", "/c", "start", "", url).Start()
}
