# dpull

一个拉取 Docker 镜像的轻量工具，底层基于官方生态的 [containers/image](https://github.com/containers/image)（`skopeo` / `podman` 同款引擎）。

**只与两类目标通信**：

- **本机 docker daemon**（unix socket / tcp，`docker images` 立即可见）
- **docker-archive tar 包**（`docker load` 可直接导入）

**不调用外部 docker CLI**，直接从 registry（Docker Hub / 私有仓库）拉取，支持 HTTPS/SOCKS5 代理、私有认证、多架构选择。纯静态编译，适合在无 docker 的服务器、K8s、边缘设备上使用。

## 特性

- ✅ **`pull`**：拉取镜像到本机 docker daemon（unix socket / tcp），参数尽力对齐 `docker pull`，另支持代理
- ✅ **`save`**：拉取镜像并保存为 docker-archive tar 包（`docker save` 兼容，`docker load` 可直接导入）
- ✅ **纯 Registry API**：经由官方 `containers/image`，正确处理器 bearer token、OCI index / manifest list、digest 校验
- ✅ **多架构选择**：多架构镜像可指定 `--platform linux/arm64`
- ✅ **代理**：http/https/socks5，命令行参数或环境变量
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
> CGO_ENABLED=0 go install -tags containers_image_openpgp github.com/ziliber/dpull@latest
> ```
>
> 或显式下载源码后本地编译，或在装好 gpgme 的机器上使用默认 tag。

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

> 构建脚本已自动设置 `CGO_ENABLED=0` 并带上 `-tags containers_image_openpgp`，产出**纯静态**二进制，任何装了 Go 的机器（含 macOS/Windows）都能为 Linux 交叉编译。

## 使用

```bash
# 1. 拉取镜像到本机 docker daemon（默认），docker images 立即可见
./dpull pull nginx:latest

# 2. 从私有仓库拉取到本机 docker daemon
./dpull pull myreg:5000/foo/bar:1.0 \
    --username user --password pass \
    -k               # 私有没有 TLS

# 3. 拉取并保存为 tar 包（docker load 可直接导入）
./dpull save nginx        # 写出 nginx-latest.tar
./dpull save redis -o redis-backup.tar

# 4. 指定平台（多架构镜像）
./dpull pull redis:7 --platform linux/arm64

# 5. 拉取仓库内所有 tag
./dpull pull -a library/redis
```

> `pull` 默认连接本机 docker daemon，走 `unix:///var/run/docker.sock`（Windows 为 npipe）。
> 可通过 `DOCKER_HOST` 环境变量指定其他 daemon 地址（如 `unix:///tmp/docker.sock` 或 `tcp://host:2375`）。

### 代理

**命令行参数**（优先级最高）：
```bash
./dpull pull ubuntu --proxy http://127.0.0.1:10808          # HTTP(S) 代理
./dpull pull ubuntu --socks5 127.0.0.1:10808                # SOCKS5 代理
./dpull pull ubuntu --proxy http://127.0.0.1:10808 \
    --no-proxy localhost,192.168.0.0/8                      # 内网直连
```

**环境变量**（自动识别，走 Go 标准库 `ProxyFromEnvironment`）：
```bash
export HTTP_PROXY=http://127.0.0.1:10808
export HTTPS_PROXY=http://127.0.0.1:10808
export NO_PROXY=localhost,127.0.0.1,192.168.0.0/16   # 内网不走代理
./dpull pull ubuntu
```

> `--socks5` / `--proxy` / `--no-proxy` 参数会注入到上述环境变量，由容器运行时原生处理（含 socks5）。

## 命令详解

### `dpull pull [OPTIONS] NAME[:TAG|@DIGEST]`

拉取镜像到本机 docker daemon（走 unix socket / tcp，不调用外部 docker CLI），参数尽力对齐 `docker pull`。

| 选项 | 说明 |
|------|------|
| `-a, --all-tags` | 下载仓库中所有 tag 的镜像 |
| `-q, --quiet` | 抑制详细输出 |
| `--platform os/arch` | 选择平台（多架构镜像） |
| `--proxy / --socks5 / --no-proxy` | 代理（见上） |
| `-u, --username` / `-p, --password` | 私有仓库认证 |
| `-k, --insecure` | 允许 http 明文 / 自签证书 |
| `-C, --concurrency n` | blob 并发下载数（默认 4） |
| `--disable-content-trust` | 跳过签名验证（本工具始终不验证，保留兼容） |

### `dpull save [OPTIONS] NAME[:TAG] -o FILE`

拉取镜像并保存为 docker-archive tar 包（`docker save` 兼容）。**直接从 registry 流式写入，不经过本机 daemon**。

| 选项 | 说明 |
|------|------|
| `-o, --output <file>` | 输出文件路径；缺省自动生成 `<name>-<tag>.tar` |
| `--platform os/arch` | 选择平台 |
| `--proxy / --socks5 / --no-proxy` | 代理 |
| `-u, --username` / `-p, --password` | 私有仓库认证 |
| `-k, --insecure` | 允许 http 明文 / 自签证书 |
| `-C, --concurrency n` | blob 并发下载数 |

```bash
./dpull save nginx:latest -o nginx.tar
docker load -i nginx.tar     # 导入
```

## 镜像引用格式

`[registry-host[:port]/][namespace/]name[:tag|@digest]`

- 未指定 registry → Docker Hub
- 未指定 tag → `latest`
- 未指定 namespace（仅 Docker Hub）→ `library`

## 架构与实现

```
dpull/
├── main.go                  # CLI 入口（子命令分发、flag 重排）
├── cmd_pull.go / cmd_save.go
├── internal/
│   ├── transfer/            # 基于 containers/image 的传输引擎
│   │   └── transfer.go      #   Endpoint/Platform 配置，copy.Image 封装
│   ├── proxy/               # HTTP/SOCKS5 代理解析与 NO_PROXY
│   └── command/             # 命令逻辑与输出
├── build.sh                 # 统一编译入口（x86+arm）
├── build-x86.sh             # x86 编译脚本
└── build-arm.sh             # ARM 编译脚本
```

**数据流**（由 `copy.Image` 驱动）：

1. 用 `docker transport` 解析源引用，构建 `SystemContext`（认证、TLS、平台、代理）
2. `copy.Image` 拉取 manifest（自动处理 OCI index / manifest list），按平台选择子 manifest
3. blob 复用 + 并行下载（digest 校验）
4. 写入目标：`docker-daemon` transport（本机 daemon）或 `docker-archive` transport（tar 包）

## 构建体积

```
dist/dpull-linux-amd64   ~18MB  纯静态 ELF（stripped, statically linked）
dist/dpull-linux-arm64   ~18MB
```

## 测试

```bash
go build -tags containers_image_openpgp ./...   # 编译检查
go vet   -tags containers_image_openpgp ./...
go test  -tags containers_image_openpgp ./...
```

> 测试需 `-tags containers_image_openpgp` 才可在未装 gpgme 的机器上运行。

## 证书/安全

- 使用系统 CA 证书校验 HTTPS（可设置 `SSL_CERT_FILE` 指向自定义 CA）
- 私有 registry 用 `-k` / `--insecure` 跳过证书校验（不推荐生产）
- 镜像签名通过 containers/image 的策略不验证远程签名（`InsecureAcceptAnything`），仅普通拉取

## License

MIT