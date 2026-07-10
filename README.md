# opensvr — 三协议便携开局服务端

一个绿色单 exe 的 Windows 工具：同时提供 **FTP（主动+被动）/ SFTP（含 SCP）/ TFTP** 服务端，
外加系统托盘和本地 Web 管理页。专为网络工程师现场开局设计——机房拿笔记本给交换机/路由器
上传固件、补丁和配置，开箱即用，无需安装。

## 特性

- **三协议一体**：FTP（主动/被动、二进制安全）、SFTP（兼容老设备算法、双主机密钥、支持 SCP 单文件收发）、TFTP（tsize/blksize 协商、连发+快速重传提速）。
- **绿色便携**：单 exe，放 U 盘随走随用；默认共享 exe 同级 `开局文件` 目录，换机自动回退。
- **Web 管理页**：托盘常驻 + 自动开浏览器；协议开关、端口就地改、账号密码明文可见可改、吞吐曲线、实时连接会话、三协议操作日志（登录/上传/下载/删除/重命名，失败标红附原因）。
- **多网卡友好**：所有网卡同时监听，页面列出全部本机 IP 一键复制，临时插网卡自动刷新。
- **防火墙自动放行**：以管理员运行时，协议启动自动 netsh 放行、退出自动清理；也可页面一键放行。
- **老设备兼容**：面向华为 VRP / H3C Comware / 思科 IOS / 锐捷等，含 ssh-rsa、aes128-cbc、dh-group1 等老算法放行与 FTP ASCII 转换关闭（固件不会被行尾转换损坏）。
- **中文路径**：目录与文件名全程支持中文（三协议均已测试）。

## 快速上手

1. 把 `opensvr.exe`（和可选的 `config.yaml`）放同一目录，双击运行。
2. 浏览器自动打开管理页，点开 FTP / SFTP / TFTP 任一开关。
3. 固件放进共享根目录，设备侧照常 `ftp` / `sftp` / `tftp` 连笔记本 IP 即取（默认账号 `admin` / `admin`）。

设备侧命令示例（华为/H3C/思科）、config.yaml 全字段说明和现场排错清单见 **[docs/USAGE.md](docs/USAGE.md)**。

## 从源码构建

需 Go 1.26+（仅支持 Windows）：

```
build.bat
```

等价于 `CGO_ENABLED=0 go build -ldflags "-s -w -H=windowsgui" -o opensvr.exe ./cmd/opensvr`。

或直接安装：

```
go install github.com/cracer4869/opensvr/cmd/opensvr@latest
```

## 安全说明

本工具定位是**现场临时开局**，不是长期驻网服务：账号密码明文存于本机 config.yaml 并在管理页明文展示（方便现场抄给设备命令行），Web 管理页仅监听 127.0.0.1。用完即关，勿在不受信网络长期开放。

## License

[MIT](LICENSE)
