package sftpsrv

import (
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"

	"opensvr/internal/logbus"
	"opensvr/internal/vfs"
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

// TestFilecmdLogsMutations SFTP 删除/重命名/建目录应有日志。
func TestFilecmdLogsMutations(t *testing.T) {
	logbus.Reset()
	v, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &aferoHandler{v: v, sess: "sess1", user: "admin"}

	// mkdir
	if err := h.Filecmd(sftp.NewRequest("Mkdir", "/新目录")); err != nil {
		t.Fatal(err)
	}
	// rename
	if err := afero.WriteFile(v.Fs(), "/旧名.cfg", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	rn := sftp.NewRequest("Rename", "/旧名.cfg")
	rn.Target = "/新名.cfg"
	if err := h.Filecmd(rn); err != nil {
		t.Fatal(err)
	}
	// remove
	if err := h.Filecmd(sftp.NewRequest("Remove", "/新名.cfg")); err != nil {
		t.Fatal(err)
	}

	mk := firstEvent(t, "mkdir")
	if mk.Proto != "sftp" || mk.Path != "/新目录" || !mk.OK || mk.User != "admin" {
		t.Fatalf("mkdir 日志字段不对: %+v", mk)
	}
	re := firstEvent(t, "rename")
	if re.Path != "/旧名.cfg" || !strings.Contains(re.Msg, "/新名.cfg") || !re.OK {
		t.Fatalf("rename 日志字段不对: %+v", re)
	}
	del := firstEvent(t, "delete")
	if del.Path != "/新名.cfg" || !del.OK {
		t.Fatalf("delete 日志字段不对: %+v", del)
	}
}

// TestFilecmdLogsFailedRemove 删除不存在文件应记录失败日志并返回错误。
func TestFilecmdLogsFailedRemove(t *testing.T) {
	logbus.Reset()
	v, err := vfs.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &aferoHandler{v: v, sess: "sess1", user: "admin"}

	if err := h.Filecmd(sftp.NewRequest("Remove", "/不存在.cfg")); err == nil {
		t.Fatal("期望删除失败")
	}
	e := firstEvent(t, "delete")
	if e.OK || e.Msg == "" {
		t.Fatalf("失败日志字段不对: %+v", e)
	}
}
