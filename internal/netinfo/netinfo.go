package netinfo

import "net"

// NIC 描述一块本机网卡的一条 IPv4 地址信息。
type NIC struct {
	Name string
	IP   string
	CIDR string
}

// List 枚举本机所有 up 且非回环网卡的 IPv4 地址。
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
	return out
}
