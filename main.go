// Command dpull 是一个纯 Registry API 的 Docker 镜像拉取/同步工具。
//
// 功能：
//   - pull: 从远程仓库拉取镜像，推送到本地/目标 registry
//   - copy: 将一个源镜像分发复制到多个目标 registry
//
// 不使用外部 docker CLI，通过 Docker Registry HTTP API v2 实现。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ziliber/dpull/internal/command"
	"github.com/ziliber/dpull/internal/proxy"
	"github.com/ziliber/dpull/internal/transfer"
)

var version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	switch args[0] {
	case "pull", "p":
		return cmdPull(args[1:])
	case "copy", "cp":
		return cmdCopy(args[1:])
	case "help", "-h", "--help", "-help":
		printUsage()
		return 0
	case "version", "--version", "-v":
		fmt.Fprintf(os.Stdout, "dpull %s\n", version)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %q\n\n", args[0])
		printUsage()
		return 1
	}
}

// commonFlags 为解析后的通用选项。
type commonFlags struct {
	proxyHTTP  string
	proxySocks string
	noProxy    string
	username   string
	password   string
	insecure   bool
	concurrent int
	platform   string
}

// buildConfig 组装基础配置（代理 + 认证）。
func (c *commonFlags) buildConfig() (*command.Config, error) {
	cfg := command.NewConfig()
	cfg.Out = os.Stdout
	cfg.Warn = os.Stderr
	cfg.Proxy = proxy.Merge(proxy.FromEnv(), c.proxyHTTP, c.proxySocks, c.noProxy)
	return cfg, nil
}

// Usage 输出用法。
func printUsage() {
	fmt.Fprintf(os.Stdout, `dpull %s - 纯 Registry API 的 Docker 镜像拉取/复制工具（无需本机 docker）

用法:
  dpull pull <image> [--to <registry>] [选项]   拉取镜像并推送到目标 registry
  dpull copy <image> [--from <src>] [--to <dst>...] [选项]   跨 registry 分发镜像
  dpull help / version

镜像引用格式:
  [registry-host[:port]/]namespace/name[:tag|@digest]
  未指定 registry 默认 Docker Hub，未指定 tag 默认 latest，未指定 namespace 默认 library。

通用选项（子命令均可使用）:
  --proxy <url>            HTTP(S) 代理，如 http://127.0.0.1:10808
  --socks5 <addr>          SOCKS5 代理，如 127.0.0.1:10808（若未设置，回退到 --proxy / 环境变量）
  --no-proxy <list>        逗号分隔，不走代理的主机
  --username, -u <name>    认证用户名
  --password, -p <pass>    认证密码
  --insecure, -k           允许 http 明文 / 自签证书（针对本地/私有 registry）
  --concurrency, -C <n>    blob 并发上传数（默认 4）
  --platform <os/arch>     多架构镜像选择平台，如 linux/arm64（默认 linux/amd64）

运行 'dpull <命令> --help' 查看子命令帮助。
`, version)
}

// parseCommon 注册通用 flag 到 fs。
func parseCommon(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.proxyHTTP, "proxy", "", "HTTP(S) 代理")
	fs.StringVar(&c.proxySocks, "socks5", "", "SOCKS5 代理")
	fs.StringVar(&c.noProxy, "no-proxy", "", "不走代理的主机")
	fs.StringVar(&c.username, "username", "", "认证用户名")
	fs.StringVar(&c.username, "u", "", "认证用户名（简写）")
	fs.StringVar(&c.password, "password", "", "认证密码")
	fs.StringVar(&c.password, "p", "", "认证密码（简写）")
	fs.BoolVar(&c.insecure, "insecure", false, "允许 http 明文/自签证书")
	fs.BoolVar(&c.insecure, "k", false, "允许 http 明文/自签证书（简写）")
	fs.IntVar(&c.concurrent, "concurrency", 4, "blob 并发上传数")
	fs.IntVar(&c.concurrent, "C", 4, "blob 并发上传数（简写）")
	fs.StringVar(&c.platform, "platform", "", "选择平台（格式 os/arch，如 linux/arm64）")
}

func parsePlatform(s string) *transfer.Platform {
	if s == "" {
		return nil
	}
	osName, arch := splitPlatform(s)
	return &transfer.Platform{OS: osName, Arch: arch}
}

func splitPlatform(s string) (osName, arch string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return "linux", s
}
