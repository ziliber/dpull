#!/usr/bin/env bash
# dpull - Linux x86 (amd64) 编译脚本
# 功能: 编译面向 x86_64 / amd64 架构的 Linux 静态二进制
# 特点: 禁用 cgo，Go 工具链原生交叉编译（无需交叉 C 编译器），体积优化
set -euo pipefail

# 进入脚本所在目录（保证从任何路径执行都正确）
cd "$(dirname "$0")"

NAME="dpull"
OUTPUT="${OUTPUT:-./dist}"
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo v0.2.0)}"
BIN="${OUTPUT}/${NAME}-linux-amd64"

echo "==> 编译 $NAME (linux/amd64) ${VERSION}"
echo "    禁用 cgo，压缩体积（-s -w）"

mkdir -p "$OUTPUT"

GOOS=linux GOARCH=amd64 \
CGO_ENABLED=0 \
go build \
    -tags containers_image_openpgp \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "$BIN" .

SIZE=$(du -h "$BIN" | cut -f1)
FILE_TYPE=$(file -b "$BIN" 2>/dev/null || echo "ELF 64-bit 静态二进制")
echo "==> 完成: ${BIN} (${SIZE})"
echo "    $FILE_TYPE"
echo ""
echo "验证:"
echo "  file  $BIN"
echo "  ldd   $BIN   # 应显示 'not a dynamic executable' 或纯静态"