#!/bin/bash

# NewAPI 滑动窗口测试运行脚本
# 用途: 快速运行滑动窗口核心测试套件

set -e

echo "=========================================="
echo "NewAPI 滑动窗口测试套件"
echo "=========================================="
echo ""

# 切换到测试目录
cd "$(dirname "$0")"

echo "📂 当前目录: $(pwd)"
echo ""

# 检查依赖
echo "🔍 检查测试依赖..."
go list -m github.com/alicebob/miniredis/v2 > /dev/null 2>&1 || {
    echo "⚠️  miniredis未安装，正在安装..."
    go get github.com/alicebob/miniredis/v2
}

go list -m github.com/stretchr/testify > /dev/null 2>&1 || {
    echo "⚠️  testify未安装，正在安装..."
    go get github.com/stretchr/testify/assert
}

echo "✅ 依赖检查完成"
echo ""

# 解析命令行参数
TEST_PATTERN="${1:-.*}"
VERBOSE="${2:--v}"

echo "🧪 运行测试套件..."
echo "   测试模式: $TEST_PATTERN"
echo ""

# 运行测试
if [ "$TEST_PATTERN" = "all" ]; then
    echo "▶️  运行所有滑动窗口测试..."
    go test $VERBOSE ./...
elif [ "$TEST_PATTERN" = "p0" ]; then
    echo "▶️  运行P0优先级测试..."
    go test $VERBOSE -run "TestSW0[1-4|6-7]|TestSW10"
elif [ "$TEST_PATTERN" = "concurrency" ]; then
    echo "▶️  运行并发测试..."
    go test $VERBOSE -run ".*Concurrency.*"
elif [ "$TEST_PATTERN" = "stress" ]; then
    echo "▶️  运行压力测试..."
    go test $VERBOSE -run ".*StressTest.*"
elif [ "$TEST_PATTERN" = "bench" ]; then
    echo "▶️  运行性能基准测试..."
    go test -bench=. -benchmem
elif [ "$TEST_PATTERN" = "coverage" ]; then
    echo "▶️  生成测试覆盖率报告..."
    go test -v -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    echo "✅ 覆盖率报告已生成: coverage.html"
else
    echo "▶️  运行匹配测试: $TEST_PATTERN"
    go test $VERBOSE -run "$TEST_PATTERN"
fi

echo ""
echo "=========================================="
echo "✅ 测试完成"
echo "=========================================="

# 使用说明
cat << 'EOF'

📖 使用说明:
  ./run_tests.sh              # 运行所有测试
  ./run_tests.sh p0           # 仅运行P0优先级测试
  ./run_tests.sh concurrency  # 仅运行并发测试
  ./run_tests.sh stress       # 仅运行压力测试
  ./run_tests.sh bench        # 运行性能基准测试
  ./run_tests.sh coverage     # 生成覆盖率报告
  ./run_tests.sh "SW01"       # 运行特定测试

EOF
