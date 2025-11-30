package common

import (
	"net"
	"strings"
)

// IsIPInWhitelist 检查给定的IP地址是否在白名单中
// ipWhitelist: IP白名单列表，支持单个IP（如 "192.168.1.100"）和CIDR网段（如 "192.168.1.0/24"）
// clientIP: 待检查的客户端IP地址
// 返回: true表示IP在白名单中，false表示不在
func IsIPInWhitelist(ipWhitelist []string, clientIP string) bool {
	// 空白名单表示不限制
	if len(ipWhitelist) == 0 {
		return true
	}

	// 解析客户端IP
	parsedClientIP := net.ParseIP(clientIP)
	if parsedClientIP == nil {
		// 无法解析的IP，默认拒绝
		return false
	}

	// 遍历白名单
	for _, allowedIP := range ipWhitelist {
		allowedIP = strings.TrimSpace(allowedIP)
		if allowedIP == "" {
			continue
		}

		// 检查是否为CIDR格式
		if strings.Contains(allowedIP, "/") {
			// CIDR网段匹配
			_, ipNet, err := net.ParseCIDR(allowedIP)
			if err != nil {
				// CIDR格式错误，跳过此条
				continue
			}
			if ipNet.Contains(parsedClientIP) {
				return true
			}
		} else {
			// 单个IP精确匹配
			parsedAllowedIP := net.ParseIP(allowedIP)
			if parsedAllowedIP != nil && parsedAllowedIP.Equal(parsedClientIP) {
				return true
			}
		}
	}

	// 没有匹配项，拒绝
	return false
}
