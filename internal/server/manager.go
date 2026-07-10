package server

import (
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"opensvr/internal/auth"
	"opensvr/internal/config"
	"opensvr/internal/firewall"
	"opensvr/internal/ftpsrv"
	"opensvr/internal/hostkey"
	"opensvr/internal/logbus"
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

	v       *vfs.VFS
	a       *auth.Store
	signers []ssh.Signer

	fw firewall.Controller // 防火墙控制器，随协议启停自动放行/清理（默认 Noop）

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
	// 额外准备一把 RSA 主机密钥兜底老设备（只认 ssh-rsa/rsa-sha2 的客户端）。
	rsaSigner, err := hostkey.LoadOrCreateRSA(filepath.Join(dir, "hostkey_rsa"))
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
		signers:     []ssh.Signer{signer, rsaSigner},
		fw:          firewall.Noop{},
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
	m.sftp = sftpsrv.New(m.v, m.a, m.cfg.SFTP.Port, m.signers...)
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
	m.allowFW(m.ftpRules())
	m.saveWarn()
	return nil
}

// StopFTP 停止 FTP 服务。
func (m *Manager) StopFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ftp.Stop(); err != nil {
		return err
	}
	m.cfg.FTP.Enabled = false
	m.fw.Remove(ruleNames(m.ftpRules()))
	m.saveWarn()
	return nil
}

// StartSFTP 启动 SFTP 服务。
func (m *Manager) StartSFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.sftp.Start(); err != nil {
		return err
	}
	m.cfg.SFTP.Enabled = true
	m.allowFW(m.sftpRules())
	m.saveWarn()
	return nil
}

// StopSFTP 停止 SFTP 服务。
func (m *Manager) StopSFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.sftp.Stop(); err != nil {
		return err
	}
	m.cfg.SFTP.Enabled = false
	m.fw.Remove(ruleNames(m.sftpRules()))
	m.saveWarn()
	return nil
}

// StartTFTP 启动 TFTP 服务。
func (m *Manager) StartTFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.tftp.Start(); err != nil {
		return err
	}
	m.cfg.TFTP.Enabled = true
	m.allowFW(m.tftpRules())
	m.saveWarn()
	return nil
}

// StopTFTP 停止 TFTP 服务。
func (m *Manager) StopTFTP() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.tftp.Stop(); err != nil {
		return err
	}
	m.cfg.TFTP.Enabled = false
	m.fw.Remove(ruleNames(m.tftpRules()))
	m.saveWarn()
	return nil
}

// StopAll 停止全部协议服务，但不改动配置中的 Enabled 记忆（供进程退出时优雅关闭，
// 下次启动仍按上次的开关自动拉起）。
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.ftp.Stop()
	_ = m.sftp.Stop()
	_ = m.tftp.Stop()
}

// ----- 防火墙 -----

// SetFirewall 注入防火墙控制器（提权时注入 Netsh，否则保持 Noop）。
func (m *Manager) SetFirewall(c firewall.Controller) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fw = c
}

// RemoveFirewallRules 清理本工具添加的全部放行规则（退出时调用）。
func (m *Manager) RemoveFirewallRules() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fw.Remove(allRuleNames())
}

// ftpRules 返回 FTP 需放行的规则：控制端口 + 被动数据段（调用方需持锁）。
func (m *Manager) ftpRules() []firewall.Rule {
	rules := []firewall.Rule{
		{Name: "opensvr-ftp", Proto: "TCP", Port: strconv.Itoa(m.cfg.FTP.Port)},
	}
	if m.cfg.PassiveRange[1] > 0 {
		rules = append(rules, firewall.Rule{
			Name:  "opensvr-ftp-passive",
			Proto: "TCP",
			Port:  fmt.Sprintf("%d-%d", m.cfg.PassiveRange[0], m.cfg.PassiveRange[1]),
		})
	}
	return rules
}

// sftpRules 返回 SFTP 需放行的规则（调用方需持锁）。
func (m *Manager) sftpRules() []firewall.Rule {
	return []firewall.Rule{{Name: "opensvr-sftp", Proto: "TCP", Port: strconv.Itoa(m.cfg.SFTP.Port)}}
}

// tftpRules 返回 TFTP 需放行的规则（调用方需持锁）。
func (m *Manager) tftpRules() []firewall.Rule {
	return []firewall.Rule{{Name: "opensvr-tftp", Proto: "UDP", Port: strconv.Itoa(m.cfg.TFTP.Port)}}
}

// FirewallRules 返回三协议当前端口对应的全部放行规则，供 Web 手动放行按钮复用。
func (m *Manager) FirewallRules() []firewall.Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	rules := m.ftpRules()
	rules = append(rules, m.sftpRules()...)
	rules = append(rules, m.tftpRules()...)
	return rules
}

// allowFW 放行规则并把失败写入日志（提权下 netsh 也可能失败，如防火墙服务停用；
// 静默丢弃会让页面显示"已自动放行"而实际未放行）。调用方需持锁。
func (m *Manager) allowFW(rules []firewall.Rule) {
	if err := m.fw.Allow(rules); err != nil {
		logbus.Emit(logbus.Event{Proto: "web", Action: "firewall-allow", OK: false, Msg: err.Error()})
	}
}

// ruleNames 提取规则名切片。
func ruleNames(rules []firewall.Rule) []string {
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.Name
	}
	return names
}

// allRuleNames 返回本工具可能添加的全部规则名，供退出清理。
func allRuleNames() []string {
	return []string{"opensvr-ftp", "opensvr-ftp-passive", "opensvr-sftp", "opensvr-tftp"}
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
	m.saveWarn()
	return nil
}

// SetAuth 更新账号口令：更新 auth、cfg 并持久化。
func (m *Manager) SetAuth(cfg config.AuthCfg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.a.Update(cfg)
	m.cfg.Auth = cfg
	m.saveWarn()
}

// SetPerms 更新目录操作权限：即时作用于 vfs、更新 cfg 并持久化。
func (m *Manager) SetPerms(p config.Perms) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v.SetPerms(p)
	m.cfg.Perms = p
	m.saveWarn()
}

// SetPort 修改指定协议端口并重建实例；若该协议在运行则先停后启，并刷新防火墙放行端口。
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
				m.cfg.FTP.Enabled = false
				m.saveWarn()
				return err
			}
			// 规则同名先删后加，Allow 即完成"旧端口规则→新端口规则"的替换。
			m.allowFW(m.ftpRules())
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
				m.cfg.SFTP.Enabled = false
				m.saveWarn()
				return err
			}
			m.allowFW(m.sftpRules())
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
				m.cfg.TFTP.Enabled = false
				m.saveWarn()
				return err
			}
			m.allowFW(m.tftpRules())
		}
	default:
		return nil
	}
	m.saveWarn()
	return nil
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

// Config 返回当前配置的值拷贝（Config 全为值类型字段，浅拷贝即完整快照）。
// 不返回内部指针：调用方在锁外读取，返回指针会与 SetPort/SetAuth 等并发写构成数据竞争。
func (m *Manager) Config() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.cfg
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

// saveWarn 持久化配置；失败仅记日志不上抛——启停/改配置本身已成功，
// 磁盘只读（如 U 盘写保护）不应让页面误报操作失败。调用方需持锁。
func (m *Manager) saveWarn() {
	if err := m.save(); err != nil {
		logbus.Emit(logbus.Event{Proto: "web", Action: "save-config", OK: false, Msg: err.Error()})
	}
}
