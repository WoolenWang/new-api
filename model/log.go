package model

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type Log struct {
	Id               int    `json:"id" gorm:"index:idx_created_at_id,priority:1"`
	UserId           int    `json:"user_id" gorm:"index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota            int    `json:"quota" gorm:"default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	UseTime          int    `json:"use_time" gorm:"default:0"`
	IsStream         bool   `json:"is_stream"`
	ChannelId        int    `json:"channel" gorm:"index"`
	ChannelName      string `json:"channel_name" gorm:"->"`
	TokenId          int    `json:"token_id" gorm:"default:0;index"`
	Group            string `json:"group" gorm:"index"`
	Ip               string `json:"ip" gorm:"index;default:''"`
	Other            string `json:"other"`

	// 【新增】套餐相关字段（用于区分计费来源）
	// 相关设计：docs/NewAPI-支持多种包月套餐-优化版.md 第 11.2 节
	BillingType    string `json:"billing_type" gorm:"default:'balance';index"` // 计费类型: "balance"（余额） | "package"（套餐）
	PackageId      int    `json:"package_id" gorm:"default:0;index"`           // 使用的套餐模板 ID（0 = 使用余额）
	SubscriptionId int    `json:"subscription_id" gorm:"default:0;index"`      // 使用的订阅 ID（0 = 使用余额）
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown  = 0
	LogTypeTopup    = 1
	LogTypeConsume  = 2
	LogTypeManage   = 3
	LogTypeSystem   = 4
	LogTypeError    = 5
	LogTypeRefund   = 6
	LogTypeShare    = 7 // P2P channel sharing revenue
	LogTypeExchange = 8 // Quota exchange operations
	LogTypeCheckin  = 9 // Daily check-in rewards
)

func formatUserLogs(logs []*Log) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// delete admin
			delete(otherMap, "admin_info")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = logs[i].Id % 1024
	}
}

func GetLogByKey(key string) (logs []*Log, err error) {
	if os.Getenv("LOG_SQL_DSN") != "" {
		var tk Token
		if err = DB.Model(&Token{}).Where(logKeyCol+"=?", strings.TrimPrefix(key, "sk-")).First(&tk).Error; err != nil {
			return nil, err
		}
		err = LOG_DB.Model(&Log{}).Where("token_id=?", tk.Id).Find(&logs).Error
	} else {
		err = LOG_DB.Joins("left join tokens on tokens.id = logs.token_id").Where("tokens.key = ?", strings.TrimPrefix(key, "sk-")).Find(&logs).Error
	}
	formatUserLogs(logs)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	if common.DataPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, content))
	}
	username := c.GetString("username")
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		Other: otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`

	// 【新增】套餐相关参数
	BillingType    string `json:"billing_type"`    // "balance" | "package"
	PackageId      int    `json:"package_id"`      // 套餐模板 ID
	SubscriptionId int    `json:"subscription_id"` // 订阅 ID
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	if common.DataPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	}
	username := c.GetString("username")
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		Other: otherStr,

		// 【新增】套餐相关字段
		BillingType:    params.BillingType,
		PackageId:      params.PackageId,
		SubscriptionId: params.SubscriptionId,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}

	// P2P Channel Risk Control: Update used quota for the channel (Phase 1)
	if params.ChannelId > 0 && params.Quota > 0 {
		AddChannelUsedQuota(params.ChannelId, int64(params.Quota))

		// P2P Channel Sharing Revenue: Calculate and record owner's earnings (Phase 1)
		// Only for shared channels (owner_user_id != 0 and owner_user_id != current_user_id)
		channel, err := GetChannelById(params.ChannelId, false)
		if err == nil && channel.OwnerUserId != 0 && channel.OwnerUserId != userId {
			// This is a shared channel from another user
			// Calculate sharing revenue using configurable ShareRatio
			shareRatio := operation_setting.GetShareRatio()
			shareRevenue := int(float64(params.Quota) * shareRatio)
			if shareRevenue > 0 {
				err := IncreaseUserShareQuota(channel.OwnerUserId, shareRevenue)
				if err != nil {
					logger.LogError(c, fmt.Sprintf("failed to record share quota for channel owner: %v", err))
				} else {
					if common.DataPlaneLogEnabled {
						logger.LogInfo(c, fmt.Sprintf("recorded share quota: channel_owner_id=%d, revenue=%d (%.1f%% of %d)",
							channel.OwnerUserId, shareRevenue, shareRatio*100, params.Quota))
					}

					// Record sharing revenue log
					shareLog := &Log{
						UserId:    channel.OwnerUserId,
						CreatedAt: common.GetTimestamp(),
						Type:      LogTypeShare,
						Content: fmt.Sprintf("分享收益：模型 %s，收益 %d 配额 (%.1f%% of %d)",
							params.ModelName, shareRevenue, shareRatio*100, params.Quota),
						TokenName: username, // Consumer's username for reference
						ModelName: params.ModelName,
						Quota:     shareRevenue,
						ChannelId: params.ChannelId,
						Group:     params.Group,
					}
					if err := LOG_DB.Create(shareLog).Error; err != nil {
						logger.LogError(c, fmt.Sprintf("failed to record share revenue log: %v", err))
					}
				}
			}
		}
	}

	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if modelName != "" {
		tx = tx.Where("logs.model_name like ?", modelName)
	}
	if username != "" {
		tx = tx.Where("logs.username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
			return logs, total, err
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if modelName != "" {
		tx = tx.Where("logs.model_name like ?", modelName)
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	formatUserLogs(logs)
	return logs, total, err
}

func SearchAllLogs(keyword string) (logs []*Log, err error) {
	err = LOG_DB.Where("type = ? or content LIKE ?", keyword, keyword+"%").Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	return logs, err
}

func SearchUserLogs(userId int, keyword string) (logs []*Log, err error) {
	err = LOG_DB.Where("user_id = ? and type = ?", userId, keyword).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs)
	return logs, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat) {
	// NOTE: logType 参数目前仅用于向后兼容，实际统计始终基于 LogTypeConsume。
	// 调用方应依赖 type=LogTypeConsume 的语义来获取消费类日志的聚合结果。
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为 rpm 和 tpm 创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	if username != "" {
		tx = tx.Where("username = ?", username)
		rpmTpmQuery = rpmTpmQuery.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name like ?", modelName)
		rpmTpmQuery = rpmTpmQuery.Where("model_name like ?", modelName)
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的 rpm 和 tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询：先聚合总消费额度，再补充 rpm / tpm。
	// 在日志中输出一次详细的调试信息，方便排查仪表盘与明细日志不一致的问题。
	tx.Scan(&stat)
	if common.DataPlaneLogEnabled {
		common.SysLog(fmt.Sprintf(
			"[SumUsedQuota] after quota scan: username=%s model=%s channel=%d group=%s start=%d end=%d quota=%d",
			username, modelName, channel, group, startTimestamp, endTimestamp, stat.Quota,
		))
	}

	var rpmTpm struct {
		Rpm int `json:"rpm"`
		Tpm int `json:"tpm"`
	}
	rpmTpmQuery.Scan(&rpmTpm)
	stat.Rpm = rpmTpm.Rpm
	stat.Tpm = rpmTpm.Tpm

	if common.DataPlaneLogEnabled {
		common.SysLog(fmt.Sprintf(
			"[SumUsedQuota] after rpm/tpm scan: username=%s model=%s rpm=%d tpm=%d quota=%d",
			username, modelName, stat.Rpm, stat.Tpm, stat.Quota,
		))
	}

	return stat
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}

// ========== User Billing Group Statistics Operations ==========
// 用户计费分组统计操作
// 设计文档: docs/系统统计数据dashboard设计.md Section 6.3

// UserBillingGroupStats 用户计费分组统计结构
// 用于展示用户在不同计费分组下的消耗情况
type UserBillingGroupStats struct {
	BillingGroup string `json:"billing_group"` // 计费分组名（来自 logs.group）
	TotalTokens  int64  `json:"total_tokens"`  // 总 Token 数
	TotalQuota   int64  `json:"total_quota"`   // 总额度消耗
	RequestCount int    `json:"request_count"` // 请求次数
	TPM          int    `json:"tpm"`           // 平均每分钟 Token 数
	RPM          int    `json:"rpm"`           // 平均每分钟请求数
}

// UserBillingGroupDailyUsage 用户计费分组每日使用量结构
// 用于展示用户在不同计费分组下的日均消耗曲线
type UserBillingGroupDailyUsage struct {
	Day          string `json:"day"`           // YYYY-MM-DD
	BillingGroup string `json:"billing_group"` // 计费分组名
	Tokens       int64  `json:"tokens"`        // 当天 Token 数
	Quota        int64  `json:"quota"`         // 当天额度消耗
}

// AggregateUserBillingGroupStats 按计费分组聚合用户消耗
//
// 用途：为用户提供"我在不同计费分组下分别消耗了多少"的视图
//
// 参数：
//   - userId: 用户ID
//   - startTime: 起始时间戳（Unix）
//   - endTime: 结束时间戳（Unix）
//
// 返回：
//   - []UserBillingGroupStats: 按计费分组聚合的统计数据列表
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 6.3.1
func AggregateUserBillingGroupStats(userId int, startTime, endTime int64) ([]UserBillingGroupStats, error) {
	// 从 logs 表按计费分组聚合
	// logs.group 记录的是实际使用的 BillingGroup（已考虑 Token.billing_group 覆盖）
	var rawResults []struct {
		BillingGroup string
		TotalTokens  int64
		TotalQuota   int64
		RequestCount int
	}

	// 使用 logGroupCol 而非直接引用 "group" 避免在不同数据库（MySQL/PostgreSQL）下的保留字兼容问题。
	selectExpr := fmt.Sprintf(`
		%s AS billing_group,
		SUM(prompt_tokens + completion_tokens) AS total_tokens,
		SUM(quota) AS total_quota,
		COUNT(*) AS request_count
	`, logGroupCol)

	err := LOG_DB.Table("logs").
		Select(selectExpr).
		Where("user_id = ?", userId).
		Where("type = ?", LogTypeConsume).
		Where("created_at BETWEEN ? AND ?", startTime, endTime).
		Group("logs.group").
		Scan(&rawResults).Error

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate user billing group stats: %w", err)
	}

	// 计算时间范围（分钟数）
	timeRangeMinutes := float64(endTime-startTime) / 60.0
	if timeRangeMinutes <= 0 {
		timeRangeMinutes = 1.0
	}

	// 组装结果并计算 TPM、RPM
	results := make([]UserBillingGroupStats, 0, len(rawResults))
	for _, raw := range rawResults {
		stat := UserBillingGroupStats{
			BillingGroup: raw.BillingGroup,
			TotalTokens:  raw.TotalTokens,
			TotalQuota:   raw.TotalQuota,
			RequestCount: raw.RequestCount,
			TPM:          int(float64(raw.TotalTokens) / timeRangeMinutes),
			RPM:          int(float64(raw.RequestCount) / timeRangeMinutes),
		}
		results = append(results, stat)
	}

	return results, nil
}

// GetUserBillingGroupDailyUsage 获取用户按计费分组的每日消耗曲线
//
// 用途：为用户提供日均消耗趋势图，可按计费分组分色展示
//
// 参数：
//   - userId: 用户ID
//   - days: 向前多少天
//   - billingGroup: 可选，指定则只返回该计费分组；为空则返回所有计费分组
//
// 返回：
//   - []UserBillingGroupDailyUsage: 按日、按计费分组聚合的数据列表
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 6.3.1
func GetUserBillingGroupDailyUsage(userId int, days int, billingGroup string) ([]UserBillingGroupDailyUsage, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	now := common.GetTimestamp()
	startTime := now - int64(days*24*60*60)

	// 按自然日 + 计费分组聚合用户的 Token 和 Quota
	selectExpr := fmt.Sprintf(`
		DATE(FROM_UNIXTIME(created_at)) AS day,
		%s AS billing_group,
		SUM(prompt_tokens + completion_tokens) AS tokens,
		SUM(quota) AS quota
	`, logGroupCol)

	query := LOG_DB.Table("logs").
		Select(selectExpr).
		Where("user_id = ?", userId).
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", startTime)

	// 可选：按指定计费分组过滤
	if billingGroup != "" {
		query = query.Where(logGroupCol+" = ?", billingGroup)
	}

	var results []UserBillingGroupDailyUsage
	err := query.
		Group("day, logs.group").
		Order("day ASC, logs.group ASC").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get user billing group daily usage: %w", err)
	}

	return results, nil
}
