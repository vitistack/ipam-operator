package utils

import (
	"net"
)

func IsValidIp(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil
}
