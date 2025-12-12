package service

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

//go:embed check_and_consume_sliding_window.lua
var luaCheckAndConsumeWindow string

// scriptSHA 存储Lua脚本的SHA值，在应用启动时预加载
var scriptSHA string

// SlidingWindowConfig 滑动窗口配置
type SlidingWindowConfig struct {
	Period   string `json:"period"`   // 窗口类型: rpm, hourly, 4hourly, daily, weekly
	Duration int64  `json:"duration"` // 窗口时长（秒）
	Limit    int64  `json:"limit"`    // 限额（RPM为请求数，其他为quota）
	TTL      int64  `json:"ttl"`      // Redis Key过期时间（秒）
}

// WindowResult 窗口检查结果
type WindowResult struct {
	Success   bool  `json:"success"`    // 是否成功扣减
	Consumed  int64 `json:"consumed"`   // 扣减后的累计消耗
	StartTime int64 `json:"start_time"` // 窗口开始时间
	EndTime   int64 `json:"end_time"`   // 窗口结束时间
	TimeLeft  int64 `json:"time_left"`  // 剩余时间（秒）
}

// WindowStatus 窗口状态（用于查询）
type WindowStatus struct {
	Period    string `json:"period"`     // 窗口类型
	IsActive  bool   `json:"is_active"`  // 是否活跃
	Consumed  int64  `json:"consumed"`   // 已消耗
	Limit     int64  `json:"limit"`      // 限额
	Remaining int64  `json:"remaining"`  // 剩余额度
	StartTime int64  `json:"start_time"` // 窗口开始时间
	EndTime   int64  `json:"end_time"`   // 窗口结束时间
	TimeLeft  int64  `json:"time_left"`  // 剩余时间（秒）
}

// GetSlidingWindowConfigs 根据套餐配置生成所有滑动窗口配置
// 只为非零限额的窗口生成配置
func GetSlidingWindowConfigs(pkg *model.Package) []SlidingWindowConfig {
	configs := make([]SlidingWindowConfig, 0, 5)

	// RPM限制（单位：请求数）
	if pkg.RpmLimit > 0 {
		configs = append(configs, SlidingWindowConfig{
			Period:   "rpm",
			Duration: 60,                  // 1分钟
			Limit:    int64(pkg.RpmLimit), // 请求数
			TTL:      90,                  // 1.5分钟后过期
		})
	}

	// 小时限额（单位：quota）
	if pkg.HourlyLimit > 0 {
		configs = append(configs, SlidingWindowConfig{
			Period:   "hourly",
			Duration: 3600,            // 1小时
			Limit:    pkg.HourlyLimit, // quota
			TTL:      4200,            // 70分钟后过期
		})
	}

	// 4小时限额
	if pkg.FourHourlyLimit > 0 {
		configs = append(configs, SlidingWindowConfig{
			Period:   "4hourly",
			Duration: 14400,               // 4小时
			Limit:    pkg.FourHourlyLimit, // quota
			TTL:      18000,               // 5小时后过期
		})
	}

	// 每日限额
	if pkg.DailyLimit > 0 {
		configs = append(configs, SlidingWindowConfig{
			Period:   "daily",
			Duration: 86400,          // 24小时
			Limit:    pkg.DailyLimit, // quota
			TTL:      93600,          // 26小时后过期
		})
	}

	// 每周限额
	if pkg.WeeklyLimit > 0 {
		configs = append(configs, SlidingWindowConfig{
			Period:   "weekly",
			Duration: 604800,          // 7天
			Limit:    pkg.WeeklyLimit, // quota
			TTL:      691200,          // 8天后过期
		})
	}

	return configs
}

// init 在应用启动时预加载Lua脚本
func init() {
	// 注册启动钩子，在Redis连接建立后加载脚本
	// 这里使用延迟初始化，在第一次调用时加载
}

// ensureScriptLoaded 确保Lua脚本已加载，如果未加载则立即加载
func ensureScriptLoaded(ctx context.Context) error {
	if scriptSHA != "" {
		return nil // 已加载
	}

	if !common.RedisEnabled {
		common.SysLog("Redis is not enabled, sliding window will be disabled")
		return fmt.Errorf("redis not enabled")
	}

	sha, err := common.RDB.ScriptLoad(ctx, luaCheckAndConsumeWindow).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("Failed to load sliding window Lua script: %v", err))
		return err
	}

	scriptSHA = sha
	common.SysLog(fmt.Sprintf("Package sliding window Lua script loaded, SHA: %s", sha))
	return nil
}

// CheckAndConsumeSlidingWindow 检查并消耗单个滑动窗口
// subscriptionId: 订阅ID
// config: 窗口配置
// quota: 预扣减额度（RPM窗口传1，其他窗口传estimatedQuota）
func CheckAndConsumeSlidingWindow(
	ctx context.Context,
	subscriptionId int,
	config SlidingWindowConfig,
	quota int64,
) (*WindowResult, error) {
	// Redis降级处理
	if !common.RedisEnabled {
		return &WindowResult{Success: true}, nil
	}

	// 确保脚本已加载
	if err := ensureScriptLoaded(ctx); err != nil {
		// 降级：脚本加载失败，允许通过
		return &WindowResult{Success: true}, nil
	}

	now := time.Now().Unix()
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionId, config.Period)

	// 执行Lua脚本
	result, err := common.RDB.EvalSha(
		ctx,
		scriptSHA,
		[]string{key},
		now,
		config.Duration,
		config.Limit,
		quota,
		config.TTL,
	).Result()

	if err != nil {
		common.SysError(fmt.Sprintf("Lua script execution failed for %s window: %v", config.Period, err))
		// 降级：执行失败，允许通过
		return &WindowResult{Success: true}, nil
	}

	// 解析返回值
	resultArray, ok := result.([]interface{})
	if !ok || len(resultArray) != 4 {
		common.SysError(fmt.Sprintf("Lua script returned invalid result for %s window", config.Period))
		return &WindowResult{Success: true}, nil // 降级
	}

	status, _ := resultArray[0].(int64)
	consumed, _ := resultArray[1].(int64)
	startTime, _ := resultArray[2].(int64)
	endTime, _ := resultArray[3].(int64)

	return &WindowResult{
		Success:   status == 1,
		Consumed:  consumed,
		StartTime: startTime,
		EndTime:   endTime,
		TimeLeft:  endTime - now,
	}, nil
}

// CheckAllSlidingWindows 检查所有滑动窗口限制
// 任一窗口超限则立即返回错误，包含详细信息
func CheckAllSlidingWindows(
	ctx context.Context,
	subscription *model.Subscription,
	pkg *model.Package,
	estimatedQuota int64,
) error {
	configs := GetSlidingWindowConfigs(pkg)

	for _, config := range configs {
		// RPM窗口特殊处理：传quota=1（表示1次请求）
		quota := estimatedQuota
		if config.Period == "rpm" {
			quota = 1
		}

		result, err := CheckAndConsumeSlidingWindow(ctx, subscription.Id, config, quota)
		if err != nil {
			return fmt.Errorf("failed to check %s window: %w", config.Period, err)
		}

		if !result.Success {
			// 超限，返回详细错误信息
			return fmt.Errorf(
				"subscription %d exceeded %s limit: consumed=%d, limit=%d, window=%s~%s, time_left=%ds",
				subscription.Id,
				config.Period,
				result.Consumed,
				config.Limit,
				time.Unix(result.StartTime, 0).Format("15:04:05"),
				time.Unix(result.EndTime, 0).Format("15:04:05"),
				result.TimeLeft,
			)
		}
	}

	return nil // 所有窗口检查通过
}

// GetSlidingWindowStatus 查询单个窗口的状态
func GetSlidingWindowStatus(
	ctx context.Context,
	subscriptionId int,
	period string,
) (*WindowStatus, error) {
	if !common.RedisEnabled {
		return &WindowStatus{Period: period, IsActive: false}, nil
	}

	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionId, period)
	now := time.Now().Unix()

	// 查询窗口元数据
	fields, err := common.RDB.HGetAll(ctx, key).Result()
	if err != nil || len(fields) == 0 {
		// 窗口不存在
		return &WindowStatus{Period: period, IsActive: false}, nil
	}

	// 解析字段
	var startTime, endTime, consumed, limit int64
	fmt.Sscanf(fields["start_time"], "%d", &startTime)
	fmt.Sscanf(fields["end_time"], "%d", &endTime)
	fmt.Sscanf(fields["consumed"], "%d", &consumed)
	fmt.Sscanf(fields["limit"], "%d", &limit)

	// 检查窗口是否过期
	if now >= endTime {
		return &WindowStatus{Period: period, IsActive: false}, nil
	}

	// 计算剩余额度和时间
	remaining := limit - consumed
	if remaining < 0 {
		remaining = 0
	}

	return &WindowStatus{
		Period:    period,
		IsActive:  true,
		Consumed:  consumed,
		Limit:     limit,
		Remaining: remaining,
		StartTime: startTime,
		EndTime:   endTime,
		TimeLeft:  endTime - now,
	}, nil
}

// GetAllSlidingWindowsStatus 批量查询所有窗口状态（使用Pipeline优化）
func GetAllSlidingWindowsStatus(
	ctx context.Context,
	subscriptionId int,
	pkg *model.Package,
) ([]WindowStatus, error) {
	if !common.RedisEnabled {
		return []WindowStatus{}, nil
	}

	configs := GetSlidingWindowConfigs(pkg)
	if len(configs) == 0 {
		return []WindowStatus{}, nil
	}

	now := time.Now().Unix()

	// 使用Pipeline批量查询
	pipe := common.RDB.Pipeline()
	cmds := make([]*redis.StringStringMapCmd, len(configs))

	for i, config := range configs {
		key := fmt.Sprintf("subscription:%d:%s:window", subscriptionId, config.Period)
		cmds[i] = pipe.HGetAll(ctx, key)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		common.SysError(fmt.Sprintf("Pipeline execution failed: %v", err))
		return []WindowStatus{}, err
	}

	// 解析结果
	statuses := make([]WindowStatus, 0, len(configs))
	for i, cmd := range cmds {
		fields, err := cmd.Result()
		config := configs[i]

		if err != nil || len(fields) == 0 {
			// 窗口不存在
			statuses = append(statuses, WindowStatus{Period: config.Period, IsActive: false})
			continue
		}

		// 解析字段
		var startTime, endTime, consumed, limit int64
		fmt.Sscanf(fields["start_time"], "%d", &startTime)
		fmt.Sscanf(fields["end_time"], "%d", &endTime)
		fmt.Sscanf(fields["consumed"], "%d", &consumed)
		fmt.Sscanf(fields["limit"], "%d", &limit)

		// 检查窗口是否过期
		if now >= endTime {
			statuses = append(statuses, WindowStatus{Period: config.Period, IsActive: false})
			continue
		}

		// 计算剩余额度和时间
		remaining := limit - consumed
		if remaining < 0 {
			remaining = 0
		}

		statuses = append(statuses, WindowStatus{
			Period:    config.Period,
			IsActive:  true,
			Consumed:  consumed,
			Limit:     limit,
			Remaining: remaining,
			StartTime: startTime,
			EndTime:   endTime,
			TimeLeft:  endTime - now,
		})
	}

	return statuses, nil
}
