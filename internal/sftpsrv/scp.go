package sftpsrv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"

	"opensvr/internal/logbus"
	"opensvr/internal/sessions"
)

// SCP over SSH（rcp 协议）单文件双向实现：覆盖只走 SCP 的网络设备
// （思科 `copy scp:`、部分华为/H3C 老机型）。仅支持单文件，不支持目录递归(-r)。
//
// 控制报文（ASCII 行 + 二进制负载）：
//   C<mode> <size> <name>\n  文件头
//   T.../D.../E              时间/目录（目录本轮不支持）
// 应答字节：0x00=OK，0x01=警告+消息+\n，0x02=致命+消息+\n。

// scpCommand 若 exec payload 是 scp 命令则返回命令串，否则返回空串。
// payload 为 SSH 线格式：4 字节长度 + 命令字符串。
func scpCommand(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	cmd := string(payload[4:])
	if fields := strings.Fields(cmd); len(fields) > 0 && fields[0] == "scp" {
		return cmd
	}
	return ""
}

// parseSCP 解析 scp 命令，取出模式('t'=接收/sink,'f'=发送/source)、目标路径与是否递归。
// 识别但忽略 -d/-p/-v/-q 等标志；-r 仅置位（本轮据此拒绝目录传输）。
func parseSCP(cmd string) (mode byte, target string, recursive bool, err error) {
	fields := strings.Fields(cmd)
	if len(fields) < 2 || fields[0] != "scp" {
		return 0, "", false, fmt.Errorf("非 scp 命令: %q", cmd)
	}
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") {
			for _, c := range f[1:] {
				switch c {
				case 't':
					mode = 't'
				case 'f':
					mode = 'f'
				case 'r':
					recursive = true
				}
			}
		} else {
			target = f // 最后一个非标志参数即目标路径
		}
	}
	if mode == 0 {
		return 0, "", false, fmt.Errorf("scp 缺少 -t/-f: %q", cmd)
	}
	if target == "" {
		return 0, "", false, fmt.Errorf("scp 缺少目标路径: %q", cmd)
	}
	return mode, target, recursive, nil
}

// handleSCP 在 exec channel 上跑完一次 SCP 传输，结束发 exit-status 并关闭 channel。
func (s *Server) handleSCP(ch ssh.Channel, cmd, sessID string) {
	defer ch.Close()
	mode, target, recursive, err := parseSCP(cmd)
	if err != nil {
		scpError(ch, err.Error())
		sendExit(ch, 1)
		return
	}
	switch mode {
	case 't':
		err = s.scpSink(ch, target, sessID)
	case 'f':
		err = s.scpSource(ch, target, recursive, sessID)
	}
	if err != nil {
		sendExit(ch, 1)
		return
	}
	sendExit(ch, 0)
}

// scpSink 处理服务端接收（scp -t，设备上传）：单文件，写入 vfs 囚笼。
func (s *Server) scpSink(ch ssh.Channel, target, sessID string) error {
	fs := s.v.Fs()
	br := bufio.NewReader(ch)
	if _, err := ch.Write([]byte{0}); err != nil { // 就绪
		return err
	}
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF {
			return nil // 客户端结束
		}
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		switch line[0] {
		case 'C':
			size, name, perr := parseCRecord(line)
			if perr != nil {
				scpError(ch, perr.Error())
				return perr
			}
			dest := scpDest(fs, target, name)
			f, oerr := fs.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if oerr != nil {
				scpError(ch, oerr.Error())
				return oerr
			}
			if _, err := ch.Write([]byte{0}); err != nil { // 接受文件头
				f.Close()
				return err
			}
			_, cerr := io.CopyN(&upCounter{w: f, sess: sessID}, br, size)
			f.Close()
			if cerr != nil {
				logbus.Emit(logbus.Event{Proto: "sftp", Action: "scp-write", Path: dest, OK: false, Msg: cerr.Error()})
				return cerr
			}
			if _, err := br.ReadByte(); err != nil { // 读客户端结尾状态字节
				return err
			}
			if _, err := ch.Write([]byte{0}); err != nil { // 确认接收完成
				return err
			}
			logbus.Emit(logbus.Event{Proto: "sftp", Action: "scp-write", Path: dest, OK: true})
			sessions.Update(sessID, func(ss *sessions.Session) { ss.Action = "scp upload"; ss.File = dest })
		case 'T': // 时间戳：接受并忽略
			if _, err := ch.Write([]byte{0}); err != nil {
				return err
			}
		case 'D':
			scpError(ch, "不支持目录传输(scp -r)")
			return fmt.Errorf("scp 目录传输(-r)不支持")
		case 'E':
			if _, err := ch.Write([]byte{0}); err != nil {
				return err
			}
		default:
			scpError(ch, "不支持的 scp 控制报文")
			return fmt.Errorf("未知 scp 控制报文: %q", line)
		}
	}
}

// scpSource 处理服务端发送（scp -f，设备下载）：单文件，从 vfs 囚笼读出。
func (s *Server) scpSource(ch ssh.Channel, target string, recursive bool, sessID string) error {
	fs := s.v.Fs()
	br := bufio.NewReader(ch)
	if b, err := br.ReadByte(); err != nil { // 等客户端就绪
		return err
	} else if b != 0 {
		return fmt.Errorf("scp 客户端未就绪: 0x%02x", b)
	}
	clean := path.Clean("/" + target)
	info, err := fs.Stat(clean)
	if err != nil {
		scpError(ch, err.Error())
		return err
	}
	if info.IsDir() {
		scpError(ch, "不支持目录传输(scp -r)")
		return fmt.Errorf("scp 目录传输(-r)不支持: %s", clean)
	}
	hdr := fmt.Sprintf("C0644 %d %s\n", info.Size(), path.Base(clean))
	if _, err := ch.Write([]byte(hdr)); err != nil {
		return err
	}
	if b, err := br.ReadByte(); err != nil { // 客户端确认文件头
		return err
	} else if b != 0 {
		return fmt.Errorf("scp 客户端拒绝文件头: 0x%02x", b)
	}
	f, err := fs.Open(clean)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.CopyN(&downCounter{w: ch, sess: sessID}, f, info.Size()); err != nil {
		logbus.Emit(logbus.Event{Proto: "sftp", Action: "scp-read", Path: clean, OK: false, Msg: err.Error()})
		return err
	}
	if _, err := ch.Write([]byte{0}); err != nil { // 结尾
		return err
	}
	if _, err := br.ReadByte(); err != nil { // 客户端最终确认
		return err
	}
	logbus.Emit(logbus.Event{Proto: "sftp", Action: "scp-read", Path: clean, OK: true})
	sessions.Update(sessID, func(ss *sessions.Session) { ss.Action = "scp download"; ss.File = clean })
	return nil
}

// sendExit 发送 exit-status 请求（SCP 结束约定）。
func sendExit(ch ssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
}

// scpError 向客户端发送致命错误（0x02 + 消息 + \n）。
func scpError(w io.Writer, msg string) {
	_, _ = w.Write(append([]byte{2}, append([]byte(msg), '\n')...))
}

// parseCRecord 解析 "C<mode> <size> <name>" 文件头，取出大小与文件名。
func parseCRecord(line string) (size int64, name string, err error) {
	parts := strings.SplitN(line[1:], " ", 3)
	if len(parts) != 3 {
		return 0, "", fmt.Errorf("非法 C 报文: %q", line)
	}
	size, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("非法 C 报文长度: %q", line)
	}
	return size, parts[2], nil
}

// scpDest 决定 sink 写入路径：target 若是已存在目录则写入其下，否则即为目标文件。
func scpDest(fs afero.Fs, target, name string) string {
	clean := path.Clean("/" + target)
	if info, err := fs.Stat(clean); err == nil && info.IsDir() {
		return path.Join(clean, name)
	}
	return clean
}

// upCounter 顺序写计数（设备上传→上行），回填会话面板。
// metrics 由底层 vfs 的 countingFile 统一计数，此处不再重复累加。
type upCounter struct {
	w    io.Writer
	sess string
}

func (c *upCounter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		sessions.AddBytes(c.sess, int64(n))
	}
	return n, err
}

// downCounter 顺序写计数（设备下载→下行），回填会话面板。
// metrics 由读文件侧的 countingFile 统一计数，此处不再重复累加。
type downCounter struct {
	w    io.Writer
	sess string
}

func (c *downCounter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		sessions.AddBytes(c.sess, int64(n))
	}
	return n, err
}
