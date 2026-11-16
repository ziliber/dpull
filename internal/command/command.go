// Package command 实现顶层命令逻辑（pull / copy）。
package command

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"

	"github.com/ziliber/dpull/internal/proxy"
	"github.com/ziliber/dpull/internal/transfer"
)

// Config 为命令需要的基础配置。
type Config struct {
	// Out / Warn 控制输出流。
	Out  io.Writer
	Warn io.Writer
	// Proxy 为代理配置。
	Proxy proxy.Config
}

// NewConfig 返回带默认输出流的配置。
func NewConfig() *Config {
	return &Config{
		Out:  os.Stdout,
		Warn: os.Stderr,
	}
}

// ProxyURL 返回合并后的代理 URL（socks5 优先），无代理返回 nil。
func (c *Config) ProxyURL() (*url.URL, error) {
	raw := c.Proxy.SocksProxy
	if raw == "" {
		raw = c.Proxy.HTTPProxy
	}
	if raw == "" {
		return nil, nil
	}
	return proxy.ParseURL(raw)
}

// Endpoint 从 registry + 认证 + insecure 构建 transfer.Endpoint，并合并代理（含 NO_PROXY）。
func (c *Config) Endpoint(registry, user, pass string, insecure, plainHTTP bool) (*transfer.Endpoint, error) {
	p, err := c.ProxyURL()
	if err != nil {
		return nil, err
	}
	if p != nil && c.Proxy.NoProxy != "" && proxy.MatchNoProxy(c.Proxy.NoProxy, registry) {
		p = nil
	}
	return &transfer.Endpoint{
		Registry:      registry,
		Username:      user,
		Password:      pass,
		PlainHTTP:     plainHTTP,
		SkipTLSVerify: insecure,
		ProxyURL:      p,
	}, nil
}

// CopyImage 复制镜像：source 为完整源引用，src 为推断/指定的源端点，dst 为目标端点。
func CopyImage(ctx context.Context, cfg *Config, srcEp, dstEp *transfer.Endpoint,
	source string, concurrency uint, platform *transfer.Platform) (*transfer.CopyResult, error) {
	return transfer.DoCopy(ctx, srcEp, dstEp, source, "", transfer.CopyOpts{
		Platform:    platform,
		MaxParallel: concurrency,
	})
}

// CopyImageToDaemon 从 registry 拉取 source 并渲染到本机 docker daemon（docker images 立即可见）。
// daemonHost 为 daemon 地址（如 "unix:///var/run/docker.sock"）；为空则读取 DOCKER_HOST，
// 仍为空则使用 docker 默认 unix socket。
func CopyImageToDaemon(ctx context.Context, cfg *Config, srcEp *transfer.Endpoint,
	source string, concurrency uint, platform *transfer.Platform) (*transfer.CopyResult, error) {
	daemonHost := dockerHostFromEnv()
	return transfer.DoCopyToDaemon(ctx, srcEp, source, daemonHost, transfer.CopyOpts{
		Platform:    platform,
		MaxParallel: concurrency,
	})
}

// dockerHostFromEnv 返回 DOCKER_HOST 环境变量；用作 daemon 地址覆盖，未设置返回 ""。
func dockerHostFromEnv() string {
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		return v
	}
	return ""
}

// PlatformCurrent 返回当前平台。
func PlatformCurrent() *transfer.Platform {
	return &transfer.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// FmtBytes 格式化字节数为人类可读。
func FmtBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}