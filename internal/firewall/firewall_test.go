package firewall

import (
	"strings"
	"testing"
)

func joined(args []string) string { return strings.Join(args, " ") }

// TestAddArgs 校验单端口放行规则的 netsh 参数串。
func TestAddArgs(t *testing.T) {
	got := joined(addArgs(Rule{Name: "opensvr-ftp", Proto: "TCP", Port: "21"}))
	want := "advfirewall firewall add rule name=opensvr-ftp dir=in action=allow protocol=TCP localport=21"
	if got != want {
		t.Fatalf("addArgs 不符:\n want %q\n  got %q", want, got)
	}
}

// TestAddArgsRange 校验端口段（被动 FTP）参数串。
func TestAddArgsRange(t *testing.T) {
	got := joined(addArgs(Rule{Name: "opensvr-ftp-passive", Proto: "TCP", Port: "50000-50100"}))
	want := "advfirewall firewall add rule name=opensvr-ftp-passive dir=in action=allow protocol=TCP localport=50000-50100"
	if got != want {
		t.Fatalf("addArgs 段不符:\n want %q\n  got %q", want, got)
	}
}

// TestDelArgs 校验按规则名删除的 netsh 参数串。
func TestDelArgs(t *testing.T) {
	got := joined(delArgs("opensvr-ftp"))
	want := "advfirewall firewall delete rule name=opensvr-ftp"
	if got != want {
		t.Fatalf("delArgs 不符:\n want %q\n  got %q", want, got)
	}
}

// TestNoopController 校验 Noop 控制器不实际执行、不报错。
func TestNoopController(t *testing.T) {
	var c Controller = Noop{}
	if err := c.Allow([]Rule{{Name: "x", Proto: "TCP", Port: "21"}}); err != nil {
		t.Fatalf("Noop.Allow 不应报错: %v", err)
	}
	if err := c.Remove([]string{"x"}); err != nil {
		t.Fatalf("Noop.Remove 不应报错: %v", err)
	}
}
