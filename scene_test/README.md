# Scene Test - NewAPI 集成测试框架

本目录包含 NewAPI 服务的场景化集成测试。

## 测试框架概述

测试框架基于 Go 的 `testing` 包实现，管理被测应用的完整生命周期：

1. **编译 (Compilation)**: 将 NewAPI 主程序编译为测试可执行文件
2. **启动 (Setup)**: 以测试专用配置启动服务器（独立 SQLite 数据库、随机端口）
3. **执行 (Execution)**: 运行所有测试套件
4. **清理 (Teardown)**: 优雅关闭服务器并清理资源

## 目录结构

```
scene_test/
├── main_test.go                              # 测试主入口
├── testutil/
│   ├── server.go                             # 服务器生命周期管理
│   └── client.go                             # API 客户端封装
├── new-api-data-plane/
│   ├── billing/
│   │   └── billing_test.go                   # 计费正确性测试
│   └── routing-authorization/
│       └── routing_test.go                   # 路由与鉴权测试
└── new-api-management-plane/
    ├── group-management/
    │   └── group_management_test.go          # P2P分组管理测试
    └── cache-consistency/
        └── cache_test.go                     # 缓存一致性测试
```

## 运行测试

### 运行所有测试
```bash
# 从项目根目录
go test -v ./scene_test/...
```

### 运行特定测试套件
```bash
# 只运行路由测试
go test -v ./scene_test/new-api-data-plane/routing-authorization/...

# 只运行计费测试
go test -v ./scene_test/new-api-data-plane/billing/...

# 只运行 P2P 分组管理测试
go test -v ./scene_test/new-api-management-plane/group-management/...
```

### 运行特定测试
```bash
# 运行框架设置验证测试
go test -v -run TestFrameworkSetup ./scene_test/...

# 运行服务器启动/停止测试
go test -v -run TestServerStartStop ./scene_test/...
```

### 跳过长时间运行的测试
```bash
go test -v -short ./scene_test/...
```

## 测试配置

测试框架使用以下默认配置：
- **数据库**: SQLite（在临时目录中创建独立数据库）
- **端口**: 随机可用端口
- **启动超时**: 60 秒

### 自定义配置

在测试代码中可以通过 `testutil.ServerConfig` 自定义配置：

```go
cfg := testutil.ServerConfig{
    UseInMemoryDB:  true,           // 使用 SQLite
    StartupTimeout: 60 * time.Second,
    Verbose:        true,           // 打印服务器日志
    CustomEnv: map[string]string{
        "SOME_VAR": "value",
    },
}
server, err := testutil.StartServer(cfg)
```

## 测试用例状态

当前测试用例为骨架代码，标记为 `t.Skip()`。需要逐步实现具体的测试逻辑。

### 实现优先级
1. **P0 (最高)**: 核心路由和计费逻辑测试
2. **P1**: 边界条件和错误处理测试
3. **P2**: 性能和并发测试

## 添加新测试

1. 在相应目录下创建 `*_test.go` 文件
2. 使用 `testutil.StartServer()` 启动测试服务器
3. 使用 `testutil.NewAPIClient()` 创建 API 客户端
4. 编写测试逻辑和断言

示例：

```go
func TestMyFeature(t *testing.T) {
    cfg := testutil.DefaultConfig()
    server, err := testutil.StartServer(cfg)
    if err != nil {
        t.Fatalf("Failed to start server: %v", err)
    }
    defer server.Stop()

    client := testutil.NewAPIClient(server)

    // 测试逻辑...
    status, err := client.GetStatus()
    if err != nil {
        t.Fatalf("Failed to get status: %v", err)
    }

    // 断言...
}
```

## 故障排查

### 服务器启动超时
- 检查是否有端口冲突
- 增加 `StartupTimeout` 配置
- 启用 `Verbose` 模式查看服务器日志

### 数据库错误
- 确保 SQLite 支持已编译（CGO_ENABLED=1）
- 检查临时目录权限
