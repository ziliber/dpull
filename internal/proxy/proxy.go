// Package proxy 负责解析和构建 HTTP/SOCKS5 代理配置。
// 代理来源优先级：命令行参数 > 环境变量 > 无代理。
package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// Config 代理配置。EnvError 标记从环境变量读取是否出错。
type Config struct {
	// HTTPProxy 用于 http/https 请求的代理地址，如 http://127.0.0.1:10808 或 socks5://127.0.0.1:10808
	HTTPProxy string
	// SocksProxy 专用的 socks5 代理地址，如 127.0.0.1:10808；优先级高于 HTTPProxy
	SocksProxy string
	// NoProxy 逗号分隔的主机白名单（不走代理），支持 * 通配符
	NoProxy string
}

// FromEnv 从标准环境变量构建代理配置。
// 识别的变量：HTTPS_PROXY, HTTP_PROXY, ALL_PROXY, SOCKS5_PROXY, NO_PROXY（均支持小写变体）。
func FromEnv() Config {
	return Config{
		HTTPProxy:  firstNonEmpty(getEnv("HTTPS_PROXY"), getEnv("HTTP_PROXY"), getEnv("ALL_PROXY")),
		SocksProxy: firstNonEmpty(getEnv("SOCKS5_PROXY"), getEnv("ALL_PROXY")),
		NoProxy:    getEnv("NO_PROXY"),
	}
}

func getEnv(key string) string {
	if v := osGetenv(key); v != "" {
		return v
	}
	return osGetenv(lower(key))
}

// Merge 返回命令行参数覆盖后的配置。cmdHTTP/cmdSocks/cmdNoProxy 为空表示不覆盖。
func Merge(env Config, cmdHTTP, cmdSocks, cmdNoProxy string) Config {
	if cmdHTTP != "" {
		env.HTTPProxy = cmdHTTP
	}
	if cmdSocks != "" {
		env.SocksProxy = cmdSocks
	}
	if cmdNoProxy != "" {
		env.NoProxy = cmdNoProxy
	}
	return env
}

// ParseURL 解析代理地址字符串为 URL，兼容裸地址（自动补 scheme）。
// 支持 http/https/socks5/socks5h；无代理时返回空串。
func ParseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !hasScheme(raw) {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析代理地址 %q: %w", raw, err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h", "socks":
		return u, nil
	default:
		return nil, fmt.Errorf("不支持的代理协议 %q（支持 http/https/socks5）", u.Scheme)
	}
}

// Transport 构造带代理与拨号拨代能力的 http.Transport。
func Transport(cfg Config) (*http.Transport, error) {
	proxyURL, err := effectiveProxyURL(cfg)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	tr := &http.Transport{
		Proxy:                 noProxyFunc(cfg, proxyURL),
		DialContext:           dialer.DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	// 如果代理是 socks5，则需要自定义拨号器走 socks 通道。
	if isSocksURL(proxyURL) {
		socksDialer, err := socksDialer(proxyURL)
		if err != nil {
			return nil, err
		}
		tr.DialContext = socksDialer.DialContext
		tr.Proxy = nil
	}

	return tr, nil
}

// socksDialer 基于 socks5 代理构建拨号器。
func socksDialer(u *url.URL) (proxy.ContextDialer, error) {
	addr := u.Host
	auth := &proxy.Auth{}
	if u.User != nil {
		auth.User = u.User.Username()
		auth.Password, _ = u.User.Password()
	}
	if auth.User == "" && auth.Password == "" {
		auth = nil
	}
	d, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("socks5 代理 %s: %w", addr, err)
	}
	if cd, ok := d.(proxy.ContextDialer); ok {
		return cd, nil
	}
	return nil, fmt.Errorf("socks5 代理 %s: 不支持 ContextDialer", addr)
}

// effectiveProxyURL 返回应使用的代理 URL（socks 优先）。
func effectiveProxyURL(cfg Config) (*url.URL, error) {
	raw := cfg.SocksProxy
	if raw == "" {
		raw = cfg.HTTPProxy
	}
	if raw == "" {
		return nil, nil
	}
	// 裸地址补全 scheme
	if !hasScheme(raw) {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析代理地址 %q: %w", raw, err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h", "socks":
		return u, nil
	default:
		return nil, fmt.Errorf("不支持的代理协议 %q（支持 http/https/socks5）", u.Scheme)
	}
}

func isSocksURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "socks5" || u.Scheme == "socks5h" || u.Scheme == "socks")
}

func noProxyFunc(cfg Config, proxyURL *url.URL) func(*http.Request) (*url.URL, error) {
	return func(r *http.Request) (*url.URL, error) {
		if proxyURL == nil {
			return nil, nil
		}
		if cfg.NoProxy != "" && matchNoProxy(cfg.NoProxy, r.URL.Hostname()) {
			return nil, nil
		}
		return proxyURL, nil
	}
}
