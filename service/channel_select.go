package service

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// CacheGetRandomSatisfiedChannel selects a channel with P2P priority routing:
// Priority 1: Private channels (user's own channels)
// Priority 2: Shared channels (other users' channels with owner_user_id != 0)
// Priority 3: Public channels (platform channels with owner_user_id = 0)
//
// This function now supports multi-group routing via routingGroups parameter.
// If routingGroups is empty, falls back to the single group parameter for backward compatibility.
func CacheGetRandomSatisfiedChannel(c *gin.Context, group string, modelName string, retry int) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := group
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)

	// Extract client IP from Gin context
	clientIP := c.ClientIP()

	// Build an exclusion set from channels that have already been used in this request.
	// This ensures that retry logic does not select the same failing channel again,
	// but instead chooses from the remaining candidates.
	excluded := map[int]struct{}{}
	used := c.GetStringSlice("use_channel")
	for _, idStr := range used {
		if id, convErr := strconv.Atoi(idStr); convErr == nil && id > 0 {
			excluded[id] = struct{}{}
		}
	}

	if group == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		for _, autoGroup := range GetUserAutoGroup(userGroup) {
			logger.LogDebug(c, "Auto selecting group:", autoGroup)
			channel, _ = model.GetRandomSatisfiedChannelWithPriority(autoGroup, modelName, userId, userGroup, clientIP, retry, excluded)
			if channel == nil {
				continue
			} else {
				c.Set("auto_group", autoGroup)
				selectGroup = autoGroup
				logger.LogDebug(c, "Auto selected group:", autoGroup)
				break
			}
		}
	} else {
		channel, err = model.GetRandomSatisfiedChannelWithPriority(group, modelName, userId, userGroup, clientIP, retry, excluded)
		if err != nil {
			return nil, group, err
		}
	}
	return channel, selectGroup, nil
}

// CacheGetRandomSatisfiedChannelMultiGroup selects a channel from multiple routing groups with P2P priority:
// Priority 1: Private channels (user's own channels)
// Priority 2: Shared channels (other users' channels with owner_user_id != 0)
// Priority 3: Public channels (platform channels with owner_user_id = 0)
//
// This function iterates over all routingGroups, collects matching channels, deduplicates,
// and applies the 3-tier priority sorting logic.
func CacheGetRandomSatisfiedChannelMultiGroup(c *gin.Context, routingGroups []string, modelName string, retry int) (*model.Channel, string, error) {
	if len(routingGroups) == 0 {
		return nil, "", errors.New("routing groups cannot be empty")
	}

	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	clientIP := c.ClientIP()

	// If only one routing group, use the original single-group logic for efficiency
	if len(routingGroups) == 1 {
		group := routingGroups[0]
		channel, err := model.GetRandomSatisfiedChannelWithPriority(group, modelName, userId, userGroup, clientIP, retry, nil)
		return channel, group, err
	}

	// Multi-group routing: call the new multi-group function in model layer
	channel, selectedGroup, err := model.GetRandomSatisfiedChannelWithPriorityMultiGroup(routingGroups, modelName, userId, userGroup, clientIP, retry)
	if err != nil {
		return nil, "", err
	}

	logger.LogDebug(c, "Multi-group routing selected channel from group:", selectedGroup)
	return channel, selectedGroup, nil
}
