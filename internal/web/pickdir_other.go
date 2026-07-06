//go:build !windows

package web

import "errors"

// pickFolder 仅在 Windows 上提供原生文件夹选择框；其它平台返回错误（本工具面向 Windows）。
func pickFolder() (string, error) {
	return "", errors.New("原生目录选择框仅支持 Windows")
}
