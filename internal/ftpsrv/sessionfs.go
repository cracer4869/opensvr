package ftpsrv

import (
	"os"

	"github.com/spf13/afero"

	"opensvr/internal/sessions"
)

// sessionFs 包装 afero.Fs，把文件读写字节回填到指定会话，并更新会话的当前动作/文件。
// 底层 fs 仍是 vfs 的囚笼+metrics 计数实现，故本层只负责会话级归属。
type sessionFs struct {
	afero.Fs
	sess string
}

func newSessionFs(base afero.Fs, sess string) afero.Fs {
	return &sessionFs{Fs: base, sess: sess}
}

func (s *sessionFs) Open(name string) (afero.File, error) {
	f, err := s.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	sessions.Update(s.sess, func(se *sessions.Session) { se.Action = "download"; se.File = name })
	return &sessionFile{File: f, sess: s.sess, down: true}, nil
}

func (s *sessionFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := s.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	write := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) != 0
	action := "download"
	if write {
		action = "upload"
	}
	sessions.Update(s.sess, func(se *sessions.Session) { se.Action = action; se.File = name })
	return &sessionFile{File: f, sess: s.sess, down: !write}, nil
}

func (s *sessionFs) Create(name string) (afero.File, error) {
	f, err := s.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	sessions.Update(s.sess, func(se *sessions.Session) { se.Action = "upload"; se.File = name })
	return &sessionFile{File: f, sess: s.sess, down: false}, nil
}

type sessionFile struct {
	afero.File
	sess string
	down bool
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
