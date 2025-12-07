## 四、 自动化测试实现方案 (Automated Test Implementation Plan)

为确保测试的效率、稳定性和可重复性，我们将采用纯代码驱动的自动化测试方案，集成在项目的 `scene_test` 目录下。

### 4.1 测试目录结构

所有场景化集成测试将统一存放在 `scene_test/` 目录中，并按被测核心模块进行组织：

```
new-api/
├── scene_test/
│   ├── main_test.go                  # 测试主入口, 负责编译和管理被测程序生命周期
│   ├── new-api-data-plane/
│   │   ├── billing/
│   │   │   └── billing_test.go       # 计费正确性测试
│   │   └── routing-authorization/
│   │       └── routing_test.go       # 核心路由与鉴权测试
│   └── new-api-management-plane/
│       ├── group-management/
│       │   └── group_management_test.go # P2P分组管理API测试
│       └── cache-consistency/
│           └── cache_test.go          # 缓存一致性测试
└── ... (其他项目源码)
```

### 4.2 测试生命周期与环境管理

我们将利用 `go test` 的 `TestMain` 函数来统一管理被测应用的生命周期。

1.  **编译 (Compilation)**:
    *   在 `scene_test/main_test.go` 的 `TestMain` 函数中，测试开始前，首先调用 Go 编译器 (`go build`) 编译 `main.go`，生成一个用于测试的、临时的可执行文件（如 `new-api.test.exe`）。

2.  **启动 (Setup)**:
    *   启动编译好的被测程序。通过设置**环境变量**的方式，强制其连接到**内存 SQLite 数据库**并监听一个随机的本地端口。
        *   `GIN_MODE=release`
        *   `SQL_DSN="file::memory:?cache=shared"`
        *   `PORT=0` (由系统自动选择可用端口)
    *   程序启动后，测试框架会捕获其实际监听的端口地址，用于后续的 API 调用。

3.  **运行测试 (Run)**:
    *   `TestMain` 调用 `m.Run()` 来执行 `*_test.go` 文件中定义的所有测试用例。

4.  **关闭 (Teardown)**:
    *   所有测试用例执行完毕后，`TestMain` 负责向被测程序进程发送终止信号 (`SIGTERM`)，确保其优雅退出。
    *   清理编译生成的可执行文件。

#### **`scene_test/main_test.go` 伪代码**
```go
package scene_test

import (
    "os"
    "os/exec"
    "testing"
)

var (
    testAppPath string
    serverCmd   *exec.Cmd
)

func TestMain(m *testing.M) {
    // 1. 编译被测程序
    testAppPath = "./new-api.test.exe"
    buildCmd := exec.Command("go", "build", "-o", testAppPath, "../main.go")
    if err := buildCmd.Run(); err != nil {
        panic("Failed to build test app: " + err.Error())
    }

    // 2. 启动服务 (在每个测试子目录的 Setup 中完成)
    // serverCmd = startTestServer()

    // 3. 运行所有测试
    exitCode := m.Run()

    // 4. 清理
    // if serverCmd != nil && serverCmd.Process != nil {
    //     serverCmd.Process.Kill()
    // }
    os.Remove(testAppPath)

    os.Exit(exitCode)
}
```

### 4.3 测试套件与用例实现

每个具体的测试目录（如 `billing/`, `routing-authorization/`）将遵循相似的结构：

1.  **套件级 Setup/Teardown**:
    *   使用 `testing` 包的功能或 `testify/suite` 框架，在每个测试文件（或套件）的 `SetupTest` / `BeforeTest` 方法中，启动和关闭被测服务。这确保了每个测试文件都在一个独立、干净的环境中运行。
    *   `SetupTest`:
        *   调用 `main_test.go` 中定义的函数，以预设的环境变量（内存DB、随机端口）启动 `new-api.test.exe`。
        *   通过 API 调用，准备该测试场景所需的基础数据（如创建用户、渠道）。
    *   `TearDownTest`:
        *   关闭被测服务进程。

2.  **测试用例 (Test Functions)**:
    *   每个 `TestXxx` 函数负责一个具体的测试场景。
    *   **Arrange**: 准备该场景的特定输入（如构建一个 `v1/chat/completions` 的请求体）。
    *   **Act**: 使用 Go 的 `net/http` 客户端向测试服务器发送请求。
    *   **Assert**:
        *   使用 `testify/assert` 库断言 HTTP 响应码、响应体内容是否符合预期。
        *   直接连接到内存数据库（因为是 `cache=shared` 模式），查询 `logs`, `users` 等表，断言数据变更是否正确。

#### **`billing_test.go` 伪代码**
```go
package billing_test

import (
    "net/http"
    "testing"
    "github.com/stretchr/testify/assert"
)

// (SetupTest / TeardownTest 在这里启动和关闭服务)

func TestBilling_HighRateUserUsesLowRateChannel(t *testing.T) {
    // Arrange: 准备用户A(vip), B(default), 渠道Ch-B(owner B), A加入B的分组
    // ... 通过 API 调用预置数据 ...
    // 获取用户A的Token

    // Act: 用户A使用其Token请求一个通过Ch-B路由的模型
    req, _ := http.NewRequest("POST", testServerURL+"/v1/chat/completions", ...)
    req.Header.Set("Authorization", "Bearer "+tokenOfUserA)
    resp, _ := http.DefaultClient.Do(req)

    // Assert:
    assert.Equal(t, http.StatusOK, resp.StatusCode)
    
    // 连接到内存数据库，查询消费日志
    log := queryLatestLogFromDB(userA.ID)
    
    // 预期扣费应基于用户A的vip费率(rate=2)，而不是渠道B的default费率(rate=1)
    expectedQuota := calculateQuota(baseTokens, 2.0)
    assert.Equal(t, expectedQuota, log.Quota)
}
```
通过这种方式，整个测试流程实现了完全的自动化和代码化，保证了测试的一致性和高效率。