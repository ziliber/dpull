#!/usr/bin/env bash
# dpull - 跨架构编译统一入口（Linux x86 + ARM）
#
# 用法:
#   ./build.sh                # 同时编译 linux/amd64 和 linux/arm64
#   ./build.sh x86            # 仅 x86 (amd64)
#   ./build.sh arm            # 仅 ARM (arm64)
#   ./build.sh x86 arm        # 显式指定多个
#
# 说明: 禁用 cgo（CGO_ENABLED=0），静态编译，体积优化（-s -w）。
#       纯 Go 实现，无需交叉 C 编译器，任何装了 Go 的机器都能交叉编译。
set -euo pipefail

cd "$(dirname "$0")"

# 默认全部编译
if [ "$#" -eq 0 ]; then
    set -- all
fi

build_x86()  { echo ""; ./build-x86.sh; }
build_arm()  { echo ""; ./build-arm.sh; }

for target in "$@"; do
    case "$target" in
        all)
            echo "========== 编译 x86 (amd64) =========="
            ./build-x86.sh
            echo ""
            echo "========== 编译 ARM (arm64) =========="
            ./build-arm.sh
            ;;
        x86|x64|amd64)  build_x86 ;;
        arm)            build_arm ;;
        clean | dist | re)
            rm -rf ./dist
            echo "已清理 ./dist"
            ;;
        *)
            echo "未知目标: $target（可选: all | x86 | arm | clean）" >&2
            exit 1
            ;;
    esac
done

echo ""
echo "========================================"
echo "全部完成。产物位于 ./dist/:"
ls -lh ./dist/ 2>/dev/null | awk 'NR>1{print "  "$9"\t"$5}'
echo ""
echo "提示: 部署到目标服务器后 chmod +x 并直接运行，无需安装任何运行时。"