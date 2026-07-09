package netinfo

import (
	"net"
	"sort"
	"strings"
)

// NIC 描述一块本机网卡的一条 IPv4 地址信息。
type NIC struct {
	Name        string
	IP          string
	CIDR        string
	Recommended bool // 是否为「设备优先连这个」的推荐网卡
}

// List 枚举本机所有 up 且非回环网卡的 IPv4 地址，并标注推荐项。
func List() []NIC {
	var out []NIC
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil {
				continue
			}
			out = append(out, NIC{Name: ifc.Name, IP: ip4.String(), CIDR: ipn.String()})
		}
	}
	return rank(out)
}

// rank 对网卡按「机房开局优先级」排序，并把最合适的一块标记为 Recommended。
// 优先级：私网物理 > 私网虚拟 > 其它。仅当存在私网地址时才推荐（公网/APIPA 不推荐）。
func rank(nics []NIC) []NIC {
	out := make([]NIC, len(nics))
	copy(out, nics)
	sort.SliceStable(out, func(i, j int) bool {
		return score(out[i]) > score(out[j])
	})
	// 排序后第一块若为私网则推荐（score 私网 >= 10）。
	if len(out) > 0 && score(out[0]) >= 10 {
		out[0].Recommended = true
	}
	return out
}

// score 为一块网卡打分，分值越高越适合机房开局。
func score(n NIC) int {
	ip := net.ParseIP(n.IP)
	switch {
	case isAPIPA(ip):
		return 0 // 169.254：链路本地，未拿到地址，最不可用
	case isPrivate(ip) && !isVirtualName(n.Name):
		return 20 // 私网物理网卡：最佳
	case isPrivate(ip):
		return 10 // 私网虚拟网卡：次之
	default:
		return 5 // 公网/其它
	}
}

// isPrivate 判断是否 RFC1918 私网地址。
func isPrivate(ip net.IP) bool {
	return ip != nil && ip.IsPrivate()
}

// isAPIPA 判断是否 169.254.0.0/16 链路本地地址（未获得有效 IP）。
func isAPIPA(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 169 && ip4[1] == 254
}

// virtualKeywords 是常见虚拟/隧道网卡名称关键字（大小写不敏感）。
var virtualKeywords = []string{
	"vmware", "vmnet", "virtualbox", "vbox", "hyper-v", "virtual",
	"vethernet", "loopback", "tap", "tun", "docker", "wsl", "npcap", "teredo",
}

// isVirtualName 依据网卡名判断是否虚拟网卡。
func isVirtualName(name string) bool {
	l := strings.ToLower(name)
	for _, k := range virtualKeywords {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}
