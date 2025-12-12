#!/bin/bash
# 计费测试代码验证脚本
# 用于检查测试代码的语法正确性和结构完整性

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "========================================"
echo "Billing Test Code Verification"
echo "========================================"
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 1. 检查文件存在性
echo -e "${BLUE}[1/5] Checking file existence...${NC}"
files=(
    "billing_accuracy_test.go"
    "billing_exception_test.go"
    "billing_integration_test.go"
    "README.md"
    "test_config.conf"
    "BILLING_TEST_IMPLEMENTATION_SUMMARY.md"
)

for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo -e "  ${GREEN}✓${NC} $file"
    else
        echo -e "  ${RED}✗${NC} $file (missing)"
        exit 1
    fi
done
echo ""

# 2. 检查Go语法
echo -e "${BLUE}[2/5] Checking Go syntax...${NC}"
if go fmt ./... > /dev/null 2>&1; then
    echo -e "  ${GREEN}✓${NC} Go syntax is valid"
else
    echo -e "  ${RED}✗${NC} Go syntax errors found"
    exit 1
fi
echo ""

# 3. 检查测试函数命名
echo -e "${BLUE}[3/5] Checking test function names...${NC}"
test_functions=$(grep -h "^func (s \*.*TestSuite) Test" *.go | wc -l)
echo -e "  ${GREEN}✓${NC} Found $test_functions test functions"

# 检查特定测试ID
expected_tests=(
    "TestBA01"
    "TestBA02"
    "TestBA03"
    "TestBA04"
    "TestBA05"
    "TestBA06"
    "TestBA07"
    "TestBA08"
    "TestBA09"
)

for test_id in "${expected_tests[@]}"; do
    if grep -q "func.*${test_id}" *.go; then
        echo -e "  ${GREEN}✓${NC} $test_id"
    else
        echo -e "  ${YELLOW}⚠${NC} $test_id (not found)"
    fi
done
echo ""

# 4. 检查依赖导入
echo -e "${BLUE}[4/5] Checking imports...${NC}"
required_imports=(
    "github.com/stretchr/testify/suite"
    "github.com/stretchr/testify/assert"
    "one-api/model"
    "scene_test/testutil"
)

for import in "${required_imports[@]}"; do
    if grep -q "\"$import\"" *.go; then
        echo -e "  ${GREEN}✓${NC} $import"
    else
        echo -e "  ${YELLOW}⚠${NC} $import (not found)"
    fi
done
echo ""

# 5. 统计测试覆盖
echo -e "${BLUE}[5/5] Test coverage summary...${NC}"
echo ""
echo "  Test Suites:"
echo "  - BillingAccuracyTestSuite (正常计费)"
echo "  - BillingExceptionTestSuite (异常计费)"
echo "  - BillingIntegrationTestSuite (端到端)"
echo ""

echo "  Test Cases by Priority:"
p0_count=$(grep -c "Priority: P0" *.go || echo "0")
p1_count=$(grep -c "Priority: P1" *.go || echo "0")
p2_count=$(grep -c "Priority: P2" *.go || echo "0")
echo "  - P0 (Critical): $p0_count"
echo "  - P1 (Important): $p1_count"
echo "  - P2 (Optional): $p2_count"
echo ""

echo "  Test Files:"
echo "  - billing_accuracy_test.go: $(wc -l < billing_accuracy_test.go) lines"
echo "  - billing_exception_test.go: $(wc -l < billing_exception_test.go) lines"
echo "  - billing_integration_test.go: $(wc -l < billing_integration_test.go) lines"
echo ""

# 总结
echo "========================================"
echo -e "${GREEN}✓ Code verification passed${NC}"
echo "========================================"
echo ""
echo "Next steps:"
echo "  1. Review test code: vim billing_*_test.go"
echo "  2. Run tests: ./run_billing_tests.sh"
echo "  3. Check coverage: go test -coverprofile=coverage.out ./..."
echo ""
