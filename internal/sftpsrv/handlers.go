package sftpsrv

import (
	"io"
	"os"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"

	"github.com/cracer4869/opensvr/internal/logbus"
	"github.com/cracer4869/opensvr/internal/metrics"
	"github.com/cracer4869/opensvr/internal/sessions"
	"github.com/cracer4869/opensvr/internal/vfs"
)

// newHandlers 基于 vfs 的 afero.Fs（囚笼在 root 内）构造 sftp.Handlers。
// sessID 用于把读写进度回填到会话面板，user 用于操作日志归属。
func newHandlers(v *vfs.VFS, sessID, user string) sftp.Handlers {
	h := &aferoHandler{v: v, sess: sessID, user: user}
	return sftp.Handlers{
		FileGet:  h,
		FilePut:  h,
		FileCmd:  h,
		FileList: h,
	}
}

// aferoHandler 把 sftp 请求转发到 vfs 的 afero.Fs，天然囚笼且防越界。
type aferoHandler struct {
	v    *vfs.VFS
	sess string
	user string
}

func (h *aferoHandler) fs() afero.Fs { return h.v.Fs() }

// emit 写一条 SFTP 操作日志；err 非空时记为失败并附错误信息。
func (h *aferoHandler) emit(action, path, msg string, err error) {
	e := logbus.Event{Proto: "sftp", User: h.user, Action: action, Path: path, OK: err == nil, Msg: msg}
	if err != nil {
		e.Msg = err.Error()
	}
	logbus.Emit(e)
}

// Fileread 处理下载（Get）。
func (h *aferoHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, err := h.fs().Open(r.Filepath)
	h.emit("download", r.Filepath, "", err)
	if err != nil {
		return nil, err
	}
	sessions.Update(h.sess, func(s *sessions.Session) { s.Action = "download"; s.File = r.Filepath })
	return &countingReaderAt{f: f, sess: h.sess}, nil
}

// Filewrite 处理上传（Put/Open 写）。
func (h *aferoHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	p := r.Pflags()
	flags := os.O_WRONLY
	if p.Creat {
		flags |= os.O_CREATE
	}
	if p.Trunc {
		flags |= os.O_TRUNC
	}
	if p.Excl {
		flags |= os.O_EXCL
	}
	// 客户端可能不带任何标志（如某些 sftp 库），默认创建+截断。
	if flags == os.O_WRONLY {
		flags |= os.O_CREATE | os.O_TRUNC
	}
	f, err := h.fs().OpenFile(r.Filepath, flags, 0644)
	h.emit("upload", r.Filepath, "", err)
	if err != nil {
		return nil, err
	}
	sessions.Update(h.sess, func(s *sessions.Session) { s.Action = "upload"; s.File = r.Filepath })
	return &countingWriterAt{f: f, sess: h.sess}, nil
}

// Filecmd 处理 Mkdir/Rmdir/Remove/Rename/Setstat 等，增删改记入日志。
func (h *aferoHandler) Filecmd(r *sftp.Request) error {
	fs := h.fs()
	switch r.Method {
	case "Mkdir":
		err := fs.MkdirAll(r.Filepath, 0755)
		h.emit("mkdir", r.Filepath, "", err)
		return err
	case "Rmdir", "Remove":
		err := fs.Remove(r.Filepath)
		h.emit("delete", r.Filepath, "", err)
		return err
	case "Rename":
		err := fs.Rename(r.Filepath, r.Target)
		h.emit("rename", r.Filepath, "→ "+r.Target, err)
		return err
	case "Setstat":
		// 忽略权限/时间设置，避免客户端因不支持而报错。
		return nil
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
}

// Filelist 处理 List（列目录）与 Stat。
func (h *aferoHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	fs := h.fs()
	switch r.Method {
	case "List":
		f, err := fs.Open(r.Filepath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		infos, err := f.Readdir(-1)
		if err != nil {
			return nil, err
		}
		return listerAt(infos), nil
	case "Stat":
		info, err := fs.Stat(r.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt{info}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

// listerAt 把 []os.FileInfo 适配为 sftp.ListerAt。
type listerAt []os.FileInfo

func (l listerAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

// countingReaderAt 在随机读上累加下行字节（afero 计数只覆盖顺序 Read）。
type countingReaderAt struct {
	f    afero.File
	sess string
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.f.ReadAt(p, off)
	if n > 0 {
		metrics.AddDown(int64(n))
		sessions.AddBytes(c.sess, int64(n))
	}
	return n, err
}

func (c *countingReaderAt) Close() error { return c.f.Close() }

// countingWriterAt 在随机写上累加上行字节。
type countingWriterAt struct {
	f    afero.File
	sess string
}

func (c *countingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	n, err := c.f.WriteAt(p, off)
	if n > 0 {
		metrics.AddUp(int64(n))
		sessions.AddBytes(c.sess, int64(n))
	}
	return n, err
}

func (c *countingWriterAt) Close() error { return c.f.Close() }
