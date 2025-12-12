#!/bin/bash
# 计费准确性测试运行脚本
# 用法：./run_billing_tests.sh [test_pattern]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "========================================"
echo "NewAPI Package Billing Tests"
echo "========================================"
echo ""

# 默认测试模式：运行所有测试
TEST_PATTERN="${1:-./...}"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Running billing tests with pattern: ${TEST_PATTERN}${NC}"
echo ""

# 运行测试
if go test -v -count=1 "$TEST_PATTERN" 2>&1 | tee test_output.log; then
    echo ""
    echo -e "${GREEN}✓ All tests passed${NC}"
    exit 0
else
    echo ""
    echo -e "${RED}✗ Some tests failed${NC}"
    echo "See test_output.log for details"
    exit 1
fi
