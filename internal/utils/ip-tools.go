package utils

import (
	"net"
)

func IsValidIp(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil
}

func IsIPv4(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil && ip.To4() != nil
}

func IsIPv6(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil && ip.To16() != nil && ip.To4() == nil
}
