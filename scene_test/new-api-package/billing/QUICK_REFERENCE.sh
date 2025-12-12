#!/bin/bash
# 测试用例快速参考指南
# 快速查找和运行特定测试

echo "============================================"
echo "NewAPI 计费准确性测试 - 快速参考"
echo "============================================"
echo ""

cat << 'EOF'
📋 测试用例清单
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

## 正常计费测试 (billing_accuracy_test.go)

  [P0] BA-01  套餐消耗基础计费
       命令: go test -v -run TestBA01_PackageConsumption_BasicFormula
       验证: (input+output×ratio)×model×group 公式

  [P0] BA-02  Fallback时应用GroupRatio
       命令: go test -v -run TestBA02_Fallback_AppliesGroupRatio
       验证: 套餐超限后用户余额扣减正确

  [P0] BA-03  流式请求预扣与补差
       命令: go test -v -run TestBA03_StreamRequest_PreConsumeAndAdjust
       验证: 预估扣费和实际补差机制

  [P1] BA-04  缓存Token计费 (简化实现)
       命令: go test -v -run TestBA04_CachedTokenBilling
       验证: cached×0.1 + normal 计费

  [P1] BA-05  多模型混合计费
       命令: go test -v -run TestBA05_MultiModelMixedBilling
       验证: 不同模型的累加正确性

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

## 异常计费测试 (billing_exception_test.go)

  [P1] BA-06  上游返回空usage
       命令: go test -v -run TestBA06_EmptyUsage_UsesEstimation
       验证: 使用估算值，不crash

  [P1] BA-06-Multi  多次空usage累积
       命令: go test -v -run TestBA06_EmptyUsage_MultipleRequests
       验证: 估算值累加正确

  [P1] BA-06-Malformed  畸形usage字段
       命令: go test -v -run TestBA06_MalformedUsage_GracefulHandling
       验证: 容错处理

  [P0] BA-07  请求失败不扣费 (500)
       命令: go test -v -run TestBA07_RequestFailed_NoCharge
       验证: 500错误不扣费

  [P0] BA-07-401  401错误不扣费
       命令: go test -v -run TestBA07_RequestFailed_401_NoCharge
       验证: 鉴权失败不扣费

  [P0] BA-07-RateLimit  429限流不扣费
       命令: go test -v -run TestBA07_RateLimitError_NoCharge
       验证: 上游限流不扣费

  [P1] BA-07-Timeout  超时不扣费
       命令: go test -v -run TestBA07_RequestTimeout_NoCharge
       验证: 请求超时不扣费

  [P1] BA-08  流式中断 (简化实现)
       命令: go test -v -run TestBA08_StreamingInterrupted_PartialCharge
       验证: 部分扣费正确

  [P2] BA-09  套餐刚好用尽
       命令: go test -v -run TestBA09_PackageExactlyExhausted_BoundaryHandling
       验证: 99.9M + 0.2M 边界处理

  [P2] BA-09-Strict  严格月度限额
       命令: go test -v -run TestBA09_PackageNearExhaustion_StrictLimit
       验证: 不允许Fallback的严格限制

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

## 端到端集成测试 (billing_integration_test.go)

  E2E-1  完整计费流程
       命令: go test -v -run TestE2E_PackageBilling_CompleteFlow
       验证: 订阅→使用→超限→Fallback全流程

  E2E-2  多套餐优先级降级
       命令: go test -v -run TestE2E_MultiPackage_PriorityDegradation
       验证: 高优先级→低优先级自动切换

  E2E-3  错误恢复状态一致性
       命令: go test -v -run TestE2E_ErrorRecovery_ConsistentState
       验证: 成功-失败交替的状态一致

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📊 测试统计
  - 测试文件: 3个 (1771行代码)
  - 测试套件: 3个
  - 测试函数: 22个
  - P0用例: 9个
  - P1用例: 9个
  - P2用例: 4个

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🚀 常用命令

  # 运行所有测试
  go test -v ./...

  # 仅运行P0测试
  go test -v -run "TestBA0[123]|TestBA07"

  # 运行特定套件
  go test -v -run TestBillingAccuracyTestSuite
  go test -v -run TestBillingExceptionTestSuite
  go test -v -run TestBillingIntegrationTestSuite

  # 生成覆盖率报告
  go test -v -coverprofile=coverage.out ./...
  go tool cover -html=coverage.out

  # 使用测试脚本
  ./run_billing_tests.sh
  ./verify_tests.sh

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📖 更多信息
  - 详细说明: cat README.md
  - 实施总结: cat BILLING_TEST_IMPLEMENTATION_SUMMARY.md
  - 配置参数: cat test_config.conf

EOF
