#!/bin/bash
# 套餐优先级与Fallback测试执行脚本

set -e

echo "=================================="
echo "NewAPI 套餐优先级与Fallback测试"
echo "=================================="
echo ""

# 进入测试目录
cd "$(dirname "$0")"

echo "[1/4] 清理之前的测试输出..."
rm -f coverage.out coverage.html test-output.log

echo "[2/4] 运行所有P0级别测试..."
echo "  - PF-01: 单套餐未超限"
echo "  - PF-02: 单套餐超限-允许Fallback"
echo "  - PF-03: 单套餐超限-禁止Fallback"
echo "  - PF-04: 多套餐优先级降级"
echo "  - PF-06: 所有套餐超限-Fallback"
echo "  - PF-07: 所有套餐超限-无Fallback"
echo "  - PF-09: 多窗口任一超限即失败"
echo ""

go test -v -run "TestPF0[1-4,6-7,9]" 2>&1 | tee test-output.log

echo ""
echo "[3/4] 运行P1级别测试..."
echo "  - PF-05: 优先级相同按ID排序"
echo "  - PF-08: 月度总限额优先检查"
echo ""

go test -v -run "TestPF0[5,8]" 2>&1 | tee -a test-output.log

echo ""
echo "[4/4] 生成覆盖率报告..."
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
echo "  ✓ 覆盖率报告已生成: coverage.html"

echo ""
echo "=================================="
echo "测试完成！"
echo "=================================="
echo ""
echo "查看详细日志: cat test-output.log"
echo "查看覆盖率: open coverage.html"
echo ""

# 统计测试结果
PASSED=$(grep -c "PASS" test-output.log || echo "0")
FAILED=$(grep -c "FAIL" test-output.log || echo "0")

echo "测试结果统计:"
echo "  - 通过: $PASSED"
echo "  - 失败: $FAILED"
echo ""

if [ "$FAILED" -gt 0 ]; then
    echo "❌ 存在失败的测试用例，请检查日志"
    exit 1
else
    echo "✅ 所有测试用例通过！"
    exit 0
fi
