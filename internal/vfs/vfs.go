package vfs

import (
	"os"
	"sync"

	"github.com/spf13/afero"

	"github.com/cracer4869/opensvr/internal/config"
	"github.com/cracer4869/opensvr/internal/metrics"
)

// VFS 是三协议共用的根目录文件系统抽象：afero 囚笼 + 字节计数 + 权限强制。
type VFS struct {
	mu    sync.RWMutex
	root  string
	perms config.Perms
	base  afero.Fs
}

func New(root string) (*VFS, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	v := &VFS{root: root, perms: config.AllPerms()}
	v.base = &countingFs{Fs: afero.NewBasePathFs(afero.NewOsFs(), root), v: v}
	return v, nil
}

func (v *VFS) Fs() afero.Fs {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.base
}

func (v *VFS) Root() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.root
}

func (v *VFS) SetRoot(root string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.root = root
	v.base = &countingFs{Fs: afero.NewBasePathFs(afero.NewOsFs(), root), v: v}
	return nil
}

// SetPerms 更新目录操作权限（即时生效，作用于根目录及所有子目录）。
func (v *VFS) SetPerms(p config.Perms) {
	v.mu.Lock()
	v.perms = p
	v.mu.Unlock()
}

// Perms 返回当前权限快照。
func (v *VFS) Perms() config.Perms {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.perms
}

// permErr 构造带路径的权限拒绝错误（errors.Is(err, os.ErrPermission) 为真）。
func permErr(op, name string) error {
	return &os.PathError{Op: op, Path: name, Err: os.ErrPermission}
}

// countingFs 包装 afero.Fs：对读写计入 metrics，并按 VFS.perms 强制权限。
// 三协议（FTP/SFTP/TFTP）都经由此，故权限只需在这一处拦截即全覆盖。
type countingFs struct {
	afero.Fs
	v *VFS
}

func (c *countingFs) perms() config.Perms { return c.v.Perms() }

// Open 用于读文件或列目录：按目标类型强制 Read / List。
func (c *countingFs) Open(name string) (afero.File, error) {
	p := c.perms()
	if fi, err := c.Fs.Stat(name); err == nil {
		if fi.IsDir() {
			if !p.List {
				return nil, permErr("list", name)
			}
		} else if !p.Read {
			return nil, permErr("read", name)
		}
	}
	f, err := c.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f}, nil
}

// OpenFile 按标志判定读/写意图并强制对应权限。
func (c *countingFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	p := c.perms()
	write := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	// O_RDWR 兼具读意图：关闭"读"权限时不允许借读写模式读到内容。
	if flag&os.O_RDWR != 0 && !p.Read {
		return nil, permErr("read", name)
	}
	if write {
		if !p.Write {
			return nil, permErr("write", name)
		}
	} else {
		if fi, err := c.Fs.Stat(name); err == nil {
			if fi.IsDir() {
				if !p.List {
					return nil, permErr("list", name)
				}
			} else if !p.Read {
				return nil, permErr("read", name)
			}
		}
	}
	f, err := c.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f}, nil
}

func (c *countingFs) Create(name string) (afero.File, error) {
	if !c.perms().Write {
		return nil, permErr("write", name)
	}
	f, err := c.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f}, nil
}

func (c *countingFs) Mkdir(name string, perm os.FileMode) error {
	if !c.perms().Mkdir {
		return permErr("mkdir", name)
	}
	return c.Fs.Mkdir(name, perm)
}

func (c *countingFs) MkdirAll(path string, perm os.FileMode) error {
	if !c.perms().Mkdir {
		return permErr("mkdir", path)
	}
	return c.Fs.MkdirAll(path, perm)
}

func (c *countingFs) Remove(name string) error {
	if !c.perms().Delete {
		return permErr("delete", name)
	}
	return c.Fs.Remove(name)
}

func (c *countingFs) RemoveAll(path string) error {
	if !c.perms().Delete {
		return permErr("delete", path)
	}
	return c.Fs.RemoveAll(path)
}

func (c *countingFs) Rename(oldname, newname string) error {
	if !c.perms().Rename {
		return permErr("rename", oldname)
	}
	return c.Fs.Rename(oldname, newname)
}

// countingFile 统计顺序读写字节到 metrics（权限已在 Fs 层拦截）。
type countingFile struct{ afero.File }

func (f *countingFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	if n > 0 {
		metrics.AddDown(int64(n))
	}
	return n, err
}

func (f *countingFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	if n > 0 {
		metrics.AddUp(int64(n))
	}
	return n, err
}
