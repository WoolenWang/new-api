# NewAPI 系统统计数据 Dashboard 设计

| 文档属性 | 内容 |
| :--- | :--- |
| **版本** | V1.0 |
| **最后更新** | 2025-12-13 |
| **状态** | 设计中 |
| **作者** | Codex |
| **关联文档** | `NewAPI-总体设计.md`, `01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md`, `01-P2P共享分组与用户创建渠道的状态信息监控统计与展示-测试方案.md`, `01-P2P共享分组与用户创建渠道的状态信息监控统计与展示-Wquant对接NewAPI设计.md`, `08-渠道统计系统运维指南.md` |

---

## 1. 背景与总体目标

### 1.1 背景

NewAPI 已经完成：

- 渠道统计三层架构：`channel_statistics` + `channels` 实时字段，提供 TPM/RPM、失败率、首字延迟、停服时间占比等指标。
- P2P 分组聚合统计：`group_statistics` 表 + GroupStatsScheduler/Aggregator，实现分组维度的窗口聚合。
- 模型质量监控：`model_monitoring_results` 表及相关 API。
- 用量看板：基于 `quota_data` 的 `/api/data` 与 `/api/data/self`，支持管理员与用户的日均用量曲线。

WQuant 对接设计提出了更上层的“数据看板 / 分组广场”展示需求，需要在现有统计链路之上，构建：

1. **共享分组排名视图**：按 Token 消耗、TPM/RPM、响应时间、失败/停服占比，对 P2P 共享分组做多维度排序。
2. **计费分组统计视图**：即“系统分组性能对比”，对 `default/vip/svip` 等系统分组的整体服务质量进行对比。
3. **整系统统计视图**：全局 TPM/RPM、7/30 天 Token 总消耗以及日均 Token 消耗曲线。

本设计在“不新增核心表结构”的前提下，复用已有统计与用量体系，为上述需求提供统一的数据接口与计算规范。

### 1.2 目标

- 明确“共享分组排名 / 计费分组统计 / 整系统统计”的指标定义与计算口径。
- 梳理当前实现的支持程度，标注缺口。
- 设计一个**只补充聚合与接口层、尽量不改动数据采集链路**的扩展方案。
- 为后续实现与测试提供可直接对照的接口列表与伪代码。

---

## 2. 现有能力盘点

### 2.1 渠道与分组统计链路

- **底层数据表**
  - `channel_statistics`：按 `(channel_id, model_name, time_window_start)` 存储 15 分钟窗口统计。
    - 指标：`request_count`, `fail_count`, `total_tokens`, `total_quota`, `total_latency_ms`, `stream_req_count`, `cache_hit_count`, `downtime_seconds`, `unique_users` 等。
  - `channels`：实时统计字段（最新窗口快照）
    - 字段：`tpm`, `rpm`, `quota_pm`, `avg_response_time`, `fail_rate`, `avg_cache_hit_rate`, `stream_req_ratio`, `total_sessions`, `downtime_percentage`, `unique_users` 等。
  - `group_statistics`：P2P 分组聚合表 (`model.GroupStatistics`)
    - 复合主键：`(group_id, model_name, time_window_start)`
    - 指标：`TPM`, `RPM`, `FailRate`, `AvgResponseTimeMs`, `AvgCacheHitRate`, `StreamReqRatio`, `QuotaPM`, `TotalTokens`, `TotalQuota`, `AvgConcurrency`, `TotalSessions`, `DowntimePercentage`, `UniqueUsers`。

- **聚合服务**
  - `service/channel_stats_l1.go` / `channel_stats_l2.go` / `channel_stats_l3.go`：负责 L1→L2→L3 的统计写入。
  - `service/group_stats_scheduler.go` + `group_stats_aggregator.go`：
    - 从渠道统计更新事件触发，按 30 分钟节流，计算各 P2P 分组的窗口聚合并写入 `group_statistics`。

- **已有统计 API**
  - 渠道维度：
    - `GET /api/channels/:id/stats`（管理员）：按 `period` 返回单渠道统计。
    - `GET /api/channel/self/stats`（WQuant 设计）：当前用户自有渠道的统计列表。
  - 分组维度：
    - `GET /api/p2p_groups/:id/stats`：单分组的整体聚合视图或单模型最新窗口视图。
    - `GET /api/p2p_groups/:id/stats/history`：单分组的时间序列。
  - 系统分组维度：
    - `GET /api/groups/system/stats`：对 `default/vip/svip` 做系统分组聚合（按用户系统分组聚合其名下渠道统计），内部使用 `model.AggregateChannelStatsByUserGroup`。

### 2.2 用量看板与全站用量

- **表结构**
  - `quota_data`：
    - 维度：`user_id`, `username`, `model_name`, `created_at(按小时对齐)`。
    - 指标：`count`（调用次数）、`quota`（额度）、`token_used`（Token）。

- **缓存与写入**
  - `model.LogQuotaData`：在请求后按小时维度缓存用量，再由 `UpdateQuotaData` 周期性落库。

- **现有 API**
  - `GET /api/data`（管理员）：全站用量按日期/小时统计（由 `GetAllQuotaDates` 提供数据）。
  - `GET /api/data/self`（用户）：当前用户用量按日期/小时统计。

- **前端使用**
  - React 后台 Dashboard 中，管理员视角通过 `/api/data` 得到：
    - 不同模型的时间序列（`quota`, `token_used`, `count`, `created_at`）。
    - 前端自行计算 `avgRPM` / `avgTPM` 用于展示性能指标。

---

## 3. 需求对照与支持程度分析

本节只做“支持程度”分析，具体方案在后续章节展开。

### 3.1 共享分组排名（P2P 分组排名）

目标维度：

- 按 7 天/30 天 Token 总消耗排名。
- 按 TPM / RPM 排名。
- 按渠道平均响应时间排名。
- 按分组失败率 / 无渠道服务时间占比（停服时间占比）排名。

分析：

- **数据是否具备？**
  - ✅ `group_statistics` 已包含：
    - `TotalTokens`, `TPM`, `RPM`, `AvgResponseTimeMs`, `FailRate`, `DowntimePercentage`。
  - ✅ 时间窗口粒度为 15 分钟，可以通过 `time_window_start` 聚合 7/30 天区间。
  - ✅ `groups` 表有 `type` / `join_method` 字段区分私有组与公开共享组。

- **现有接口是否直接支持“排名”？**
  - `GET /api/p2p_groups/:id/stats` 仅返回**单个分组**的聚合视图。
  - `GET /api/groups/public` 仅返回公开分组的基本信息 + `member_count`，**没有任何聚合统计字段**。
  - ❌ 当前没有“跨分组聚合 + 排序”的接口，需要新增聚合查询与 API。

结论：

- **统计数据层面：能力完备。**
- **接口层面：缺少“按指标聚合所有公开分组并排序”的查询接口。**

### 3.2 计费分组统计（系统分组性能对比）

需求说明：

- “计费分组统计”即原先的“系统分组性能对比”，对 `default/vip/svip` 等系统级分组做整体性能对比。

现状：

- `model.AggregateChannelStatsByUserGroup(userGroup, startTime, endTime)`：
  - 按 `users.group` 维度聚合其名下渠道在 `channel_statistics` 中的统计数据。
  - 输出：`AggStats`，包含 `TPM`, `RPM`, `QuotaPM`, `FailRate`, `AvgResponseTimeMs`, `CacheHitRate`, `StreamReqRatio`, `DowntimePercentage`, `UniqueUsers` 等。
- `controller/system_group_stats.go: GetSystemGroupsStats`：
  - 固定系统分组列表：`["default", "vip", "svip"]`。
  - 支持 `period` 参数：`1h/6h/24h/7d/30d`。
  - 返回每个系统分组的聚合指标（不含 `TotalTokens/TotalQuota` 字段）。

结论：

- 从“性能对比”视角看，**现有实现已基本满足计费分组统计需求**。
- 如需为系统分组提供“7/30 天 Token 总消耗”指标，只需在响应中补充 `TotalTokens/TotalQuota` 即可，无需改动底层聚合逻辑。

### 3.3 整系统统计信息

需求维度：

- 整个 NewAPI 实例的：
  - 总 TPM、总 RPM（按指定区间）。
  - 7 天 / 30 天 Token 总消耗。
  - 日均 Token 消耗曲线（全站）。

现状：

- 全站用量：
  - 已有 `quota_data` + `GET /api/data` 提供“全站按时间桶（小时）聚合的 Token / Quota / Count”。
  - React Dashboard 前端已经利用这些数据计算 `avgTPM`/`avgRPM` 并绘制时间序列图。
- 全站 TPM / RPM：
  - 当前仅在前端基于 `/api/data` 结果计算平均值，**没有统一的后端聚合接口**。
- 全站 Token 总消耗：
  - `/api/data` 可以通过时间窗口参数计算出 7/30 天总 Token（前端聚合），但未在后端接口中显式给出。

结论：

- **数据已经存在，但缺少“统一的后端聚合接口与统一口径定义”**。
- 可以基于 `channel_statistics` 或 `quota_data` 设计新的“系统统计”接口，给出：
  - 汇总指标（summary）。
  - 日均 Token 曲线（time-series）。

---

## 4. 总体设计思路

### 4.1 设计原则

- **不改动现有采集链路**：继续使用现有 L1/L2/L3 与 `quota_data` 写入逻辑。
- **优先复用现有表结构**：`group_statistics`, `channel_statistics`, `quota_data`，尽量不新增表。
- **聚合逻辑后移到查询层**：通过新增 Model 层聚合函数 + Controller 层 API 来实现新的统计视图。
- **口径统一**：
  - 分组与系统分组的 TPM/RPM/停服占比等指标，优先以 `channel_statistics`/`group_statistics` 为准。
  - 日均 Token 曲线采用 `channel_statistics.total_tokens` 聚合，确保与分组/渠道统计口径一致。

### 4.2 数据源与口径选择

- **分组 & 系统分组统计**：
  - 统一使用 `channel_statistics` → `group_statistics` / `AggregateChannelStatsByUserGroup`。
- **整系统汇总指标**：
  - 使用 `channel_statistics` 跨渠道聚合得到全局指标（TPM/RPM/FailRate/AvgLatency/...）。
- **整系统 Token 曲线**：
  - 使用 `channel_statistics` 以“自然日”为粒度聚合 `total_tokens`，避免维护多套口径。

---

## 5. 共享分组排名设计

### 5.1 指标定义与计算公式

以 `now` 为当前时间，`start_7d = now - 7d`, `start_30d = now - 30d`。

对任意 P2P 共享分组 `G`，使用 `group_statistics` 表：

- **7/30 天 Token 总消耗**：
  - `tokens_7d(G) = SUM(gs.total_tokens)`  
    `FROM group_statistics gs WHERE gs.group_id = G AND gs.time_window_start BETWEEN start_7d AND now`
  - `tokens_30d(G)` 同理，窗口改为 `start_30d`。

- **TPM / RPM**（区间平均值）：
  - `tpm_avg(G, period) = AVG(gs.tpm)`
  - `rpm_avg(G, period) = AVG(gs.rpm)`
  - `period` 支持 `1h/6h/24h/7d/30d`。

- **平均响应时间**：
  - `avg_latency(G, period) = AVG(gs.avg_response_time_ms)`。

- **失败率**：
  - `fail_rate(G, period) = AVG(gs.fail_rate)`（已在 `group_statistics` 中按请求数加权聚合）。

- **无渠道服务时间占比（停服时间占比）**：
  - `downtime_percentage(G, period) = AVG(gs.downtime_percentage)`。
  - 上游计算基于渠道停服时间与统计窗口长度，已经在 `aggregateChannelStats` 中处理。

> 备注：由于 `group_statistics` 本身已经执行了一次“渠道维度 → 分组维度”的加权聚合，这里再对时间窗口做 `AVG`/`SUM` 聚合时，不再引入额外权重，以保持实现简单且符合设计文档 5.1 的语义。

### 5.2 排名规则

为简化客户端逻辑，约定由服务端完成排序，并返回已排序的列表：

- **按 Token 消耗排名**：
  - `metric = tokens_7d` 或 `tokens_30d`，默认按**降序**排序（Token 越多排名越靠前）。
- **按 TPM/RPM 排名**：
  - `metric = tpm` 或 `rpm`，按区间平均值降序排序。
- **按平均响应时间排名**：
  - `metric = latency`，按 `avg_latency` 升序排序（延迟越低排名越靠前）。
- **按失败率 / 停服占比排名**：
  - 视业务使用场景不同，有两种模式：
    - “最稳定分组榜”：按 `fail_rate` 或 `downtime_percentage` 升序。
    - “风险榜”：按同一指标降序。
  - 通过 `order` 参数（`asc/desc`）由调用方显式指定，默认选择“最稳定”（`asc`）。

### 5.3 聚合查询设计（模型层）

新增模型层聚合方法（示意伪代码）：

```go
// RankingMetric 枚举
type RankingMetric string

const (
    RankingTokens7d   RankingMetric = "tokens_7d"
    RankingTokens30d  RankingMetric = "tokens_30d"
    RankingTPM        RankingMetric = "tpm"
    RankingRPM        RankingMetric = "rpm"
    RankingLatency    RankingMetric = "latency"
    RankingFailRate   RankingMetric = "fail_rate"
    RankingDowntime   RankingMetric = "downtime"
)

type GroupRankingRow struct {
    GroupId            int     `json:"group_id"`
    GroupName          string  `json:"group_name"`
    DisplayName        string  `json:"display_name"`
    MemberCount        int64   `json:"member_count"`
    ChannelCount       int64   `json:"channel_count"`
    Tokens7d           int64   `json:"tokens_7d"`
    Tokens30d          int64   `json:"tokens_30d"`
    AvgTPM             float64 `json:"tpm"`
    AvgRPM             float64 `json:"rpm"`
    AvgLatencyMs       float64 `json:"avg_response_time_ms"`
    AvgFailRate        float64 `json:"fail_rate"`
    AvgDowntimePercent float64 `json:"downtime_percentage"`
}
```

SQL 思路（以 7 天 Token 排名为例）：

```sql
SELECT
    g.id            AS group_id,
    g.name          AS group_name,
    g.display_name  AS display_name,
    COALESCE(mc.member_count, 0)  AS member_count,
    COALESCE(cc.channel_count, 0) AS channel_count,
    COALESCE(SUM(gs.total_tokens), 0)                    AS tokens_7d,
    COALESCE(AVG(gs.tpm), 0)                             AS tpm,
    COALESCE(AVG(gs.rpm), 0)                             AS rpm,
    COALESCE(AVG(gs.avg_response_time_ms), 0)            AS avg_response_time_ms,
    COALESCE(AVG(gs.fail_rate), 0)                       AS fail_rate,
    COALESCE(AVG(gs.downtime_percentage), 0)             AS downtime_percentage
FROM groups g
LEFT JOIN group_statistics gs
    ON gs.group_id = g.id
    AND gs.time_window_start BETWEEN :start_7d AND :now
LEFT JOIN (
    SELECT group_id, COUNT(*) AS member_count
    FROM user_groups
    WHERE status = 1
    GROUP BY group_id
) mc ON mc.group_id = g.id
LEFT JOIN (
    -- 通过 channels.allowed_groups JSON 字段大致统计渠道数
    SELECT :group_id_placeholder AS group_id, COUNT(*) AS channel_count
    FROM channels
) cc ON cc.group_id = g.id
WHERE g.type = 2 -- Shared
  AND g.join_method != 0 -- 非纯私有邀请
GROUP BY g.id, g.name, g.display_name, mc.member_count, cc.channel_count
ORDER BY tokens_7d DESC
LIMIT :limit OFFSET :offset;
```

> 实际实现中，`channel_count` 可通过类似 `getGroupChannelIds` 的逻辑复用，或在后续迭代中单独优化，不影响排名功能本身。

### 5.4 新增 API 设计：共享分组排名

**Endpoint**

- `GET /api/groups/public/rankings`

**鉴权**

- 任何已登录用户（`middleware.UserAuth()`），不需要管理员权限。

**Query 参数**

- `metric`（必填）：`tokens_7d` / `tokens_30d` / `tpm` / `rpm` / `latency` / `fail_rate` / `downtime`。
- `period`（可选）：`1h` / `6h` / `24h` / `7d` / `30d`，默认 `7d`。
  - 对 `tokens_7d/tokens_30d` 可忽略 `period`，直接使用固定窗口。
- `order`（可选）：`asc` / `desc`，默认：
  - 容量类（`tokens_*`, `tpm`, `rpm`）：`desc`。
  - 稳定性类（`latency`, `fail_rate`, `downtime`）：`asc`。
- `limit`（可选）：每页数量，默认 `20`，最大 `100`。
- `offset`（可选）：偏移量，支持分页。

**响应示例**

```json
{
  "success": true,
  "data": {
    "metric": "tokens_7d",
    "period": "7d",
    "order": "desc",
    "items": [
      {
        "group_id": 101,
        "group_name": "gpt4_fleet",
        "display_name": "GPT-4 稳定车队",
        "member_count": 12,
        "channel_count": 5,
        "tokens_7d": 123456789,
        "tokens_30d": 456789012,
        "tpm": 50000.0,
        "rpm": 900.0,
        "avg_response_time_ms": 950.0,
        "fail_rate": 0.8,
        "downtime_percentage": 0.1
      }
    ]
  }
}
```

### 5.5 前端使用建议（WQuant 分组广场）

- WQuant “分组广场”页面在获取公开分组列表时：
  1. 调用 `GET /svr/v1/newapi/group_public_rankings`（WQuant 后端转发到 NewAPI `/api/groups/public/rankings`）。
  2. 直接使用 `items` 中的字段渲染卡片上的“总 TPM / 平均成功率 / 成员数 / 渠道数”等指标。
  3. 排序切换可以通过切换 `metric`/`order` 参数重新请求，不需要在前端自行排序。

---

## 6. 计费分组统计（系统分组性能对比）增强方案

### 6.1 保留现有接口

- 继续使用：`GET /api/groups/system/stats?period=7d`
  - 定位：**系统级计费分组统计总览接口**，用于前端展示“所有计费分组（系统分组）的性能对比”。
  - 内部调用 `AggregateChannelStatsByUserGroup`，按“用户所属系统分组”聚合其名下渠道的统计信息。
  - 当前实现中，分组列表暂时硬编码为 `["default", "vip", "svip"]`；
    设计目标是：后续改造为**从系统计费分组配置动态枚举全部计费分组**（例如从 `ratio_setting.GetGroupRatioCopy()` 或等价配置中获取），
    并过滤掉 `auto` 等非计费组。

当前响应结构（简化）：

```json
[
  {
    "group_name": "default",
    "stats": {
      "tpm": 10000,
      "rpm": 200,
      "quota_pm": 500000,
      "avg_response_time": 1200,
      "fail_rate": 0.5,
      "total_sessions": 1000,
      "unique_users": 50,
      "avg_cache_hit_rate": 20,
      "stream_req_ratio": 40,
      "downtime_percentage": 1.2
    }
  }
]
```

### 6.2 补充 Token 维度（可选增强）

为支持“系统分组 7/30 天 Token 总消耗”统计，可以在 `GetSystemGroupsStats` 中直接透出 `AggregatedStats.TotalTokens/TotalQuota`：

- 增加响应字段：

```json
"stats": {
  "total_tokens": 123456789,
  "total_quota": 987654321,
  ...
}
```

- 计算口径：
  - `TotalTokens` / `TotalQuota` 基于 `channel_statistics` 中选定时间窗口内的 `SUM(total_tokens/total_quota)`。
  - TPM/RPM/QuotaPM 继续保持现有算法（总量除以区间分钟数）。

该调整不改变现有字段语义，前端可以选择性使用。

### 6.3 用户级计费分组统计方案（个人视角）

在系统级计费分组统计之外，还需要从**单个用户视角**查看其在不同计费分组上的消耗情况，典型用例：

- 用户自己查看：最近 7/30 天，在 `default/vip/svip` 等计费分组下分别消耗了多少 Token / 额度。
- 管理员查看：某个用户在不同计费分组下的消耗分布，用于运营分析与风控。

#### 6.3.1 数据来源与口径

- 数据来源：
  - **`logs` 表（模型 `model.Log`，存于 `LOG_DB`）**：
    - 每条消费日志中包含：
      - `user_id`：用户 ID
      - `group`：本次请求使用的计费分组（来自 `relayInfo.UsingGroup`，即最终 BillingGroup）
      - `model_name`：模型
      - `quota`：本次请求实际扣除额度（已考虑分组倍率）
      - `prompt_tokens` / `completion_tokens`
      - `created_at`：时间戳
    - 日志类型 `type = LogTypeConsume` 表示一次成功扣费请求。

- 统计口径：
  - **个人维度**：固定 `user_id = 当前登录用户`（或管理员指定的 user_id）。
  - **计费分组维度**：按 `logs.group` 作为 BillingGroup 进行聚合。
  - 指标：
    - `total_tokens = SUM(prompt_tokens + completion_tokens)`
    - `total_quota = SUM(quota)`
    - `request_count = COUNT(*)`
    - `tpm = total_tokens / minutes`
    - `rpm = request_count / minutes`
  - 日消耗曲线：
    - `day = DATE(FROM_UNIXTIME(created_at))`，按 (day, group) 聚合 `tokens/ quota`。

> 说明：个人计费分组统计更关注“计费逻辑最终选用哪个 BillingGroup”，因此以 `logs.group` 为基础，而不是用户主分组 `users.group`。

---

## 7. 整系统统计信息设计

### 7.1 汇总指标（Summary）

新增聚合函数（模型层伪代码）：

```go
// AggregateGlobalChannelStatsByTime 聚合所有渠道在指定时间范围内的统计
func AggregateGlobalChannelStatsByTime(startTime, endTime int64) (*AggregatedStats, error) {
    query := DB.Table("channel_statistics").
        Select(`
            SUM(request_count) as request_count,
            SUM(fail_count) as fail_count,
            SUM(total_tokens) as total_tokens,
            SUM(total_quota) as total_quota,
            SUM(total_latency_ms) as total_latency_ms,
            SUM(stream_req_count) as stream_req_count,
            SUM(cache_hit_count) as cache_hit_count,
            SUM(downtime_seconds) as downtime_seconds,
            SUM(unique_users) as unique_users,
            CASE
                WHEN SUM(request_count) > 0 THEN (SUM(fail_count) * 100.0 / SUM(request_count))
                ELSE 0
            END as fail_rate,
            CASE
                WHEN SUM(request_count) > 0 THEN (SUM(total_latency_ms) * 1.0 / SUM(request_count))
                ELSE 0
            END as avg_response_time_ms,
            CASE
                WHEN SUM(request_count) > 0 THEN (SUM(cache_hit_count) * 100.0 / SUM(request_count))
                ELSE 0
            END as cache_hit_rate,
            CASE
                WHEN SUM(request_count) > 0 THEN (SUM(stream_req_count) * 100.0 / SUM(request_count))
                ELSE 0
            END as stream_req_ratio
        `)

    if startTime > 0 {
        query = query.Where("time_window_start >= ?", startTime)
    }
    if endTime > 0 {
        query = query.Where("time_window_start <= ?", endTime)
    }

    var result AggregatedStats
    if err := query.Scan(&result).Error; err != nil {
        return nil, err
    }

    minutes := float64(endTime-startTime) / 60.0
    if minutes <= 0 {
        minutes = 1.0
    }

    result.TPM = int(float64(result.TotalTokens) / minutes)
    result.RPM = int(float64(result.RequestCount) / minutes)
    result.QuotaPM = int64(float64(result.TotalQuota) / minutes)

    totalSeconds := endTime - startTime
    if totalSeconds > 0 {
        result.DowntimePercentage = float64(result.DowntimeSeconds) * 100.0 / float64(totalSeconds)
    }

    return &result, nil
}
```

新增 API：

- `GET /api/system/stats/summary?period=7d`

参数：

- `period`：`1d/7d/30d` 等，复用 `calculateStartTime` 逻辑。

响应示例：

```json
{
  "success": true,
  "data": {
    "period": "7d",
    "tpm": 123456,
    "rpm": 2345,
    "quota_pm": 789012,
    "total_tokens": 987654321,
    "total_quota": 123456789,
    "avg_response_time_ms": 1100,
    "fail_rate": 0.8,
    "cache_hit_rate": 25.0,
    "stream_req_ratio": 40.0,
    "downtime_percentage": 0.5,
    "unique_users": 1234
  }
}
```

### 7.2 日均 Token 消耗曲线

基于 `channel_statistics` 以“自然日”为粒度聚合 `total_tokens`：

SQL 思路：

```sql
SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    SUM(total_tokens) AS tokens,
    SUM(total_quota)  AS quota
FROM channel_statistics
WHERE time_window_start BETWEEN :start AND :end
GROUP BY day
ORDER BY day ASC;
```

新增模型方法：

```go
type DailyTokenUsage struct {
    Day    string `json:"day"`    // YYYY-MM-DD
    Tokens int64  `json:"tokens"`
    Quota  int64  `json:"quota"`
}
```

新增 API：

- `GET /api/system/stats/daily_tokens?days=30`

参数：

- `days`（可选，默认 30，最大 90）：向前滚动的自然日数。

响应示例：

```json
{
  "success": true,
  "data": [
    { "day": "2025-12-01", "tokens": 1234567, "quota": 890123 },
    { "day": "2025-12-02", "tokens": 2345678, "quota": 901234 }
  ]
}
```

### 7.3 权限与用途

- 两个系统统计接口仅面向**管理员**：
  - 路由组：`apiRouter.Group("/system/stats").Use(middleware.AdminAuth())`。
  - WQuant 场景中，由后端携带管理员 Token 调用，再向前端导出可视化数据。

---

## 8. 日志与运维观测

为方便后续排查统计异常，所有新增接口建议增加精简日志：

- 在 Controller 层：
  - 记录查询维度（`metric/period/order/limit` 或 `period/days`）与耗时。
  - 当聚合 SQL 返回错误或耗时异常（>1s）时，输出 `LogWarn` 含 SQL 条件。
- 在 Model 聚合层：
  - 避免在热路径打印详细日志，只在错误时记录。

运维观测建议：

- 增加简单的 Prometheus 指标（如后续接入）：
  - `newapi_stats_query_duration_seconds{endpoint="system_summary"}`。
  - `newapi_stats_query_duration_seconds{endpoint="group_rankings"}`。

---

## 9. 实施任务清单（TODO）

> 以下为后续编码实现时的任务拆分，本设计文档本身不直接修改代码，仅提供实现参考。

1. **共享分组排名**
   - [ ] 在 `model` 层新增 P2P 分组排名聚合函数（基于 `group_statistics` + `groups` + `user_groups`）。
   - [ ] 在 `controller` 层新增 `GetPublicGroupRankings`，路由 `GET /api/groups/public/rankings`。
   - [ ] 为 WQuant 场景补充集成测试：验证不同指标与排序方向返回结果正确。

2. **计费分组统计增强**
   - [ ] 在 `GetSystemGroupsStats` 响应中补充 `total_tokens` / `total_quota` 字段（可选）。
   - [ ] 将 `GetSystemGroupsStats` 中的系统分组列表由硬编码 `["default", "vip", "svip"]` 改为动态枚举“全部计费分组”，
         例如复用 `ratio_setting.GetGroupRatioCopy()`，并在接口层明确其是“系统计费分组统计总览”。
   - [ ] 补充/调整文档 `01-P2P共享分组与用户创建渠道的状态信息监控统计与展示-Wquant对接NewAPI设计.md` 中“系统分组性能对比”部分使用的新字段。

3. **整系统统计信息**
   - [ ] 在 `model/channel_statistics.go` 中新增 `AggregateGlobalChannelStatsByTime` 函数。
   - [ ] 在 `model` 层新增日均 Token 曲线查询函数（`[]DailyTokenUsage`）。
   - [ ] 新增 Controller `system_stats.go`：
     - `GET /api/system/stats/summary`
     - `GET /api/system/stats/daily_tokens`
   - [ ] 补充集成测试：
     - 利用 scene_test 中已有的渠道统计写入工具构造数据，验证全局聚合结果。

4. **前端与对接方适配**
   - [ ] WQuant 后端：新增调用 NewAPI 的客户端封装，与现有 `newapi_client.py` 复用鉴权与重试逻辑。
   - [ ] NewAPI 自身 React 后台（如需要）：可选接入 `/api/system/stats/*` 以替换 Dashboard 内部的“本地聚合逻辑”。

以上方案在现有实现基础上只增加聚合层函数与接口，不引入新的存储表，有利于快速落地和回归验证。

---

## 10. 模型维度统计设计（系统级 / P2P 分组 / 计费分组）

本节在前述统计设计基础上，扩展**按模型（例如 `gemini-2.5-pro`, `gpt-5.1` 等）维度**的统计能力，覆盖三个层级：

- **系统级（全局）**：整个 NewAPI 实例在每个模型上的消耗与性能。
- **P2P 分组级**：某个 P2P 分组在其所有渠道上、按模型的消耗与性能。
- **计费分组级（系统分组级）**：某个系统分组（`default/vip/svip`）在其所有渠道上、按模型的消耗与性能。

目标指标与本次新增需求对齐：

- 额度消耗 / Tokens 消耗（7 天 / 30 天总额）。
- TPM、RPM（按时间窗口区间平均）。
- 每日 Token/Quota 消耗曲线（按自然日）。

### 10.1 数据来源与口径统一

#### 10.1.1 底层数据映射

- **按模型的 Token/Quota/TPM/RPM 数据源**
  - `channel_statistics`：
    - 维度：`(channel_id, model_name, time_window_start)`。
    - 指标：`total_tokens`, `total_quota`, `request_count`, `fail_count`, `total_latency_ms` 等。
  - `group_statistics`：
    - 维度：`(group_id, model_name, time_window_start)`。
    - 指标：`TotalTokens`, `TotalQuota`, `TPM`, `RPM`, `FailRate`, `AvgResponseTimeMs`, `DowntimePercentage` 等。

- **系统分组（计费分组）与渠道关系**
  - `users.group`：用户所属系统分组（`default/vip/svip`）。
  - `channels.owner_user_id`：渠道归属用户。
  - 通过 `channels.owner_user_id -> users.group` 关联渠道到计费分组。

#### 10.1.2 统一口径原则

- **7/30 天 Token / Quota 总额**：统一基于 `SUM(total_tokens)` / `SUM(total_quota)`。
- **TPM/RPM**：
  - 继续使用“总量 / 时间范围分钟数”的口径：
    - `TPM = SUM(total_tokens) / minutes`
    - `RPM = SUM(request_count) / minutes`
  - 其中 `minutes = (endTime - startTime) / 60`。
- **每天消耗曲线**：
  - 对应 “自然日” 粒度：
    - `day = DATE(FROM_UNIXTIME(time_window_start))`
    - 按 `day` 维度 `SUM(total_tokens)` / `SUM(total_quota)`。

### 10.2 系统级按模型统计

#### 10.2.1 汇总指标：按模型的 7/30 天 Token/Quota + TPM/RPM

新增模型结构：

```go
type GlobalModelStats struct {
    ModelName           string  `json:"model_name"`
    TotalTokens7d       int64   `json:"total_tokens_7d"`
    TotalQuota7d        int64   `json:"total_quota_7d"`
    TotalTokens30d      int64   `json:"total_tokens_30d"`
    TotalQuota30d       int64   `json:"total_quota_30d"`
    TPM7d               int     `json:"tpm_7d"`
    RPM7d               int     `json:"rpm_7d"`
    TPM30d              int     `json:"tpm_30d"`
    RPM30d              int     `json:"rpm_30d"`
    AvgResponseTimeMs7d float64 `json:"avg_response_time_ms_7d"`
    AvgResponseTimeMs30d float64 `json:"avg_response_time_ms_30d"`
    FailRate7d          float64 `json:"fail_rate_7d"`
    FailRate30d         float64 `json:"fail_rate_30d"`
}
```

SQL 思路（以 7 天窗口为例）：

```sql
SELECT
    model_name,
    SUM(total_tokens)                  AS total_tokens_7d,
    SUM(total_quota)                   AS total_quota_7d,
    SUM(request_count)                 AS request_count_7d,
    SUM(total_latency_ms)              AS total_latency_ms_7d,
    SUM(fail_count)                    AS fail_count_7d
FROM channel_statistics
WHERE time_window_start BETWEEN :start_7d AND :now
GROUP BY model_name;
```

在 Go 层计算：

- `minutes_7d = (now - start_7d) / 60`。
- `TPM7d = total_tokens_7d / minutes_7d`。
- `RPM7d = request_count_7d / minutes_7d`。
- `AvgResponseTimeMs7d = total_latency_ms_7d / request_count_7d`（避免在 SQL 中重复写 CASE）。
- `FailRate7d = fail_count_7d * 100 / request_count_7d`。

30 天口径完全类似，只是时间窗口不同，可以在一次 SQL 中通过多个 CASE/多次查询处理，具体实现由后续编码时权衡。

#### 10.2.2 系统级按模型每日曲线

结构：

```go
type GlobalModelDailyUsage struct {
    Day       string `json:"day"`        // YYYY-MM-DD
    ModelName string `json:"model_name"`
    Tokens    int64  `json:"tokens"`
    Quota     int64  `json:"quota"`
}
```

SQL 思路：

```sql
SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    model_name,
    SUM(total_tokens) AS tokens,
    SUM(total_quota)  AS quota
FROM channel_statistics
WHERE time_window_start BETWEEN :start AND :end
  AND (:model_name = '' OR model_name = :model_name)
GROUP BY day, model_name
ORDER BY day ASC, model_name ASC;
```

#### 10.2.3 新增系统级按模型统计 API

1. **按模型汇总视图（列表）**

   - `GET /api/system/stats/models?period=7d`

   参数：
   - `period`：`7d` 或 `30d`，默认 `7d`。
   - `model_name`（可选）：若指定，则只返回该模型的汇总；否则返回所有模型列表。

   响应（示例）：

   ```json
   {
     "success": true,
     "data": [
       {
         "model_name": "gpt-5.1",
         "total_tokens_7d": 123456789,
         "total_quota_7d": 987654321,
         "tpm_7d": 45678,
         "rpm_7d": 890,
         "avg_response_time_ms_7d": 980.0,
         "fail_rate_7d": 0.7
       }
     ]
   }
   ```

2. **按模型每日曲线**

   - `GET /api/system/stats/models/daily_tokens?days=30&model_name=gpt-5.1`

   参数：
   - `days`：默认 30。
   - `model_name`（可选）：若为空，则返回所有模型在每天的总 Token；若指定，则只返回该模型。

   响应数据结构参考 `GlobalModelDailyUsage`。

权限：

- 与 7 章系统级接口一致，使用 `AdminAuth()`。

### 10.3 P2P 分组级按模型统计

#### 10.3.1 汇总指标：某 P2P 分组 + 各模型

利用 `group_statistics` 表：

```sql
SELECT
    model_name,
    SUM(total_tokens)            AS total_tokens,
    SUM(total_quota)             AS total_quota,
    AVG(tpm)                     AS tpm,
    AVG(rpm)                     AS rpm,
    AVG(avg_response_time_ms)    AS avg_response_time_ms,
    AVG(fail_rate)               AS fail_rate,
    AVG(downtime_percentage)     AS downtime_percentage
FROM group_statistics
WHERE group_id = :group_id
  AND time_window_start BETWEEN :start AND :end
GROUP BY model_name;
```

结构：

```go
type GroupModelStats struct {
    GroupId            int     `json:"group_id"`
    ModelName          string  `json:"model_name"`
    TotalTokens        int64   `json:"total_tokens"`
    TotalQuota         int64   `json:"total_quota"`
    TPM                float64 `json:"tpm"`
    RPM                float64 `json:"rpm"`
    AvgResponseTimeMs  float64 `json:"avg_response_time_ms"`
    FailRate           float64 `json:"fail_rate"`
    DowntimePercentage float64 `json:"downtime_percentage"`
}
```

这里 `tpm/rpm` 为窗口级平均，通常已经够用；如果需要与系统口径完全一致，也可以在 Go 层改为：

- 先只求 `SUM(total_tokens)`, `SUM(total_quota)`，再除以 `minutes` 计算 TPM/RPM。

#### 10.3.2 P2P 分组按模型每日曲线

同样基于 `group_statistics`：

```sql
SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    model_name,
    SUM(total_tokens) AS tokens,
    SUM(total_quota)  AS quota
FROM group_statistics
WHERE group_id = :group_id
  AND time_window_start BETWEEN :start AND :end
  AND (:model_name = '' OR model_name = :model_name)
GROUP BY day, model_name
ORDER BY day ASC, model_name ASC;
```

结构可复用 `GlobalModelDailyUsage`，增加 `GroupId` 字段：

```go
type GroupModelDailyUsage struct {
    GroupId   int    `json:"group_id"`
    Day       string `json:"day"`
    ModelName string `json:"model_name"`
    Tokens    int64  `json:"tokens"`
    Quota     int64  `json:"quota"`
}
```

#### 10.3.3 新增 P2P 分组级按模型统计 API

在现有 P2P 分组统计接口基础上扩展两个新接口（权限沿用 5 章的“分组成员或管理员”规则）：

1. **分组按模型汇总视图**

   - `GET /api/p2p_groups/:id/stats/models?period=7d`

   参数：
   - `period`：`1d/7d/30d`，复用 `calculateStartTime`。

   响应：

   ```json
   {
     "success": true,
     "data": [
       {
         "group_id": 101,
         "model_name": "gpt-5.1",
         "total_tokens": 123456,
         "total_quota": 7890,
         "tpm": 4567.8,
         "rpm": 89.0,
         "avg_response_time_ms": 1000.0,
         "fail_rate": 0.5,
         "downtime_percentage": 0.3
       }
     ]
   }
   ```

2. **分组按模型每日曲线**

   - `GET /api/p2p_groups/:id/stats/model/:model_name/daily_tokens?days=30`

   或统一使用 query 参数：

   - `GET /api/p2p_groups/:id/stats/models/daily_tokens?days=30&model_name=gpt-5.1`

   响应结构参考 `GroupModelDailyUsage`。

### 10.4 计费分组（系统分组）按模型统计

#### 10.4.1 汇总指标：系统分组 + 各模型

对某个系统分组 `user_group`（例如 `default`），需要在 `channel_statistics` 中筛选：

1. 找出所有属于该系统分组的渠道：

   ```sql
   SELECT c.id
   FROM channels c
   LEFT JOIN users u ON c.owner_user_id = u.id
   WHERE u.group = :user_group;
   ```

2. 在 `channel_statistics` 中按 `model_name` 聚合这些渠道：

   ```sql
   SELECT
       model_name,
       SUM(total_tokens)     AS total_tokens,
       SUM(total_quota)      AS total_quota,
       SUM(request_count)    AS request_count,
       SUM(total_latency_ms) AS total_latency_ms,
       SUM(fail_count)       AS fail_count
   FROM channel_statistics
   WHERE channel_id IN (:channel_ids)
     AND time_window_start BETWEEN :start AND :end
   GROUP BY model_name;
   ```

3. 在 Go 层计算 TPM/RPM/FailRate/AvgLatency，与系统级口径保持一致。

结构：

```go
type BillingGroupModelStats struct {
    UserGroup          string  `json:"user_group"` // default/vip/svip
    ModelName          string  `json:"model_name"`
    TotalTokens        int64   `json:"total_tokens"`
    TotalQuota         int64   `json:"total_quota"`
    TPM                int     `json:"tpm"`
    RPM                int     `json:"rpm"`
    AvgResponseTimeMs  float64 `json:"avg_response_time_ms"`
    FailRate           float64 `json:"fail_rate"`
    DowntimePercentage float64 `json:"downtime_percentage,omitempty"` // 如需要可扩展
}
```

#### 10.4.2 系统分组按模型每日曲线

同样基于 `channel_statistics`：

```sql
SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    model_name,
    SUM(total_tokens) AS tokens,
    SUM(total_quota)  AS quota
FROM channel_statistics
WHERE channel_id IN (:channel_ids)
  AND time_window_start BETWEEN :start AND :end
  AND (:model_name = '' OR model_name = :model_name)
GROUP BY day, model_name
ORDER BY day ASC, model_name ASC;
```

结构类似 `GlobalModelDailyUsage`，增加 `UserGroup` 字段：

```go
type BillingGroupModelDailyUsage struct {
    UserGroup string `json:"user_group"`
    Day       string `json:"day"`
    ModelName string `json:"model_name"`
    Tokens    int64  `json:"tokens"`
    Quota     int64  `json:"quota"`
}
```

#### 10.4.3 新增计费分组按模型统计 API

基于现有系统分组统计路由扩展：

1. **系统分组按模型汇总视图**

   - `GET /api/groups/system/model_stats?period=7d&group=default`

   参数：
   - `group`：系统分组名，默认 `default`，支持 `vip/svip`。
   - `period`：`1d/7d/30d`。

   响应：

   ```json
   {
     "success": true,
     "data": [
       {
         "user_group": "default",
         "model_name": "gpt-5.1",
         "total_tokens": 123456,
         "total_quota": 7890,
         "tpm": 4567,
         "rpm": 89,
         "avg_response_time_ms": 1000.0,
         "fail_rate": 0.6
       }
     ]
   }
   ```

2. **系统分组按模型每日曲线**

   - `GET /api/groups/system/model_daily_tokens?group=default&days=30&model_name=gpt-5.1`

   响应参考 `BillingGroupModelDailyUsage`。

权限：

- 与系统分组统计相同，仅管理员可见（`AdminAuth()`）。

### 10.5 额外 TODO（在 9. 实施任务清单基础上的补充）

在第 9 节的 TODO 基础上，为模型维度统计补充以下任务项（编码时可一并纳入）：

5. **系统级按模型统计**
   - [ ] 在 `model/channel_statistics.go` 中新增：
     - `AggregateGlobalModelStats(startTime, endTime)`。
     - `GetGlobalModelDailyUsage(startTime, endTime, modelName)`。
   - [ ] 在 `controller/system_stats.go` 中新增：
     - `GET /api/system/stats/models`
     - `GET /api/system/stats/models/daily_tokens`

6. **P2P 分组级按模型统计**
   - [ ] 在 `model/group_statistics.go` 中新增：
     - `AggregateGroupModelStats(groupId, startTime, endTime)`。
     - `GetGroupModelDailyUsage(groupId, startTime, endTime, modelName)`。
   - [ ] 在 `controller/group_stats.go` 或新文件中新增：
     - `GET /api/p2p_groups/:id/stats/models`
     - `GET /api/p2p_groups/:id/stats/models/daily_tokens`

7. **计费分组（系统分组）按模型统计**
   - [ ] 在 `model/channel_statistics.go` 中新增：
     - `AggregateBillingGroupModelStats(userGroup, startTime, endTime)`。
     - `GetBillingGroupModelDailyUsage(userGroup, startTime, endTime, modelName)`。
   - [ ] 在 `controller/system_group_stats.go` 或新文件中新增：
     - `GET /api/groups/system/model_stats`
     - `GET /api/groups/system/model_daily_tokens`

上述设计全部基于现有 `channel_statistics` / `group_statistics` 表，无需新增存储结构，只在查询与聚合层扩展模型维度视图，即可满足“系统级 / P2P 分组 / 计费分组级按模型统计（7/30 天 Token/额度总额、TPM/RPM、每日消耗曲线）”的需求。

---

## 11. 统计接口与指标映射一览

本节将前文出现的**每一个统计视图**明确落到：

- API 接口与参数列表
- 使用到的表
- 关键过滤条件 / 聚合逻辑伪代码

方便后续实现与排查。

> 说明：伪代码中使用 `:param` 表示绑定变量，实际实现时通过 GORM 构建等价查询。

### 11.1 系统级统计（全局）

#### 11.1.1 系统汇总视图：`GET /api/system/stats/summary`

- **用途**
  - 返回整个系统在指定 period 内的汇总指标：
    - `total_tokens`, `total_quota`, `tpm`, `rpm`, `quota_pm`,
      `avg_response_time_ms`, `fail_rate`, `cache_hit_rate`,
      `stream_req_ratio`, `downtime_percentage`, `unique_users`。
- **权限**
  - 管理员；路由组：`/api/system/stats` + `AdminAuth()`。
- **参数**
  - `period`（query，可选）：`1d/7d/30d` 等，默认 `7d`。
- **使用表**
  - `channel_statistics`
- **过滤与聚合伪代码**

```go
// 1. 解析 period -> startTime, endTime
startTime, endTime := calculateStartTime(period)

// 2. 聚合所有渠道全部模型
SELECT
    SUM(request_count)      AS request_count,
    SUM(fail_count)         AS fail_count,
    SUM(total_tokens)       AS total_tokens,
    SUM(total_quota)        AS total_quota,
    SUM(total_latency_ms)   AS total_latency_ms,
    SUM(stream_req_count)   AS stream_req_count,
    SUM(cache_hit_count)    AS cache_hit_count,
    SUM(downtime_seconds)   AS downtime_seconds,
    SUM(unique_users)       AS unique_users
FROM channel_statistics
WHERE time_window_start BETWEEN :startTime AND :endTime;

// 3. 在 Go 中计算:
minutes := (endTime - startTime) / 60
tpm     := total_tokens / minutes
rpm     := request_count / minutes
quotaPm := total_quota / minutes
avgLatencyMs := total_latency_ms / request_count
failRate     := fail_count * 100.0 / request_count
cacheHitRate := cache_hit_count * 100.0 / request_count
streamRatio  := stream_req_count * 100.0 / request_count
downtimePct  := downtime_seconds * 100.0 / (endTime - startTime)
```

#### 11.1.2 系统每日 Token 曲线：`GET /api/system/stats/daily_tokens`

- **用途**
  - 返回全局按自然日聚合的 Token 与额度消耗曲线。
- **权限**
  - 管理员。
- **参数**
  - `days`（query，可选）：向前多少天，默认 `30`，最大 `90`。
- **使用表**
  - `channel_statistics`
- **过滤与聚合伪代码**

```go
endTime   := now
startTime := endTime - days*24h

SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    SUM(total_tokens)                      AS tokens,
    SUM(total_quota)                       AS quota
FROM channel_statistics
WHERE time_window_start BETWEEN :startTime AND :endTime
GROUP BY day
ORDER BY day ASC;
```

#### 11.1.3 系统按模型汇总：`GET /api/system/stats/models`

- **用途**
  - 返回全局**按模型**聚合的 7/30 天 Token/Quota、TPM/RPM、平均延迟、失败率等指标。
- **权限**
  - 管理员。
- **参数**
  - `period`（query，可选）：`7d` 或 `30d`，默认 `7d`。
  - `model_name`（query，可选）：指定则只返回该模型，否则返回所有模型。
- **使用表**
  - `channel_statistics`
- **过滤与聚合伪代码**

```go
startTime, endTime := calculateStartTime(period)

SELECT
    model_name,
    SUM(total_tokens)       AS total_tokens,
    SUM(total_quota)        AS total_quota,
    SUM(request_count)      AS request_count,
    SUM(fail_count)         AS fail_count,
    SUM(total_latency_ms)   AS total_latency_ms
FROM channel_statistics
WHERE time_window_start BETWEEN :startTime AND :endTime
  AND (:modelName = '' OR model_name = :modelName)
GROUP BY model_name;

// Go 层按 11.1.1 的口径，按模型计算 tpm/rpm/avg_latency/fail_rate
```

#### 11.1.4 系统按模型每日曲线：`GET /api/system/stats/models/daily_tokens`

- **用途**
  - 返回全局按“日 + 模型”聚合的 Token/Quota 曲线。
- **权限**
  - 管理员。
- **参数**
  - `days`（query，可选）：默认 `30`。
  - `model_name`（query，可选）：若指定只返回该模型。
- **使用表**
  - `channel_statistics`
- **过滤与聚合伪代码**

```go
endTime   := now
startTime := endTime - days*24h

SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    model_name,
    SUM(total_tokens)                      AS tokens,
    SUM(total_quota)                       AS quota
FROM channel_statistics
WHERE time_window_start BETWEEN :startTime AND :endTime
  AND (:modelName = '' OR model_name = :modelName)
GROUP BY day, model_name
ORDER BY day ASC, model_name ASC;
```

### 11.2 系统分组（计费分组）统计

#### 11.2.1 系统分组整体视图：`GET /api/groups/system/stats`

- **用途**
  - 对 `default/vip/svip` 系统分组，返回每个分组的整体汇总指标（非按模型拆分）。
- **权限**
  - 任何已登录用户（用于 Dashboard 展示）。
- **参数**
  - `period`（query，可选）：`1h/6h/24h/7d/30d`，默认 `1h`。
- **使用表**
  - `users`（确定用户所属系统分组）
  - `channels`（确定渠道 owner）
  - `channel_statistics`（实际统计数据）
- **过滤与聚合伪代码**

```go
startTime, endTime := calculateStartTime(period)
systemGroups := []string{"default", "vip", "svip"}

// 对每个 groupName:
SELECT c.id
FROM channels c
LEFT JOIN users u ON c.owner_user_id = u.id
WHERE u.`group` = :groupName;

// 得到 channel_ids[] 后，按 11.1.1 的方式在 channel_statistics 中聚合:
SELECT
    SUM(request_count)    AS request_count,
    SUM(fail_count)       AS fail_count,
    SUM(total_tokens)     AS total_tokens,
    SUM(total_quota)      AS total_quota,
    SUM(total_latency_ms) AS total_latency_ms,
    SUM(stream_req_count) AS stream_req_count,
    SUM(cache_hit_count)  AS cache_hit_count,
    SUM(downtime_seconds) AS downtime_seconds
FROM channel_statistics
WHERE channel_id IN (:channel_ids)
  AND time_window_start BETWEEN :startTime AND :endTime;

// Go 层计算 tpm/rpm/等同 11.1.1
```

#### 11.2.2 系统分组按模型汇总：`GET /api/groups/system/model_stats`

- **用途**
  - 对一个系统分组（默认 `default`），按模型维度返回 Token/Quota/TPM/RPM 等汇总。
- **权限**
  - 管理员。
- **参数**
  - `group`（query，可选）：系统分组名，默认 `default`。
  - `period`（query，可选）：`1d/7d/30d`。
- **使用表**
  - `users`
  - `channels`
  - `channel_statistics`
- **过滤与聚合伪代码**

```go
startTime, endTime := calculateStartTime(period)

// 1. 找到该系统分组的所有渠道
SELECT c.id
FROM channels c
LEFT JOIN users u ON c.owner_user_id = u.id
WHERE u.`group` = :userGroup;

// 2. 在 channel_statistics 中按 model 聚合
SELECT
    model_name,
    SUM(total_tokens)       AS total_tokens,
    SUM(total_quota)        AS total_quota,
    SUM(request_count)      AS request_count,
    SUM(fail_count)         AS fail_count,
    SUM(total_latency_ms)   AS total_latency_ms
FROM channel_statistics
WHERE channel_id IN (:channel_ids)
  AND time_window_start BETWEEN :startTime AND :endTime
GROUP BY model_name;

// Go 层按 11.1.3 口径计算 tpm/rpm/等
```

#### 11.2.3 系统分组按模型每日曲线：`GET /api/groups/system/model_daily_tokens`

- **用途**
  - 对某系统分组 + 模型，返回按日聚合的 Token/Quota 曲线。
- **权限**
  - 管理员。
- **参数**
  - `group`（query，必填）：系统分组名。
  - `days`（query，可选）：默认 `30`。
  - `model_name`（query，可选）：缺省则返回该分组所有模型的曲线。
- **使用表**
  - `users`
  - `channels`
  - `channel_statistics`
- **过滤与聚合伪代码**

```go
endTime   := now
startTime := endTime - days*24h

// 1. 同 11.2.2: 查出该 group 对应的 channel_ids[]

// 2. 按日+模型聚合
SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    model_name,
    SUM(total_tokens)                      AS tokens,
    SUM(total_quota)                       AS quota
FROM channel_statistics
WHERE channel_id IN (:channel_ids)
  AND time_window_start BETWEEN :startTime AND :endTime
  AND (:modelName = '' OR model_name = :modelName)
GROUP BY day, model_name
ORDER BY day ASC, model_name ASC;
```

### 11.3 P2P 分组统计

#### 11.3.1 P2P 分组整体视图：`GET /api/p2p_groups/:id/stats`

- **用途**
  - 对单个 P2P 分组，提供：
    - 若指定 `model`：返回该模型最新一个时间窗口的聚合快照。
    - 若未指定 `model`：按 `period` 聚合所有模型的总体视图。
- **权限**
  - 分组成员或管理员。
- **参数**
  - `id`（path，必填）：分组 ID。
  - `model`（query，可选）：模型名。
  - `period`（query，可选）：默认 `24h`。
- **使用表**
  - `groups`（校验是否存在）
  - `user_groups`（权限校验）
  - `group_statistics`
- **过滤与聚合伪代码**

```go
// 校验 group 存在且用户有权限（略）

if modelName != "" {
    // 指定模型：取最新一条
    SELECT *
    FROM group_statistics
    WHERE group_id = :groupId
      AND model_name = :modelName
    ORDER BY time_window_start DESC, updated_at DESC
    LIMIT 1;
} else {
    // 不指定模型：按时间范围聚合所有模型
    startTime, endTime := calculateStartTime(period)

    SELECT
        group_id,
        /* model_name留空 */,
        AVG(tpm)                 AS tpm,
        AVG(rpm)                 AS rpm,
        AVG(fail_rate)           AS fail_rate,
        AVG(avg_response_time_ms) AS avg_response_time_ms,
        AVG(avg_cache_hit_rate)   AS avg_cache_hit_rate,
        AVG(stream_req_ratio)     AS stream_req_ratio,
        AVG(quota_pm)             AS quota_pm,
        SUM(total_tokens)         AS total_tokens,
        SUM(total_quota)          AS total_quota,
        AVG(avg_concurrency)      AS avg_concurrency,
        SUM(total_sessions)       AS total_sessions,
        AVG(downtime_percentage)  AS downtime_percentage,
        MAX(unique_users)         AS unique_users
    FROM group_statistics
    WHERE group_id = :groupId
      AND time_window_start BETWEEN :startTime AND :endTime
    GROUP BY group_id;
}
```

#### 11.3.2 P2P 分组历史序列：`GET /api/p2p_groups/:id/stats/history`

- **用途**
  - 返回该分组在一段时间内的历史窗口序列（主要用作前端折线图）。
- **权限**
  - 分组成员或管理员。
- **参数**
  - `id`（path，必填）：分组 ID。
  - `model`（query，可选）：模型名。
  - `start_time` / `end_time`（query，可选）：Unix 时间戳，若缺省则由前端控制。
- **使用表**
  - `group_statistics`
- **过滤伪代码**

```go
SELECT *
FROM group_statistics
WHERE group_id = :groupId
  AND (:modelName = '' OR model_name = :modelName)
  AND (:startTime = 0 OR updated_at >= :startTime)
  AND (:endTime   = 0 OR updated_at <= :endTime)
ORDER BY updated_at DESC;
```

#### 11.3.3 P2P 分组按模型汇总：`GET /api/p2p_groups/:id/stats/models`

- **用途**
  - 对某分组，在指定 period 内按模型维度返回 Token/Quota/TPM/RPM/延迟/失败率等。
- **权限**
  - 分组成员或管理员。
- **参数**
  - `id`（path，必填）：分组 ID。
  - `period`（query，可选）：`1d/7d/30d`，默认 `7d`。
- **使用表**
  - `group_statistics`
- **过滤与聚合伪代码**

```go
startTime, endTime := calculateStartTime(period)

SELECT
    model_name,
    SUM(total_tokens)        AS total_tokens,
    SUM(total_quota)         AS total_quota,
    /* tpm/rpm 可用 SUM(total_tokens/requests)/minutes 算，也可直接 AVG(tpm/rpm) */
    SUM(total_tokens)        AS tokens_sum,
    SUM(total_quota)         AS quota_sum,
    AVG(tpm)                 AS tpm_avg,
    AVG(rpm)                 AS rpm_avg,
    AVG(avg_response_time_ms) AS avg_response_time_ms,
    AVG(fail_rate)           AS fail_rate,
    AVG(downtime_percentage) AS downtime_percentage
FROM group_statistics
WHERE group_id = :groupId
  AND time_window_start BETWEEN :startTime AND :endTime
GROUP BY model_name;
```

#### 11.3.4 P2P 分组按模型每日曲线：`GET /api/p2p_groups/:id/stats/models/daily_tokens`

- **用途**
  - 返回某分组在最近若干天内，按模型/自然日聚合的 Token/Quota 曲线。
- **权限**
  - 分组成员或管理员。
- **参数**
  - `id`（path，必填）：分组 ID。
  - `days`（query，可选）：默认 `30`。
  - `model_name`（query，可选）。
- **使用表**
  - `group_statistics`
- **过滤与聚合伪代码**

```go
endTime   := now
startTime := endTime - days*24h

SELECT
    DATE(FROM_UNIXTIME(time_window_start)) AS day,
    model_name,
    SUM(total_tokens)                      AS tokens,
    SUM(total_quota)                       AS quota
FROM group_statistics
WHERE group_id = :groupId
  AND time_window_start BETWEEN :startTime AND :endTime
  AND (:modelName = '' OR model_name = :modelName)
GROUP BY day, model_name
ORDER BY day ASC, model_name ASC;
```

### 11.4 公开分组排名（Shared Group Rankings）

#### 11.4.1 公开分组排行榜：`GET /api/groups/public/rankings`

- **用途**
  - 在“分组广场”展示公开 P2P 分组的排行榜：
    - 支持按 `tokens_7d`, `tokens_30d`, `tpm`, `rpm`, `latency`, `fail_rate`, `downtime` 排序。
    - 主要统计粒度是“分组整体”而不是“按模型拆开”。
- **权限**
  - 任意已登录用户（浏览分组广场）。
- **参数**
  - `metric`（query，必填）：`tokens_7d` / `tokens_30d` / `tpm` / `rpm` / `latency` / `fail_rate` / `downtime`。
  - `period`（query，可选）：`1h/6h/24h/7d/30d`，默认 `7d`（仅对 `tpm/rpm/latency/fail_rate/downtime` 有效）。
  - `order`（query，可选）：`asc` / `desc`，默认：
    - 容量类（`tokens_*`, `tpm`, `rpm`）为 `desc`；
    - 稳定性类（`latency`, `fail_rate`, `downtime`）为 `asc`。
  - `limit`（query，可选）：默认 `20`，最大 `100`。
  - `offset`（query，可选）：用于分页。
- **使用表**
  - `groups`（公开共享分组元数据）
  - `user_groups`（成员数统计）
  - `group_statistics`（分组聚合指标）
  - 可选：`channels`（估算 channel_count）
- **过滤与聚合伪代码**

```go
// 1. 取公开共享分组基础列表
SELECT id, name, display_name
FROM groups
WHERE type = :GroupTypeShared      -- 共享分组
  AND join_method != :JoinMethodInvite; -- 非纯邀请私有

// 2. 可选: 成员数
SELECT group_id, COUNT(*) AS member_count
FROM user_groups
WHERE status = :MemberStatusActive
  AND group_id IN (:group_ids)
GROUP BY group_id;

// 3. 从 group_statistics 聚合 tokens/tpm/rpm/等
start7d  := now - 7d
start30d := now - 30d

SELECT
    group_id,
    SUM(CASE WHEN time_window_start BETWEEN :start7d AND :now THEN total_tokens ELSE 0 END) AS tokens_7d,
    SUM(CASE WHEN time_window_start BETWEEN :start30d AND :now THEN total_tokens ELSE 0 END) AS tokens_30d,
    AVG(tpm)                 AS tpm,
    AVG(rpm)                 AS rpm,
    AVG(avg_response_time_ms) AS avg_response_time_ms,
    AVG(fail_rate)           AS fail_rate,
    AVG(downtime_percentage) AS downtime_percentage
FROM group_statistics
WHERE group_id IN (:group_ids)
  AND time_window_start BETWEEN :startForPeriod AND :endForPeriod
GROUP BY group_id;

// 4. 在 Go 中根据 metric/order 做排序与分页
//   - metric == "tokens_7d" -> 按 tokens_7d 排序
//   - metric == "latency"   -> 按 avg_response_time_ms 排序
```

> 注：`channel_count` 可通过复用 `getGroupChannelIds(groupId)` 的逻辑，从 `channels.allowed_groups` 字段解析分组关联渠道，设计上不影响排名主逻辑，故略去详细 SQL。

### 11.5 个人计费分组统计（Per-user Billing Group Stats）

本小节对应 6.3 中的设计，给出“以某个用户为基础”的计费分组统计接口与实现思路。

#### 11.5.1 当前用户计费分组汇总：`GET /api/billing_groups/self/stats`

- **用途**
  - 返回“当前登录用户”在指定 period 内，按计费分组（BillingGroup）聚合的统计信息：
    - 每个 BillingGroup 的 `total_tokens/total_quota/tpm/rpm`。
  - 前端可用其构建个人计费分组消耗柱状图或表格。
- **权限**
  - 任何已登录用户（`UserAuth()`），仅统计自己的数据。
- **参数**
  - `period`（query，可选）：`7d/30d` 等，复用 `calculateStartTime` 逻辑，默认 `7d`。
  - 将来如有需要，可扩展 `user_id` 参数在管理员视角下按指定用户查询。
- **使用表**
  - `logs`（`LOG_DB`）：消费日志表 `Log`。
- **过滤与聚合伪代码**

```go
// userId = 当前登录用户 ID
startTime, endTime := calculateStartTime(period)

SELECT
    `group`                                   AS billing_group,
    SUM(prompt_tokens + completion_tokens)    AS total_tokens,
    SUM(quota)                                AS total_quota,
    COUNT(*)                                  AS request_count
FROM logs
WHERE user_id   = :userId
  AND type      = :LogTypeConsume         -- 仅统计消费日志
  AND created_at BETWEEN :startTime AND :endTime
GROUP BY `group`;

// Go 层计算:
minutes := (endTime - startTime) / 60
tpm     := total_tokens   / minutes
rpm     := request_count  / minutes
```

> 说明：`logs.group` 字段在消费日志中记录的是本次请求使用的计费分组（`relayInfo.UsingGroup`），
> 已经考虑了 Token.billing_group 覆盖等逻辑，因此按该字段聚合可以准确反映「此用户在各计费分组下的实际计费消耗」。

#### 11.5.2 当前用户计费分组每日曲线：`GET /api/billing_groups/self/daily_tokens`

- **用途**
  - 返回当前用户在最近 N 天内，按“计费分组 + 自然日”聚合的 Token/Quota 曲线。
  - 适合绘制“按计费分组分色的日消耗堆叠图”等。
- **权限**
  - 任何已登录用户。
- **参数**
  - `days`（query，可选）：默认 `30`，最大值可配置。
  - `billing_group`（query，可选）：若指定，则仅返回该计费分组的曲线；若为空，则返回该用户所有计费分组。
- **使用表**
  - `logs`（`LOG_DB`）。
- **过滤与聚合伪代码**

```go
userId   := currentUserId
endTime  := now
startTime := endTime - days*24h

SELECT
    DATE(FROM_UNIXTIME(created_at))          AS day,
    `group`                                  AS billing_group,
    SUM(prompt_tokens + completion_tokens)   AS tokens,
    SUM(quota)                               AS quota
FROM logs
WHERE user_id   = :userId
  AND type      = :LogTypeConsume
  AND created_at BETWEEN :startTime AND :endTime
  AND (:billingGroup = '' OR `group` = :billingGroup)
GROUP BY day, `group`
ORDER BY day ASC, `group` ASC;
```

#### 11.5.3 管理员视角按用户查询（可选扩展）

在上述自助接口基础上，若需要管理员查看某个指定用户的计费分组统计，可设计管理员接口：

- `GET /api/admin/billing_groups/user/:id/stats?period=7d`
- `GET /api/admin/billing_groups/user/:id/daily_tokens?days=30&billing_group=vip`

其实现与 `self` 版本完全相同，只是将 `user_id` 从 path 参数读取，并使用 `AdminAuth()` 进行权限控制。

#### 11.5.4 系统计费分组汇总：`GET /api/billing_groups/system/stats`

- **用途**
  - 从“整系统视角”统计所有用户在各计费分组下的总体消耗情况，用于让普通用户感知系统整体运行状况（例如各计费分组的整体 Token/额度占比、相对繁忙程度等）。
  - 返回结构与 `/api/billing_groups/self/stats` 完全一致，只是统计范围从「当前用户」扩展为「全体用户」。
- **权限**
  - 任何已登录用户（`UserAuth()`），不要求管理员权限。
- **参数**
  - `period`（query，可选）：同 `/api/billing_groups/self/stats`，默认 `7d`，支持 `1d/7d/30d`，复用 `calculateStartTime` 逻辑。
- **返回格式**
  - `data` 字段为 `[]UserBillingGroupStats` 列表，字段定义与 `/api/billing_groups/self/stats` 完全一致：
    - `billing_group`：计费分组名。
    - `total_tokens`：该分组在指定周期内的总 Token 消耗。
    - `total_quota`：该分组在指定周期内的总额度消耗。
    - `request_count`：该分组在指定周期内的总请求次数。
    - `tpm` / `rpm`：在指定周期内按分钟计算的平均 TPM / RPM。
- **使用表**
  - `logs`（`LOG_DB`）：消费日志表 `Log`，不再按 `user_id` 过滤，聚合范围为全体用户。
- **过滤与聚合伪代码**

```go
startTime, endTime := calculateStartTime(period)

// 与 AggregateUserBillingGroupStats 的唯一区别：不按 user_id 过滤，直接做全局聚合
SELECT
    `group`                                AS billing_group,
    SUM(prompt_tokens + completion_tokens) AS total_tokens,
    SUM(quota)                             AS total_quota,
    COUNT(*)                               AS request_count
FROM logs
WHERE type      = :LogTypeConsume         -- 仅统计消费日志
  AND created_at BETWEEN :startTime AND :endTime
GROUP BY `group`;

// Go 层：沿用 UserBillingGroupStats 结构，保持与 /api/billing_groups/self/stats 完全一致
timeRangeMinutes := float64(endTime-startTime) / 60.0
if timeRangeMinutes <= 0 {
    timeRangeMinutes = 1.0
}

for each row in rawResults {
    stat := UserBillingGroupStats{
        BillingGroup: row.BillingGroup,
        TotalTokens:  row.TotalTokens,
        TotalQuota:   row.TotalQuota,
        RequestCount: row.RequestCount,
        TPM:          int(float64(row.TotalTokens) / timeRangeMinutes),
        RPM:          int(float64(row.RequestCount) / timeRangeMinutes),
    }
    results = append(results, stat)
}
```

> 模型层可新增 `AggregateSystemBillingGroupStats(startTime, endTime)`，内部实现与 `AggregateUserBillingGroupStats` 相同，仅去掉 `user_id` 过滤条件；Controller 层新增 `/api/billing_groups/system/stats` 路由，调用该函数并直接返回 `[]UserBillingGroupStats`，从而保证前端在消费系统级与个人级计费分组统计时可以复用同一套渲染逻辑。
