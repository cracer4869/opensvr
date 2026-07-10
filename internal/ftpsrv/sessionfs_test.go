package ftpsrv

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/cracer4869/opensvr/internal/logbus"
)

// firstEvent 返回第一条匹配 action 的日志事件。
func firstEvent(t *testing.T, action string) logbus.Event {
	t.Helper()
	for _, e := range logbus.Recent() {
		if e.Action == action {
			return e
		}
	}
	t.Fatalf("未找到 action=%s 的日志，现有: %+v", action, logbus.Recent())
	return logbus.Event{}
}

// TestSessionFsLogsUploadDownload 传输打开时应产生 ftp upload/download 日志（带用户与路径）。
func TestSessionFsLogsUploadDownload(t *testing.T) {
	logbus.Reset()
	base := afero.NewMemMapFs()
	fs := newSessionFs(base, "sess1", "admin")

	// STOR 上传：写意图打开
	f, err := fs.OpenFile("/固件.bin", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("data"))
	f.Close()

	// RETR 下载：只读打开
	f2, err := fs.OpenFile("/固件.bin", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()

	up := firstEvent(t, "upload")
	if up.Proto != "ftp" || up.Path != "/固件.bin" || up.User != "admin" || !up.OK {
		t.Fatalf("upload 日志字段不对: %+v", up)
	}
	down := firstEvent(t, "download")
	if down.Proto != "ftp" || down.Path != "/固件.bin" || down.User != "admin" || !down.OK {
		t.Fatalf("download 日志字段不对: %+v", down)
	}
}

// TestSessionFsLogsFailedDownload 打开不存在文件应记录失败日志。
func TestSessionFsLogsFailedDownload(t *testing.T) {
	logbus.Reset()
	fs := newSessionFs(afero.NewMemMapFs(), "sess1", "admin")

	if _, err := fs.OpenFile("/不存在.bin", os.O_RDONLY, 0); err == nil {
		t.Fatal("期望打开失败")
	}
	e := firstEvent(t, "download")
	if e.OK || e.Path != "/不存在.bin" || e.Msg == "" {
		t.Fatalf("失败日志字段不对: %+v", e)
	}
}

// TestSessionFsLogsMutations 删除/重命名/建目录应有日志。
func TestSessionFsLogsMutations(t *testing.T) {
	logbus.Reset()
	base := afero.NewMemMapFs()
	afero.WriteFile(base, "/旧名.cfg", []byte("x"), 0644)
	fs := newSessionFs(base, "sess1", "admin")

	if err := fs.Mkdir("/新目录", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rename("/旧名.cfg", "/新名.cfg"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove("/新名.cfg"); err != nil {
		t.Fatal(err)
	}

	mk := firstEvent(t, "mkdir")
	if mk.Path != "/新目录" || !mk.OK || mk.User != "admin" {
		t.Fatalf("mkdir 日志字段不对: %+v", mk)
	}
	rn := firstEvent(t, "rename")
	if rn.Path != "/旧名.cfg" || !strings.Contains(rn.Msg, "/新名.cfg") || !rn.OK {
		t.Fatalf("rename 日志字段不对: %+v", rn)
	}
	del := firstEvent(t, "delete")
	if del.Path != "/新名.cfg" || !del.OK {
		t.Fatalf("delete 日志字段不对: %+v", del)
	}
}

// TestSessionFsLogsFailedDelete 删除不存在文件应记录失败日志。
func TestSessionFsLogsFailedDelete(t *testing.T) {
	logbus.Reset()
	fs := newSessionFs(afero.NewMemMapFs(), "sess1", "admin")

	if err := fs.Remove("/不存在.cfg"); err == nil {
		t.Fatal("期望删除失败")
	}
	e := firstEvent(t, "delete")
	if e.OK || e.Msg == "" {
		t.Fatalf("失败日志字段不对: %+v", e)
	}
}

// TestSessionFsLogsRecursiveOps MKD 多级/RMD 递归（MkdirAll/RemoveAll）也应有日志。
func TestSessionFsLogsRecursiveOps(t *testing.T) {
	logbus.Reset()
	base := afero.NewMemMapFs()
	fs := newSessionFs(base, "sess1", "admin")

	if err := fs.MkdirAll("/a/b/c", 0755); err != nil {
		t.Fatal(err)
	}
	mk := firstEvent(t, "mkdir")
	if mk.Path != "/a/b/c" || !mk.OK {
		t.Fatalf("MkdirAll 日志字段不对: %+v", mk)
	}

	logbus.Reset()
	if err := fs.RemoveAll("/a"); err != nil {
		t.Fatal(err)
	}
	del := firstEvent(t, "delete")
	if del.Path != "/a" || !del.OK {
		t.Fatalf("RemoveAll 日志字段不对: %+v", del)
	}
}

// TestSessionFsOpenDirIsSilent LIST 走 Open：不应产生传输日志（避免列目录刷屏）。
func TestSessionFsOpenDirIsSilent(t *testing.T) {
	logbus.Reset()
	base := afero.NewMemMapFs()
	base.MkdirAll("/dir", 0755)
	fs := newSessionFs(base, "sess1", "admin")

	f, err := fs.Open("/dir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if n := len(logbus.Recent()); n != 0 {
		t.Fatalf("Open 目录不应产生日志，得到 %d 条: %+v", n, logbus.Recent())
	}
}
