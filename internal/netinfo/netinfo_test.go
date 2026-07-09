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

// recommendedOf 返回被标记为推荐的网卡（便于断言）。
func recommendedOf(nics []NIC) []NIC {
	var out []NIC
	for _, n := range nics {
		if n.Recommended {
			out = append(out, n)
		}
	}
	return out
}

// TestRankRecommendsPrivatePhysical：私网物理网卡应被推荐，且仅推荐一个。
func TestRankRecommendsPrivatePhysical(t *testing.T) {
	in := []NIC{
		{Name: "VMware Network Adapter VMnet1", IP: "192.168.100.1", CIDR: "192.168.100.1/24"},
		{Name: "以太网", IP: "192.168.1.10", CIDR: "192.168.1.10/24"},
	}
	got := rank(in)
	reco := recommendedOf(got)
	if len(reco) != 1 {
		t.Fatalf("应恰好推荐 1 个, 实得 %d: %+v", len(reco), reco)
	}
	if reco[0].IP != "192.168.1.10" {
		t.Fatalf("应推荐私网物理网卡 192.168.1.10, 实得 %s", reco[0].IP)
	}
}

// TestRankVirtualNotRecommendedOverPhysical：有物理私网时虚拟网卡不应被推荐。
func TestRankVirtualNotRecommendedOverPhysical(t *testing.T) {
	in := []NIC{
		{Name: "以太网", IP: "10.0.0.5", CIDR: "10.0.0.5/24"},
		{Name: "Hyper-V Virtual Ethernet Adapter", IP: "192.168.50.1", CIDR: "192.168.50.1/24"},
	}
	got := rank(in)
	for _, n := range got {
		if n.Recommended && isVirtualName(n.Name) {
			t.Fatalf("虚拟网卡不应在有物理私网时被推荐: %+v", n)
		}
	}
}

// TestRankNoPrivateNoRecommend：只有公网/APIPA 时不推荐任何网卡。
func TestRankNoPrivateNoRecommend(t *testing.T) {
	in := []NIC{
		{Name: "以太网", IP: "8.8.8.8", CIDR: "8.8.8.8/24"},
		{Name: "WLAN", IP: "169.254.10.20", CIDR: "169.254.10.20/16"},
	}
	got := rank(in)
	if reco := recommendedOf(got); len(reco) != 0 {
		t.Fatalf("无私网时不应推荐, 实得 %+v", reco)
	}
}
