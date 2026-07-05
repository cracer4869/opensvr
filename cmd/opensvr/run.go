package main

import (
	"sync"

	"opensvr/internal/config"
	"opensvr/internal/metrics"
	"opensvr/internal/server"
	"opensvr/internal/web"
)

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

	w := web.New(m)
	if err := w.Start(cfg.Web.Port); err != nil {
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
		once.Do(func() { close(stopCh) })
	}
	return stop, url, nil
}
