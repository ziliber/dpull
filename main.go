// Command dpull 是一个 Docker 镜像拉取工具，基于官方 containers/image 引擎。
//
// 功能：
//   - pull: 从远程仓库拉取镜像到本机 docker daemon（unix socket / tcp）
//   - save: 拉取镜像并保存为 docker-archive tar 包（docker load 可导入）
//
// 不调用外部 docker CLI，通过 containers/image 的 transport 实现。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ziliber/dpull/internal/command"
	"github.com/ziliber/dpull/internal/proxy"
	"github.com/ziliber/dpull/internal/transfer"
)

var version = "0.2.0"

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
	case "save":
		return cmdSave(args[1:])
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
	fmt.Fprintf(os.Stdout, `dpull %s - 拉取镜像到本机 docker daemon / 保存为 tar 包

用法:
  dpull pull <image> [选项]      从 registry 拉取镜像到本机 docker daemon
  dpull save <image> -o <file>   从 registry 拉取并保存为 docker-archive tar 包
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
  --concurrency, -C <n>    blob 并发下载数（默认 4）
  --platform <os/arch>     多架构镜像选择平台，如 linux/arm64

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

// reorderFlags 将位置参数（镜像名）之后的 flag 重排到最前，使 flag 可放在镜像名后。
// Go 的 flag.Parse 遇到第一个非 flag 参数即停止解析，这与 docker pull 的用法不一致。
// 这里通过 flag.Value 的 IsBoolFlag 判断某 flag 是否消费值，从而在保持副本的情况下重排。
func reorderFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// 之后的全部作为位置参数
			pos = append(pos, args[i:]...)
			break
		}
		if len(a) >= 2 && a[0] == '-' {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			// 处理 --flag=value 形式：不消费下一个参数
			hasEqual := false
			if eq := strings.Index(name, "="); eq >= 0 {
				name = name[:eq]
				hasEqual = true
			}
			f := fs.Lookup(name)
			if f == nil {
				return nil, fmt.Errorf("未知 flag: %s", a)
			}
			// 若 flag 消费值且非 --flag=value 形式，则把下一个参数也归入 flag 列表
			if !hasEqual && flagNeedsValue(f.Value) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	// 返回 flag 在前、位置参数在后的重排结果
	return append(flags, pos...), nil
}

// flagNeedsValue 判断 flag.Value 是否需要消费一个参数值。
// bool 类型 flag（实现了 IsBoolFlag() bool 且返回 true）不消费值。
func flagNeedsValue(v flag.Value) bool {
	if bf, ok := v.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
		return false
	}
	return true
}
