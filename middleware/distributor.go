package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type ModelRequest struct {
	Model string `json:"model"`
	Group string `json:"group,omitempty"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		var channel *model.Channel
		var selectGroup string
		channelId, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "Invalid request, "+err.Error())
			return
		}

		sessionID := extractSessionID(c)
		if sessionID != "" {
			common.SetContextKey(c, constant.ContextKeySessionID, sessionID)
		}
		userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		var sessionEntry *service.SessionIndexEntry
		bindingKey := ""
		if sessionID != "" && modelRequest.Model != "" && userId != 0 {
			bindingKey = service.BuildSessionBindingKey(userId, modelRequest.Model, sessionID)
			common.SetContextKey(c, constant.ContextKeySessionBindingKey, bindingKey)
			entry, getErr := service.GetSessionBinding(c.Request.Context(), userId, modelRequest.Model, sessionID)
			if getErr != nil {
				logger.LogWarn(c, fmt.Sprintf("failed to load session binding %s: %v", bindingKey, getErr))
			} else {
				sessionEntry = entry
			}
		}
		usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		var routingGroups []string

		if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			if channel.Status != common.ChannelStatusEnabled {
				abortWithOpenAiMessage(c, http.StatusForbidden, "该渠道已被禁用")
				return
			}
		} else {
			// Select a channel for the user
			// check token model mapping
			modelLimitEnable := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
			if modelLimitEnable {
				s, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
				if !ok {
					// token model limit is empty, all models are not allowed
					abortWithOpenAiMessage(c, http.StatusForbidden, "该令牌无权访问任何模型")
					return
				}
				var tokenModelLimit map[string]bool
				tokenModelLimit, ok = s.(map[string]bool)
				if !ok {
					tokenModelLimit = map[string]bool{}
				}
				matchName := ratio_setting.FormatMatchingModelName(modelRequest.Model) // match gpts & thinking-*
				if _, ok := tokenModelLimit[matchName]; !ok {
					abortWithOpenAiMessage(c, http.StatusForbidden, "该令牌无权访问模型 "+modelRequest.Model)
					return
				}
			}

			if shouldSelectChannel {
				if modelRequest.Model == "" {
					abortWithOpenAiMessage(c, http.StatusBadRequest, "未指定模型名称，模型名称不能为空")
					return
				}
				// check path is /pg/chat/completions
				if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
					playgroundRequest := &dto.PlayGroundRequest{}
					err = common.UnmarshalBodyReusable(c, playgroundRequest)
					if err != nil {
						abortWithOpenAiMessage(c, http.StatusBadRequest, "无效的playground请求, "+err.Error())
						return
					}
					if playgroundRequest.Group != "" {
						if !service.GroupInUserUsableGroups(usingGroup, playgroundRequest.Group) && playgroundRequest.Group != usingGroup {
							abortWithOpenAiMessage(c, http.StatusForbidden, "无权访问该分组")
							return
						}
						usingGroup = playgroundRequest.Group
						common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
					}
				}
				// 计算 RoutingGroups (支持 P2P 分组多分组选路)
				routingGroups = ComputeRoutingGroups(c, usingGroup)
			}
		}

		if sessionEntry != nil {
			expectedChannelID := 0
			if channel != nil {
				expectedChannelID = channel.Id
			}
			boundChannel, forcedKey, forcedIndex, valid := validateSessionBinding(c, sessionEntry, expectedChannelID)
			if valid {
				if channel == nil {
					channel = boundChannel
				}
				shouldSelectChannel = false
				common.SetContextKey(c, constant.ContextKeyChannelForcedKey, forcedKey)
				common.SetContextKey(c, constant.ContextKeyChannelForcedKeyIndex, forcedIndex)
				common.SetContextKey(c, constant.ContextKeyStickyChannelId, boundChannel.Id)
				common.SetContextKey(c, constant.ContextKeySessionBindingHit, true)
				common.SetContextKey(c, constant.ContextKeySessionIsNew, false)
				if sessionEntry.Group != "" {
					common.SetContextKey(c, constant.ContextKeySessionSelectedGroup, sessionEntry.Group)
					selectGroup = sessionEntry.Group
				}
			} else {
				_, _ = service.RemoveSessionBinding(c.Request.Context(), bindingKey)
				sessionEntry = nil
			}
		}

		if sessionID != "" && sessionEntry == nil {
			common.SetContextKey(c, constant.ContextKeySessionIsNew, true)
		}

		if channel == nil && shouldSelectChannel {
			if sessionID != "" && sessionEntry == nil {
				limit := service.GetEffectiveSessionLimit(
					common.GetContextKeyInt(c, constant.ContextKeyUserMaxConcurrentSessions),
					common.GetContextKeyString(c, constant.ContextKeyUserGroup),
				)
				if limit > 0 {
					count, countErr := service.GetUserSessionCount(c.Request.Context(), userId)
					if countErr != nil {
						logger.LogWarn(c, fmt.Sprintf("failed to get user session count: %v", countErr))
					} else if count >= int64(limit) {
						abortWithOpenAiMessage(c, http.StatusTooManyRequests, "当前并发会话数已达上限")
						return
					}
				}
			}

			if len(routingGroups) == 0 {
				routingGroups = ComputeRoutingGroups(c, usingGroup)
			}

			channel, selectGroup, err = service.CacheGetRandomSatisfiedChannelMultiGroup(c, routingGroups, modelRequest.Model, 0)
			if err != nil {
				showGroup := usingGroup
				if usingGroup == "auto" {
					showGroup = fmt.Sprintf("auto(%s)", selectGroup)
				}
				message := fmt.Sprintf("获取分组 %s 下模型 %s 的可用渠道失败（distributor）: %s", showGroup, modelRequest.Model, err.Error())
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable, message, string(types.ErrorCodeModelNotFound))
				return
			}
			if channel == nil {
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable, fmt.Sprintf("分组 %s 下模型 %s 无可用渠道（distributor）", usingGroup, modelRequest.Model), string(types.ErrorCodeModelNotFound))
				return
			}
		}

		if selectGroup != "" {
			common.SetContextKey(c, constant.ContextKeySessionSelectedGroup, selectGroup)
		}

		if channel == nil {
			abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "未找到可用渠道（distributor）", string(types.ErrorCodeModelNotFound))
			return
		}

		// P2P Channel Concurrency Tracking (Phase 1)
		// Note: Risk control checks (quota, rate limits) are now performed during channel selection
		// Here we only track concurrency and request counts for the selected P2P channel
		if channel != nil && channel.OwnerUserId != 0 {
			// Increment concurrency counter before processing request
			model.IncrementChannelConcurrency(channel.Id)
			// Increment request counter
			model.IncrementChannelRequest(channel.Id)
			// Ensure decrement on request completion
			defer model.DecrementChannelConcurrency(channel.Id)
		}

		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		SetupContextForSelectedChannel(c, channel, modelRequest.Model)
		c.Next()
	}
}

// getModelFromRequest 从请求中读取模型信息
// 根据 Content-Type 自动处理：
// - application/json
// - application/x-www-form-urlencoded
// - multipart/form-data
func getModelFromRequest(c *gin.Context) (*ModelRequest, error) {
	var modelRequest ModelRequest
	err := common.UnmarshalBodyReusable(c, &modelRequest)
	if err != nil {
		return nil, errors.New("无效的请求, " + err.Error())
	}
	return &modelRequest, nil
}

func getModelRequest(c *gin.Context) (*ModelRequest, bool, error) {
	var modelRequest ModelRequest
	shouldSelectChannel := true
	var err error
	if strings.Contains(c.Request.URL.Path, "/mj/") {
		relayMode := relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeMidjourneyTaskFetch ||
			relayMode == relayconstant.RelayModeMidjourneyTaskFetchByCondition ||
			relayMode == relayconstant.RelayModeMidjourneyNotify ||
			relayMode == relayconstant.RelayModeMidjourneyTaskImageSeed {
			shouldSelectChannel = false
		} else {
			midjourneyRequest := dto.MidjourneyRequest{}
			err = common.UnmarshalBodyReusable(c, &midjourneyRequest)
			if err != nil {
				return nil, false, errors.New("无效的midjourney请求, " + err.Error())
			}
			midjourneyModel, mjErr, success := service.GetMjRequestModel(relayMode, &midjourneyRequest)
			if mjErr != nil {
				return nil, false, fmt.Errorf(mjErr.Description)
			}
			if midjourneyModel == "" {
				if !success {
					return nil, false, fmt.Errorf("无效的请求, 无法解析模型")
				} else {
					// task fetch, task fetch by condition, notify
					shouldSelectChannel = false
				}
			}
			modelRequest.Model = midjourneyModel
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/suno/") {
		relayMode := relayconstant.Path2RelaySuno(c.Request.Method, c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeSunoFetch ||
			relayMode == relayconstant.RelayModeSunoFetchByID {
			shouldSelectChannel = false
		} else {
			modelName := service.CoverTaskActionToModelName(constant.TaskPlatformSuno, c.Param("action"))
			modelRequest.Model = modelName
		}
		c.Set("platform", string(constant.TaskPlatformSuno))
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos") {
		//curl https://api.openai.com/v1/videos \
		//  -H "Authorization: Bearer $OPENAI_API_KEY" \
		//  -F "model=sora-2" \
		//  -F "prompt=A calico cat playing a piano on stage"
		//	-F input_reference="@image.jpg"
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			relayMode = relayconstant.RelayModeVideoSubmit
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			if req != nil {
				modelRequest.Model = req.Model
			}
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/video/generations") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
		}
		if _, ok := c.Get("relay_mode"); !ok {
			c.Set("relay_mode", relayMode)
		}
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") || strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		// Gemini API 路径处理: /v1beta/models/gemini-2.0-flash:generateContent
		relayMode := relayconstant.RelayModeGemini
		modelName := extractModelNameFromGeminiPath(c.Request.URL.Path)
		if modelName != "" {
			modelRequest.Model = modelName
		}
		c.Set("relay_mode", relayMode)
	} else if !strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") && !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") {
		//wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01
		modelRequest.Model = c.Query("model")
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
		if modelRequest.Model == "" {
			modelRequest.Model = "text-moderation-stable"
		}
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		if modelRequest.Model == "" {
			modelRequest.Model = c.Param("model")
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
		modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "dall-e")
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		//modelRequest.Model = common.GetStringIfEmpty(c.PostForm("model"), "gpt-image-1")
		contentType := c.ContentType()
		if slices.Contains([]string{gin.MIMEPOSTForm, gin.MIMEMultipartPOSTForm}, contentType) {
			req, err := getModelFromRequest(c)
			if err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		relayMode := relayconstant.RelayModeAudioSpeech
		if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/speech") {

			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "tts-1")
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranslation
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranscription
		}
		c.Set("relay_mode", relayMode)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
		// playground chat completions
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
		modelRequest.Group = req.Group
		common.SetContextKey(c, constant.ContextKeyTokenGroup, modelRequest.Group)
	}
	return &modelRequest, shouldSelectChannel, nil
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	c.Set("original_model", modelName) // for retry
	if channel == nil {
		return types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, channel.GetParamOverride())
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, channel.GetHeaderOverride())
	if nil != channel.OpenAIOrganization && *channel.OpenAIOrganization != "" {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())

	forcedKey := common.GetContextKeyString(c, constant.ContextKeyChannelForcedKey)
	forcedIndex := common.GetContextKeyInt(c, constant.ContextKeyChannelForcedKeyIndex)
	stickyChannelId := common.GetContextKeyInt(c, constant.ContextKeyStickyChannelId)

	// Derive AccountHint for CLIProxyAPI channels.
	// 优先使用独立的 account_hint 字段；若为空且为 CLIProxy 渠道，则尝试从 channel.Other JSON 中回退解析。
	if channel.Type == constant.ChannelTypeCliProxy {
		accountHint := ""
		if channel.AccountHint != nil && *channel.AccountHint != "" {
			accountHint = *channel.AccountHint
		} else if channel.Other != "" {
			var other map[string]interface{}
			if err := common.Unmarshal([]byte(channel.Other), &other); err != nil {
				// 仅日志提醒配置/数据不一致，不阻塞请求
				logger.LogWarn(c, fmt.Sprintf(
					"CLIProxyAPI channel other field is not valid JSON when parsing account_hint: channel_id=%d name=%q error=%v",
					channel.Id, channel.Name, err,
				))
			} else {
				if v, ok := other["account_hint"]; ok {
					if s, ok2 := v.(string); ok2 && strings.TrimSpace(s) != "" {
						accountHint = strings.TrimSpace(s)
					}
				}
			}
		}
		if accountHint != "" {
			common.SetContextKey(c, constant.ContextKeyChannelAccountHint, accountHint)
		}

		// 如果 CLIProxy 渠道的 key 为空，提前打日志，便于快速定位 401 问题（CLIProxy API Key 未配置）。
		if channel.Key == "" {
			logger.LogWarn(c, fmt.Sprintf(
				"CLIProxyAPI channel selected with empty API key: channel_id=%d name=%q base_url=%q account_hint=%q model=%q – this will cause 401 Unauthorized from CLIProxyAPI, please set channel.key to a value in CLIProxy config.yaml api-keys",
				channel.Id,
				channel.Name,
				common.MaskSensitiveInfo(channel.GetBaseURL()),
				accountHint,
				modelName,
			))
		}
	}

	key := ""
	index := 0
	useStickyKey := stickyChannelId != 0 && stickyChannelId == channel.Id
	if useStickyKey && forcedKey != "" {
		key = forcedKey
		index = forcedIndex
	} else if useStickyKey && forcedIndex >= 0 {
		var newAPIError *types.NewAPIError
		key, newAPIError = channel.GetKeyByIndex(forcedIndex)
		if newAPIError != nil {
			return newAPIError
		}
		index = forcedIndex
	} else {
		var newAPIError *types.NewAPIError
		key, index, newAPIError = channel.GetNextEnabledKey()
		if newAPIError != nil {
			return newAPIError
		}
	}
	if channel.ChannelInfo.IsMultiKey {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		// 必须设置为 false，否则在重试到单个 key 的时候会导致日志显示错误
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	}
	// c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	// TODO: api_version统一
	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
	}
	return nil
}

func validateSessionBinding(c *gin.Context, entry *service.SessionIndexEntry, expectedChannelID int) (*model.Channel, string, int, bool) {
	if entry == nil {
		return nil, "", 0, false
	}

	if expectedChannelID != 0 && entry.ChannelID != expectedChannelID {
		return nil, "", 0, false
	}

	channel, err := model.CacheGetChannel(entry.ChannelID)
	if err != nil || channel == nil {
		channel, err = model.GetChannelById(entry.ChannelID, true)
		if err != nil {
			logger.LogWarn(c, fmt.Sprintf("session binding channel %d not found: %v", entry.ChannelID, err))
			return nil, "", 0, false
		}
	}

	if channel.Status != common.ChannelStatusEnabled {
		return nil, "", 0, false
	}

	if err := model.CheckChannelRiskControl(channel, 0); err != nil {
		logger.LogWarn(c, fmt.Sprintf("session binding channel %d failed risk control: %v", channel.Id, err))
		return nil, "", 0, false
	}

	if !channelSupportsModel(channel, entry.Model) {
		logger.LogWarn(c, fmt.Sprintf("session binding model mismatch: channel_id=%d model=%s", channel.Id, entry.Model))
		return nil, "", 0, false
	}

	forcedKey := channel.Key
	forcedIndex := entry.KeyID
	if channel.ChannelInfo.IsMultiKey {
		key, apiErr := channel.GetKeyByIndex(entry.KeyID)
		if apiErr != nil {
			logger.LogWarn(c, fmt.Sprintf("session binding key invalid: channel_id=%d key_id=%d err=%v", channel.Id, entry.KeyID, apiErr))
			return nil, "", 0, false
		}
		forcedKey = key
	} else {
		forcedIndex = 0
	}

	if entry.KeyHash != "" && common.Sha1([]byte(forcedKey)) != entry.KeyHash {
		logger.LogWarn(c, fmt.Sprintf("session binding key hash mismatch: channel_id=%d key_id=%d", channel.Id, forcedIndex))
		return nil, "", 0, false
	}

	return channel, forcedKey, forcedIndex, true
}

func extractSessionID(c *gin.Context) string {
	headerKeys := []string{
		"X-NewAPI-Session-ID",
		"Session-ID",
		"Conversation-ID",
		"X-Gemini-Api-Privileged-User-Id",
	}
	for _, key := range headerKeys {
		if val := strings.TrimSpace(c.GetHeader(key)); val != "" {
			return val
		}
	}

	if val := strings.TrimSpace(c.Query("session_id")); val != "" {
		return val
	}

	body, err := common.GetRequestBody(c)
	if err == nil && len(body) > 0 {
		payload := map[string]interface{}{}
		if err := common.Unmarshal(body, &payload); err == nil {
			if val := pickSessionField(payload, "session_id", "conversation_id", "chat_id"); val != "" {
				return val
			}
			if meta, ok := payload["metadata"].(map[string]interface{}); ok {
				if val := pickSessionField(meta, "session_id", "conversation_id"); val != "" {
					return val
				}
			}
		}
	}

	return ""
}

func pickSessionField(payload map[string]interface{}, keys ...string) string {
	if payload == nil {
		return ""
	}
	lower := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		lower[strings.ToLower(k)] = v
	}
	for _, key := range keys {
		if v, ok := lower[strings.ToLower(key)]; ok {
			if s, ok2 := v.(string); ok2 {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func channelSupportsModel(channel *model.Channel, modelName string) bool {
	if modelName == "" {
		return false
	}
	target := strings.TrimSpace(modelName)
	targetNormalized := ratio_setting.FormatMatchingModelName(target)
	models := channel.GetModels()
	for _, m := range models {
		mTrim := strings.TrimSpace(m)
		if mTrim == "" {
			continue
		}
		if mTrim == target || ratio_setting.FormatMatchingModelName(mTrim) == targetNormalized {
			return true
		}
	}
	return false
}

// extractModelNameFromGeminiPath 从 Gemini API URL 路径中提取模型名
// 输入格式: /v1beta/models/gemini-2.0-flash:generateContent
// 输出: gemini-2.0-flash
func extractModelNameFromGeminiPath(path string) string {
	// 查找 "/models/" 的位置
	modelsPrefix := "/models/"
	modelsIndex := strings.Index(path, modelsPrefix)
	if modelsIndex == -1 {
		return ""
	}

	// 从 "/models/" 之后开始提取
	startIndex := modelsIndex + len(modelsPrefix)
	if startIndex >= len(path) {
		return ""
	}

	// 查找 ":" 的位置，模型名在 ":" 之前
	colonIndex := strings.Index(path[startIndex:], ":")
	if colonIndex == -1 {
		// 如果没有找到 ":"，返回从 "/models/" 到路径结尾的部分
		return path[startIndex:]
	}

	// 返回模型名部分
	return path[startIndex : startIndex+colonIndex]
}

// ComputeRoutingGroups 计算用户在当前请求中的路由分组集合
// RoutingGroups = {BillingGroup (或 usingGroup)} ∪ {用户的 Active P2P 分组}
// 此函数复用 relay_info.go 中的逻辑，确保选路与计费使用相同的分组计算规则
func ComputeRoutingGroups(c *gin.Context, usingGroup string) []string {
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)

	// Step 1: 确定 BillingGroup (与 relay_info.go 保持一致)
	billingGroup := usingGroup
	if billingGroup == "" {
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		billingGroup = userGroup
	}

	// Step 2: 获取用户的 Active P2P 分组 ID
	var userP2PGroupIDs []int
	userP2PGroupIDs, err := model.GetUserActiveGroups(userId, false)
	if err != nil {
		// P2P 分组获取失败不阻塞请求，仅记录日志
		common.SysLog(fmt.Sprintf("failed to get user P2P groups for user %d in distributor: %v", userId, err))
		userP2PGroupIDs = []int{}
	}

	// Step 3: 应用 Token 的 P2P 分组限制 (取交集)
	var effectiveP2PGroupIDs []int
	tokenAllowedP2PGroupIDs, exists := c.Get(string(constant.ContextKeyTokenAllowedP2PGroups))
	if !exists || tokenAllowedP2PGroupIDs == nil {
		effectiveP2PGroupIDs = userP2PGroupIDs
	} else {
		tokenP2PList, ok := tokenAllowedP2PGroupIDs.([]int)
		if !ok {
			common.SysLog(fmt.Sprintf("token_allowed_p2p_groups type assertion failed for user %d in distributor", userId))
			effectiveP2PGroupIDs = userP2PGroupIDs
		} else {
			allowedMap := make(map[int]bool)
			for _, id := range tokenP2PList {
				allowedMap[id] = true
			}
			for _, groupID := range userP2PGroupIDs {
				if allowedMap[groupID] {
					effectiveP2PGroupIDs = append(effectiveP2PGroupIDs, groupID)
				}
			}
		}
	}

	// Step 4: 构建 RoutingGroups
	routingGroups := []string{billingGroup}

	// 将 P2P 分组 ID 转换为 "p2p_{id}" 格式
	for _, groupID := range effectiveP2PGroupIDs {
		routingGroups = append(routingGroups, fmt.Sprintf("p2p_%d", groupID))
	}

	// 去重
	uniqueGroups := make([]string, 0, len(routingGroups))
	groupMap := make(map[string]bool)
	for _, group := range routingGroups {
		if !groupMap[group] {
			groupMap[group] = true
			uniqueGroups = append(uniqueGroups, group)
		}
	}

	return uniqueGroups
}
