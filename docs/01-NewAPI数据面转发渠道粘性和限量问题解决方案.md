# NewAPI 数据面转发渠道粘性和限量问题解决方案

## 1. 背景与目标
- 依据《docs/NewAPI-总体设计.md》的“无状态架构 + 智能路由”原则，当前数据面由路由层将请求分发到具体渠道，并在 `relay` 模块完成协议转换与扣费。现有逻辑未覆盖“会话级粘性、会话监控、用户会话并发上限、渠道分时额度”四个诉求，导致缓存失效与风控缺口。
- 目标：在保持现有负载均衡与重试机制的前提下，为单会话提供渠道粘性、补齐会话监控与用户并发限制，并扩展渠道的小时/天/周/月额度控制能力。

## 2. 当前数据面流程梳理
- 路由层：`router/relay-router.go:13-182` 统一挂载 `/v1` 等路径，串联 `TokenAuth -> ModelRequestRateLimit -> Distribute -> controller.Relay`。
- 渠道选路：`middleware/distributor.go:30-142` 解析请求模型，调用 `service.CacheGetRandomSatisfiedChannelMultiGroup` 按分组+模型随机/权重选路；未记录“会话->渠道”绑定。
- 转发与重试：`controller/relay.go:64-222` 首次使用上一步选中的渠道，失败后按优先级/权重重选；不同请求间无粘性。
- 渠道风控：`model/channel_risk_control.go:12-205` 仅对 P2P 渠道检查总额度、并发、每小时/每日请求数，并在 `Distribute` 里做计数；不覆盖平台渠道，且无周/月额度，也未按额度单位分时限额。
- 监控：`middleware/StatsMiddleware` 仅记录活跃连接，`GetChannelConcurrencySnapshot` 只返回渠道并发，不含“用户会话数”维度。

## 3. 问题与根因
1) 会话渠道粘性缺失：每个请求独立选路（`Distribute` + `Relay` 重试），没有基于“session/conversation”维度的绑定，导致同一对话落在不同渠道/不同上游账号。
2) 无法监控用户会话数：当前只统计连接/渠道并发，没有“用户->活跃会话”或“会话->渠道”视图，无法运维侧查看。
3) 用户并发会话无法限制：仅有时间窗口限流（ModelRequestRateLimit）与渠道并发控制，没有按用户/令牌的“同时活跃会话数”限制。
4) 渠道分时额度缺口：风控仅支持总额度 + 每小时/每日“请求数”限制，而且只对 P2P 渠道生效；缺少按额度单位的小时/天/周/月窗口控制。

## 4. 方案设计

### 4.1 会话渠道粘性（Session Stickiness）
- **Session 标识提取**：新增统一解析器，按优先级提取 `session_id`：  
  `X-NewAPI-Session` 头 -> 查询参数 `session_id` -> 请求体字段 `session_id`/`conversation_id`/`chat_id` -> `metadata.session_id|conversation_id`（兼容 Claude Code/Codex 常见字段）。无显式值则沿用现状（不做粘性）。
- **绑定存储**：在 Redis（有 Redis 优先）持久化 `session:{user_id}:{model}:{session_id}` -> `{channel_id, multi_key_index, group, ttl}`，TTL 滑动刷新（默认 30~60 分钟，可配置）。
- **读路径**：`Distribute` 选路前先查绑定；命中后验证渠道是否启用、模型能力是否仍存在、风控校验是否通过。通过则复用绑定的渠道/Key 索引；失败则删除绑定并走常规选路。
- **写路径**：当首次选路成功后写入绑定；`Relay` 中若因渠道禁用/风控/自动封禁触发重试，需同步更新/删除绑定，避免粘滞到失效渠道。
- **多节点一致性**：Redis 作为共享状态；无 Redis 时降级为本地 LRU + TTL（只在单节点或测试环境可接受），并在日志中提示。
- **多 Key 渠道**：绑定中记录 `multi_key_index`，`SetupContextForSelectedChannel` 在存在绑定时强制回用指定 key，避免同一渠道内部轮询打散上游会话。

### 4.2 会话监控与用户并发上限
- **会话跟踪数据结构**：  
  - Redis Set：`session:user:{user_id}` 存活跃 session_id；  
  - Redis Hash：`session:chan:{channel_id}` 计活跃 session 数；  
  - 绑定 TTL 失效时由后台任务或惰性清理同步扣减。
- **监控接口**：新增管理端只读接口（如 `/api/admin/relay/sessions/summary`），返回总活跃会话数、按用户/分组/渠道的活跃会话数与最近 10 个 session 详情，用于看板/报警。
- **用户并发会话限制**：新增配置（系统默认、分组覆盖、用户级 override 字段）如 `UserMaxConcurrentSessions`。在写入绑定前检查 `SCARD session:user:{uid}`，超限时返回 429/自定义错误码，已存在的同 session_id 视为复用不计入新增。

### 4.3 渠道额度分时限额（小时/天/周/月）
- **模型与字段**：在 `Channel` 增加 `HourlyQuotaLimit/DailyQuotaLimit/WeeklyQuotaLimit/MonthlyQuotaLimit`（额度单位，与 `used_quota` 同制），同时保留请求数限制；可配置是否对平台渠道生效。
- **计数实现**：  
  - Redis 计数（优先）：键形如 `chan:{id}:quota:{period}:{bucket}`，`INCRBY` 额度值并设定 TTL 等于窗口长度（1h/1d/1w/1m）。  
  - 无 Redis 回落到内存结构，启动时从 DB 预热，定时/退出时刷回 DB，风险告警。
- **校验时机**：  
  - 预检查：在 `CheckChannelRiskControl` 中基于“预计预扣额度（QuotaToPreConsume 或估算 tokens）”做试探性校验，防止超卖；  
  - 结算：在实际用量落地时（成功/失败扣返）调用 `AddChannelUsedQuota` 的同时更新各窗口计数，失败时回滚窗口增量。
- **策略交互**：当窗口额度超限，返回专用错误码（新增 `ErrorCodeChannelWeeklyLimitExceeded` / `Monthly...`），并触发自动重试切换渠道；若是会话粘性命中该渠道，应清理绑定再重选。

### 4.4 兼容与边界
- 粘性仅在显式 session 标识存在时生效，避免误将不同对话绑定。
- 渠道熔断/禁用时主动删除 session 绑定，防止落到坏渠道。
- 集群场景下强依赖 Redis；无 Redis 时需提示监控风险，并可通过配置关闭粘性/分时限额。

## 5. 落地步骤（开发拆解）
1. **Session 解析与绑定**：在 `middleware/distributor.go` 增加 session 提取与绑定查找；扩展 `SetupContextForSelectedChannel` 支持复用绑定的渠道与 key 索引；在 `controller/relay.go` 的错误处理/重试分支同步更新绑定。
2. **会话监控与限流**：实现 Redis Set/Hash 维护；新增配置项及校验逻辑；在 admin 路由下暴露会话概要接口及日志落点。
3. **渠道分时额度**：扩展 `model.Channel` 字段、迁移脚本；在 `model/channel_risk_control.go` 增加按额度的小时/天/周/月窗口校验与计数；在扣费/返还路径更新窗口值。
4. **配置与文档**：补充部署说明（启用 Redis 才能跨节点保证粘性与准确限量）、前端/SDK 传递 `session_id` 的对接约定，以及错误码说明。
5. **验证**：编写单元/集成用例覆盖：同 session 粘性、限额触发、限额回滚、会话上限触发、Redis 缺失降级。压测验证粘性命中率与限额准确性。

## 6. 预期效果
- 同一会话请求命中同一渠道/同一 key（在渠道健康前提下），避免上游缓存失效。
- 运维可实时查看活跃会话数量、分布与渠道占用，支持用户级并发会话控制。
- 渠道支持按额度的小时/天/周/月配额防护，超限可自动切换渠道或快速反馈。

## 7. 渠道剩余额度感知与自动启停预留
- **统一接口定义**：在渠道模型/配置层新增“额度探针”接口约定（如 `QuotaProbe` 配置块），支持不同渠道的剩余额度获取方式：HTTP 查询（OpenAI billing、Anthropic usage）、SDK 调用、静态阈值、手工填报。
- **探针适配层**：`service/channel_quota_probe`（预留），按渠道类型路由到具体实现，返回标准化结构：`{remaining_quota, currency, refresh_at, source}`；失败时降级为“未知”状态，不阻塞请求。
- **自动启停策略**：
  - 当探针返回剩余额度 <= 0 或低于警戒阈值时，将渠道置为“额度耗尽待恢复”状态（区别于故障禁用），暂停分发；记录原因和探针时间。
  - 定期/手动触发探针刷新，若额度恢复则自动重启渠道；重启时清理相关会话粘性绑定以避免命中旧坏状态。
- **交互与观测**：在管理端展示探针最近结果、来源和下一次刷新时间；支持 webhook/邮件告警；与现有分时限额并行存在，探针结果优先级高于分时限额（额度耗尽直接暂停）。
- **数据一致性**：探针状态写入内存缓存并持久化 DB（状态 + 时间 + 来源）；多节点通过 Redis 或数据库轮询感知，避免单点决策。
