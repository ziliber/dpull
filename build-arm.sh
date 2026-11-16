#!/usr/bin/env bash
# dpull - Linux ARM 编译脚本
# 功能: 编译面向 ARM64 (aarch64) 架构的 Linux 静态二进制
#       也支持 32 位 ARM (armv7)，通过 ARCH 环境变量切换
# 特点: 禁用 cgo，Go 原生交叉编译（无需交叉 C 编译器），体积优化
set -euo pipefail

cd "$(dirname "$0")"

NAME="dpull"
OUTPUT="${OUTPUT:-./dist}"
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo v0.1.0)}"

# 默认编译 ARM64；可通过 ARCH=v7 编译 32 位 ARM
ARCH_MODE="${ARCH:-arm64}"

case "$ARCH_MODE" in
    arm64|aarch64)
        GOARCH=arm64
        SUFFIX="arm64"
        ;;
    arm|armv7|v7)
        GOARCH=arm
        GOARM=7
        SUFFIX="armv7"
        ;;
    *)
        echo "未知 ARM 架构: $ARCH_MODE（支持 arm64 或 v7）" >&2
        exit 1
        ;;
esac

BIN="${OUTPUT}/${NAME}-linux-${SUFFIX}"

echo "==> 编译 $NAME (linux/${SUFFIX}) ${VERSION}"
echo "    禁用 cgo，压缩体积（-s -w）"

mkdir -p "$OUTPUT"

GOOS=linux GOARCH="$GOARCH" GOARM="${GOARM:-}" \
CGO_ENABLED=0 \
go build \
    -tags containers_image_openpgp \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "$BIN" .

SIZE=$(du -h "$BIN" | cut -f1)
FILE_TYPE=$(file -b "$BIN" 2>/dev/null || echo "ELF 64-bit ARM 二进制")
echo "==> 完成: ${BIN} (${SIZE})"
echo "    $FILE_TYPE"