package service

import (
	"errors"

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
func CacheGetRandomSatisfiedChannel(c *gin.Context, group string, modelName string, retry int) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := group
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)

	if group == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		for _, autoGroup := range GetUserAutoGroup(userGroup) {
			logger.LogDebug(c, "Auto selecting group:", autoGroup)
			channel, _ = model.GetRandomSatisfiedChannelWithPriority(autoGroup, modelName, userId, retry)
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
		channel, err = model.GetRandomSatisfiedChannelWithPriority(group, modelName, userId, retry)
		if err != nil {
			return nil, group, err
		}
	}
	return channel, selectGroup, nil
}

