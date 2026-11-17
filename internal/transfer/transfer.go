// Package transfer 基于 github.com/containers/image 提供镜像拉取能力。
//
// 本工具只与两种目标通信：
//   - docker daemon（通过 unix socket / tcp，docker-daemon transport）
//   - docker-archive tar 包（docker load 可导入）
//
// 源始终为 registry（Docker Hub / 私有仓库），基于官方 containers/image，
// 与 skopeo/podman 同源的成熟实现。
package transfer

import (
	"context"
	"fmt"
	"time"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/archive"
	"github.com/containers/image/v5/docker/daemon"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/distribution/reference"
)

// Endpoint 描述源 registry 的连接参数。
type Endpoint struct {
	// Registry 为 registry 主机（含端口），如 "registry-1.docker.io"、"192.168.1.10:5000"
	Registry string
	// Username / Password 为可选 Basic 认证。
	Username string
	// Password 为认证密码。
	Password string
	// PlainHTTP 为 true 时强制使用 http 明文（自建 http registry）。
	PlainHTTP bool
	// SkipTLSVerify 为 true 时跳过 TLS 证书校验。
	SkipTLSVerify bool
}

// Platform 表示目标平台。
type Platform struct {
	OS      string
	Arch    string
	Variant string
}

// CopyOpts 为一次拉取的配置。
type CopyOpts struct {
	// Platform 指定要拉取的平台；空则拉取源默认平台。
	Platform *Platform
	// MaxParallel 为并行的 blob 下载数；0 使用库默认。
	MaxParallel uint
	// Progress 为可选进度事件通道；非空时以 ProgressInterval 间隔上报下载进度。
	// 事件类型见 github.com/containers/image/v5/types.ProgressProperties。
	Progress         chan types.ProgressProperties
	ProgressInterval time.Duration
}

// CopyResult 记录拉取结果。
type CopyResult struct {
	CopiedManifest []byte
}

// ParseSourceRef 解析源镜像引用（可含 registry/namespace/tag/digest），
// 返回完整的 docker 引用与从引用中推断的 registry 主机。
// 例如 "nginx:latest" → (named: docker.io/library/nginx:latest, domain: docker.io)。
func ParseSourceRef(image string) (reference.Named, string, error) {
	n, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return nil, "", fmt.Errorf("解析源镜像引用 %q: %w", image, err)
	}
	return reference.TagNameOnly(n), reference.Domain(n), nil
}

// DoCopyToDaemon 将源镜像拉取到本机（或指定）docker daemon，使 `docker images` 立即可见。
// 通过 docker-daemon transport 走 unix socket（Linux）或 npipe（Windows），不调用外部 docker CLI。
//
//   - src 为源 registry 端点（认证、TLS、代理）。
//   - source 为源镜像引用，如 "nginx"、"nginx:1.25"、"myreg:5000/foo/bar:1.0"。
//   - daemonHost 指定 daemon 地址，如 "unix:///var/run/docker.sock"、""（默认）
//     或 "tcp://127.0.0.1:2375"；可用 DOCKER_HOST 语义覆盖。
//   - opts.Platform 用于多架构镜像选择平台；daemon 只能接收单平台，故该参数通常必填。
//
// 目标 tag 与 source 相同（无 tag 默认 latest）。
func DoCopyToDaemon(ctx context.Context, src *Endpoint, source, daemonHost string, opts CopyOpts) (*CopyResult, error) {
	// 1. 解析源引用
	srcNamed, inferredRegistry, err := ParseSourceRef(source)
	if err != nil {
		return nil, err
	}
	if src.Registry == "" || defaultRegistry(src.Registry) {
		src.Registry = inferredRegistry
	}

	// 2. docker daemon 目标引用：必须是 name:tag（NamedTagged）
	dstRef, err := daemonDestRef(srcNamed)
	if err != nil {
		return nil, err
	}

	// 3. 源引用
	srcRef, err := docker.NewReference(srcNamed)
	if err != nil {
		return nil, fmt.Errorf("构造源引用: %w", err)
	}

	// 4. 安全策略
	policyCtx, err := defaultPolicyCtx()
	if err != nil {
		return nil, err
	}
	defer policyCtx.Destroy()

	// 5. SystemContext：源带平台/认证/代理；目标指定 daemon 地址
	srcSys := readSysCtx(src, opts.Platform)
	dstSys := &types.SystemContext{DockerDaemonHost: daemonHost}

	// 6. 执行复制（registry 源 → docker daemon 目标）
	manifest, err := copy.Image(ctx, policyCtx, dstRef, srcRef, &copy.Options{
		SourceCtx:            srcSys,
		DestinationCtx:       dstSys,
		MaxParallelDownloads: opts.MaxParallel,
		Progress:             opts.Progress,
		ProgressInterval:     opts.ProgressInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("拉取到 docker daemon: %w", err)
	}
	return &CopyResult{CopiedManifest: manifest}, nil
}

// DoCopyToArchive 将源镜像拉取并保存为 docker-archive tar 包（`docker load` 可直接导入）。
//
//   - src 为源 registry 端点（认证、TLS、代理）。
//   - source 为源镜像引用，如 "nginx"、"nginx:1.25"。
//   - outputPath 为输出 tar 文件路径（不存在会自动创建）。
//   - opts.Platform 用于多架构镜像选择平台；tar 也只能保存单平台。
func DoCopyToArchive(ctx context.Context, src *Endpoint, source, outputPath string, opts CopyOpts) (*CopyResult, error) {
	// 1. 解析源引用
	srcNamed, inferredRegistry, err := ParseSourceRef(source)
	if err != nil {
		return nil, err
	}
	if src.Registry == "" || defaultRegistry(src.Registry) {
		src.Registry = inferredRegistry
	}

	// 2. 构造 docker-archive 目标引用：路径 + tar 内镜像名（名:tag）
	dstRef, err := archiveDestRef(srcNamed, outputPath)
	if err != nil {
		return nil, err
	}

	// 3. 源引用
	srcRef, err := docker.NewReference(srcNamed)
	if err != nil {
		return nil, fmt.Errorf("构造源引用: %w", err)
	}

	// 4. 安全策略
	policyCtx, err := defaultPolicyCtx()
	if err != nil {
		return nil, err
	}
	defer policyCtx.Destroy()

	// 5. SystemContext：源带平台/认证/代理
	srcSys := readSysCtx(src, opts.Platform)

	// 6. 执行复制（registry 源 → docker-archive 目标）
	manifest, err := copy.Image(ctx, policyCtx, dstRef, srcRef, &copy.Options{
		SourceCtx:            srcSys,
		MaxParallelDownloads: opts.MaxParallel,
		Progress:             opts.Progress,
		ProgressInterval:     opts.ProgressInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("保存镜像到 %s: %w", outputPath, err)
	}
	return &CopyResult{CopiedManifest: manifest}, nil
}

// daemonDestRef 由源引用构造 docker daemon 目标引用。
// 经 FamiliarString 规整为可读短名（如 "nginx:latest"）再解析，由 daemon transport 处理。
func daemonDestRef(srcNamed reference.Named) (types.ImageReference, error) {
	familiar := reference.FamiliarString(srcNamed)
	ref, err := daemon.ParseReference(familiar)
	if err != nil {
		return nil, fmt.Errorf("构造 daemon 目标引用 %q: %w", familiar, err)
	}
	return ref, nil
}

// archiveDestRef 由源引用 + 输出路径构造 docker-archive 目标引用。
// tar 内镜像名为源引用的 familiar 短名（如 "nginx:latest"）。
func archiveDestRef(srcNamed reference.Named, outputPath string) (types.ImageReference, error) {
	if outputPath == "" {
		return nil, fmt.Errorf("输出路径为空")
	}
	// 源引用经 TagNameOnly 处理后必含 tag，可安全断言为 NamedTagged
	familiar := reference.FamiliarString(srcNamed)
	dstNamed, err := reference.ParseNormalizedNamed(familiar)
	if err != nil {
		return nil, fmt.Errorf("解析 archive 目标名 %q: %w", familiar, err)
	}
	namedTagged, ok := dstNamed.(reference.NamedTagged)
	if !ok {
		// 理论上不会走到：TagNameOnly 保证有 tag。此处防御性兜底。
		namedTagged, err = reference.WithTag(dstNamed, "latest")
		if err != nil {
			return nil, fmt.Errorf("构造 archive 目标名: %w", err)
		}
	}
	ref, err := archive.NewReference(outputPath, namedTagged)
	if err != nil {
		return nil, fmt.Errorf("构造 archive 目标引用: %w", err)
	}
	return ref, nil
}

// readSysCtx 构建源端点的 SystemContext（认证、TLS、代理、平台）。
func readSysCtx(e *Endpoint, platform *Platform) *types.SystemContext {
	s := &types.SystemContext{}
	if e.Username != "" || e.Password != "" {
		s.DockerAuthConfig = &types.DockerAuthConfig{
			Username: e.Username,
			Password: e.Password,
		}
	}
	if e.PlainHTTP || e.SkipTLSVerify {
		s.DockerInsecureSkipTLSVerify = types.OptionalBoolTrue
	}
	if platform != nil {
		s.ArchitectureChoice = platform.Arch
		s.OSChoice = platform.OS
		s.VariantChoice = platform.Variant
	}
	return s
}

// defaultRegistry 判断是否使用 Docker Hub 默认 registry。
func defaultRegistry(r string) bool {
	switch r {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return true
	}
	return false
}

// defaultPolicyCtx 构造"接受一切、不验证远程签名"的默认策略上下文。
// 本工具不做镜像签名验证（与 skopeo --insecure-policy 或国内镜像场景一致），
// 且不要求系统存在 /etc/containers/policy.json，故用内存构造而非读取配置文件。
func defaultPolicyCtx() (*signature.PolicyContext, error) {
	policy := &signature.Policy{
		Default: signature.PolicyRequirements{signature.NewPRInsecureAcceptAnything()},
	}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, fmt.Errorf("构造策略上下文: %w", err)
	}
	return policyCtx, nil
}

// ListRepositoryTags 列出 source 所在仓库的全部 tag（用于 --all-tags）。
// source 可为仓库名（tag 会被忽略），如 "nginx"；返回该仓库所有 tag 字符串。
func ListRepositoryTags(ctx context.Context, src *Endpoint, source string) ([]string, error) {
	srcNamed, inferredRegistry, err := ParseSourceRef(source)
	if err != nil {
		return nil, err
	}
	if src.Registry == "" || defaultRegistry(src.Registry) {
		src.Registry = inferredRegistry
	}
	srcRef, err := docker.NewReference(srcNamed)
	if err != nil {
		return nil, fmt.Errorf("构造源引用: %w", err)
	}
	sys := readSysCtx(src, nil)
	tags, err := docker.GetRepositoryTags(ctx, sys, srcRef)
	if err != nil {
		return nil, fmt.Errorf("列出仓库 tag: %w", err)
	}
	return tags, nil
}
