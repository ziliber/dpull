package proxy

import (
	"os"
	"strings"
)

func osGetenv(key string) string {
	return os.Getenv(key)
}

// MatchNoProxy 判断 host 是否命中 NO_PROXY 白名单（支持 * 通配与逗号分隔）。
func MatchNoProxy(noProxy, host string) bool {
	return matchNoProxy(noProxy, host)
}

func lower(s string) string {
	return strings.ToLower(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func hasScheme(s string) bool {
	return strings.Contains(s, "://")
}

// matchNoProxy 判断 host 是否命中 NO_PROXY 白名单（支持 * 通配与逗号分隔）。
func matchNoProxy(noProxy, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, p := range strings.Split(noProxy, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(strings.ToLower(p), ".")
		// 精确匹配
		if p == host || strings.TrimPrefix(host, p) == "" {
			return true
		}
		// 通配符匹配 host 尾部（子域名）
		if strings.HasSuffix(host, "."+p) {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(host, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}
