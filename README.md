# dpull

一个基于 **Docker Registry HTTP API v2** 的镜像拉取/复制工具。**不依赖本机 docker daemon**，**不使用 tar 中转**，直接在 registry 之间流式/并行传输镜像。

底层使用 Go 官方生态的 [containers/image](https://github.com/containers/image)（`skopeo` / `podman` 同款引擎），正确处理 bearer token、OCI index / manifest list、digest 校验等繁琐细节，你只需关注目标。

- **`pull`**：拉取镜像——**默认送到本机 docker daemon**（`docker images` 立即可见），也可 `--to` 推送到目标 registry
- **`copy`**：将一个源镜像**并发分发**到多个目标 registry

支持 Docker Hub（官方仓库）、私有仓库、HTTPS/SOCKS5 代理。静态编译，适合在无 docker 的服务器、K8s、边缘设备上使用。

## 特性

- ✅ **默认进本机 docker daemon**：`pull` 走 `docker-daemon` transport + unix socket/npipe，`docker images` 立即可见，无需外部 docker CLI
- ✅ **纯 Registry API**：跨 registry 复制走 `/v2/` 协议，通过官方 `containers/image`，无 tar 中转
- ✅ **官方引擎**：复用 `copy.Image`，bearer token、多架构、digest 校验开箱即用
- ✅ **多架构选择**：多架构镜像可指定 `--platform linux/arm64`
- ✅ **代理**：http/https/socks5，通过环境变量或 `--proxy` / `--socks5`
- ✅ **多目标并发复制**：`--to` 可重复指定，各目标并行
- ✅ **体积优化**：`CGO_ENABLED=0` + `-s -w` + `-trimpath`，纯静态链接 ~18MB
- ✅ **交叉编译**：x86 / arm64 / armv7 零依赖（纯 Go，无需交叉编译器）

## 环境要求

- **Go ≥ 1.23.3**（containers/image v5 的要求）

## 快速开始

### 方式一：直接安装

```bash
go install github.com/ziliber/dpull@latest
```

> `containers/image` 的 GPG 签名验证用到了 cgo 的 gpgme。**推荐**禁用 CGO 安装以避免本机需要 pkg-config/gpgme：
>
> ```bash
> CGO_ENABLED=0 go install github.com/ziliber/dpull@latest
> ```
>
> 或显式切换到纯 Go 的签名实现（不依赖 cgo）：
>
> ```bash
> go install -tags containers_image_openpgp github.com/ziliber/dpull@latest
> ```

### 方式二：编译（Linux）

```bash
# 同时编译 x86 + arm
./build.sh

# 或分别编译
./build.sh x86        # dist/dpull-linux-amd64
./build.sh arm        # dist/dpull-linux-arm64

# 指定版本号
VERSION=v1.2.0 ./build.sh
```

> 构建脚本已自动设置 `CGO_ENABLED=0` 并带上 `-tags containers_image_openpgp`，产出**纯静态**二进制，且任何装了 Go 的机器（含 macOS/Windows）都能为 Linux 交叉编译。

### 使用

```bash
# 1. 拉取镜像到本机 docker daemon（默认），docker images 立即可见
./dpull pull nginx:latest

# 2. 拉取并推送到本地/内网 registry
./dpull pull nginx:latest --to localhost:5000

# 3. 从私有仓库拉取到本机 docker daemon
./dpull pull myreg:5000/foo/bar:1.0 \
    --username user --password pass \
    -k               # 私有没有 TLS

# 4. 跨服务器复制到多个目标（并发，copy 命令）
./dpull copy nginx:latest \
    --to 192.168.1.10:5000 \
    --to 192.168.1.11:5000

# 5. 指定平台（多架构镜像）
./dpull pull redis:7 --platform linux/arm64 --to arm-cluster:5000
```

> `pull` 默认渲染到本机 docker daemon（走 `unix:///var/run/docker.sock`，Windows 为 npipe）。
> 可通过 `DOCKER_HOST` 环境变量指定其他 daemon 地址（如 `unix:///tmp/docker.sock` 或 `tcp://host:2375`）。

### 代理

**命令行参数**（优先级最高）：
```bash
./dpull pull ubuntu --to local:5000 \
    --proxy http://127.0.0.1:10808          # HTTP(S) 代理
./dpull pull ubuntu --to local:5000 \
    --socks5 127.0.0.1:10808                # SOCKS5 代理
./dpull pull ubuntu --to local:5000 \
    --proxy http://127.0.0.1:10808 \
    --no-proxy localhost,192.168.0.0/8      # 内网直连
```

**环境变量**（自动识别）：
```bash
export HTTP_PROXY=http://127.0.0.1:10808
export HTTPS_PROXY=http://127.0.0.1:10808
export SOCKS5_PROXY=127.0.0.1:10808
export NO_PROXY=localhost,127.0.0.1,192.168.0.0/16   # 内网不走代理
export ALL_PROXY=socks5://127.0.0.1:10808            # 全流量代理
./dpull pull gcr.io/... --to local:5000
```

## 命令详解

### `dpull pull <image> [--to <registry>]`

拉取镜像。**默认渲染到本机 docker daemon**（`docker images` 立即可见，走 unix socket / npipe，不调用外部 docker CLI）；指定 `--to <registry>` 时推送到目标 registry。镜像引用格式：
`[registry-host[:port]/][namespace/]name[:tag|@digest]`

- 未指定 registry → Docker Hub
- 未指定 tag → `latest`
- 未指定 namespace（仅 Docker Hub）→ `library`

| 选项 | 说明 |
|------|------|
| `--to <reg>` | 目标 registry（可选，可含 `user:pass@` 或 `http(s)://` 前缀）；缺省送到本机 docker daemon |
| `--to-username/--to-password` | 目标认证 |
| `--to-insecure` | 目标使用 http/自签证书 |
| `--proxy/--socks5/--no-proxy` | 代理（见上） |
| `--platform os/arch` | 选择平台 |
| `-C/--concurrency n` | blob 并发数（默认 4） |

> **本机 daemon**：默认连接 `unix:///var/run/docker.sock`（Linux）/ npipe（Windows），
> 通过 `DOCKER_HOST` 覆盖，如 `DOCKER_HOST=unix:///tmp/docker.sock ./dpull pull nginx`。
> 需要当前用户对 socket 有权限（naive 组）或使用 `tcp://` 远程 daemon。

### `dpull copy <image> --to <dst>...`

从 `image`（可带 `--from`）拉取后**并发**推送到多个目标。

| 选项 | 说明 |
|------|------|
| `--from <reg>` | 源 registry（默认取镜像引用，可含 `user:pass@`） |
| `--from-username/--from-password` | 源认证 |
| `--from-insecure` | 源 http/自签 |
| `--to <reg>` | 目标（**可重复**） |
| `--to-username/--to-password` | 目标认证（作用于所有目标） |
| `--to-insecure` | 目标 http/自签 |

> 认证也可以用目标地址内置的 `user:pass@host` 形式，例如 `--to admin:pw@localhost:5000`。

## 认证

- **Docker Hub**：公开镜像无需认证；私有/走 token 流的镜像由 containers/image 自动处理
- **私有 registry**：`--username user --password pass`（源）；目标用 `--to-*` 变体
- 未提供凭据时，程序会尝试**匿名拉取**（受目标 registry 策略限制）

## 架构与实现

```
dpull/
├── main.go                  # CLI 入口（子命令分发）
├── cmd_pull.go / cmd_copy.go
├── internal/
│   ├── transfer/            # 基于 containers/image 的传输引擎
│   │   └── transfer.go      #   Endpoint/Platform 配置，copy.Image 封装
│   ├── proxy/               # HTTP/SOCKS5 代理解析与 NO_PROXY
│   └── command/             # 命令逻辑与输出
├── build.sh                 # 统一编译入口（x86+arm）
├── build-x86.sh             # x86 编译脚本
└── build-arm.sh             # ARM 编译脚本
```

**同步流程**（`pull`/`copy` 由 `copy.Image` 驱动）：

1. 用 `docker transport` 解析源/目标引用，构建 `SystemContext`（含认证、TLS、代理、平台）
2. `copy.Image` 拉取 manifest（自动处理 OCI index / manifest list），按平台选择子 manifest
3. 复用已有 blob（digest 去重），下载源 blob → 上传目标（digest 校验）
4. `PUT manifest` 打 tag 完成

## 构建体积

```
dist/dpull-linux-amd64   ~18MB  纯静态 ELF（stripped, statically linked）
dist/dpull-linux-arm64   ~18MB
```

体积来自 containers/image 的完整依赖链（sigstore、ocicrypt、storage 等）。默认构建已用 `-s -w -trimpath` 压缩；gpgme 等 cgo 依赖通过 build tag 排除，保证纯静态。

## 测试

```bash
go build -tags containers_image_openpgp ./...   # 编译检查
go vet   -tags containers_image_openpgp ./...
go test  -tags containers_image_openpgp ./...
```

> 除 cgo 依赖外，测试亦需 `-tags containers_image_openpgp` 才可在未装 gpgme 的机器上运行。

## 证书/安全

- 使用系统 CA 证书校验 HTTPS（可设置 `SSL_CERT_FILE` 指向自定义 CA）
- 私有 registry 用 `-k` / `--*-insecure` 跳过证书校验（不推荐生产）
- 镜像签名通过 containers/image 的策略默认拒绝任何未知名义者，不影响普通拉取/推送

## License

MIT