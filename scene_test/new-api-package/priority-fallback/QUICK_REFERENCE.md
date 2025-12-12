# 套餐优先级与Fallback测试 - 快速参考

## 📋 测试用例速查表

| ID | 场景 | 配置 | 预期结果 | 验证点 |
|----|------|------|----------|--------|
| **PF-01** | 单套餐未超限 | 限额10M，请求3M | ✅ 使用套餐 | subscription+3M, 余额不变 |
| **PF-02** | 单套餐超限-允许Fallback | 限额5M，请求8M，fallback=true | ✅ Fallback | subscription不变, 余额-8M |
| **PF-03** | 单套餐超限-禁止Fallback | 限额5M，请求8M，fallback=false | ❌ 返回429 | 都不扣减 |
| **PF-04** | 多套餐降级 | 高P15限5M，低P5限20M，请求3M+4M | ✅ 降级 | 第1次高，第2次低 |
| **PF-05** | 相同优先级排序 | 2个P10套餐 | ✅ ID小优先 | 使用ID小的 |
| **PF-06** | 全部超限-Fallback | 2套餐限5M和3M，请求10M，都=true | ✅ Fallback | 都不扣，余额-10M |
| **PF-07** | 全部超限-无Fallback | 2套餐，最后=false | ❌ 返回429 | 都不扣减 |
| **PF-08** | 月度限额优先 | 月100M已用95M，请求10M | ✅ 月度超限 | Fallback或429 |
| **PF-09** | 多窗口任一超限 | 小时9/10M，日15/20M，请求2M | ✅ 小时超限 | Fallback |

## 🚀 快速执行

### Windows
```cmd
cd scene_test\new-api-package\priority-fallback
run_tests.bat
```

### Linux/Mac
```bash
cd scene_test/new-api-package/priority-fallback
chmod +x run_tests.sh
./run_tests.sh
```

### 手动执行
```bash
# 运行所有测试
go test -v

# 只运行P0测试
go test -v -run "TestPF0[1-4,6-7,9]"

# 单个测试
go test -v -run TestPF01_SinglePackage_NotExceeded
```

## 🔍 调试技巧

### 查看套餐查询结果
```go
// 在测试中添加
packages, _ := model.GetUserAvailablePackages(user.Id, nil, time.Now().Unix())
for _, pkg := range packages {
    t.Logf("Package: ID=%d, Priority=%d, HourlyLimit=%d",
           pkg.PackageId, pkg.Package.Priority, pkg.Package.HourlyLimit)
}
```

### 查看Redis窗口状态
```go
windowKey := testutil.FormatWindowKey(sub.Id, "hourly")
values, _ := s.server.MiniRedis.HGetAll(windowKey)
t.Logf("Hourly window: %+v", values)
```

### 查看用户余额变化
```go
quota1, _ := model.GetUserQuota(user.Id, true)
// ... 执行请求 ...
quota2, _ := model.GetUserQuota(user.Id, true)
t.Logf("Quota changed: %d → %d (delta=%d)", quota1, quota2, quota2-quota1)
```

## ⚠️ 常见问题

### Q1: 测试被Skip？
**A**: 后端套餐服务未实现，移除测试中的`t.Skip()`行以启用

### Q2: 429错误但预期200？
**A**: 检查：
1. 套餐是否正确激活（status=active）
2. 时间窗口限额配置是否正确
3. fallback_to_balance是否设置为true

### Q3: 余额未扣减但预期应扣减？
**A**: 检查：
1. 套餐是否被正确使用（应该是0）
2. Fallback逻辑是否触发
3. PostConsumeQuota是否正确执行

### Q4: 优先级降级未生效？
**A**: 检查SQL排序：
```sql
ORDER BY packages.priority DESC, subscriptions.id ASC
```

## 📊 测试覆盖矩阵

### 配置维度
- **优先级**: 5, 10, 15
- **限额类型**: 月度总限额、小时限额、日限额
- **Fallback**: true, false
- **套餐数量**: 1个、2个

### 场景覆盖
- ✅ 单套餐正常流程
- ✅ 单套餐超限（允许/禁止Fallback）
- ✅ 多套餐优先级降级
- ✅ 多套餐全部超限
- ✅ 月度限额检查
- ✅ 多窗口AND逻辑

## 🎯 核心验证逻辑

### 优先级选择
```
GetUserAvailablePackages()
  → ORDER BY priority DESC, id ASC
  → 遍历套餐
    → CheckMonthlyQuota()
    → CheckAllSlidingWindows()
      → 任一窗口超限 → continue
      → 全部通过 → return success
  → 全部失败 → check fallback_to_balance
```

### Fallback决策
```
所有套餐遍历完毕：
├── lastPackage.fallback_to_balance == true
│   └── 使用用户余额 → HTTP 200
└── lastPackage.fallback_to_balance == false
    └── 返回错误 → HTTP 429
```

## 📁 相关文件

- **测试代码**: `priority_test.go` (~950行)
- **辅助函数**: `testutil/package_helper.go` (+100行)
- **说明文档**: `README.md`
- **完成报告**: `IMPLEMENTATION_REPORT.md`
- **执行脚本**: `run_tests.bat` (Windows), `run_tests.sh` (Linux)

## 📚 参考文档

1. **设计文档**: `docs/NewAPI-支持多种包月套餐-优化版.md`
   - 第5.1节: 套餐消耗与优先级逻辑
   - 第5.4节: 与现有计费系统集成

2. **测试方案**: `docs/NewAPI-支持多种包月套餐-优化版-测试方案.md`
   - 第2.3节: 套餐优先级与Fallback测试

3. **总体测试框架**: `scene_test/README.md`

---

**最后更新**: 2025-12-12
