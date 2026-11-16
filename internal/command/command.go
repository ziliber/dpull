// Package command 实现顶层命令逻辑（pull / save）。
package command

import (
	"context"
	"fmt"
	"io"
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

// Endpoint 从 registry + 认证 + insecure 构建 transfer.Endpoint，并注入代理环境变量。
//
// 代理不写入 transfer.Endpoint：containers/image 内部 transport 仅支持 http(s) 代理
// （http.ProxyURL），对 socks5 无效。因此统一注入 HTTP_PROXY/HTTPS_PROXY/NO_PROXY，
// 交由 c/image 默认的 http.ProxyFromEnvironment 处理——Go 标准库原生支持 socks5 代理。
func (c *Config) Endpoint(registry, user, pass string, insecure, plainHTTP bool) (*transfer.Endpoint, error) {
	if err := c.injectProxyEnv(); err != nil {
		return nil, err
	}
	return &transfer.Endpoint{
		Registry:      registry,
		Username:      user,
		Password:      pass,
		PlainHTTP:     plainHTTP,
		SkipTLSVerify: insecure,
	}, nil
}

// injectProxyEnv 把代理配置写入 HTTP_PROXY/HTTPS_PROXY/NO_PROXY 环境变量。
// socks5 优先于 http(s)；NO_PROXY 交由 http.ProxyFromEnvironment 原生处理。
func (c *Config) injectProxyEnv() error {
	raw := c.Proxy.SocksProxy
	if raw == "" {
		raw = c.Proxy.HTTPProxy
	}
	if raw == "" {
		return nil
	}
	if _, err := proxy.ParseURL(raw); err != nil {
		return err
	}
	// 裸地址补 scheme 交给 proxy.ParseURL 已校验；直接注入（透传原样）。
	os.Setenv("HTTP_PROXY", raw)
	os.Setenv("HTTPS_PROXY", raw)
	if c.Proxy.NoProxy != "" {
		os.Setenv("NO_PROXY", c.Proxy.NoProxy)
	}
	return nil
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

// CopyImageToArchive 从 registry 拉取 source 并保存为 docker-archive tar 包（docker load 可导入）。
func CopyImageToArchive(ctx context.Context, cfg *Config, srcEp *transfer.Endpoint,
	source, outputPath string, concurrency uint, platform *transfer.Platform) (*transfer.CopyResult, error) {
	return transfer.DoCopyToArchive(ctx, srcEp, source, outputPath, transfer.CopyOpts{
		Platform:    platform,
		MaxParallel: concurrency,
	})
}

// ListRepositoryTags 列出 source 仓库的全部 tag（用于 pull --all-tags）。
func ListRepositoryTags(ctx context.Context, cfg *Config, srcEp *transfer.Endpoint, source string) ([]string, error) {
	return transfer.ListRepositoryTags(ctx, srcEp, source)
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
