package main

import (
	"os"
	"path/filepath"

	"github.com/getlantern/systray"

	"opensvr/internal/web"
)

// 托盘运行期状态：由 onReady 填充，onExit 消费。
var (
	trayStop func()
	trayURL  string
)

func main() {
	systray.Run(onReady, onExit)
}

// baseDir 返回可执行文件所在目录；取不到时回退到当前工作目录。
// config.yaml、hostkey、默认 data 目录均相对它定位，保证绿色便携。
func baseDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

// onReady 在托盘就绪后装配并启动服务，并建立菜单。
func onReady() {
	systray.SetTitle("opensvr")
	systray.SetTooltip("三协议便携开局服务端")

	dir := baseDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	stop, url, err := run(cfgPath, dir)
	if err != nil {
		systray.SetTooltip("opensvr 启动失败: " + err.Error())
	} else {
		trayStop = stop
		trayURL = url
		systray.SetTooltip("opensvr 管理页: " + url)
	}

	mOpen := systray.AddMenuItem("打开管理页", "在浏览器中打开本地管理页")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "停止服务并退出")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				if trayURL != "" {
					_ = web.OpenBrowser(trayURL)
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// onExit 在托盘退出时收尾，停止采样等后台任务。
func onExit() {
	if trayStop != nil {
		trayStop()
	}
}
