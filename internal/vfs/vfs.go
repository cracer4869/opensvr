package vfs

import (
	"os"
	"sync"

	"github.com/spf13/afero"

	"opensvr/internal/metrics"
)

type VFS struct {
	mu   sync.RWMutex
	root string
	base afero.Fs
}

func New(root string) (*VFS, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	v := &VFS{root: root}
	v.base = &countingFs{Fs: afero.NewBasePathFs(afero.NewOsFs(), root)}
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
	v.base = &countingFs{Fs: afero.NewBasePathFs(afero.NewOsFs(), root)}
	return nil
}

// countingFs 包装 afero.Fs，对返回的文件读写计入 metrics。
type countingFs struct{ afero.Fs }

func (c *countingFs) Open(name string) (afero.File, error) {
	f, err := c.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f}, nil
}

func (c *countingFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := c.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f}, nil
}

func (c *countingFs) Create(name string) (afero.File, error) {
	f, err := c.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: f}, nil
}

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
