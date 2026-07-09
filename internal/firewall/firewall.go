// Package firewall 封装 Windows 防火墙入站放行/清理与管理员权限检测。
// 命令构造（addArgs/delArgs）为纯函数，便于单测；实际执行走 netsh。
package firewall

import (
	"os"
	"os/exec"
	"sync"
)

// Rule 描述一条入站放行规则。Port 可为单端口("21")或端口段("50000-50100")。
type Rule struct {
	Name  string
	Proto string // TCP / UDP
	Port  string
}

// Controller 是防火墙控制器接口：随协议启停自动放行/清理。
type Controller interface {
	Allow(rules []Rule) error   // 幂等放行给定规则
	Remove(names []string) error // 按规则名删除
}

// Noop 是空实现：未提权时使用，不做任何操作也不报错。
type Noop struct{}

func (Noop) Allow([]Rule) error    { return nil }
func (Noop) Remove([]string) error { return nil }

// Netsh 通过 netsh advfirewall 实际操作 Windows 防火墙（需管理员）。
type Netsh struct{}

// Allow 幂等放行：每条规则先按名删除再添加，避免重复累积。
func (Netsh) Allow(rules []Rule) error {
	var firstErr error
	for _, ru := range rules {
		_ = run(delArgs(ru.Name)) // 忽略"规则不存在"的删除错误
		if err := run(addArgs(ru)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Remove 按规则名删除放行规则。
func (Netsh) Remove(names []string) error {
	var firstErr error
	for _, n := range names {
		if err := run(delArgs(n)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// run 执行一次 netsh 命令。
func run(args []string) error {
	return exec.Command("netsh", args...).Run()
}

// addArgs 构造添加入站放行规则的 netsh 参数（不含 "netsh" 本身）。
func addArgs(r Rule) []string {
	return []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + r.Name, "dir=in", "action=allow",
		"protocol=" + r.Proto, "localport=" + r.Port,
	}
}

// delArgs 构造按名删除规则的 netsh 参数（不含 "netsh" 本身）。
func delArgs(name string) []string {
	return []string{"advfirewall", "firewall", "delete", "rule", "name=" + name}
}

var (
	elevatedOnce sync.Once
	elevated     bool
)

// IsElevated 检测当前进程是否以管理员权限运行（结果缓存）。
// 探测方式：尝试打开物理磁盘设备 \\.\PHYSICALDRIVE0，仅管理员可成功。
func IsElevated() bool {
	elevatedOnce.Do(func() {
		f, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		if err == nil {
			f.Close()
			elevated = true
		}
	})
	return elevated
}
