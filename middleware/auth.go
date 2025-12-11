package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func validUserInfo(username string, role int) bool {
	// check username is empty
	if strings.TrimSpace(username) == "" {
		return false
	}
	if !common.IsValidateRole(role) {
		return false
	}
	return true
}

func authHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	username := session.Get("username")
	role := session.Get("role")
	id := session.Get("id")
	status := session.Get("status")
	useAccessToken := false
	if username == nil {
		// Check access token
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，未登录且未提供 access token",
			})
			c.Abort()
			return
		}
		user := model.ValidateAccessToken(accessToken)
		if user != nil && user.Username != "" {
			if !validUserInfo(user.Username, user.Role) {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "无权进行此操作，用户信息无效",
				})
				c.Abort()
				return
			}
			// Token is valid
			username = user.Username
			role = user.Role
			id = user.Id
			status = user.Status
			useAccessToken = true
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无权进行此操作，access token 无效",
			})
			c.Abort()
			return
		}
	}
	// get header New-Api-User
	apiUserIdStr := c.Request.Header.Get("New-Api-User")
	if apiUserIdStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，未提供 New-Api-User",
		})
		c.Abort()
		return
	}
	apiUserId, err := strconv.Atoi(apiUserIdStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，New-Api-User 格式错误",
		})
		c.Abort()
		return

	}
	if id != apiUserId {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，New-Api-User 与登录用户不匹配",
		})
		c.Abort()
		return
	}
	if status.(int) == common.UserStatusDisabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户已被封禁",
		})
		c.Abort()
		return
	}
	if role.(int) < minRole {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权进行此操作，权限不足",
		})
		c.Abort()
		return
	}
	if !validUserInfo(username.(string), role.(int)) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权进行此操作，用户信息无效",
		})
		c.Abort()
		return
	}
	c.Set("username", username)
	c.Set("role", role)
	c.Set("id", id)
	c.Set("group", session.Get("group"))
	c.Set("user_group", session.Get("group"))
	c.Set("use_access_token", useAccessToken)

	//userCache, err := model.GetUserCache(id.(int))
	//if err != nil {
	//	c.JSON(http.StatusOK, gin.H{
	//		"success": false,
	//		"message": err.Error(),
	//	})
	//	c.Abort()
	//	return
	//}
	//userCache.WriteContext(c)

	c.Next()
}

func TryUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := session.Get("id")
		if id != nil {
			c.Set("id", id)
		}
		c.Next()
	}
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

func WssAuth(c *gin.Context) {

}

func TokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 先检测是否为ws
		if c.Request.Header.Get("Sec-WebSocket-Protocol") != "" {
			// Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.sk-xxx, openai-beta.realtime-v1
			// read sk from Sec-WebSocket-Protocol
			key := c.Request.Header.Get("Sec-WebSocket-Protocol")
			parts := strings.Split(key, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "openai-insecure-api-key") {
					key = strings.TrimPrefix(part, "openai-insecure-api-key.")
					break
				}
			}
			c.Request.Header.Set("Authorization", "Bearer "+key)
		}
		// 检查path包含/v1/messages
		if strings.Contains(c.Request.URL.Path, "/v1/messages") {
			anthropicKey := c.Request.Header.Get("x-api-key")
			if anthropicKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+anthropicKey)
			}
		}
		// gemini api 从query中获取key
		if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/openai/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
			skKey := c.Query("key")
			if skKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+skKey)
			}
			// 从x-goog-api-key header中获取key
			xGoogKey := c.Request.Header.Get("x-goog-api-key")
			if xGoogKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+xGoogKey)
			}
		}
		key := c.Request.Header.Get("Authorization")
		parts := make([]string, 0)
		key = strings.TrimPrefix(key, "Bearer ")
		if key == "" || key == "midjourney-proxy" {
			key = c.Request.Header.Get("mj-api-secret")
			key = strings.TrimPrefix(key, "Bearer ")
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		} else {
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		}
		token, err := model.ValidateUserToken(key)
		if token != nil {
			id := c.GetInt("id")
			if id == 0 {
				c.Set("id", token.UserId)
			}
		}
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusUnauthorized, err.Error())
			return
		}

		allowIpsMap := token.GetIpLimitsMap()
		if len(allowIpsMap) != 0 {
			clientIp := c.ClientIP()
			if _, ok := allowIpsMap[clientIp]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, "您的 IP 不在令牌允许访问的列表中")
				return
			}
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, err.Error())
			return
		}
		userEnabled := userCache.Status == common.UserStatusEnabled
		if !userEnabled {
			abortWithOpenAiMessage(c, http.StatusForbidden, "用户已被封禁")
			return
		}

		userCache.WriteContext(c)

		userGroup := userCache.Group
		tokenGroup := token.Group

		// 如果 Token.Group 是 JSON 数组格式（例如 ["vip","default"] 或 []），则只通过
		// billingGroupList 分支处理，不再走兼容的单字符串逻辑。特别地：
		//   - Token.Group="[]" 表示“显式配置为空列表”，应当回退到用户默认分组，
		//     而不是当作字符串 "[]" 参与权限校验。
		trimmedTokenGroup := strings.TrimSpace(tokenGroup)
		if strings.HasPrefix(trimmedTokenGroup, "[") && strings.HasSuffix(trimmedTokenGroup, "]") {
			tokenGroup = ""
		}

		// 解析 Token 的计费分组列表 (支持 JSON 数组或单字符串)
		billingGroupList := token.GetBillingGroupList()

		if len(billingGroupList) > 0 {
			// Token 配置了计费分组列表，需要校验所有分组的权限。
			// 这里同时支持 "auto" 语义：当列表中出现 "auto" 时，
			// 将其展开为针对当前用户的自动计费分组集合（GetUserAutoGroup），
			// 从而在后续多计费组选路中按顺序遍历实际分组。
			userUsableGroups := service.GetUserUsableGroups(userGroup)
			expandedBillingList := make([]string, 0, len(billingGroupList))

			for _, bg := range billingGroupList {
				if bg == "auto" {
					// "auto" 本身不是实际计费分组，而是根据系统配置与用户可用分组
					// 展开的「候选计费组列表」。
					autoGroups := service.GetUserAutoGroup(userGroup)
					if len(autoGroups) == 0 {
						// 当前环境未为该用户配置任何 auto 可用分组，视为无可用计费组。
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable,
							"no available channel in billing groups [auto]", string(types.ErrorCodeModelNotFound))
						return
					}
					expandedBillingList = append(expandedBillingList, autoGroups...)
					continue
				}

				if _, ok := userUsableGroups[bg]; !ok {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", bg))
					return
				}
				// 检查分组是否已弃用
				if !ratio_setting.ContainsGroupRatio(bg) {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", bg))
					return
				}
				expandedBillingList = append(expandedBillingList, bg)
			}

			if len(expandedBillingList) == 0 {
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable,
					"no available channel in billing groups []", string(types.ErrorCodeModelNotFound))
				return
			}

			// 将展开后的计费分组列表存入 Context，供后续路由逻辑使用
			common.SetContextKey(c, constant.ContextKeyTokenBillingGroupList, expandedBillingList)
			// UsingGroup 使用列表中的第一个分组（优先级最高的分组）
			userGroup = expandedBillingList[0]
		} else if tokenGroup != "" {
			// 兼容旧逻辑：Token.Group 为单字符串（非 JSON 数组格式）
			if _, ok := service.GetUserUsableGroups(userGroup)[tokenGroup]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", tokenGroup))
				return
			}
			if !ratio_setting.ContainsGroupRatio(tokenGroup) {
				if tokenGroup != "auto" {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", tokenGroup))
					return
				}
			}
			userGroup = tokenGroup
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)

		err = SetupContextForToken(c, token, parts...)
		if err != nil {
			return
		}
		c.Next()
	}
}

func SetupContextForToken(c *gin.Context, token *model.Token, parts ...string) error {
	if token == nil {
		return fmt.Errorf("token is nil")
	}
	c.Set("id", token.UserId)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	if token.ModelLimitsEnabled {
		c.Set("token_model_limit_enabled", true)
		c.Set("token_model_limit", token.GetModelLimitsMap())
	} else {
		c.Set("token_model_limit_enabled", false)
	}
	c.Set("token_group", token.Group)
	// 解析 Token 的 P2P 分组限制
	allowedP2PGroupIDs := token.GetAllowedP2PGroupIDs()
	if len(allowedP2PGroupIDs) > 0 {
		common.SetContextKey(c, constant.ContextKeyTokenAllowedP2PGroups, allowedP2PGroupIDs)
	}
	if len(parts) > 1 {
		if model.IsAdmin(token.UserId) {
			c.Set("specific_channel_id", parts[1])
		} else {
			abortWithOpenAiMessage(c, http.StatusForbidden, "普通用户不支持指定渠道")
			return fmt.Errorf("普通用户不支持指定渠道")
		}
	}
	return nil
}
