package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type OpenAIModel struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy    string `json:"owned_by"`
	Permission []struct {
		ID                 string `json:"id"`
		Object             string `json:"object"`
		Created            int64  `json:"created"`
		AllowCreateEngine  bool   `json:"allow_create_engine"`
		AllowSampling      bool   `json:"allow_sampling"`
		AllowLogprobs      bool   `json:"allow_logprobs"`
		AllowSearchIndices bool   `json:"allow_search_indices"`
		AllowView          bool   `json:"allow_view"`
		AllowFineTuning    bool   `json:"allow_fine_tuning"`
		Organization       string `json:"organization"`
		Group              string `json:"group"`
		IsBlocking         bool   `json:"is_blocking"`
	} `json:"permission"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type OpenAIModelsResponse struct {
	Data    []OpenAIModel `json:"data"`
	Success bool          `json:"success"`
}

func parseStatusFilter(statusParam string) int {
	switch strings.ToLower(statusParam) {
	case "enabled", "1":
		return common.ChannelStatusEnabled
	case "disabled", "0":
		return 0
	default:
		return -1
	}
}

func clearChannelInfo(channel *model.Channel) {
	if channel.ChannelInfo.IsMultiKey {
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
	}
}

func GetAllChannels(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	channelData := make([]*model.Channel, 0)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	statusParam := c.Query("status")
	// statusFilter: -1 all, 1 enabled, 0 disabled (include auto & manual)
	statusFilter := parseStatusFilter(statusParam)
	// type filter
	typeStr := c.Query("type")
	typeFilter := -1
	if typeStr != "" {
		if t, err := strconv.Atoi(typeStr); err == nil {
			typeFilter = t
		}
	}

	var total int64

	if enableTagMode {
		tags, err := model.GetPaginatedTags(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		for _, tag := range tags {
			if tag == nil || *tag == "" {
				continue
			}
			tagChannels, err := model.GetChannelsByTag(*tag, idSort, false)
			if err != nil {
				continue
			}
			filtered := make([]*model.Channel, 0)
			for _, ch := range tagChannels {
				if statusFilter == common.ChannelStatusEnabled && ch.Status != common.ChannelStatusEnabled {
					continue
				}
				if statusFilter == 0 && ch.Status == common.ChannelStatusEnabled {
					continue
				}
				if typeFilter >= 0 && ch.Type != typeFilter {
					continue
				}
				filtered = append(filtered, ch)
			}
			channelData = append(channelData, filtered...)
		}
		total, _ = model.CountAllTags()
	} else {
		baseQuery := model.DB.Model(&model.Channel{})
		if typeFilter >= 0 {
			baseQuery = baseQuery.Where("type = ?", typeFilter)
		}
		if statusFilter == common.ChannelStatusEnabled {
			baseQuery = baseQuery.Where("status = ?", common.ChannelStatusEnabled)
		} else if statusFilter == 0 {
			baseQuery = baseQuery.Where("status != ?", common.ChannelStatusEnabled)
		}

		baseQuery.Count(&total)

		order := "priority desc"
		if idSort {
			order = "id desc"
		}

		err := baseQuery.Order(order).Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("key").Find(&channelData).Error
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
	}

	for _, datum := range channelData {
		clearChannelInfo(datum)
	}

	countQuery := model.DB.Model(&model.Channel{})
	if statusFilter == common.ChannelStatusEnabled {
		countQuery = countQuery.Where("status = ?", common.ChannelStatusEnabled)
	} else if statusFilter == 0 {
		countQuery = countQuery.Where("status != ?", common.ChannelStatusEnabled)
	}
	var results []struct {
		Type  int64
		Count int64
	}
	_ = countQuery.Select("type, count(*) as count").Group("type").Find(&results).Error
	typeCounts := make(map[int64]int64)
	for _, r := range results {
		typeCounts[r.Type] = r.Count
	}
	common.ApiSuccess(c, gin.H{
		"items":       channelData,
		"total":       total,
		"page":        pageInfo.GetPage(),
		"page_size":   pageInfo.GetPageSize(),
		"type_counts": typeCounts,
	})
	return
}

func FetchUpstreamModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	var url string
	switch channel.Type {
	case constant.ChannelTypeGemini:
		// curl https://example.com/v1beta/models?key=$GEMINI_API_KEY
		url = fmt.Sprintf("%s/v1beta/openai/models", baseURL) // Remove key in url since we need to use AuthHeader
	case constant.ChannelTypeAli:
		url = fmt.Sprintf("%s/compatible-mode/v1/models", baseURL)
	case constant.ChannelTypeZhipu_v4:
		url = fmt.Sprintf("%s/api/paas/v4/models", baseURL)
	case constant.ChannelTypeVolcEngine:
		if baseURL == volcengine.DoubaoCodingPlan {
			url = fmt.Sprintf("%s/v1/models", volcengine.DoubaoCodingPlanOpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/v1/models", baseURL)
		}
	default:
		url = fmt.Sprintf("%s/v1/models", baseURL)
	}

	// 获取用于请求的可用密钥（多密钥渠道优先使用启用状态的密钥）
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取渠道密钥失败: %s", apiErr.Error()),
		})
		return
	}
	key = strings.TrimSpace(key)

	// 获取响应体 - 根据渠道类型决定是否添加 AuthHeader
	var body []byte
	switch channel.Type {
	case constant.ChannelTypeAnthropic:
		body, err = GetResponseBody("GET", url, channel, GetClaudeAuthHeader(key))
	default:
		body, err = GetResponseBody("GET", url, channel, GetAuthHeader(key))
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var result OpenAIModelsResponse
	if err = json.Unmarshal(body, &result); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("解析响应失败: %s", err.Error()),
		})
		return
	}

	var ids []string
	for _, model := range result.Data {
		id := model.ID
		if channel.Type == constant.ChannelTypeGemini {
			id = strings.TrimPrefix(id, "models/")
		}
		ids = append(ids, id)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ids,
	})
}

func FixChannelsAbilities(c *gin.Context) {
	success, fails, err := model.FixAbility()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"success": success,
			"fails":   fails,
		},
	})
}

func SearchChannels(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	modelKeyword := c.Query("model")
	statusParam := c.Query("status")
	statusFilter := parseStatusFilter(statusParam)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	channelData := make([]*model.Channel, 0)
	if enableTagMode {
		tags, err := model.SearchTags(keyword, group, modelKeyword, idSort)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		for _, tag := range tags {
			if tag != nil && *tag != "" {
				tagChannel, err := model.GetChannelsByTag(*tag, idSort, false)
				if err == nil {
					channelData = append(channelData, tagChannel...)
				}
			}
		}
	} else {
		channels, err := model.SearchChannels(keyword, group, modelKeyword, idSort)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		channelData = channels
	}

	if statusFilter == common.ChannelStatusEnabled || statusFilter == 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if statusFilter == common.ChannelStatusEnabled && ch.Status != common.ChannelStatusEnabled {
				continue
			}
			if statusFilter == 0 && ch.Status == common.ChannelStatusEnabled {
				continue
			}
			filtered = append(filtered, ch)
		}
		channelData = filtered
	}

	// calculate type counts for search results
	typeCounts := make(map[int64]int64)
	for _, channel := range channelData {
		typeCounts[int64(channel.Type)]++
	}

	typeParam := c.Query("type")
	typeFilter := -1
	if typeParam != "" {
		if tp, err := strconv.Atoi(typeParam); err == nil {
			typeFilter = tp
		}
	}

	if typeFilter >= 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if ch.Type == typeFilter {
				filtered = append(filtered, ch)
			}
		}
		channelData = filtered
	}

	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	total := len(channelData)
	startIdx := (page - 1) * pageSize
	if startIdx > total {
		startIdx = total
	}
	endIdx := startIdx + pageSize
	if endIdx > total {
		endIdx = total
	}

	pagedData := channelData[startIdx:endIdx]

	for _, datum := range pagedData {
		clearChannelInfo(datum)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":       pagedData,
			"total":       total,
			"type_counts": typeCounts,
		},
	})
	return
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel != nil {
		clearChannelInfo(channel)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

// GetChannelKey 获取渠道密钥（需要通过安全验证中间件）
// 此函数依赖 SecureVerificationRequired 中间件，确保用户已通过安全验证
func GetChannelKey(c *gin.Context) {
	userId := c.GetInt("id")
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("渠道ID格式错误: %v", err))
		return
	}

	// 获取渠道信息（包含密钥）
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("获取渠道信息失败: %v", err))
		return
	}

	if channel == nil {
		common.ApiError(c, fmt.Errorf("渠道不存在"))
		return
	}

	// 记录操作日志
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("查看渠道密钥信息 (渠道ID: %d)", channelId))

	// 返回渠道密钥
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "获取成功",
		"data": map[string]interface{}{
			"key": channel.Key,
		},
	})
}

// validateTwoFactorAuth 统一的2FA验证函数
func validateTwoFactorAuth(twoFA *model.TwoFA, code string) bool {
	// 尝试验证TOTP
	if cleanCode, err := common.ValidateNumericCode(code); err == nil {
		if isValid, _ := twoFA.ValidateTOTPAndUpdateUsage(cleanCode); isValid {
			return true
		}
	}

	// 尝试验证备用码
	if isValid, err := twoFA.ValidateBackupCodeAndUpdateUsage(code); err == nil && isValid {
		return true
	}

	return false
}

// validateChannel 通用的渠道校验函数
func validateChannel(channel *model.Channel, isAdd bool) error {
	// 先做空指针检查，避免在上游传入空 channel 时触发 panic
	if channel == nil {
		if isAdd {
			return fmt.Errorf("channel cannot be empty")
		}
		return fmt.Errorf("channel is nil")
	}

	// 校验 channel settings
	if err := channel.ValidateSettings(); err != nil {
		return fmt.Errorf("渠道额外设置[channel setting] 格式错误：%s", err.Error())
	}

	// 校验 P2P 渠道的速率限制配置
	// hourly_limit: 每小时请求数限制，0表示不限制，仅对P2P渠道生效
	if channel.HourlyLimit < 0 {
		return fmt.Errorf("每小时请求数限制不能为负数")
	}
	// daily_limit: 每日请求数限制，0表示不限制，仅对P2P渠道生效
	if channel.DailyLimit < 0 {
		return fmt.Errorf("每日请求数限制不能为负数")
	}

	// 如果是添加操作，检查 key 是否为空
	// 说明：
	//   - 对普通 HTTP 渠道，Key 依然表示上游模型提供商的 API Key；
	//   - 对 CLIProxy 类型渠道（channel.Type == ChannelTypeCliProxy），Key 表示 CLIProxyAPI 网关的 API Key，
	//     必须与 CLIProxy config.yaml 中 api-keys 列表的某一项保持一致，用于通过 Authorization 头完成 CLIProxy 入口鉴权。
	if isAdd {
		trimmedKey := strings.TrimSpace(channel.Key)
		if trimmedKey == "" {
			if channel.Type == constant.ChannelTypeCliProxy {
				return fmt.Errorf("CLIProxyAPI 渠道必须配置 CLIProxy API Key，请在 CLIProxy config.yaml 的 api-keys 中添加密钥，并在此填写相同值")
			}
			return fmt.Errorf("channel key cannot be empty")
		}
		channel.Key = trimmedKey

		// 检查模型名称长度是否超过 255
		for _, m := range channel.GetModels() {
			if len(m) > 255 {
				return fmt.Errorf("模型名称过长: %s", m)
			}
		}
	}

	// VertexAI 特殊校验
	if channel.Type == constant.ChannelTypeVertexAi {
		if channel.Other == "" {
			return fmt.Errorf("部署地区不能为空")
		}

		regionMap, err := common.StrToMap(channel.Other)
		if err != nil {
			return fmt.Errorf("部署地区必须是标准的Json格式，例如{\"default\": \"us-central1\", \"region2\": \"us-east1\"}")
		}

		if regionMap["default"] == nil {
			return fmt.Errorf("部署地区必须包含default字段")
		}
	}

	return nil
}

type AddChannelRequest struct {
	Mode                      string                `json:"mode"`
	MultiKeyMode              constant.MultiKeyMode `json:"multi_key_mode"`
	BatchAddSetKeyPrefix2Name bool                  `json:"batch_add_set_key_prefix_2_name"`
	Channel                   *model.Channel        `json:"channel"`
}

// SelfChannelCreateRequest 描述 /api/channel/self 创建渠道时 WQuant 侧透传的扁平请求体
// 结构参考 wquant/docs/设计文档/07-NEW-API-集成-接口约定.md 以及
// woolen_quant/controllers/api/svr/v1/newapi/channels.py::NewapiChannelApi.post
type SelfChannelCreateRequest struct {
	Name            string          `json:"name"`
	Type            string          `json:"type"`           // openai / claude / gemini / cli_proxy 等
	Key             string          `json:"key"`            // 部分类型可为空（如 cli_proxy）
	BaseURL         string          `json:"base_url"`       // 可选
	ModelsRaw       json.RawMessage `json:"models"`         // 字符串或数组，延后解析
	Remark          string          `json:"remark"`         // 可选
	Priority        int64           `json:"priority"`       // 可选，默认 0
	Weight          int             `json:"weight"`         // 可选，默认 1
	ModelMappingRaw json.RawMessage `json:"model_mapping"`  // 可选，JSON 对象或字符串
	Group           string          `json:"group"`          // 可选，计费分组，支持逗号分隔或JSON数组字符串格式
	Tag             string          `json:"tag"`            // 可选
	AutoDisable     int             `json:"auto_disable"`   // 1=自动禁用，其余视为关闭
	Config          string          `json:"config"`         // 参数覆盖（JSON 字符串）
	Headers         string          `json:"headers"`        // header 覆盖（JSON 字符串）
	Other           string          `json:"other"`          // OAuth/CLIProxy 等场景使用
	AllowedModels   string          `json:"allowed_models"` // P2P权限白名单：允许共享的模型列表(逗号分隔)
	IsPrivate       bool            `json:"is_private"`     // 是否为私有渠道：true=仅Owner可见
	// P2P Channel Sharing Fields
	AccountHint       string `json:"account_hint"`        // CLIProxyAPI 凭证映射标识
	TotalQuota        int64  `json:"total_quota"`         // 总额度限制(quota单位)，0表示不限制
	Concurrency       int    `json:"concurrency"`         // 并发数限制，0表示不限制
	HourlyQuotaLimit  int64  `json:"hourly_quota_limit"`  // 每小时额度限制(quota单位)，0表示不限制
	DailyQuotaLimit   int64  `json:"daily_quota_limit"`   // 每日额度限制(quota单位)，0表示不限制
	WeeklyQuotaLimit  int64  `json:"weekly_quota_limit"`  // 每周额度限制(quota单位)，0表示不限制
	MonthlyQuotaLimit int64  `json:"monthly_quota_limit"` // 每月额度限制(quota单位)，0表示不限制
	AllowedGroups     string `json:"allowed_groups"`      // 允许访问的P2P分组ID列表(逗号分隔或JSON数组)
	IPWhitelist       string `json:"ip_whitelist"`        // IP白名单(逗号分隔或JSON数组)
}

// mapExternalChannelType 将 WQuant 侧字符串类型映射到内部的 ChannelType 常量
func mapExternalChannelType(typeStr string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(typeStr)) {
	case "openai":
		return constant.ChannelTypeOpenAI, nil
	case "azure":
		return constant.ChannelTypeAzure, nil
	case "claude", "anthropic":
		return constant.ChannelTypeAnthropic, nil
	case "gemini":
		return constant.ChannelTypeGemini, nil
	case "cliproxy", "cli_proxy":
		return constant.ChannelTypeCliProxy, nil
	default:
		return 0, fmt.Errorf("不支持的渠道类型: %s", typeStr)
	}
}

// ToModelChannel 将自服务创建请求转换为内部的 model.Channel
func (r *SelfChannelCreateRequest) ToModelChannel() (*model.Channel, error) {
	name := strings.TrimSpace(r.Name)
	if name == "" {
		return nil, fmt.Errorf("渠道名称不能为空")
	}
	if strings.TrimSpace(r.Type) == "" {
		return nil, fmt.Errorf("渠道类型不能为空")
	}

	channelType, err := mapExternalChannelType(r.Type)
	if err != nil {
		return nil, err
	}

	ch := &model.Channel{
		Name: name,
		Type: channelType,
		Key:  strings.TrimSpace(r.Key),
	}

	// 处理 BaseURL：去掉尾部斜杠
	if r.BaseURL != "" {
		base := strings.TrimSpace(r.BaseURL)
		base = strings.TrimRight(base, "/")
		if base != "" {
			ch.BaseURL = &base
		}
	}

	// 处理 models：既支持 ["gpt-4","gpt-3.5"] 也支持 "gpt-4,gpt-3.5"
	if len(r.ModelsRaw) > 0 && string(r.ModelsRaw) != "null" {
		var models []string
		switch r.ModelsRaw[0] {
		case '[':
			var arr []string
			if err := json.Unmarshal(r.ModelsRaw, &arr); err != nil {
				return nil, fmt.Errorf("models 字段格式错误: %w", err)
			}
			for _, m := range arr {
				m = strings.TrimSpace(m)
				if m != "" {
					models = append(models, m)
				}
			}
		case '"':
			var s string
			if err := json.Unmarshal(r.ModelsRaw, &s); err != nil {
				return nil, fmt.Errorf("models 字段格式错误: %w", err)
			}
			s = strings.TrimSpace(s)
			if s != "" {
				for _, m := range strings.Split(s, ",") {
					m = strings.TrimSpace(m)
					if m != "" {
						models = append(models, m)
					}
				}
			}
		}
		if len(models) > 0 {
			ch.Models = strings.Join(models, ",")
		}
	}

	if r.Remark != "" {
		remark := r.Remark
		ch.Remark = &remark
	}

	if r.Priority != 0 {
		p := r.Priority
		ch.Priority = &p
	}

	if r.Weight > 0 {
		w := uint(r.Weight)
		ch.Weight = &w
	}

	// 处理模型映射：前端可能传对象，也可能传已经序列化好的字符串
	if len(r.ModelMappingRaw) > 0 && string(r.ModelMappingRaw) != "null" {
		if json.Valid(r.ModelMappingRaw) {
			mappingStr := string(r.ModelMappingRaw)
			ch.ModelMapping = &mappingStr
		}
	}

	// 处理分组：直接设置，支持逗号分隔或JSON数组字符串格式
	if r.Group != "" {
		ch.Group = strings.TrimSpace(r.Group)
	} else {
		ch.Group = "user_default"
	}

	if r.Tag != "" {
		tag := r.Tag
		ch.Tag = &tag
	}

	// auto_disable -> AutoBan
	if r.AutoDisable != 0 {
		auto := 1
		ch.AutoBan = &auto
	}

	if strings.TrimSpace(r.Config) != "" {
		cfg := strings.TrimSpace(r.Config)
		ch.ParamOverride = &cfg
	}

	if strings.TrimSpace(r.Headers) != "" {
		h := strings.TrimSpace(r.Headers)
		ch.HeaderOverride = &h
	}

	if r.Other != "" {
		ch.Other = r.Other
	}

	// 处理 allowed_models: P2P权限白名单
	if r.AllowedModels != "" {
		allowedModels := strings.TrimSpace(r.AllowedModels)
		ch.AllowedModels = &allowedModels
	}

	// 处理 allowed_groups: 允许访问的P2P分组ID列表
	// 支持逗号分隔字符串或JSON数组格式
	if r.AllowedGroups != "" {
		allowedGroups := strings.TrimSpace(r.AllowedGroups)
		ch.AllowedGroups = &allowedGroups
	}

	// 处理 ip_whitelist: IP白名单
	if r.IPWhitelist != "" {
		ipWhitelist := strings.TrimSpace(r.IPWhitelist)
		ch.IPWhitelist = &ipWhitelist
	}

	// 处理 account_hint: CLIProxyAPI 凭证映射标识
	if r.AccountHint != "" {
		accountHint := strings.TrimSpace(r.AccountHint)
		ch.AccountHint = &accountHint
	}

	// 处理额度和限流配置
	if r.TotalQuota > 0 {
		ch.TotalQuota = r.TotalQuota
	}
	if r.Concurrency > 0 {
		ch.Concurrency = r.Concurrency
	}
	if r.HourlyQuotaLimit > 0 {
		ch.HourlyQuotaLimit = r.HourlyQuotaLimit
	}
	if r.DailyQuotaLimit > 0 {
		ch.DailyQuotaLimit = r.DailyQuotaLimit
	}
	if r.WeeklyQuotaLimit > 0 {
		ch.WeeklyQuotaLimit = r.WeeklyQuotaLimit
	}
	if r.MonthlyQuotaLimit > 0 {
		ch.MonthlyQuotaLimit = r.MonthlyQuotaLimit
	}

	// is_private: 是否为私有渠道。默认为 false，与现有行为保持一致；
	// 显式透传 true 时，将渠道标记为私有，仅 Owner 可见。
	ch.IsPrivate = r.IsPrivate

	return ch, nil
}

func getVertexArrayKeys(keys string) ([]string, error) {
	if keys == "" {
		return nil, nil
	}
	var keyArray []interface{}
	err := common.Unmarshal([]byte(keys), &keyArray)
	if err != nil {
		return nil, fmt.Errorf("批量添加 Vertex AI 必须使用标准的JsonArray格式，例如[{key1}, {key2}...]，请检查输入: %w", err)
	}
	cleanKeys := make([]string, 0, len(keyArray))
	for _, key := range keyArray {
		var keyStr string
		switch v := key.(type) {
		case string:
			keyStr = strings.TrimSpace(v)
		default:
			bytes, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("Vertex AI key JSON 编码失败: %w", err)
			}
			keyStr = string(bytes)
		}
		if keyStr != "" {
			cleanKeys = append(cleanKeys, keyStr)
		}
	}
	if len(cleanKeys) == 0 {
		return nil, fmt.Errorf("批量添加 Vertex AI 的 keys 不能为空")
	}
	return cleanKeys, nil
}

func AddChannel(c *gin.Context) {
	addChannelRequest := AddChannelRequest{}
	err := c.ShouldBindJSON(&addChannelRequest)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 使用统一的校验函数
	if err := validateChannel(addChannelRequest.Channel, true); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	addChannelRequest.Channel.CreatedTime = common.GetTimestamp()
	keys := make([]string, 0)
	switch addChannelRequest.Mode {
	case "multi_to_single":
		addChannelRequest.Channel.ChannelInfo.IsMultiKey = true
		addChannelRequest.Channel.ChannelInfo.MultiKeyMode = addChannelRequest.MultiKeyMode
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			array, err := getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(array)
			addChannelRequest.Channel.Key = strings.Join(array, "\n")
		} else {
			cleanKeys := make([]string, 0)
			for _, key := range strings.Split(addChannelRequest.Channel.Key, "\n") {
				if key == "" {
					continue
				}
				key = strings.TrimSpace(key)
				cleanKeys = append(cleanKeys, key)
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(cleanKeys)
			addChannelRequest.Channel.Key = strings.Join(cleanKeys, "\n")
		}
		keys = []string{addChannelRequest.Channel.Key}
	case "batch":
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			// multi json
			keys, err = getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
		} else {
			keys = strings.Split(addChannelRequest.Channel.Key, "\n")
		}
	case "single":
		keys = []string{addChannelRequest.Channel.Key}
	default:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "不支持的添加模式",
		})
		return
	}

	channels := make([]model.Channel, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		localChannel := addChannelRequest.Channel
		localChannel.Key = key
		if addChannelRequest.BatchAddSetKeyPrefix2Name && len(keys) > 1 {
			keyPrefix := localChannel.Key
			if len(localChannel.Key) > 8 {
				keyPrefix = localChannel.Key[:8]
			}
			localChannel.Name = fmt.Sprintf("%s %s", localChannel.Name, keyPrefix)
		}
		channels = append(channels, *localChannel)
	}
	err = model.BatchInsertChannels(channels)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if common.ControlPlaneLogEnabled && len(channels) > 0 {
		adminId := c.GetInt("id")
		// Log a concise summary; do not log keys or secrets.
		if len(channels) == 1 {
			ch := channels[0]
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane channel created: admin_id=%d channel_id=%d name=%q type=%d group=%q models=%q owner_user_id=%d is_private=%t total_quota=%d concurrency=%d hourly_limit=%d daily_limit=%d",
				adminId,
				ch.Id,
				ch.Name,
				ch.Type,
				ch.Group,
				ch.Models,
				ch.OwnerUserId,
				ch.IsPrivate,
				ch.TotalQuota,
				ch.Concurrency,
				ch.HourlyLimit,
				ch.DailyLimit,
			))
		} else {
			first := channels[0]
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane channels batch created: admin_id=%d count=%d first_channel_id=%d type=%d group=%q owner_user_id=%d",
				adminId,
				len(channels),
				first.Id,
				first.Type,
				first.Group,
				first.OwnerUserId,
			))
		}
	}
	// New channels should be visible to the in-memory routing cache immediately.
	// Rebuild the channel cache so that subsequent data-plane requests can
	// discover the newly created channels without waiting for the periodic sync.
	model.InitChannelCache()
	service.ResetProxyClientCache()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	channel := model.Channel{Id: id}
	err := channel.Delete()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane channel deleted: admin_id=%d channel_id=%d",
			adminId, id,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteDisabledChannel(c *gin.Context) {
	rows, err := model.DeleteDisabledChannel()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane disabled channels deleted: admin_id=%d rows=%d",
			adminId, rows,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

type ChannelTag struct {
	Tag            string  `json:"tag"`
	NewTag         *string `json:"new_tag"`
	Priority       *int64  `json:"priority"`
	Weight         *uint   `json:"weight"`
	ModelMapping   *string `json:"model_mapping"`
	Models         *string `json:"models"`
	Groups         *string `json:"groups"`
	ParamOverride  *string `json:"param_override"`
	HeaderOverride *string `json:"header_override"`
}

func DisableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.DisableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane tag channels disabled: admin_id=%d tag=%q",
			adminId, channelTag.Tag,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EnableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.EnableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane tag channels enabled: admin_id=%d tag=%q",
			adminId, channelTag.Tag,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EditTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	if channelTag.Tag == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "tag不能为空",
		})
		return
	}
	if channelTag.ParamOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.ParamOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "参数覆盖必须是合法的 JSON 格式",
			})
			return
		}
		channelTag.ParamOverride = common.GetPointer[string](trimmed)
	}
	if channelTag.HeaderOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.HeaderOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "请求头覆盖必须是合法的 JSON 格式",
			})
			return
		}
		channelTag.HeaderOverride = common.GetPointer[string](trimmed)
	}
	err = model.EditChannelByTag(channelTag.Tag, channelTag.NewTag, channelTag.ModelMapping, channelTag.Models, channelTag.Groups, channelTag.Priority, channelTag.Weight, channelTag.ParamOverride, channelTag.HeaderOverride)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		newTag := ""
		if channelTag.NewTag != nil {
			newTag = *channelTag.NewTag
		}
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane tag channels edited: admin_id=%d tag=%q new_tag=%q",
			adminId, channelTag.Tag, newTag,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type ChannelBatch struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

func DeleteChannelBatch(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.BatchDeleteChannels(channelBatch.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane channels batch deleted: admin_id=%d count=%d ids=%v",
			adminId, len(channelBatch.Ids), channelBatch.Ids,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    len(channelBatch.Ids),
	})
	return
}

type PatchChannel struct {
	model.Channel
	MultiKeyMode *string `json:"multi_key_mode"`
	KeyMode      *string `json:"key_mode"` // 多key模式下密钥覆盖或者追加
}

func UpdateChannel(c *gin.Context) {
	channel := PatchChannel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 使用统一的校验函数
	if err := validateChannel(&channel.Channel, false); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// Preserve existing ChannelInfo to ensure multi-key channels keep correct state even if the client does not send ChannelInfo in the request.
	originChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Phase 8.4: CS4-2 记录状态变化（用于停服时间追踪）
	// 在更新前记录原始状态，更新后比较并调用停服追踪器
	oldStatus := originChannel.Status
	newStatus := channel.Status

	// Always copy the original ChannelInfo so that fields like IsMultiKey and MultiKeySize are retained.
	channel.ChannelInfo = originChannel.ChannelInfo

	// If the request explicitly specifies a new MultiKeyMode, apply it on top of the original info.
	if channel.MultiKeyMode != nil && *channel.MultiKeyMode != "" {
		channel.ChannelInfo.MultiKeyMode = constant.MultiKeyMode(*channel.MultiKeyMode)
	}

	// 处理多key模式下的密钥追加/覆盖逻辑
	if channel.KeyMode != nil && channel.ChannelInfo.IsMultiKey {
		switch *channel.KeyMode {
		case "append":
			// 追加模式：将新密钥添加到现有密钥列表
			if originChannel.Key != "" {
				var newKeys []string
				var existingKeys []string

				// 解析现有密钥
				if strings.HasPrefix(strings.TrimSpace(originChannel.Key), "[") {
					// JSON数组格式
					var arr []json.RawMessage
					if err := json.Unmarshal([]byte(strings.TrimSpace(originChannel.Key)), &arr); err == nil {
						existingKeys = make([]string, len(arr))
						for i, v := range arr {
							existingKeys[i] = string(v)
						}
					}
				} else {
					// 换行分隔格式
					existingKeys = strings.Split(strings.Trim(originChannel.Key, "\n"), "\n")
				}

				// 处理 Vertex AI 的特殊情况
				if channel.Type == constant.ChannelTypeVertexAi && channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
					// 尝试解析新密钥为JSON数组
					if strings.HasPrefix(strings.TrimSpace(channel.Key), "[") {
						array, err := getVertexArrayKeys(channel.Key)
						if err != nil {
							c.JSON(http.StatusOK, gin.H{
								"success": false,
								"message": "追加密钥解析失败: " + err.Error(),
							})
							return
						}
						newKeys = array
					} else {
						// 单个JSON密钥
						newKeys = []string{channel.Key}
					}
					// 合并密钥
					allKeys := append(existingKeys, newKeys...)
					channel.Key = strings.Join(allKeys, "\n")
				} else {
					// 普通渠道的处理
					inputKeys := strings.Split(channel.Key, "\n")
					for _, key := range inputKeys {
						key = strings.TrimSpace(key)
						if key != "" {
							newKeys = append(newKeys, key)
						}
					}
					// 合并密钥
					allKeys := append(existingKeys, newKeys...)
					channel.Key = strings.Join(allKeys, "\n")
				}
			}
		case "replace":
			// 覆盖模式：直接使用新密钥（默认行为，不需要特殊处理）
		}
	}
	err = channel.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Phase 8.4: CS4-2 处理状态变化（手动禁用/启用时记录停服时间）
	if oldStatus != newStatus {
		tracker := service.GetChannelDowntimeTracker()

		// 状态从启用变为禁用
		if oldStatus == common.ChannelStatusEnabled && (newStatus == common.ChannelStatusManuallyDisabled || newStatus == common.ChannelStatusAutoDisabled) {
			if err := tracker.RecordDisable(channel.Id, newStatus, 0); err != nil {
				common.SysLog(fmt.Sprintf("Failed to record manual disable for channel %d: %v", channel.Id, err))
			} else {
				common.SysLog(fmt.Sprintf("Recorded manual disable for channel %d (status: %d -> %d)", channel.Id, oldStatus, newStatus))
			}
		}

		// 状态从禁用变为启用
		if (oldStatus == common.ChannelStatusManuallyDisabled || oldStatus == common.ChannelStatusAutoDisabled) && newStatus == common.ChannelStatusEnabled {
			if err := tracker.RecordEnable(channel.Id, 0); err != nil {
				common.SysLog(fmt.Sprintf("Failed to record manual enable for channel %d: %v", channel.Id, err))
			} else {
				common.SysLog(fmt.Sprintf("Recorded manual enable for channel %d (status: %d -> %d)", channel.Id, oldStatus, newStatus))
			}
		}
	}

	model.InitChannelCache()
	service.ResetProxyClientCache()
	channel.Key = ""
	clearChannelInfo(&channel.Channel)
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane channel updated: admin_id=%d channel_id=%d name=%q type=%d group=%q models=%q owner_user_id=%d is_private=%t total_quota=%d concurrency=%d hourly_limit=%d daily_limit=%d",
			adminId,
			channel.Id,
			channel.Name,
			channel.Type,
			channel.Group,
			channel.Models,
			channel.OwnerUserId,
			channel.IsPrivate,
			channel.TotalQuota,
			channel.Concurrency,
			channel.HourlyLimit,
			channel.DailyLimit,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

func FetchModels(c *gin.Context) {
	var req struct {
		BaseURL string `json:"base_url"`
		Type    int    `json:"type"`
		Key     string `json:"key"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request",
		})
		return
	}

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[req.Type]
	}

	client := &http.Client{}
	url := fmt.Sprintf("%s/v1/models", baseURL)

	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// remove line breaks and extra spaces.
	key := strings.TrimSpace(req.Key)
	// If the key contains a line break, only take the first part.
	key = strings.Split(key, "\n")[0]
	request.Header.Set("Authorization", "Bearer "+key)

	response, err := client.Do(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	//check status code
	if response.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to fetch models",
		})
		return
	}
	defer response.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var models []string
	for _, model := range result.Data {
		models = append(models, model.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    models,
	})
}

func BatchSetChannelTag(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.BatchSetChannelTag(channelBatch.Ids, channelBatch.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		adminId := c.GetInt("id")
		tag := ""
		if channelBatch.Tag != nil {
			tag = *channelBatch.Tag
		}
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane batch set channel tag: admin_id=%d count=%d tag=%q ids=%v",
			adminId, len(channelBatch.Ids), tag, channelBatch.Ids,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    len(channelBatch.Ids),
	})
	return
}

func GetTagModels(c *gin.Context) {
	tag := c.Query("tag")
	if tag == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "tag不能为空",
		})
		return
	}

	channels, err := model.GetChannelsByTag(tag, false, false) // idSort=false, selectAll=false
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var longestModels string
	maxLength := 0

	// Find the longest models string among all channels with the given tag
	for _, channel := range channels {
		if channel.Models != "" {
			currentModels := strings.Split(channel.Models, ",")
			if len(currentModels) > maxLength {
				maxLength = len(currentModels)
				longestModels = channel.Models
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    longestModels,
	})
	return
}

// CopyChannel handles cloning an existing channel with its key.
// POST /api/channel/copy/:id
// Optional query params:
//
//	suffix         - string appended to the original name (default "_复制")
//	reset_balance  - bool, when true will reset balance & used_quota to 0 (default true)
func CopyChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid id"})
		return
	}

	suffix := c.DefaultQuery("suffix", "_复制")
	resetBalance := true
	if rbStr := c.DefaultQuery("reset_balance", "true"); rbStr != "" {
		if v, err := strconv.ParseBool(rbStr); err == nil {
			resetBalance = v
		}
	}

	// fetch original channel with key
	origin, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	// clone channel
	clone := *origin // shallow copy is sufficient as we will overwrite primitives
	clone.Id = 0     // let DB auto-generate
	clone.CreatedTime = common.GetTimestamp()
	clone.Name = origin.Name + suffix
	clone.TestTime = 0
	clone.ResponseTime = 0
	if resetBalance {
		clone.Balance = 0
		clone.UsedQuota = 0
	}

	// insert
	if err := model.BatchInsertChannels([]model.Channel{clone}); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.InitChannelCache()
	// success
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": clone.Id}})
}

// MultiKeyManageRequest represents the request for multi-key management operations
type MultiKeyManageRequest struct {
	ChannelId int    `json:"channel_id"`
	Action    string `json:"action"`              // "disable_key", "enable_key", "delete_key", "delete_disabled_keys", "get_key_status"
	KeyIndex  *int   `json:"key_index,omitempty"` // for disable_key, enable_key, and delete_key actions
	Page      int    `json:"page,omitempty"`      // for get_key_status pagination
	PageSize  int    `json:"page_size,omitempty"` // for get_key_status pagination
	Status    *int   `json:"status,omitempty"`    // for get_key_status filtering: 1=enabled, 2=manual_disabled, 3=auto_disabled, nil=all
}

// MultiKeyStatusResponse represents the response for key status query
type MultiKeyStatusResponse struct {
	Keys       []KeyStatus `json:"keys"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
	// Statistics
	EnabledCount        int `json:"enabled_count"`
	ManualDisabledCount int `json:"manual_disabled_count"`
	AutoDisabledCount   int `json:"auto_disabled_count"`
}

type KeyStatus struct {
	Index        int    `json:"index"`
	Status       int    `json:"status"` // 1: enabled, 2: disabled
	DisabledTime int64  `json:"disabled_time,omitempty"`
	Reason       string `json:"reason,omitempty"`
	KeyPreview   string `json:"key_preview"` // first 10 chars of key for identification
}

// ManageMultiKeys handles multi-key management operations
// =============== P2P Channel Self-Service APIs (Phase 1) ===============

// limitedChannelFields defines the fields visible when querying channels by IDs that user doesn't own
var limitedChannelFields = []string{"id", "type", "name", "models", "group"}

// GetUserChannels lists all channels owned by the current user
// Supports query parameter channel_ids for querying specific channels (comma-separated)
// When channel_ids is specified and channels are not owned by user, only limited fields are returned
func GetUserChannels(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	channelIdsStr := c.Query("channel_ids")

	var channels []*model.Channel
	var total int64

	// If channel_ids is specified, query those specific channels
	if channelIdsStr != "" {
		// Parse comma-separated channel_ids
		var channelIds []int
		parts := strings.Split(channelIdsStr, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if id, err := strconv.Atoi(part); err == nil {
				channelIds = append(channelIds, id)
			}
		}

		if len(channelIds) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无效的channel_ids参数",
			})
			return
		}

		// Query channels by IDs
		query := model.DB.Model(&model.Channel{}).Where("id IN ?", channelIds)
		query.Count(&total)

		err := query.Order("id desc").
			Limit(pageInfo.GetPageSize()).
			Offset(pageInfo.GetStartIdx()).
			Find(&channels).Error

		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}

		// Filter channels and apply limited fields for non-owned channels
		type LimitedChannel struct {
			Id     int    `json:"id"`
			Type   int    `json:"type"`
			Name   string `json:"name"`
			Models string `json:"models"`
			Group  string `json:"group"`
		}

		var result []interface{}
		for _, ch := range channels {
			if ch.OwnerUserId == userId {
				// User owns this channel, return full info (without key)
				ch.Key = ""
				clearChannelInfo(ch)
				result = append(result, ch)
			} else {
				// User doesn't own this channel, return limited fields only
				result = append(result, LimitedChannel{
					Id:     ch.Id,
					Type:   ch.Type,
					Name:   ch.Name,
					Models: ch.Models,
					Group:  ch.Group,
				})
			}
		}

		common.ApiSuccess(c, gin.H{
			"items":     result,
			"total":     total,
			"page":      pageInfo.GetPage(),
			"page_size": pageInfo.GetPageSize(),
		})
		return
	}

	// Default behavior: Query channels owned by this user
	query := model.DB.Model(&model.Channel{}).Where("owner_user_id = ?", userId)
	query.Count(&total)

	err := query.Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Omit("key"). // Don't return API keys
		Find(&channels).Error

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Clear sensitive channel info
	for _, ch := range channels {
		clearChannelInfo(ch)
	}

	common.ApiSuccess(c, gin.H{
		"items":     channels,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

// GetUserChannel returns a single channel owned by the current user
// GET /api/channel/self/:id
func GetUserChannel(c *gin.Context) {
	userId := c.GetInt("id")
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(channelId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权查看此渠道",
		})
		return
	}

	// 清理多 key 运行时信息等敏感字段
	clearChannelInfo(channel)
	common.ApiSuccess(c, channel)
}

// CreateUserChannel creates a new channel owned by the current user
func CreateUserChannel(c *gin.Context) {
	userId := c.GetInt("id")

	// 读取原始请求体，兼容两种格式：
	// 1) 管理端使用的 AddChannelRequest 结构（包含 mode + channel）
	// 2) WQuant 侧透传的扁平结构（name/type/key/...）
	body, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 优先尝试解析为 AddChannelRequest 结构（兼容旧有调用方式）
	addChannelRequest := AddChannelRequest{}
	var channel *model.Channel
	mode := "single"

	if err := common.Unmarshal(body, &addChannelRequest); err == nil && addChannelRequest.Channel != nil {
		channel = addChannelRequest.Channel
		if addChannelRequest.Mode != "" {
			mode = addChannelRequest.Mode
		}
	} else {
		// 否则按 WQuant 扁平结构解析
		selfReq := SelfChannelCreateRequest{}
		if err := common.Unmarshal(body, &selfReq); err != nil {
			common.ApiError(c, err)
			return
		}
		ch, err := selfReq.ToModelChannel()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		channel = ch
	}

	// Set owner_user_id to current user
	channel.OwnerUserId = userId
	channel.CreatedTime = common.GetTimestamp()

	// 对自服务渠道，仅支持单密钥模式
	if mode != "single" && mode != "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "P2P渠道仅支持单密钥模式",
		})
		return
	}

	// P2P用户创建渠道时，必须选择分组且不能选择default分组
	if strings.TrimSpace(channel.Group) == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "必须选择分组",
		})
		return
	}
	// 检查是否包含default分组
	groups := strings.Split(channel.Group, ",")
	for _, g := range groups {
		if strings.TrimSpace(g) == "default" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "不能选择default分组",
			})
			return
		}
	}

	// Validate channel
	if err := validateChannel(channel, true); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Insert channel
	err = model.BatchInsertChannels([]model.Channel{*channel})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	service.ResetProxyClientCache()
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane p2p channel created: owner_user_id=%d channel_id=%d name=%q type=%d group=%q models=%q is_private=%t total_quota=%d concurrency=%d hourly_limit=%d daily_limit=%d",
			userId,
			channel.Id,
			channel.Name,
			channel.Type,
			channel.Group,
			channel.Models,
			channel.IsPrivate,
			channel.TotalQuota,
			channel.Concurrency,
			channel.HourlyLimit,
			channel.DailyLimit,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "渠道创建成功",
	})
}

// UpdateUserChannel updates a channel owned by the current user
func UpdateUserChannel(c *gin.Context) {
	userId := c.GetInt("id")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Check ownership
	existingChannel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if existingChannel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权修改此渠道",
		})
		return
	}

	// Parse update request
	// 说明：
	// - 这里使用 GetRawData + common.Unmarshal，是为了在绑定结构体前准确探测
	//   请求体中是否显式携带了 is_private 字段，避免 GORM 忽略 bool 零值导致
	//   无法将 is_private 从 true 更新为 false。
	rawBody, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel := PatchChannel{}
	if err := common.Unmarshal(rawBody, &channel); err != nil {
		common.ApiError(c, err)
		return
	}

	// 检测请求是否显式包含 is_private 字段，用于后续强制持久化 bool 值（包括 false）
	var isPrivateWrapper struct {
		IsPrivate *bool `json:"is_private"`
	}
	_ = json.Unmarshal(rawBody, &isPrivateWrapper)
	hasIsPrivate := isPrivateWrapper.IsPrivate != nil
	requestedIsPrivate := channel.IsPrivate

	// Ensure ID matches
	channel.Id = id

	// Validate channel
	if err := validateChannel(&channel.Channel, false); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Preserve ChannelInfo and owner_user_id
	originChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	channel.ChannelInfo = originChannel.ChannelInfo
	channel.OwnerUserId = originChannel.OwnerUserId // Cannot change owner

	// Update channel
	if err := channel.Update(); err != nil {
		common.ApiError(c, err)
		return
	}

	// 如果请求显式携带了 is_private，则确保该布尔字段能够从 true 正确更新为 false
	// 说明：
	// - GORM 使用 struct 进行 Updates 时会忽略 bool 的零值（false），导致
	//   无法把 is_private 从 true 更新为 false；
	// - 这里在通用 Update 之后，使用单列 Update 强制写入 is_private，且仅在客户端
	//   显式传入 is_private 时才执行，避免误把未传字段当作“清空”操作。
	if hasIsPrivate {
		if err := model.DB.Model(&model.Channel{}).
			Where("id = ?", channel.Id).
			Update("is_private", requestedIsPrivate).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		// 确保响应体中的 is_private 与请求保持一致
		channel.IsPrivate = requestedIsPrivate
	}

	model.InitChannelCache()
	service.ResetProxyClientCache()
	channel.Key = ""
	clearChannelInfo(&channel.Channel)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "渠道更新成功",
		"data":    channel,
	})
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane p2p channel updated: owner_user_id=%d channel_id=%d name=%q type=%d group=%q models=%q is_private=%t total_quota=%d concurrency=%d hourly_limit=%d daily_limit=%d",
			userId,
			channel.Id,
			channel.Name,
			channel.Type,
			channel.Group,
			channel.Models,
			channel.IsPrivate,
			channel.TotalQuota,
			channel.Concurrency,
			channel.HourlyLimit,
			channel.DailyLimit,
		))
	}
}

// DeleteUserChannel deletes a channel owned by the current user
func DeleteUserChannel(c *gin.Context) {
	userId := c.GetInt("id")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Check ownership
	existingChannel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if existingChannel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权删除此渠道",
		})
		return
	}

	// Delete channel
	channel := model.Channel{Id: id}
	err = channel.Delete()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	model.InitChannelCache()
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane p2p channel deleted: owner_user_id=%d channel_id=%d",
			userId, id,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "渠道删除成功",
	})
}

// =============== End of P2P Channel Self-Service APIs ===============

// ManageMultiKeys handles multi-key management operations
func ManageMultiKeys(c *gin.Context) {
	request := MultiKeyManageRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(request.ChannelId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道不存在",
		})
		return
	}

	if !channel.ChannelInfo.IsMultiKey {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该渠道不是多密钥模式",
		})
		return
	}

	lock := model.GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	switch request.Action {
	case "get_key_status":
		keys := channel.GetKeys()

		// Default pagination parameters
		page := request.Page
		pageSize := request.PageSize
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 50 // Default page size
		}

		// Statistics for all keys (unchanged by filtering)
		var enabledCount, manualDisabledCount, autoDisabledCount int

		// Build all key status data first
		var allKeyStatusList []KeyStatus
		for i, key := range keys {
			status := 1 // default enabled
			var disabledTime int64
			var reason string

			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = s
				}
			}

			// Count for statistics (all keys)
			switch status {
			case 1:
				enabledCount++
			case 2:
				manualDisabledCount++
			case 3:
				autoDisabledCount++
			}

			if status != 1 {
				if channel.ChannelInfo.MultiKeyDisabledTime != nil {
					disabledTime = channel.ChannelInfo.MultiKeyDisabledTime[i]
				}
				if channel.ChannelInfo.MultiKeyDisabledReason != nil {
					reason = channel.ChannelInfo.MultiKeyDisabledReason[i]
				}
			}

			// Create key preview (first 10 chars)
			keyPreview := key
			if len(key) > 10 {
				keyPreview = key[:10] + "..."
			}

			allKeyStatusList = append(allKeyStatusList, KeyStatus{
				Index:        i,
				Status:       status,
				DisabledTime: disabledTime,
				Reason:       reason,
				KeyPreview:   keyPreview,
			})
		}

		// Apply status filter if specified
		var filteredKeyStatusList []KeyStatus
		if request.Status != nil {
			for _, keyStatus := range allKeyStatusList {
				if keyStatus.Status == *request.Status {
					filteredKeyStatusList = append(filteredKeyStatusList, keyStatus)
				}
			}
		} else {
			filteredKeyStatusList = allKeyStatusList
		}

		// Calculate pagination based on filtered results
		filteredTotal := len(filteredKeyStatusList)
		totalPages := (filteredTotal + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		// Calculate range for current page
		start := (page - 1) * pageSize
		end := start + pageSize
		if end > filteredTotal {
			end = filteredTotal
		}

		// Get the page data
		var pageKeyStatusList []KeyStatus
		if start < filteredTotal {
			pageKeyStatusList = filteredKeyStatusList[start:end]
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": MultiKeyStatusResponse{
				Keys:                pageKeyStatusList,
				Total:               filteredTotal, // Total of filtered results
				Page:                page,
				PageSize:            pageSize,
				TotalPages:          totalPages,
				EnabledCount:        enabledCount,        // Overall statistics
				ManualDisabledCount: manualDisabledCount, // Overall statistics
				AutoDisabledCount:   autoDisabledCount,   // Overall statistics
			},
		})
		return

	case "disable_key":
		if request.KeyIndex == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "未指定要禁用的密钥索引",
			})
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "密钥索引超出范围",
			})
			return
		}

		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}

		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = 2 // disabled

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if common.ControlPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane multikey disable_key: channel_id=%d key_index=%d",
				channel.Id, keyIndex,
			))
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密钥已禁用",
		})
		return

	case "enable_key":
		if request.KeyIndex == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "未指定要启用的密钥索引",
			})
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "密钥索引超出范围",
			})
			return
		}

		// 从状态列表中删除该密钥的记录，使其回到默认启用状态
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		}

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if common.ControlPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane multikey enable_key: channel_id=%d key_index=%d",
				channel.Id, keyIndex,
			))
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密钥已启用",
		})
		return

	case "enable_all_keys":
		// 清空所有禁用状态，使所有密钥回到默认启用状态
		var enabledCount int
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			enabledCount = len(channel.ChannelInfo.MultiKeyStatusList)
		}

		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if common.ControlPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane multikey enable_all_keys: channel_id=%d enabled_count=%d",
				channel.Id, enabledCount,
			))
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已启用 %d 个密钥", enabledCount),
		})
		return

	case "disable_all_keys":
		// 禁用所有启用的密钥
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}

		var disabledCount int
		for i := 0; i < channel.ChannelInfo.MultiKeySize; i++ {
			status := 1 // default enabled
			if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
				status = s
			}

			// 只禁用当前启用的密钥
			if status == 1 {
				channel.ChannelInfo.MultiKeyStatusList[i] = 2 // disabled
				disabledCount++
			}
		}

		if disabledCount == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "没有可禁用的密钥",
			})
			return
		}

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if common.ControlPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane multikey disable_all_keys: channel_id=%d disabled_count=%d",
				channel.Id, disabledCount,
			))
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已禁用 %d 个密钥", disabledCount),
		})
		return

	case "delete_key":
		if request.KeyIndex == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "未指定要删除的密钥索引",
			})
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "密钥索引超出范围",
			})
			return
		}

		keys := channel.GetKeys()
		var remainingKeys []string
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)

		newIndex := 0
		for i, key := range keys {
			// 跳过要删除的密钥
			if i == keyIndex {
				continue
			}

			remainingKeys = append(remainingKeys, key)

			// 保留其他密钥的状态信息，重新索引
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if status, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists && status != 1 {
					newStatusList[newIndex] = status
				}
			}
			if channel.ChannelInfo.MultiKeyDisabledTime != nil {
				if t, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
					newDisabledTime[newIndex] = t
				}
			}
			if channel.ChannelInfo.MultiKeyDisabledReason != nil {
				if r, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
					newDisabledReason[newIndex] = r
				}
			}
			newIndex++
		}

		if len(remainingKeys) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "不能删除最后一个密钥",
			})
			return
		}

		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if common.ControlPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane multikey delete_key: channel_id=%d key_index=%d",
				channel.Id, keyIndex,
			))
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密钥已删除",
		})
		return

	case "delete_disabled_keys":
		keys := channel.GetKeys()
		var remainingKeys []string
		var deletedCount int
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)

		newIndex := 0
		for i, key := range keys {
			status := 1 // default enabled
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = s
				}
			}

			// 只删除自动禁用（status == 3）的密钥，保留启用（status == 1）和手动禁用（status == 2）的密钥
			if status == 3 {
				deletedCount++
			} else {
				remainingKeys = append(remainingKeys, key)
				// 保留非自动禁用密钥的状态信息，重新索引
				if status != 1 {
					newStatusList[newIndex] = status
					if channel.ChannelInfo.MultiKeyDisabledTime != nil {
						if t, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
							newDisabledTime[newIndex] = t
						}
					}
					if channel.ChannelInfo.MultiKeyDisabledReason != nil {
						if r, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
							newDisabledReason[newIndex] = r
						}
					}
				}
				newIndex++
			}
		}

		if deletedCount == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "没有需要删除的自动禁用密钥",
			})
			return
		}

		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		if common.ControlPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf(
				"control-plane multikey delete_disabled_keys: channel_id=%d deleted_count=%d",
				channel.Id, deletedCount,
			))
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已删除 %d 个自动禁用的密钥", deletedCount),
			"data":    deletedCount,
		})
		return

	default:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "不支持的操作",
		})
		return
	}
}
