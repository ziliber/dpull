package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ziliber/dpull/internal/command"
	"github.com/ziliber/dpull/internal/transfer"
)

// cmdPull 实现 pull 子命令：拉取镜像到本机 docker daemon。
// 参数与 `docker pull` 尽量对齐，另支持代理与私有仓库认证。
func cmdPull(args []string) int {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	var c commonFlags
	parseCommon(fs, &c)

	var allTags bool
	var quiet bool
	var disableContentTrust bool
	fs.BoolVar(&allTags, "all-tags", false, "下载仓库中所有 tag 的镜像")
	fs.BoolVar(&allTags, "a", false, "下载仓库中所有 tag 的镜像（简写）")
	fs.BoolVar(&quiet, "quiet", false, "抑制详细输出")
	fs.BoolVar(&quiet, "q", false, "抑制详细输出（简写）")
	fs.BoolVar(&disableContentTrust, "disable-content-trust", true, "跳过镜像验证（本工具始终不验证，保留兼容）")
	_ = disableContentTrust
	fs.Usage = func() { printPullUsage(fs) }

	ordered, err := reorderFlags(fs, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析参数: %v\n", err)
		return 1
	}
	fs.Parse(ordered)
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
	concurrency := uint(c.concurrent)

	// --all-tags：列出仓库全部 tag，逐个拉取
	if allTags {
		return pullAllTags(ctx, cfg, srcEp, image, c.platform, concurrency, quiet)
	}

	if !quiet {
		fmt.Fprintf(os.Stdout, "拉取 %s → 本机 docker daemon\n", image)
	}
	_, err = command.CopyImageToDaemon(ctx, cfg, srcEp, image, concurrency, platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "拉取失败: %v\n", err)
		return 1
	}
	if !quiet {
		fmt.Fprintln(os.Stdout, "完成。")
	}
	return 0
}

// pullAllTags 列出仓库所有 tag 并逐个拉取到本机 daemon。
func pullAllTags(ctx context.Context, cfg *command.Config, srcEp *transfer.Endpoint,
	image, platform string, concurrency uint, quiet bool) int {

	// 用仓库名（去 tag/digest）列出 tag
	repoName := stripTag(image)
	tags, err := command.ListRepositoryTags(ctx, cfg, srcEp, repoName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "列出仓库 tag 失败: %v\n", err)
		return 1
	}
	sort.Strings(tags)
	if len(tags) == 0 {
		fmt.Fprintln(os.Stderr, "仓库没有可用 tag。")
		return 1
	}

	if !quiet {
		fmt.Fprintf(os.Stdout, "仓库 %s 共 %d 个 tag:\n", repoName, len(tags))
	}
	for i, tag := range tags {
		ref := strings.TrimSuffix(repoName, ":") + ":" + tag
		if !quiet {
			fmt.Fprintf(os.Stdout, "[%d/%d] 拉取 %s\n", i+1, len(tags), ref)
		}
		if _, err := command.CopyImageToDaemon(ctx, cfg, srcEp, ref, concurrency, parsePlatform(platform)); err != nil {
			fmt.Fprintf(os.Stderr, "拉取 %s 失败: %v\n", ref, err)
			return 1
		}
	}
	if !quiet {
		fmt.Fprintln(os.Stdout, "全部完成。")
	}
	return 0
}

// stripTag 去掉引用的 tag/digest 部分，保留仓库名（registry+namespace+name）。
func stripTag(image string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	if i := strings.LastIndex(image, ":"); i >= 0 {
		// 避免 IPv6 地址端口被误判（如 [::1]:5000）——简单处理：只在包含 "/" 之后的冒号为 tag
		if strings.Contains(image[:i], "/") {
			image = image[:i]
		}
	}
	return image
}

func printPullUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `用法: dpull pull [OPTIONS] NAME[:TAG|@DIGEST]

从 registry 拉取镜像到本机 docker daemon（docker images 立即可见），
走 unix socket / tcp，不调用外部 docker CLI。参数与 `+"`docker pull`"+` 尽量对齐。

示例:
  dpull pull nginx:latest
  dpull pull redis                              # 默认 latest
  dpull pull alpine --platform linux/arm64
  dpull pull -a library/redis                   # 拉取所有 tag
  dpull pull mypriv:5000/app:1.0 -u user -p pass -k

本机 daemon 地址默认 unix:///var/run/docker.sock，可用 DOCKER_HOST 覆盖。

选项:
`)
	fs.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
镜像未指定 registry 时默认 Docker Hub；未指定 tag 默认 latest。
`)
}
