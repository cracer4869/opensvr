package main

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cracer4869/opensvr/internal/config"
	"github.com/cracer4869/opensvr/internal/firewall"
	"github.com/cracer4869/opensvr/internal/metrics"
	"github.com/cracer4869/opensvr/internal/server"
	"github.com/cracer4869/opensvr/internal/web"
)

// ErrAlreadyRunning 表示管理页端口已被另一个 opensvr 实例占用。
// 此时返回的 url 指向已有实例的管理页，调用方应打开它并退出本进程。
var ErrAlreadyRunning = errors.New("已有 opensvr 实例在运行")

// run 装配所有模块并启动本地 Web 管理页，返回停止函数与管理页 URL。
//
// 参数：
//   - cfgPath：config.yaml 路径（缺失则用默认配置）。
//   - baseDir：config.yaml 与 hostkey 所在基准目录（通常为 exe 同级目录）。
//
// 该函数不含任何托盘/GUI 逻辑，便于 go test 直接调用而不弹窗。
func run(cfgPath, baseDir string) (stop func(), url string, err error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, "", err
	}

	m, err := server.New(cfg, baseDir)
	if err != nil {
		return nil, "", err
	}

	// 以管理员运行时注入 netsh 控制器：协议启停自动放行/清理防火墙。
	if firewall.IsElevated() {
		m.SetFirewall(firewall.Netsh{})
	}

	w := web.New(m)
	if err := w.Start(cfg.Web.Port); err != nil {
		// 端口被占：探测占用者是否为另一个 opensvr 实例（其 /api/status 可达）。
		// 是则返回其管理页地址，调用方打开后退出，避免双实例互相干扰。
		existing := fmt.Sprintf("http://127.0.0.1:%d", cfg.Web.Port)
		cli := &http.Client{Timeout: time.Second}
		if resp, perr := cli.Get(existing + "/api/status"); perr == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil, existing, ErrAlreadyRunning
			}
		}
		return nil, "", err
	}
	url = w.URL()

	// 自动拉起上次启用的协议（启动失败不阻断，错误经各协议 Status().Err 暴露到 Web 页）。
	if cfg.FTP.Enabled {
		_ = m.StartFTP()
	}
	if cfg.SFTP.Enabled {
		_ = m.StartSFTP()
	}
	if cfg.TFTP.Enabled {
		_ = m.StartTFTP()
	}

	// 每秒采样吞吐速率，供性能曲线使用。
	stopCh := make(chan struct{})
	go metrics.Start(stopCh)

	// 按配置自动拉起系统默认浏览器。
	if cfg.AutoOpenBrowser {
		_ = web.OpenBrowser(url)
	}

	var once sync.Once
	stop = func() {
		once.Do(func() {
			close(stopCh)
			// 优雅关闭：先停协议监听（不改动 Enabled 记忆，下次启动照常自动拉起），
			// 再关管理页，最后清理本工具添加的防火墙放行规则。
			m.StopAll()
			_ = w.Stop()
			m.RemoveFirewallRules()
		})
	}
	return stop, url, nil
}
