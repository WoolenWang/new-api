package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func ReturnPreConsumedQuota(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if relayInfo.FinalPreConsumedQuota != 0 {
		if common.DataPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费额度 %s", relayInfo.UserId, logger.FormatQuota(relayInfo.FinalPreConsumedQuota)))
		}
		gopool.Go(func() {
			relayInfoCopy := *relayInfo

			err := PostConsumeQuota(&relayInfoCopy, -relayInfoCopy.FinalPreConsumedQuota, 0, false)
			if err != nil {
				common.SysLog("error return pre-consumed quota: " + err.Error())
			}
		})
	}
}

// PreConsumeQuota checks if the user has enough quota to pre-consume.
// It returns the pre-consumed quota if successful, or an error if not.
func PreConsumeQuota(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	// ============ TS4: 套餐检查分支（集成点） ============
	if common.PackageEnabled {
		// 1. 获取 Token 的 P2P 分组限制
		var tokenP2PGroupID *int
		tokenAllowedP2PGroups, exists := c.Get(string(constant.ContextKeyTokenAllowedP2PGroups))
		if exists && tokenAllowedP2PGroups != nil {
			if p2pList, ok := tokenAllowedP2PGroups.([]int); ok && len(p2pList) > 0 {
				// Token 设置了 P2P 分组限制，取第一个（根据设计，Token.p2p_group_id 是单个 ID）
				tokenP2PGroupID = &p2pList[0]
			}
		}

		// 2. 尝试从套餐消耗额度
		subscriptionId, packageQuota, err := TryConsumeFromPackage(
			relayInfo.UserId,
			tokenP2PGroupID,
			int64(preConsumedQuota),
		)

		if subscriptionId > 0 {
			// 3a. 成功使用套餐，跳过用户余额扣减
			relayInfo.UsingPackageId = subscriptionId
			relayInfo.PreConsumedFromPackage = int(packageQuota)
			relayInfo.FinalPreConsumedQuota = 0 // 套餐消耗不计入用户预扣费

			if common.DataPlaneLogEnabled {
				logger.LogInfo(c, fmt.Sprintf(
					"[Package] 用户 %d 使用套餐订阅 %d，预扣 %s 额度",
					relayInfo.UserId, subscriptionId, logger.FormatQuota(int(packageQuota)),
				))
			}
			return nil // 直接返回，不执行后续用户余额扣减逻辑
		}

		if err != nil {
			// 3b. 套餐超限且不允许 fallback
			logger.LogError(c, fmt.Sprintf("[Package] 套餐额度超限: %v", err))
			return types.NewErrorWithStatusCode(
				fmt.Errorf("套餐额度不足: %v", err),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusTooManyRequests,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}

		// 3c. subscriptionId == 0 且 err == nil：无套餐或允许 fallback，继续执行原有逻辑
		// 【监控】记录使用余额的请求
		IncrementBalanceRequest()
	}
	// ==================== 套餐检查结束 ====================

	// ========== 原有逻辑：用户余额检查与扣减 ==========
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if userQuota <= 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	if userQuota-preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}

	trustQuota := common.GetTrustQuota()

	relayInfo.UserQuota = userQuota
	if userQuota > trustQuota {
		// 用户额度充足，判断令牌额度是否充足
		if !relayInfo.TokenUnlimited {
			// 非无限令牌，判断令牌额度是否充足
			tokenQuota := c.GetInt("token_quota")
			if tokenQuota > trustQuota {
				// 令牌额度充足，信任令牌
				preConsumedQuota = 0
				if common.DataPlaneLogEnabled {
					logger.LogInfo(c, fmt.Sprintf("用户 %d 剩余额度 %s 且令牌 %d 额度 %d 充足, 信任且不需要预扣费", relayInfo.UserId, logger.FormatQuota(userQuota), relayInfo.TokenId, tokenQuota))
				}
			}
		} else {
			// in this case, we do not pre-consume quota
			// because the user has enough quota
			preConsumedQuota = 0
			if common.DataPlaneLogEnabled {
				logger.LogInfo(c, fmt.Sprintf("用户 %d 额度充足且为无限额度令牌, 信任且不需要预扣费", relayInfo.UserId))
			}
		}
	}

	if preConsumedQuota > 0 {
		err := PreConsumeTokenQuota(relayInfo, preConsumedQuota)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		err = model.DecreaseUserQuota(relayInfo.UserId, preConsumedQuota)
		if err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if common.DataPlaneLogEnabled {
			logger.LogInfo(c, fmt.Sprintf("用户 %d 预扣费 %s, 预扣费后剩余额度: %s", relayInfo.UserId, logger.FormatQuota(preConsumedQuota), logger.FormatQuota(userQuota-preConsumedQuota)))
		}
	}
	relayInfo.FinalPreConsumedQuota = preConsumedQuota
	return nil
}
