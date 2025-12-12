# 计费准确性测试 - 快速访问

## 📍 测试位置

```
scene_test/new-api-package/billing/
├── billing_accuracy_test.go       # 正常计费测试（BA-01~BA-05）
├── billing_exception_test.go      # 异常计费测试（BA-06~BA-09）
├── billing_integration_test.go    # 端到端集成测试
├── README.md                      # 详细使用指南
├── BILLING_TEST_IMPLEMENTATION_SUMMARY.md  # 实施总结
└── QUICK_REFERENCE.sh             # 快速参考指南
```

## 🚀 快速开始

```bash
# 进入测试目录
cd scene_test/new-api-package/billing

# 查看快速参考
./QUICK_REFERENCE.sh

# 运行所有测试
./run_billing_tests.sh

# 验证代码质量
./verify_tests.sh
```

## 📚 详细文档

- [README.md](billing/README.md) - 完整使用指南
- [BILLING_TEST_IMPLEMENTATION_SUMMARY.md](billing/BILLING_TEST_IMPLEMENTATION_SUMMARY.md) - 实施总结

## 📊 测试覆盖

- **测试用例**: 22个（9个基础 + 13个扩展）
- **代码行数**: 1771行
- **优先级分布**: P0(9个) + P1(9个) + P2(4个)

