package server

import (
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"

	"opensvr/internal/auth"
	"opensvr/internal/config"
	"opensvr/internal/ftpsrv"
	"opensvr/internal/hostkey"
	"opensvr/internal/sftpsrv"
	"opensvr/internal/tftpsrv"
	"opensvr/internal/vfs"
)

// Status 是三协议统一的运行状态（Web/前端统一使用）。
type Status struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	Err     string `json:"err"`
}

// Manager 持有三协议实例与共享的 vfs/auth，提供统一启停、根目录、端口、账号管理。
type Manager struct {
	mu sync.Mutex

	cfg         *config.Config
	cfgPath     string
	hostkeyPath string
	baseDir     string
	rootWarning string

	v      *vfs.VFS
	a      *auth.Store
	signer ssh.Signer

	ftp  *ftpsrv.Server
	sftp *sftpsrv.Server
	tftp *tftpsrv.Server
}

// New 依据 cfg 构造 Manager。dir 为 config.yaml 与 hostkey 所在的基准目录（通常为 exe 同级）。
// 根目录经 config.ResolveRoot 解析（支持可移植默认/相对路径/换机回退）。
func New(cfg *config.Config, dir string) (*Manager, error) {
	resolvedRoot, rootWarning := config.ResolveRoot(cfg.RootDir, dir)
	v, err := vfs.New(resolvedRoot)
	if err != nil {
		return nil, err
	}
	v.SetPerms(cfg.Perms)
	a := auth.New(cfg.Auth)
	hostkeyPath := filepath.Join(dir, "hostkey")
	signer, err := hostkey.LoadOrCreate(hostkeyPath)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		cfg:         cfg,
		cfgPath:     filepath.Join(dir, "config.yaml"),
		hostkeyPath: hostkeyPath,
		baseDir:     dir,
		rootWarning: rootWarning,
		v:           v,
		a:           a,
		signer:      signer,
	}
	m.buildFTP()
	m.buildSFTP()
	m.buildTFTP()
	return m, nil
}

// buildXxx 用当前 cfg 端口重建对应协议实例（调用方需持锁）。
func (m *Manager) buildFTP() {
	m.ftp = ftpsrv.New(m.v, m.a, m.cfg.FTP.Port, m.cfg.PassiveRange)
}

func (m *Manager) buildSFTP() {
	m.sftp = sftpsrv.New(m.v, m.a, m.signer, m.cfg.SFTP.Port)
}

func (m *Manager) buildTFTP() {
	m.tftp = tftpsrv.New(m.v, m.cfg.TFTP.Port)
}

// ----- 启停 -----

// StartFTP 启动 FTP 服务。
func (m *Manager) StartFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ftp.Start(); err != nil {
		return err
	}
	m.cfg.FTP.Enabled = true
	return m.save()
}

// StopFTP 停止 FTP 服务。
func (m *Manager) StopFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ftp.Stop(); err != nil {
		return err
	}
	m.cfg.FTP.Enabled = false
	return m.save()
}

// StartSFTP 启动 SFTP 服务。
func (m *Manager) StartSFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.sftp.Start(); err != nil {
		return err
	}
	m.cfg.SFTP.Enabled = true
	return m.save()
}

// StopSFTP 停止 SFTP 服务。
func (m *Manager) StopSFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.sftp.Stop(); err != nil {
		return err
	}
	m.cfg.SFTP.Enabled = false
	return m.save()
}

// StartTFTP 启动 TFTP 服务。
func (m *Manager) StartTFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.tftp.Start(); err != nil {
		return err
	}
	m.cfg.TFTP.Enabled = true
	return m.save()
}

// StopTFTP 停止 TFTP 服务。
func (m *Manager) StopTFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.tftp.Stop(); err != nil {
		return err
	}
	m.cfg.TFTP.Enabled = false
	return m.save()
}

// ----- 配置变更 -----

// SetRoot 切换根目录：更新 vfs、cfg 并持久化。dir 支持绝对或相对（相对 baseDir）路径。
func (m *Manager) SetRoot(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	resolved, warning := config.ResolveRoot(dir, m.baseDir)
	if err := m.v.SetRoot(resolved); err != nil {
		return err
	}
	m.cfg.RootDir = dir
	m.rootWarning = warning
	return m.save()
}

// SetAuth 更新账号口令：更新 auth、cfg 并持久化。
func (m *Manager) SetAuth(cfg config.AuthCfg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.a.Update(cfg)
	m.cfg.Auth = cfg
	_ = m.save()
}

// SetPerms 更新目录操作权限：即时作用于 vfs、更新 cfg 并持久化。
func (m *Manager) SetPerms(p config.Perms) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v.SetPerms(p)
	m.cfg.Perms = p
	_ = m.save()
}

// SetPort 修改指定协议端口并重建实例；若该协议在运行则先停后启。
func (m *Manager) SetPort(proto string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch proto {
	case "ftp":
		running := m.ftp.Status().Running
		if running {
			_ = m.ftp.Stop()
		}
		m.cfg.FTP.Port = port
		m.buildFTP()
		if running {
			if err := m.ftp.Start(); err != nil {
				return err
			}
		}
	case "sftp":
		running := m.sftp.Status().Running
		if running {
			_ = m.sftp.Stop()
		}
		m.cfg.SFTP.Port = port
		m.buildSFTP()
		if running {
			if err := m.sftp.Start(); err != nil {
				return err
			}
		}
	case "tftp":
		running := m.tftp.Status().Running
		if running {
			_ = m.tftp.Stop()
		}
		m.cfg.TFTP.Port = port
		m.buildTFTP()
		if running {
			if err := m.tftp.Start(); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	return m.save()
}

// ----- 状态与配置访问 -----

// Statuses 聚合三协议状态，键为 "ftp"/"sftp"/"tftp"。
func (m *Manager) Statuses() map[string]Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	ft := m.ftp.Status()
	sf := m.sftp.Status()
	tf := m.tftp.Status()
	return map[string]Status{
		"ftp":  {Running: ft.Running, Port: ft.Port, Err: ft.Err},
		"sftp": {Running: sf.Running, Port: sf.Port, Err: sf.Err},
		"tftp": {Running: tf.Running, Port: tf.Port, Err: tf.Err},
	}
}

// Config 返回当前配置指针。
func (m *Manager) Config() *config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Auth 返回口令库，供 Web 展示明文账号。
func (m *Manager) Auth() *auth.Store { return m.a }

// ActualRoot 返回当前实际生效的根目录（已解析的绝对路径）。
func (m *Manager) ActualRoot() string { return m.v.Root() }

// RootWarning 返回根目录回退警告（无警告时为空）。
func (m *Manager) RootWarning() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rootWarning
}

// VFS 返回文件系统抽象。
func (m *Manager) VFS() *vfs.VFS { return m.v }

// Save 将当前配置写入磁盘。
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.save()
}

// save 内部持久化（调用方需持锁）。
func (m *Manager) save() error {
	return m.cfg.Save(m.cfgPath)
}
