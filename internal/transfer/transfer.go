// Package transfer 基于 github.com/containers/image 提供跨 registry 镜像复制。
//
// 这是 copy/pull 的核心引擎，替代早期手写的 Registry v2 客户端，
// 以获得各厂商 registry 的成熟兼容性（同 skopeo/podman 底层）。
package transfer

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/daemon"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/distribution/reference"
)

// Endpoint 描述一个源或目标的 registry 连接参数。
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
	// ProxyURL 为可选代理（支持 http/https/socks5）。
	ProxyURL *url.URL
}

// Platform 表示目标平台。
type Platform struct {
	OS      string
	Arch    string
	Variant string
}

// CopyOpts 为一次复制行为的配置。
type CopyOpts struct {
	// Platform 指定要复制的平台；空则复制源默认平台。
	Platform *Platform
	// MaxParallel 为并行的 blob 下载数；0 使用库默认。
	MaxParallel uint
}

// CopyResult 记录复制结果。
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

// DoCopy 将 source 指定的镜像从 src 复制到 dst，目标 tag 为 dstTag。
// source 为完整或局部引用；若 src.Registry 为空或为默认，则用引用推断的 registry。
// dstTag 为目标 tag；为空则用源 tag。
func DoCopy(ctx context.Context, src, dst *Endpoint, source, dstTag string, opts CopyOpts) (*CopyResult, error) {
	// 1. 解析源引用，得到完整 named 与推断的 registry
	srcNamed, inferredRegistry, err := ParseSourceRef(source)
	if err != nil {
		return nil, err
	}
	if src.Registry == "" || defaultRegistry(src.Registry) {
		src.Registry = inferredRegistry
	}

	// 2. 源 tag（默认 latest）
	srcTag := tagOfNamed(srcNamed)
	if dstTag == "" {
		dstTag = srcTag
	}

	// 3. 构造目标引用：dst.Registry + 源仓库路径:目标tag
	srcPath := reference.Path(srcNamed)
	dstNamed, err := ParseTargetRef(dst.Registry, srcPath, dstTag)
	if err != nil {
		return nil, err
	}

	// 4. 传输引用
	srcRef, err := docker.NewReference(srcNamed)
	if err != nil {
		return nil, fmt.Errorf("构造源引用: %w", err)
	}
	dstRef, err := docker.NewReference(dstNamed)
	if err != nil {
		return nil, fmt.Errorf("构造目标引用: %w", err)
	}

	// 5. 安全策略：不要求签名
	policyCtx, err := defaultPolicyCtx()
	if err != nil {
		return nil, err
	}
	defer policyCtx.Destroy()

	// 6. 平台选择在源上下文指定
	srcSys := readSysCtx(src, opts.Platform)
	dstSys := readSysCtx(dst, nil)

	// 7. 执行复制
	manifest, err := copy.Image(ctx, policyCtx, dstRef, srcRef, &copy.Options{
		SourceCtx:            srcSys,
		DestinationCtx:       dstSys,
		MaxParallelDownloads: opts.MaxParallel,
	})
	if err != nil {
		return nil, fmt.Errorf("复制镜像: %w", err)
	}
	return &CopyResult{CopiedManifest: manifest}, nil
}

// ParseTargetRef 由 registry 主机 + 仓库路径(无 tag) + tag 组合目标引用。
func ParseTargetRef(registry, path, tag string) (reference.Named, error) {
	full := strings.TrimSuffix(registry, "/") + "/" + path + ":" + tag
	n, err := reference.ParseNormalizedNamed(full)
	if err != nil {
		return nil, fmt.Errorf("解析目标引用 %q: %w", full, err)
	}
	return reference.TagNameOnly(n), nil
}

// readSysCtx 构建某端点的 SystemContext（认证、TLS、代理、平台）。
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
	if e.ProxyURL != nil {
		s.DockerProxyURL = e.ProxyURL
	}
	if platform != nil {
		s.ArchitectureChoice = platform.Arch
		s.OSChoice = platform.OS
		s.VariantChoice = platform.Variant
	}
	return s
}

// tagOfNamed 返回 Named 引用的 tag；无 tag 返回 "latest"。
func tagOfNamed(n reference.Named) string {
	if t, ok := n.(reference.Tagged); ok {
		return t.Tag()
	}
	return "latest"
}

// defaultRegistry 判断是否使用 Docker Hub 默认 registry。
func defaultRegistry(r string) bool {
	switch r {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return true
	}
	return false
}

// defaultPolicyCtx 构造"仅本地信任、不要求远程签名"的默认安全策略上下文。
// 对本工具（拉取/分发，不验证签名）足够；与 skopeo 默认行为一致。
func defaultPolicyCtx() (*signature.PolicyContext, error) {
	policy, err := signature.DefaultPolicy(&types.SystemContext{})
	if err != nil {
		return nil, fmt.Errorf("构造默认策略: %w", err)
	}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, fmt.Errorf("构造策略上下文: %w", err)
	}
	return policyCtx, nil
}

// DoCopyToDaemon 将源镜像复制到本机（或指定）docker daemon，使 `docker images` 立即可见。
// 通过 docker-daemon transport 走 unix socket（Linux）或 npipe（Windows），不调用外部 docker CLI。
//
//   - src 为源 registry 端点（认证、TLS、代理、平台）。
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
	})
	if err != nil {
		return nil, fmt.Errorf("复制镜像到 docker daemon: %w", err)
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