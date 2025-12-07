# NewAPI - 会话粘性与分时限额 测试设计与分析说明书

| 文档信息 | 内容 |
| :--- | :--- |
| **模块名称** | *Relay - Session Stickiness & Time-based Quota* |
| **文档作者** | *Gemini* |
| **测试环境** | *SIT (Mock Mode) / UAT (Real Channel)* |
| **版本日期** | *2025-12-07* |

---

## 一、 测试方案原理 (Test Scheme & Methodology)

> **核心策略**：本次测试将采用**灰盒测试**模式，聚焦于 NewAPI 的会话粘性、用户并发控制及渠道分时限额功能。所有测试场景都将通过**自动化测试代码**（例如，使用 Golang 的 `testing` 包）实现。我们将把 NewAPI 视为被测主体，通过在代码中动态创建 **Mock Server**（`httptest.Server`）来模拟上游 LLM 渠道的行为，同时直接与 **Redis** 客户端交互来验证和控制缓存状态，从而精确地验证新功能在各种场景下的逻辑正确性。

### 1.1 测试拓扑与控制流 (Topology & Control)
测试代码将通过控制**输入参数**（如 `session_id`、请求模型）和**Mock Server 的响应处理器**来驱动被测系统的逻辑流转，并在 Redis 和最终响应中设立断言。

```mermaid
sequenceDiagram
    participant Tester as 自动化测试代码 (Go Test)
    participant System as [被测系统] NewAPI Gateway
    participant Redis as Redis 客户端
    participant Mock as [Mock Server] httptest.Server

    Note over Tester, Mock: 1. 输入控制 (Input Control)
    Tester->>System: 发起HTTP请求 (携带 session_id)
    
    System->>Redis: 2. 查询会话绑定 (session:{id})
    alt 会话已绑定
        Redis-->>System: 返回 channel_id
        System->>Mock: 3a. 请求已绑定的渠道
    else 会话未绑定
        System->>System: 3b. 执行标准负载均衡
        System->>Mock: 3c. 请求新选择的渠道
    end
    
    Note over System, Redis: 4. 中间状态检测 (Checkpoint A)
    Mock-->>System: 4a. 返回模拟结果 (成功/失败)
    System->>Redis: 4b. 创建/更新/删除会话绑定
    System->>Redis: 4c. 更新渠道分时限额计数
    
    System-->>Tester: 5. 返回最终响应
    
    Note over Tester, Redis: 6. 最终状态验证 (Checkpoint B)
    Tester-->>Redis: [断言] 验证 session 绑定, user 并发数, channel 额度
```

### 1.2 关键检测点设计 (Checkpoints)
在业务流中，我们将设立以下关键检测点：

*   **代码注入点**：
    *   在测试函数中，构造 `http.Request` 对象，并设置其 Header、Query Param 和 Body，用于注入不同的 `session_id`。
    *   在测试的 `Setup` 阶段，通过代码直接调用渠道管理 API 或操作数据库，来配置分时限额和用户并发数限制。
*   **中间态断言 (Redis)**：
    *   **会话绑定**: 使用 Redis 客户端（如 `go-redis`）直接执行 `HGETALL`，断言 `session:{...}` HASH 键的内容与预期一致。
    *   **用户并发**: 执行 `SCARD` 命令，断言 `session:user:{id}` 集合的大小。
    *   **渠道额度**: 执行 `GET` 命令，断言 `channel_quota:{id}:{period}` 计数器的值。
*   **外部模拟 (Mock Server)**：
    *   每个测试用例启动一个 `httptest.Server`，其处理器 `HandlerFunc` 按需返回指定的 HTTP 状态码和响应体。
    *   通过 `time.Sleep` 或 `context.WithTimeout` 模拟网络超时。
*   **输出断言**：
    *   检查 HTTP 响应的状态码、业务码，并对响应体进行断言。
    *   对于监控API，反序列化返回的 JSON 并断言其各字段的准确性。

### 1.3 Mock Server 实现策略 (Mock Server Strategy)
Mock Server 将在测试代码中通过 `httptest.NewServer` 创建，其处理器函数根据测试场景动态配置。

| 场景分类 | Mock 处理器行为 (HandlerFunc) | 模拟返回数据 (Payload Example) | 目的 |
| :--- | :--- | :--- | :--- |
| **标准成功** | 写入 HTTP 200，并返回 JSON 报文。 | `w.Write([]byte(`{"id": "...", "usage": {"completion_tokens": 20}}`))` | 验证主流程、会话绑定创建与额度累加。 |
| **渠道失败** | 写入 HTTP 503 状态码。 | `w.WriteHeader(http.StatusServiceUnavailable)` | 验证会话绑定在渠道失败时的自动解绑与重试。 |
| **可识别渠道**| 在响应头中写入自定义标识。 | `w.Header().Set("X-Mock-Channel-ID", "mock-channel-A")` | 用于在测试代码中断言请求是否命中了预期的粘性渠道。 |
| **额度超限** | 返回业务错误码。 | `w.Write([]byte(`{"error": {"code": "insufficient_quota"}}`))` | 验证渠道因上游额度耗尽而被动禁用的场景。 |

---

## 二、 测试点分析列表 (Test Point Analysis)

### 2.1 会话粘性与生命周期测试 (Session Stickiness & Lifecycle)

| ID | 测试子项 | 变量控制 (输入 & Mock) | 中间状态检测 (Redis) | 预期结果 (输出) | 优先级 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **S-01** | **首次请求成功绑定** | 输入：请求携带 `X-NewAPI-Session-ID` <br>Mock：返回 200 OK | 1. 成功创建 `session:{...}` HASH，包含正确的 `channel_id`。 <br> 2. `session:{...}` 键设置了 TTL。 | 接口返回成功，用户获得响应。 | **P0** |
| **S-02** | **后续请求命中粘性** | 输入：使用与 S-01 相同的 `session_id` <br>Mock：所有渠道均返回可识别的 `X-Mock-Channel-ID` | Redis HASH 键被访问，TTL 被刷新。 | 第二次请求的响应头包含与第一次相同的 `X-Mock-Channel-ID`，证明命中了同一渠道。 | **P0** |
| **S-03** | **渠道失败自动解绑与重路由** | 1. 请求A，绑定到渠道-1。<br>2. Mock 渠道-1 返回 503 错误。<br>3. 请求B（同会话），再发一次。 | 1. 请求B后，`session:{...}` HASH 键被删除。<br>2. 请求B成功后，创建了指向新渠道（非渠道-1）的新绑定。 | 1. 请求B被路由到其他可用渠道（例如渠道-2）。<br>2. 后续请求将粘滞在渠道-2。 | **P0** |
| **S-04** | **粘性渠道失效后恢复** | 1. 请求A，绑定到渠道-1。<br>2. Redis 中手动修改绑定，指向一个已禁用的 `channel_id`。<br>3. 请求B（同会话）发出。 | `session:{...}` HASH 键被删除，并重新创建了指向可用渠道的新绑定。 | 请求B被重新路由到健康的渠道，而不是卡在已失效的绑定上。 | P1 |
| **S-05**| **会话ID提取优先级**| 依次在 Header, Query Param, Body 中设置不同的 `session_id`。 | - | 系统应按 `Header > Query > Body` 的优先级使用正确的 `session_id` 进行绑定。 | P1 |
| **S-06**| **会话超时自动失效**| 1. 请求A成功，创建绑定。<br>2. 等待超过 TTL 时间。<br>3. 请求B（同会话）发出。 | `session:{...}` HASH 键已因 TTL 过期而被 Redis 删除。 | 请求B会触发新的负载均衡选路，而不是使用旧的绑定。 | P2 |

### 2.2 用户并发会话与监控测试 (User Concurrency & Monitoring)

| ID | 测试子项 | 变量控制 (输入 & 配置) | 中间状态检测 (Redis) | 预期结果 (输出) | 优先级 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **C-01** | **超出并发会话数限制** | 1. 用户组 `max_concurrent_sessions` 设为 2。<br>2. 并发发送3个**不同** `session_id` 的新会话请求。 | `SCARD session:user:{id}` 结果为 2。 | 前2个请求成功，第3个请求返回 `429 Too Many Requests` 错误，提示并发超限。 | **P0** |
| **C-02** | **复用已有会话不计入并发**| 1. 用户组 `max_concurrent_sessions` 设为 1。<br>2. 请求A (sid=1) 成功。<br>3. 请求B (sid=1) 再次发送。 | `SCARD session:user:{id}` 结果为 1。 | 请求A和B均成功，请求B不因并发限制被拒绝。 | P1 |
| **C-03** | **会话过期后并发数恢复**| 1. 并发会话数达到上限 (2/2)。<br>2. 等待其中一个会话的 Redis 绑定过期。<br>3. 发送一个新的会话请求 (sid=3)。 | 过期会话的 `session_id` 从 `session:user:{id}` SET 中被移除。 | 新会话请求(sid=3)成功，不被拒绝。 | P2 |
| **C-04** | **监控API数据准确性** | 1. 创建多个用户的多个会话。<br>2. 调用 `GET /api/admin/sessions/summary`。 | - | 1. `total_active_sessions` 总数正确。<br>2. `sessions_by_channel` 中各渠道的会话计数正确。<br>3. `top_users_by_session` 列表按会话数降序排列且数据准确。 | P1 |

### 2.3 渠道分时额度与风控测试 (Time-based Quota & Risk Control)

| ID | 测试子项 | 变量控制 (输入 & 配置) | 中间状态检测 (Redis) | 预期结果 (输出) | 优先级 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Q-01** | **小时额度精确控制** | 1. 设置渠道 `hourly_quota_limit` 为 1000。<br>2. Mock Server 每次返回 `usage` 消耗 400。<br>3. 发送3次请求。 | `GET channel_quota:{id}:hourly:{ts}` 的值依次为 400, 800。第3次请求前检查值为800。 | 1. 前2次请求成功。<br>2. 第3次请求时，因 `800+400 > 1000`，该渠道被过滤，请求被路由到其他渠道或返回无可用渠道。 | **P0** |
| **Q-02** | **请求后额度原子累加** | 1. 设置渠道 `daily_quota_limit` 为 5000。<br>2. 并发发送 5 个请求，每个消耗 1000。 | `GET channel_quota:{id}:daily:{ts}` 的最终值应为 5000，不多不少。 | 所有请求都成功（因为预检查时额度均未超限）。 | P1 |
| **Q-03** | **时间窗口滚动与重置** | 1. 设置渠道 `hourly_quota_limit`。<br>2. 在当前小时内耗尽额度，验证渠道不可用。<br>3. 等待进入下一个小时。 | `channel_quota:{...}` 键因 TTL 过期而消失，或新小时的键值为0。 | 在新的一小时内，首次请求该渠道能够成功。 | P1 |
| **Q_04** | **Redis不可用时降级** | 1. 停止 Redis 服务。<br>2. 发送请求。 | - | 系统应降级为不限流或使用内存限流（取决于设计），服务不应崩溃，并打印明确的警告日志。 | P2 |

---

## 三、 测试数据与环境准备 (Test Data & Environment)

> 所有测试数据和环境配置均应在测试代码的 `Setup` / `BeforeEach` 钩子中以编程方式动态创建，在 `Teardown` / `AfterEach` 中清理，确保测试的独立性和可重复性。

1.  **数据库预置 (Programmatic Seeding)**:
    *   **渠道 (Channels)**: 在测试开始前，通过代码向数据库中插入 `channel-A`, `channel-B`, `channel-C`, `channel-D` 等测试渠道，并设置其属性。
    *   **用户与分组 (Users & Groups)**: 创建 `user-normal` 和 `user-concurrent-limit` 等测试用户，并设置其所属分组的 `max_concurrent_sessions` 属性。

2.  **Mock Server 动态配置**:
    *   每个测试用例根据需要，在 `httptest.NewServer` 的处理器函数中编写特定的响应逻辑。例如，为 `Q-01` 用例配置 Mock Server 返回 `{"usage": {"completion_tokens": 400}}`。

3.  **Redis 状态控制**:
    *   在每个测试用例开始前，执行 `FLUSHDB` 命令清空 Redis 数据库，避免数据污染。
    *   在需要构造特定场景时（如 `S-04`），通过代码执行 `HSET` 等命令，直接修改 Redis 中的数据来创建测试前提。
