package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"opensvr/internal/logbus"
)

// defaultRootName 是可移植默认根目录的子目录名（位于 exe 同级）。
const defaultRootName = "开局文件"

// ProtoCfg 单个协议的启用开关与端口。
type ProtoCfg struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// WebCfg Web 管理页配置。
type WebCfg struct {
	Port int `yaml:"port"`
}

// AuthCfg 口令与匿名开关。
type AuthCfg struct {
	User      string `yaml:"user"`
	Pass      string `yaml:"pass"`
	Anonymous bool   `yaml:"anonymous"`
}

// Perms 目录操作权限（作用于根目录及其所有子目录）。默认全开=最大权限。
type Perms struct {
	Read   bool `yaml:"read" json:"read"`     // 读文件 / 下载
	Write  bool `yaml:"write" json:"write"`   // 写文件 / 上传
	List   bool `yaml:"list" json:"list"`     // 列目录
	Mkdir  bool `yaml:"mkdir" json:"mkdir"`   // 新建目录
	Delete bool `yaml:"delete" json:"delete"` // 删除
	Rename bool `yaml:"rename" json:"rename"` // 改名 / 移动
}

// AllPerms 返回最大权限（全部允许）。
func AllPerms() Perms {
	return Perms{Read: true, Write: true, List: true, Mkdir: true, Delete: true, Rename: true}
}

// Config 全局配置。
type Config struct {
	RootDir         string   `yaml:"root_dir"`
	FTP             ProtoCfg `yaml:"ftp"`
	SFTP            ProtoCfg `yaml:"sftp"`
	TFTP            ProtoCfg `yaml:"tftp"`
	Web             WebCfg   `yaml:"web"`
	Auth            AuthCfg  `yaml:"auth"`
	Perms           Perms    `yaml:"perms"`
	PassiveRange    [2]int   `yaml:"passive_range"`
	AutoOpenBrowser bool     `yaml:"auto_open_browser"`
	LogToFile       bool     `yaml:"log_to_file"`
}

// Default 返回带默认值的配置。
// RootDir 为空表示使用可移植默认目录（exe 同级的 defaultRootName 子目录），
// 由 ResolveRoot 在运行时解析，避免把绝对路径硬编码进程序。
func Default() *Config {
	return &Config{
		RootDir:         "",
		FTP:             ProtoCfg{Enabled: false, Port: 21},
		SFTP:            ProtoCfg{Enabled: false, Port: 22},
		TFTP:            ProtoCfg{Enabled: false, Port: 69},
		Web:             WebCfg{Port: 31944},
		Auth:            AuthCfg{User: "admin", Pass: "admin", Anonymous: false},
		Perms:           AllPerms(),
		PassiveRange:    [2]int{50000, 50100},
		AutoOpenBrowser: true,
		LogToFile:       false,
	}
}

// ResolveRoot 将配置中的 rootDir 解析为可用的绝对根目录，保证可移植与换机适配。
//
// 规则：
//   - 空          -> baseDir/开局文件（可移植默认）
//   - 相对路径     -> 相对 baseDir 解析（跨机器通用）
//   - 绝对路径     -> 原样使用
//
// 若目标目录不可创建/访问（如换机后盘符缺失），则回退到 baseDir/开局文件，
// 并返回非空 warning 供 Web 页提示用户重新选择。
func ResolveRoot(rootDir, baseDir string) (resolved string, warning string) {
	fallback := filepath.Join(baseDir, defaultRootName)

	var candidate string
	switch {
	case rootDir == "":
		candidate = fallback
	case filepath.IsAbs(rootDir):
		candidate = rootDir
	default:
		candidate = filepath.Join(baseDir, rootDir)
	}

	if err := os.MkdirAll(candidate, 0755); err == nil {
		return candidate, ""
	}
	// 候选不可用 -> 回退默认目录
	_ = os.MkdirAll(fallback, 0755)
	if candidate == fallback {
		return fallback, ""
	}
	return fallback, fmt.Sprintf("配置的目录 %q 不可用，已临时使用 %q，请在页面重新选择。", candidate, fallback)
}

// Load 从 path 读取配置；文件不存在时返回默认配置。
// 文件损坏（如断电写坏）时不让程序启动失败：把坏文件改名备份为 <path>.bad，
// 返回默认配置并附 warning 供上层提示。
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := yaml.Unmarshal(b, c); err != nil {
		_ = os.Rename(path, path+".bad")
		logbus.Emit(logbus.Event{Proto: "web", Action: "load-config", OK: false,
			Msg: fmt.Sprintf("config.yaml 解析失败(%v)，已备份为 config.yaml.bad 并使用默认配置", err)})
		return Default(), nil
	}
	return c, nil
}

// Save 将配置原子写入 path：先写临时文件再 rename 替换，
// 断电/崩溃时不会留下半截的 config.yaml。
func (c *Config) Save(path string) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
