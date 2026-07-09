# opensvr 使用说明

三协议便携开局服务端：一个绿色单 exe，同时提供 **FTP（主动+被动）/ SFTP / TFTP**，
外加系统托盘 + 本地 Web 管理页。用于机房拿笔记本给网络设备开局，上传版本补丁（固件）+ 配置。

## 快速上手

1. 把 `opensvr.exe` 和 `config.yaml`（可选）放同一目录，双击 `opensvr.exe`。
2. 托盘出现图标，浏览器自动打开管理页 `http://127.0.0.1:31944`（无需手输网址）。
3. 页面上：
   - 看到本机所有网卡 IP —— 选和设备管理口同网段的那个（**点 IP 一键复制**）。
   - 点开 **FTP / SFTP / TFTP** 任一开关启动对应服务；端口可就地修改；顶部有**全部启动/全部停止**。
   - 账号密码明文显示（默认 `admin` / `admin`），可现场改。
   - **当前连接**面板：实时看哪些设备连着、传什么文件、已传字节。
   - **共享根目录**：点"选择目录…"弹出 Windows 原生文件夹选择框（现代资源管理器外观、无闪窗），或手动填路径。固件、配置就放这里。
   - 传输时看吞吐曲线和实时上/下行速率，中断时速率归零一眼可见。
   - 首次可点"一键放行防火墙"（需管理员/UAC）。**以管理员身份运行 exe 时，协议启动会自动放行防火墙、退出自动清理**，无需手动点。
   - 看到本机网卡列表里标绿并带"设备连这个 →"徽标的那一行，就是推荐让设备连的 IP（私网物理网卡优先，虚拟/APIPA 靠后）。
   - 深色仪表盘界面，随系统自动适配浅色。

## 根目录与换机适配

- 默认根目录 = **exe 同级的 `开局文件` 子目录**（随 exe 走，U 盘/换机开箱即用）。
- 本机通过 `config.yaml` 的 `root_dir` 预设为
  `D:\01.System\02.Project\99.Opening_file\01.当前开局`。
- `root_dir` 支持**绝对或相对路径**（相对 exe 目录）；写相对路径跨机器通用。
- 换到没有该盘符/目录的机器时，程序**自动回退到 exe 同级 `开局文件`**，
  并在页面顶部黄条提示"原目录不可用，请重新选择"，点一下重选即可。
- **中文目录/文件名**全程支持（FTP/SFTP/TFTP 均已测试）。

## config.yaml 说明

| 字段 | 含义 | 默认 |
|---|---|---|
| `root_dir` | 共享根目录（空=exe 同级 `开局文件`） | 空 |
| `ftp.enabled` / `ftp.port` | FTP 开关与端口 | false / 21 |
| `sftp.enabled` / `sftp.port` | SFTP 开关与端口 | false / 22 |
| `tftp.enabled` / `tftp.port` | TFTP 开关与端口 | false / 69 |
| `web.port` | 管理页端口 | 31944 |
| `auth.user` / `auth.pass` / `auth.anonymous` | 账号/密码/匿名开关 | admin / admin / false |
| `passive_range` | FTP 被动端口段 | 50000–50100 |
| `auto_open_browser` | 启动时自动开浏览器 | true |
| `log_to_file` | 是否写日志文件 | false |

- 端口也可直接在 Web 页改，改完自动存回 config.yaml。
- 上次启用过的协议，下次启动会**自动拉起**。

## 网络设备开局侧命令示例

> 下面 `<PC_IP>` 换成管理页里显示的、与设备同网段的笔记本 IP。

### 华为（VRP）
```
# TFTP 拉固件
tftp <PC_IP> get <firmware>.cc flash:/<firmware>.cc
# FTP
ftp <PC_IP>            # 输入 admin / admin，然后 get/put
# SFTP
sftp <PC_IP>          # 默认 22 端口
```

### H3C（Comware）
```
tftp <PC_IP> get <firmware>.ipe
ftp <PC_IP>
sftp <PC_IP>
```

### 思科（IOS）
```
copy tftp: flash:     # 按提示输入 <PC_IP> 与文件名
copy ftp://admin:admin@<PC_IP>/<file> flash:
copy scp://admin:admin@<PC_IP>/<file> flash:   # SCP：从 PC 下载
copy flash:<file> scp://admin:admin@<PC_IP>/   # SCP：上传到 PC
```

## 设备兼容性（针对老旧网络设备）

为最大化对华为/H3C/思科/锐捷等设备的兼容，工具默认做了以下处理，无需配置：

- **FTP 二进制安全**：默认二进制（TYPE I）传输并关闭 ASCII 行尾转换，固件/补丁按原样字节收发，不会被损坏。
- **SFTP 双主机密钥**：同时提供 ed25519 与 RSA 两把主机密钥，只认 `ssh-rsa` 的老设备也能连；密钥持久化，重启后指纹不变，设备不会反复报"主机密钥已变更"。
- **SFTP 兼容算法**：在现代强算法之外，额外放行老设备仍在用的 KEX/加密/MAC（如 aes128-cbc、hmac-sha1、dh-group1 等），现代客户端仍优先协商强算法。
- **TFTP tsize**：传输前上报文件大小，设备可显示下载进度并按大小校验完整性。

## 现场排错清单

1. **传不上/连不上**：先关掉或放行 Windows 防火墙（公用网络），或点页面"一键放行"（以管理员运行 exe 时启动即自动放行、退出自动清理）。**这是最常见原因。**
2. **IP 不通**：机房通常无 DHCP，把网卡设成与设备同网段静态 IP，先 `ping` 通。
3. **SFTP 端口冲突**：本机若开了 Windows OpenSSH Server 会占用 22，改 SFTP 端口即可。
4. **老设备只认 SCP**：已支持 SCP（单文件上传/下载），思科 `copy scp:`、部分华为/H3C 老机型可直接用；暂不支持目录递归（`-r`）。
5. **固件名不符**：设备命令里的文件名要和根目录里的实际文件名完全一致。

## 从源码构建

需 Go 1.22+：
```
build.bat        # 等价于 CGO_ENABLED=0 go build -ldflags "-s -w -H=windowsgui" -o opensvr.exe ./cmd/opensvr
```
