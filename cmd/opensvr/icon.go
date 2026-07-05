package main

import _ "embed"

// iconICO 是内嵌的多尺寸应用/托盘图标（由 tools/mkicon 生成）。
// Windows 下 systray.SetIcon 接受 ICO 字节。
//
//go:embed icon.ico
var iconICO []byte
