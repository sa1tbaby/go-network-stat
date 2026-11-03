package collectors

import "net"

func PublicIPv4(text string) bool {
	ip := net.ParseIP(text).To4()
	if ip == nil {
		return false
	}

	return !(ip[0] == 0 || ip[0] == 10 || ip[0] == 127 || ip[0] >= 224 || ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127 || ip[0] == 169 && ip[1] == 254 || ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 || ip[0] == 192 && ip[1] == 168 || ip[0] == 198 && (ip[1] == 18 || ip[1] == 19))
}
