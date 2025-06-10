package utils

import (
	"fmt"
	"net"
)

func IsValidIp(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	fmt.Println(ip)
	return ip != nil
}
