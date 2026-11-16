package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ziliber/dpull/internal/command"
	"github.com/ziliber/dpull/internal/transfer"
)

// cmdPull 实现 pull 子命令：拉取镜像，默认送到本机 docker daemon，或推送到指定 registry。
func cmdPull(args []string) int {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	var c commonFlags
	parseCommon(fs, &c)

	var to, toUsername, toPassword string
	var toInsecure bool
	fs.StringVar(&to, "to", "", "目标 registry（如 localhost:5000 或 user:pass@host:5000）；缺省送到本机 docker daemon")
	fs.StringVar(&toUsername, "to-username", "", "目标认证用户名")
	fs.StringVar(&toPassword, "to-password", "", "目标认证密码")
	fs.BoolVar(&toInsecure, "to-insecure", false, "目标使用 http 明文/自签证书")
	fs.Usage = func() { printPullUsage(fs) }

	fs.Parse(args)
	remain := fs.Args()
	if len(remain) < 1 {
		fs.Usage()
		return 1
	}

	image := remain[0]
	cfg, err := c.buildConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		return 1
	}

	// 源端点：registry 由镜像引用推断
	srcEp, err := cfg.Endpoint("", c.username, c.password, c.insecure, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建源端点: %v\n", err)
		return 1
	}

	ctx := context.Background()
	platform := parsePlatform(c.platform)

	// 未指定 --to → 默认送到本机 docker daemon
	if to == "" {
		fmt.Fprintf(os.Stdout, "拉取 %s → 本机 docker daemon (unix socket)\n", image)
		_, err := command.CopyImageToDaemon(ctx, cfg, srcEp, image, uint(c.concurrent), platform)
		if err != nil {
			fmt.Fprintf(os.Stderr, "拉取失败: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, "完成。")
		return 0
	}

	// 指定 --to → 推送到目标 registry
	toBase, toUser, toPass, toPlain, err := parseTarget(to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "目标 %q 无效: %v\n", to, err)
		return 1
	}
	if toUser != "" {
		toUsername = toUser
	}
	if toPass != "" {
		toPassword = toPass
	}
	toInsecure = toInsecure || toPlain

	// 目标端点（http:// 前缀或 --to-insecure → 允许 http/跳过证书）
	dstInsecure := toInsecure || toPlain
	dstEp, err := cfg.Endpoint(toBase, toUsername, toPassword, dstInsecure, dstInsecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建目标端点: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "拉取 %s → %s\n", image, dstEp.Registry)
	res, err := command.CopyImage(ctx, cfg, srcEp, dstEp, image, uint(c.concurrent), platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "拉取失败: %v\n", err)
		return 1
	}
	_ = res
	fmt.Fprintln(os.Stdout, "完成。")
	return 0
}

// parseTarget 解析目标地址，支持 [http(s)://][user:pass@]host[:port] 形式。
func parseTarget(s string) (base, user, pass string, plain bool, err error) {
	if strings.HasPrefix(s, "http://") {
		plain = true
		s = strings.TrimPrefix(s, "http://")
	} else if strings.HasPrefix(s, "https://") {
		s = strings.TrimPrefix(s, "https://")
	}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		ui := s[:at]
		base = s[at+1:]
		if colon := strings.Index(ui, ":"); colon >= 0 {
			user, pass = ui[:colon], ui[colon+1:]
		} else {
			user = ui
		}
	} else {
		base = s
	}
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		err = fmt.Errorf("缺少主机")
	}
	return
}

func printPullUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `用法: dpull pull <image> [--to <registry>] [选项]

拉取镜像，默认渲染到本机 docker daemon（docker images 立即可见，走 unix socket），
也可指定 --to 推送到远程 registry（基于 containers/image，不依赖 docker CLI）。

示例:
  dpull pull nginx:latest                       # 拉到本机 docker daemon
  dpull pull nginx:latest --to localhost:5000   # 拉取并推送到本地 registry
  dpull pull redis:7 --to 192.168.1.10:5000 --platform linux/arm64
  dpull pull mypriv:5000/app:1.0 --to mirror:5000 \
      --username u --password p -k

本机 daemon 地址默认 unix:///var/run/docker.sock，可用 DOCKER_HOST 覆盖。

选项:
`)
	fs.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\n镜像未指定 registry 时默认 Docker Hub。\n")
}

var _ = transfer.CopyResult{}