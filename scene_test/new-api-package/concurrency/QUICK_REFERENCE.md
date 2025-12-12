# 并发测试快速参考卡片

## 快速索引

| ID | 测试场景 | 并发数 | 关键验证 | 优先级 | 函数名 |
|:---|:---|:---:|:---|:---:|:---|
| CR-01 | Lua脚本原子性 | 100 | 严格不超限，无TOCTOU | P0 | `TestCR01_LuaScriptAtomicity_ConcurrentDeduction` |
| CR-02 | 窗口创建竞争 | 100 | 只创建1个窗口 | P0 | `TestCR02_WindowCreation_ConcurrentRace` |
| CR-03 | 窗口过期重建 | 100 | DEL+HSET原子性 | P0 | `TestCR03_WindowExpired_ConcurrentRebuild` |
| CR-04 | 多套餐并发 | 50 | 优先级正确 | P1 | `TestCR04_MultiPackage_ConcurrentDeduction` |
| CR-05 | 订阅启用冲突 | 2 | 只有1个成功 | P1 | `TestCR05_SubscriptionActivation_ConcurrentConflict` |
| CR-06 | total_consumed并发 | 100 | GORM Expr原子性 | P0 | `TestCR06_TotalConsumed_ConcurrentUpdate` |

## 运行命令

```bash
# 运行所有并发测试
go test -v ./scene_test/new-api-package/concurrency/

# 运行单个测试
go test -v -run TestCR01 ./scene_test/new-api-package/concurrency/

# 使用竞态检测器
go test -v -race ./scene_test/new-api-package/concurrency/

# 仅运行P0级别
go test -v -run "CR01|CR02|CR03|CR06" ./scene_test/new-api-package/concurrency/
```

## 核心验证点速查

### Redis层（Lua脚本）
- ✅ **CR-01**: 并发扣减的原子性，consumed精确匹配
- ✅ **CR-02**: 并发创建的串行化，只创建1个窗口
- ✅ **CR-03**: 并发重建的原子性，旧窗口被正确删除

### 应用层（优先级选择）
- ✅ **CR-04**: 多套餐优先级降级，总consumed匹配

### DB层（事务和原子更新）
- ✅ **CR-05**: 状态转换的原子性，WHERE子句保护
- ✅ **CR-06**: Quota累加的原子性，GORM Expr保证

## 常见问题速查

| 问题 | 测试用例 | 解决方案 |
|:---|:---|:---|
| consumed超限 | CR-01 | 检查Lua脚本limit检查逻辑 |
| 窗口创建多次 | CR-02 | 检查Lua脚本EXISTS+HSET原子性 |
| 旧consumed未清除 | CR-03 | 检查Lua脚本DEL逻辑 |
| 优先级错误 | CR-04 | 检查套餐按priority排序 |
| 重复激活 | CR-05 | 检查WHERE status='inventory'条件 |
| 累加丢失 | CR-06 | 使用GORM Expr而非read-modify-write |

## 文件路径速查

```
scene_test/new-api-package/concurrency/
├── concurrency_test.go          # 主测试文件（7个测试）
├── README.md                     # 测试套件说明
├── TEST_CASES.md                 # 测试用例详细文档
├── IMPLEMENTATION_REPORT.md      # 实现完成报告
└── QUICK_REFERENCE.md            # 本文件

scene_test/testutil/
└── concurrency_helper.go         # 并发测试辅助工具
```

## 集成检查清单

### 阶段1: Service层实现
- [ ] 实现 `service.CheckAndConsumeSlidingWindow()`
- [ ] 编写 Lua脚本 `check_and_consume_sliding_window.lua`
- [ ] 实现 `model.IncrementSubscriptionConsumed()`
- [ ] 实现 `model.ActivateSubscription()`

### 阶段2: 测试集成
- [ ] 取消测试文件中的TODO注释
- [ ] 集成 testutil.CreateTestPackage()
- [ ] 集成 testutil.CreateAndActivateSubscription()
- [ ] 启动miniredis并加载Lua脚本

### 阶段3: 执行验证
- [ ] 运行所有测试: `go test -v ./scene_test/new-api-package/concurrency/`
- [ ] 竞态检测: `go test -race`
- [ ] 性能验证: `go test -bench=.`
- [ ] 覆盖率报告: `go test -cover`

### 阶段4: 优化调整
- [ ] 根据测试结果调优并发数
- [ ] 优化Lua脚本性能
- [ ] 增加额外的边界条件测试
- [ ] 补充性能基准测试

---

**版本**: v1.0
**创建时间**: 2025-12-12
