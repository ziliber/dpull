package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ziliber/dpull/internal/command"
)

// cmdSave 实现 save 子命令：从 registry 拉取镜像并保存为 docker-archive tar 包，
// 可由 docker load 导入。不经过本机 daemon，直接从 registry 流式写入。
func cmdSave(args []string) int {
	fs := flag.NewFlagSet("save", flag.ExitOnError)
	var c commonFlags
	parseCommon(fs, &c)

	var output string
	fs.StringVar(&output, "output", "", "写入的 tar 文件路径（缺省自动生成 <name>-<tag>.tar）")
	fs.StringVar(&output, "o", "", "写入的 tar 文件路径（简写）")
	fs.Usage = func() { printSaveUsage(fs) }

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

	// 目标文件名
	target := output
	if target == "" {
		target = defaultSaveName(image)
	}

	ctx := context.Background()
	platform := parsePlatform(c.platform)

	fmt.Fprintf(os.Stdout, "保存 %s → %s\n", image, target)
	_, err = command.CopyImageToArchive(ctx, cfg, srcEp, image, target, uint(c.concurrent), platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "保存失败: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "完成。可通过 docker load -i 导入该 tar。")
	return 0
}

// defaultSaveName 由镜像引用生成默认输出文件名，如 nginx → nginx-latest.tar。
func defaultSaveName(image string) string {
	name := image
	// 去掉 registry 前缀（只保留仓库名+tag），并替换 tag 分隔，避免路径穿越/非法字符
	if i := strings.Index(image, "/"); i >= 0 {
		// 仅当 ":/" 前无第二个 "/"（典型的 registry:port/...）保留，否则取最后一段
		// 简化：取最后一个 "/" 之后作为可读名
		if j := strings.LastIndex(image, "/"); j >= 0 {
			name = image[j+1:]
		}
	}
	name = strings.ReplaceAll(name, "@", "-")
	name = strings.ReplaceAll(name, ":", "-")
	if name == "" {
		name = "image"
	}
	return name + ".tar"
}

func printSaveUsage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `用法: dpull save [OPTIONS] IMAGE[:TAG]

从 registry 拉取镜像并保存为 docker-archive tar 包（docker load 可直接导入）。
直接经由 registry 流式写入，不依赖本机 daemon。参数与 docker save 尽量对齐。

示例:
  dpull save nginx:latest -o nginx.tar
  dpull save redis                # 默认写出 redis-latest.tar
  dpull save alpine --platform linux/arm64 -o alpine-arm64.tar

选项:
`)
	fs.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
未指定 -o 时，输出文件名为 <name>-<tag>.tar（当前目录）。
`)
}
