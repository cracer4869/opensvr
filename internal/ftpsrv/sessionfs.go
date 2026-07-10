package ftpsrv

import (
	"os"

	"github.com/spf13/afero"

	"opensvr/internal/logbus"
	"opensvr/internal/sessions"
)

// sessionFs 包装 afero.Fs：文件传输与增删改操作写入 logbus 日志，
// 传输字节回填到指定会话并更新会话的当前动作/文件。
// 底层 fs 仍是 vfs 的囚笼+metrics 计数实现，故本层只负责会话归属与操作日志。
//
// ftpserverlib 的调用约定：RETR/STOR/APPE 走 OpenFile；LIST/MLSD 走 Open。
// 因此 Open 保持静默（列目录不记日志、不算传输），传输日志只在 OpenFile/Create 记。
type sessionFs struct {
	afero.Fs
	sess string
	user string
}

func newSessionFs(base afero.Fs, sess, user string) afero.Fs {
	return &sessionFs{Fs: base, sess: sess, user: user}
}

// emit 写一条 FTP 操作日志；err 非空时记为失败并附错误信息。
func (s *sessionFs) emit(action, path, msg string, err error) {
	e := logbus.Event{Proto: "ftp", User: s.user, Action: action, Path: path, OK: err == nil, Msg: msg}
	if err != nil {
		e.Msg = err.Error()
	}
	logbus.Emit(e)
}

func (s *sessionFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	// 与 vfs 的写意图判定保持一致（含追加/截断）。
	write := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	action := "download"
	if write {
		action = "upload"
	}
	f, err := s.Fs.OpenFile(name, flag, perm)
	s.emit(action, name, "", err)
	if err != nil {
		return nil, err
	}
	sessions.Update(s.sess, func(se *sessions.Session) { se.Action = action; se.File = name })
	return &sessionFile{File: f, sess: s.sess}, nil
}

func (s *sessionFs) Create(name string) (afero.File, error) {
	f, err := s.Fs.Create(name)
	s.emit("upload", name, "", err)
	if err != nil {
		return nil, err
	}
	sessions.Update(s.sess, func(se *sessions.Session) { se.Action = "upload"; se.File = name })
	return &sessionFile{File: f, sess: s.sess}, nil
}

func (s *sessionFs) Remove(name string) error {
	err := s.Fs.Remove(name)
	s.emit("delete", name, "", err)
	return err
}

func (s *sessionFs) RemoveAll(path string) error {
	err := s.Fs.RemoveAll(path)
	s.emit("delete", path, "", err)
	return err
}

func (s *sessionFs) Rename(oldname, newname string) error {
	err := s.Fs.Rename(oldname, newname)
	s.emit("rename", oldname, "→ "+newname, err)
	return err
}

func (s *sessionFs) Mkdir(name string, perm os.FileMode) error {
	err := s.Fs.Mkdir(name, perm)
	s.emit("mkdir", name, "", err)
	return err
}

func (s *sessionFs) MkdirAll(path string, perm os.FileMode) error {
	err := s.Fs.MkdirAll(path, perm)
	s.emit("mkdir", path, "", err)
	return err
}

// sessionFile 把读写字节数回填到会话面板。
type sessionFile struct {
	afero.File
	sess string
}

func (f *sessionFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	if n > 0 {
		sessions.AddBytes(f.sess, int64(n))
	}
	return n, err
}

func (f *sessionFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	if n > 0 {
		sessions.AddBytes(f.sess, int64(n))
	}
	return n, err
}
