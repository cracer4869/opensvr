package netinfo

import "testing"

func TestListReturnsSlice(t *testing.T) {
	// 不假设具体网卡，只验证不 panic 且字段合法
	for _, n := range List() {
		if n.IP == "" || n.CIDR == "" {
			t.Fatalf("bad nic: %+v", n)
		}
	}
}
