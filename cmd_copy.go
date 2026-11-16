package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/ziliber/dpull/internal/command"
)

// cmdCopy 实现 copy 子命令：跨 registry 分发镜像到多个目标。
//
// 用法:
//
//	dpull copy <image> --to <dst>... [选项]
//
// image 可含 registry（如 myreg:5000/nginx:latest）；否则默认 Docker Hub。
// 多目标通过重复 --to 指定，各目标并行复制。
func cmdCopy(args []string) int {
	fs := flag.NewFlagSet("copy", flag.ExitOnError)
	var c commonFlags
	parseCommon(fs, &c)

	var from, fromUser, fromPass string
	var fromInsecure bool
	var tos multiString
	var toUser, toPass string
	var toInsecure bool
	fs.StringVar(&from, "from", "", "源 registry（覆盖镜像引用中的 registry）")
	fs.StringVar(&fromUser, "from-username", "", "源认证用户名")
	fs.StringVar(&fromPass, "from-password", "", "源认证密码")
	fs.BoolVar(&fromInsecure, "from-insecure", false, "源使用 http 明文/自签证书")
	fs.Var(&tos, "to", "目标 registry，可重复（如 -to host1:5000 -to host2:5000）")
	fs.StringVar(&toUser, "to-username", "", "目标认证用户名（作用于所有目标）")
	fs.StringVar(&toPass, "to-password", "", "目标认证密码")
	fs.BoolVar(&toInsecure, "to-insecure", false, "目标使用 http 明文/自签证书")
	fs.Usage = func() { printCopyUsage(fs) }

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 1
	}
	if len(tos) == 0 {
		fmt.Fprintln(os.Stderr, "错误：至少需要一个 --to 目标 registry")
		fs.Usage()
		return 1
	}

	image := fs.Arg(0)
	cfg, err := c.buildConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		return 1
	}
	ctx := context.Background()

	// 源端点：registry 由镜像引用推断（--from 覆盖）
	srcEp, err := cfg.Endpoint("", c.username, c.password, c.insecure, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建源端点: %v\n", err)
		return 1
	}
	if from != "" {
		base, u, p, plain, perr := parseTarget(from)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "源 %q 无效: %v\n", from, perr)
			return 1
		}
		if u != "" {
			srcEp.Username = u
		}
		if p != "" {
			srcEp.Password = p
		}
		insecure := fromInsecure || plain
		srcEp.PlainHTTP = insecure
		srcEp.SkipTLSVerify = insecure
		srcEp.Registry = base
	}

	platform := parsePlatform(c.platform)
	concurrency := uint(c.concurrent)

	fmt.Fprintf(os.Stdout, "源镜像: %s\n", image)
	fmt.Fprintf(os.Stdout, "复制到 %d 个目标（并发）...\n", len(tos))

	// 并发复制到多目标
	var wg sync.WaitGroup
	errCh := make(chan error, len(tos))
	for _, t := range tos {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			base, u, p, plain, perr := parseTarget(t)
			if perr != nil {
				errCh <- fmt.Errorf("目标 %q: %w", t, perr)
				return
			}
			// 每个目标认证独立：CLI 全局 + 内嵌 userinfo
			dstUser := toUser
			if u != "" {
				dstUser = u
			}
			dstPass := toPass
			if p != "" {
				dstPass = p
			}
			dstInsecure := toInsecure || plain
			dstEp, cerr := cfg.Endpoint(base, dstUser, dstPass, dstInsecure, dstInsecure)
			if cerr != nil {
				errCh <- fmt.Errorf("目标 %q: %w", t, cerr)
				return
			}
			fmt.Fprintf(os.Stdout, "  → 复制到 %s\n", dstEp.Registry)
			_, cerr = command.CopyImage(ctx, cfg, srcEp, dstEp, image, concurrency, platform)
			if cerr != nil {
				errCh <- fmt.Errorf("目标 %q: %w", t, cerr)
				return
			}
			fmt.Fprintf(os.Stdout, "  ✓ %s 完成\n", dstEp.Registry)
		}()
	}
	wg.Wait()
	close(errCh)

	var firstErr error
	for e := range errCh {
		if firstErr == nil {
			firstErr = e
		}
		fmt.Fprintf(os.Stderr, "错误: %v\n", e)
	}
	if firstErr != nil {
		return 1
	}
	fmt.Fprintln(os.Stdout, "全部目标复制完成。")
	return 0
}

// multiString 支持重复 flag。
type multiString []string

func (m *multiString) String() string {
	if m == nil {
		return ""
	}
	var s string
	for _, v := range *m {
		s += v + ","
	}
	return s
}

func (m *multiString) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func printCopyUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `用法: dpull copy <image> --to <dst>... [选项]

将源镜像分发复制到多个目标 registry（各目标并发，基于 containers/image）。

示例:
  dpull copy nginx:latest --to 192.168.1.10:5000 --to 192.168.1.11:5000
  dpull copy myreg:5000/nginx:1.25 --from myreg:5000 \
      --to mirror-a:5000 --to mirror-b:5000

选项:
`)
	fs.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
镜像未指定 registry 时默认 Docker Hub。
源认证使用 --username/--password，目标使用 --to-username/--to-password。
`)
}