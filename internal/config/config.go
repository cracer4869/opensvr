package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

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

// Config 全局配置。
type Config struct {
	RootDir         string   `yaml:"root_dir"`
	FTP             ProtoCfg `yaml:"ftp"`
	SFTP            ProtoCfg `yaml:"sftp"`
	TFTP            ProtoCfg `yaml:"tftp"`
	Web             WebCfg   `yaml:"web"`
	Auth            AuthCfg  `yaml:"auth"`
	PassiveRange    [2]int   `yaml:"passive_range"`
	AutoOpenBrowser bool     `yaml:"auto_open_browser"`
	LogToFile       bool     `yaml:"log_to_file"`
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		RootDir:         "./data",
		FTP:             ProtoCfg{Enabled: false, Port: 21},
		SFTP:            ProtoCfg{Enabled: false, Port: 22},
		TFTP:            ProtoCfg{Enabled: false, Port: 69},
		Web:             WebCfg{Port: 31944},
		Auth:            AuthCfg{User: "admin", Pass: "admin", Anonymous: false},
		PassiveRange:    [2]int{50000, 50100},
		AutoOpenBrowser: true,
		LogToFile:       false,
	}
}

// Load 从 path 读取配置；文件不存在时返回默认配置。
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
		return nil, err
	}
	return c, nil
}

// Save 将配置写入 path。
func (c *Config) Save(path string) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
