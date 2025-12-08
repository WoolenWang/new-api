package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// validateBillingGroupList 校验 Token.Group 字段的格式
// 支持的格式:
// 1. 空字符串 - 合法,使用用户默认分组
// 2. 单字符串 - 合法,如 "default" 或 "vip"
// 3. JSON数组 - 合法,如 ["svip", "default"]
// 返回错误信息,nil表示校验通过
func validateBillingGroupList(group string) error {
	if group == "" {
		return nil
	}

	trimmed := strings.TrimSpace(group)

	// 检查是否为 JSON 数组格式
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		var groups []string
		if err := json.Unmarshal([]byte(trimmed), &groups); err != nil {
			return fmt.Errorf("计费分组列表格式错误: 无效的JSON数组 (%v)", err)
		}
		// 检查数组元素
		for i, g := range groups {
			if strings.TrimSpace(g) == "" {
				return fmt.Errorf("计费分组列表格式错误: 第%d个分组名称不能为空", i+1)
			}
		}
		return nil
	}

	// 单字符串格式 - 检查是否为有效的分组名称（不含特殊字符）
	if strings.ContainsAny(trimmed, "[]{}\"'") {
		return fmt.Errorf("计费分组格式错误: 无效的分组名称")
	}

	return nil
}

func GetAllTokens(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	tokens, err := model.GetAllUserTokens(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	total, _ := model.CountUserTokens(userId)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tokens)
	common.ApiSuccess(c, pageInfo)
	return
}

func SearchTokens(c *gin.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")
	tokens, err := model.SearchUserTokens(userId, keyword, token)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    tokens,
	})
	return
}

func GetToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    token,
	})
	return
}

func GetTokenStatus(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := model.GetTokenByIds(tokenId, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"object":          "credit_summary",
		"total_granted":   token.RemainQuota,
		"total_used":      0, // not supported currently
		"total_available": token.RemainQuota,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, err := model.GetTokenByKey(strings.TrimPrefix(tokenKey, "sk-"), false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": gin.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        token.RemainQuota + token.UsedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"expires_at":           expiredAt,
		},
	})
}

func AddToken(c *gin.Context) {
	token := model.Token{}
	err := c.ShouldBindJSON(&token)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(token.Name) > 30 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "令牌名称过长",
		})
		return
	}
	// 校验计费分组列表格式
	if err := validateBillingGroupList(token.Group); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	key, err := common.GenerateKey()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "生成令牌失败",
		})
		common.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cleanToken := model.Token{
		UserId:             c.GetInt("id"),
		Name:               token.Name,
		Key:                key,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		AllowIps:           token.AllowIps,
		Group:              token.Group,
		P2PGroupID:         token.P2PGroupID, // 唯一允许访问的P2P分组ID
	}
	err = cleanToken.Insert()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane token created: user_id=%d token_id=%d name=%q group=%q p2p_group_id=%v remain_quota=%d unlimited=%t model_limits_enabled=%t allow_ips=%v",
			cleanToken.UserId,
			cleanToken.Id,
			cleanToken.Name,
			cleanToken.Group,
			cleanToken.P2PGroupID,
			cleanToken.RemainQuota,
			cleanToken.UnlimitedQuota,
			cleanToken.ModelLimitsEnabled,
			cleanToken.AllowIps,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	err := model.DeleteTokenById(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane token deleted: user_id=%d token_id=%d",
			userId, id,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateToken(c *gin.Context) {
	userId := c.GetInt("id")
	statusOnly := c.Query("status_only")
	token := model.Token{}
	err := c.ShouldBindJSON(&token)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(token.Name) > 30 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "令牌名称过长",
		})
		return
	}
	// 校验计费分组列表格式 (仅在非状态更新模式下校验)
	if statusOnly == "" {
		if err := validateBillingGroupList(token.Group); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	cleanToken, err := model.GetTokenByIds(token.Id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if token.Status == common.TokenStatusEnabled {
		if cleanToken.Status == common.TokenStatusExpired && cleanToken.ExpiredTime <= common.GetTimestamp() && cleanToken.ExpiredTime != -1 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "令牌已过期，无法启用，请先修改令牌过期时间，或者设置为永不过期",
			})
			return
		}
		if cleanToken.Status == common.TokenStatusExhausted && cleanToken.RemainQuota <= 0 && !cleanToken.UnlimitedQuota {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "令牌可用额度已用尽，无法启用，请先修改令牌剩余额度，或者设置为无限额度",
			})
			return
		}
	}
	if statusOnly != "" {
		cleanToken.Status = token.Status
	} else {
		// If you add more fields, please also update token.Update()
		cleanToken.Name = token.Name
		cleanToken.ExpiredTime = token.ExpiredTime
		cleanToken.RemainQuota = token.RemainQuota
		cleanToken.UnlimitedQuota = token.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
		cleanToken.ModelLimits = token.ModelLimits
		cleanToken.AllowIps = token.AllowIps
		cleanToken.Group = token.Group
		cleanToken.P2PGroupID = token.P2PGroupID // 唯一允许访问的P2P分组ID
	}
	err = cleanToken.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane token updated: user_id=%d token_id=%d status=%d remain_quota=%d unlimited=%t group=%q p2p_group_id=%v model_limits_enabled=%t allow_ips=%v",
			userId,
			cleanToken.Id,
			cleanToken.Status,
			cleanToken.RemainQuota,
			cleanToken.UnlimitedQuota,
			cleanToken.Group,
			cleanToken.P2PGroupID,
			cleanToken.ModelLimitsEnabled,
			cleanToken.AllowIps,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanToken,
	})
	return
}

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func DeleteTokenBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	userId := c.GetInt("id")
	count, err := model.BatchDeleteTokens(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if common.ControlPlaneLogEnabled {
		logger.LogInfo(c, fmt.Sprintf(
			"control-plane tokens batch deleted: user_id=%d count=%d ids=%v",
			userId, count, tokenBatch.Ids,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}
