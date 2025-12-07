package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func getPriority(group string, model string, retry int) (int, error) {

	var priorities []int
	err := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
		Order("priority DESC").              // 按优先级降序排序
		Pluck("priority", &priorities).Error // Pluck用于将查询的结果直接扫描到一个切片中

	if err != nil {
		// 处理错误
		return 0, err
	}

	if len(priorities) == 0 {
		// 如果没有查询到优先级，则返回错误
		return 0, errors.New("数据库一致性被破坏")
	}

	// 确定要使用的优先级
	var priorityToUse int
	if retry >= len(priorities) {
		// 如果重试次数大于优先级数，则使用最小的优先级
		priorityToUse = priorities[len(priorities)-1]
	} else {
		priorityToUse = priorities[retry]
	}
	return priorityToUse, nil
}

func getChannelQuery(group string, model string, retry int) (*gorm.DB, error) {
	maxPrioritySubQuery := DB.Model(&Ability{}).Select("MAX(priority)").Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true)
	channelQuery := DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = (?)", group, model, true, maxPrioritySubQuery)
	if retry != 0 {
		priority, err := getPriority(group, model, retry)
		if err != nil {
			return nil, err
		} else {
			channelQuery = DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = ?", group, model, true, priority)
		}
	}

	return channelQuery, nil
}

func GetChannel(group string, model string, retry int) (*Channel, error) {
	var abilities []Ability

	var err error = nil
	channelQuery, err := getChannelQuery(group, model, retry)
	if err != nil {
		return nil, err
	}
	if common.UsingSQLite || common.UsingPostgreSQL {
		err = channelQuery.Order("weight DESC").Find(&abilities).Error
	} else {
		err = channelQuery.Order("weight DESC").Find(&abilities).Error
	}
	if err != nil {
		return nil, err
	}
	channel := Channel{}
	if len(abilities) > 0 {
		// Randomly choose one
		weightSum := uint(0)
		for _, ability_ := range abilities {
			weightSum += ability_.Weight + 10
		}
		// Randomly choose one
		weight := common.GetRandomInt(int(weightSum))
		for _, ability_ := range abilities {
			weight -= int(ability_.Weight) + 10
			//log.Printf("weight: %d, ability weight: %d", weight, *ability_.Weight)
			if weight <= 0 {
				channel.Id = ability_.ChannelId
				break
			}
		}
	} else {
		return nil, nil
	}
	err = DB.First(&channel, "id = ?", channel.Id).Error
	return &channel, err
}

// GetChannelWithPriority selects channel with P2P priority routing (database fallback version):
// Priority 1 (Private): User's own channels with is_private=true (owner_user_id=userId AND is_private=true)
// Priority 2 (Shared): Non-private P2P channels, including:
//   - User's own non-private channels (owner_user_id=userId AND is_private=false)
//   - Other users' shared channels that passed access control (owner_user_id != 0 AND owner_user_id != userId AND is_private=false)
//
// Priority 3 (Public): Platform channels (owner_user_id =
// Priority 3 (Public): Platform channels (owner_user_id = 0)
//
// This function is used when memory cache is disabled. It queries the database directly.
func GetChannelWithPriority(group string, model string, userId int, userGroup string, clientIP string, retry int) (*Channel, error) {
	var abilities []Ability

	channelQuery, err := getChannelQuery(group, model, retry)
	if err != nil {
		return nil, err
	}

	// Get all matching abilities
	if common.UsingSQLite || common.UsingPostgreSQL {
		err = channelQuery.Order("weight DESC").Find(&abilities).Error
	} else {
		err = channelQuery.Order("weight DESC").Find(&abilities).Error
	}
	if err != nil {
		return nil, err
	}

	if len(abilities) == 0 {
		return nil, nil
	}

	// Fetch all candidate channels to classify by ownership
	channelIds := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []Channel
	err = DB.Where("id IN ?", channelIds).Find(&channels).Error
	if err != nil {
		return nil, err
	}

	// Create channel map for quick lookup
	channelMap := make(map[int]*Channel)
	for i := range channels {
		channelMap[channels[i].Id] = &channels[i]
	}

	// Separate channels into three priority tiers based on ownership and access control
	var privateChannelIds []int // Tier 1: User's own private channels (is_private=true)
	var sharedChannelIds []int  // Tier 2: Non-private P2P channels (user's own public + others' shared)
	var publicChannelIds []int  // Tier 3: Platform public channels

	for _, ability := range abilities {
		channel, ok := channelMap[ability.ChannelId]
		if !ok {
			continue
		}

		// Apply access control check - skip channels user cannot access
		// For single-group routing, pass group as a single-element routingGroups array
		if !CheckChannelAccess(channel, userId, userGroup, []string{group}, model, clientIP) {
			continue
		}

		// Apply risk control check - now applies to ALL channels (not just P2P)
		// Use estimated quota for pre-check (assuming ~500 tokens = ~2500 quota)
		const estimatedQuota int64 = 2500
		if err := CheckChannelRiskControl(channel, estimatedQuota); err != nil {
			// Check error type and log specially for different limit types
			var newAPIErr *types.NewAPIError
			if errors.As(err, &newAPIErr) {
				switch newAPIErr.GetErrorCode() {
				case types.ErrorCodeChannelConcurrencyExceeded:
					common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to concurrency limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelHourlyLimitExceeded:
					common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to hourly limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelDailyLimitExceeded:
					common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to daily limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelTotalQuotaExceeded:
					common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to total quota limit: %s",
						channel.Id, channel.Name, err.Error()))
				default:
					common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to risk control: %s",
						channel.Id, channel.Name, err.Error()))
				}
			}
			// Skip this channel if it exceeds risk control limits
			continue
		}

		// Classify into appropriate tier based on ownership and privacy settings
		if channel.OwnerUserId == userId && userId != 0 && channel.IsPrivate {
			// Tier 1: User's own private channels (explicitly marked as private)
			privateChannelIds = append(privateChannelIds, ability.ChannelId)
		} else if channel.OwnerUserId != 0 && !channel.IsPrivate {
			// Tier 2: Shared channels (both user's own public channels and others' shared channels)
			sharedChannelIds = append(sharedChannelIds, ability.ChannelId)
		} else if channel.OwnerUserId == 0 {
			// Tier 3: Platform public channels
			publicChannelIds = append(publicChannelIds, ability.ChannelId)
		}
	}

	// Try each tier in order
	tierChannelIds := [][]int{privateChannelIds, sharedChannelIds, publicChannelIds}

	for _, tierIds := range tierChannelIds {
		if len(tierIds) == 0 {
			continue
		}

		// Filter abilities to this tier
		tierAbilities := make([]Ability, 0)
		for _, ability := range abilities {
			for _, id := range tierIds {
				if ability.ChannelId == id {
					tierAbilities = append(tierAbilities, ability)
					break
				}
			}
		}

		// Apply weight-based selection within tier
		if len(tierAbilities) > 0 {
			weightSum := uint(0)
			for _, ability_ := range tierAbilities {
				weightSum += ability_.Weight + 10
			}
			weight := common.GetRandomInt(int(weightSum))
			for _, ability_ := range tierAbilities {
				weight -= int(ability_.Weight) + 10
				if weight <= 0 {
					return channelMap[ability_.ChannelId], nil
				}
			}
		}
	}

	return nil, nil
}

// GetChannelWithPriorityMultiGroup selects a channel from multiple routing groups using database query.
// This is the database fallback for GetRandomSatisfiedChannelWithPriorityMultiGroup when memory cache is disabled.
// It follows the same multi-group routing logic: collect channels from all groups, deduplicate, and apply 3-tier priority.
func GetChannelWithPriorityMultiGroup(routingGroups []string, model string, userId int, userGroup string, clientIP string, retry int) (*Channel, string, error) {
	if len(routingGroups) == 0 {
		return nil, "", errors.New("routing groups cannot be empty")
	}

	// Step 1: Query abilities from all routing groups
	var allAbilities []Ability
	abilityMap := make(map[int]Ability) // Use map for deduplication by ChannelId

	for _, group := range routingGroups {
		channelQuery, err := getChannelQuery(group, model, retry)
		if err != nil {
			continue // Skip this group if query fails
		}

		var groupAbilities []Ability
		if common.UsingSQLite || common.UsingPostgreSQL {
			err = channelQuery.Order("weight DESC").Find(&groupAbilities).Error
		} else {
			err = channelQuery.Order("weight DESC").Find(&groupAbilities).Error
		}
		if err != nil {
			continue // Skip this group if query fails
		}

		// Deduplicate by ChannelId (prefer higher weight if duplicate)
		for _, ability := range groupAbilities {
			if existing, exists := abilityMap[ability.ChannelId]; exists {
				// Keep the ability with higher weight
				if ability.Weight > existing.Weight {
					abilityMap[ability.ChannelId] = ability
				}
			} else {
				abilityMap[ability.ChannelId] = ability
			}
		}
	}

	// Convert map back to slice
	for _, ability := range abilityMap {
		allAbilities = append(allAbilities, ability)
	}

	if len(allAbilities) == 0 {
		return nil, "", errors.New(fmt.Sprintf("no satisfied channel found in any routing group (DB), groups: %v, model: %s", routingGroups, model))
	}

	// Step 2: Fetch all candidate channels
	channelIds := make([]int, 0, len(allAbilities))
	for _, ability := range allAbilities {
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []Channel
	err := DB.Where("id IN ?", channelIds).Find(&channels).Error
	if err != nil {
		return nil, "", err
	}

	// Create channel map for quick lookup
	channelMap := make(map[int]*Channel)
	for i := range channels {
		channelMap[channels[i].Id] = &channels[i]
	}

	// Track which group each channel came from (use first matching group for logging)
	channelGroupMap := make(map[int]string)
	for _, ability := range allAbilities {
		if _, exists := channelGroupMap[ability.ChannelId]; !exists {
			channelGroupMap[ability.ChannelId] = ability.Group
		}
	}

	// Step 3: Classify channels into three priority tiers
	var privateChannelIds []int // Tier 1: User's own private channels
	var sharedChannelIds []int  // Tier 2: Non-private P2P channels
	var publicChannelIds []int  // Tier 3: Platform public channels

	for _, ability := range allAbilities {
		channel, ok := channelMap[ability.ChannelId]
		if !ok {
			continue
		}

		// Apply access control check
		if !CheckChannelAccess(channel, userId, userGroup, routingGroups, model, clientIP) {
			continue
		}

		// Apply risk control check - now applies to ALL channels (not just P2P)
		// Use estimated quota for pre-check (assuming ~500 tokens = ~2500 quota)
		const estimatedQuota int64 = 2500
		if err := CheckChannelRiskControl(channel, estimatedQuota); err != nil {
			var newAPIErr *types.NewAPIError
			if errors.As(err, &newAPIErr) {
				switch newAPIErr.GetErrorCode() {
				case types.ErrorCodeChannelConcurrencyExceeded:
					common.SysLog(fmt.Sprintf("Multi-group DB routing: Channel #%d (%s) skipped due to concurrency limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelHourlyLimitExceeded:
					common.SysLog(fmt.Sprintf("Multi-group DB routing: Channel #%d (%s) skipped due to hourly limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelDailyLimitExceeded:
					common.SysLog(fmt.Sprintf("Multi-group DB routing: Channel #%d (%s) skipped due to daily limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelTotalQuotaExceeded:
					common.SysLog(fmt.Sprintf("Multi-group DB routing: Channel #%d (%s) skipped due to total quota limit: %s",
						channel.Id, channel.Name, err.Error()))
				default:
					common.SysLog(fmt.Sprintf("Multi-group DB routing: Channel #%d (%s) skipped due to risk control: %s",
						channel.Id, channel.Name, err.Error()))
				}
			}
			continue
		}

		// Classify into appropriate tier
		if channel.OwnerUserId == userId && userId != 0 && channel.IsPrivate {
			privateChannelIds = append(privateChannelIds, ability.ChannelId)
		} else if channel.OwnerUserId != 0 && !channel.IsPrivate {
			sharedChannelIds = append(sharedChannelIds, ability.ChannelId)
		} else if channel.OwnerUserId == 0 {
			publicChannelIds = append(publicChannelIds, ability.ChannelId)
		}
	}

	common.SysLog(fmt.Sprintf(
		"Multi-group DB P2P Routing - User: %d, Groups: %v, Model: %s | Tiers: Private=%d, Shared=%d, Public=%d",
		userId, routingGroups, model, len(privateChannelIds), len(sharedChannelIds), len(publicChannelIds),
	))

	// Step 4: Try each tier in order
	tierChannelIds := [][]int{privateChannelIds, sharedChannelIds, publicChannelIds}
	tierNames := []string{"Private", "Shared", "Public"}

	for tierIdx, tierIds := range tierChannelIds {
		if len(tierIds) == 0 {
			continue
		}

		// Filter abilities to this tier
		tierAbilities := make([]Ability, 0)
		for _, ability := range allAbilities {
			for _, id := range tierIds {
				if ability.ChannelId == id {
					tierAbilities = append(tierAbilities, ability)
					break
				}
			}
		}

		// Apply weight-based selection within tier
		if len(tierAbilities) > 0 {
			weightSum := uint(0)
			for _, ability_ := range tierAbilities {
				weightSum += ability_.Weight + 10
			}
			weight := common.GetRandomInt(int(weightSum))
			for _, ability_ := range tierAbilities {
				weight -= int(ability_.Weight) + 10
				if weight <= 0 {
					selectedChannel := channelMap[ability_.ChannelId]
					selectedGroup := channelGroupMap[ability_.ChannelId]
					common.SysLog(fmt.Sprintf(
						"Multi-group DB Channel Selected - Tier: %s, Group: %s, Channel: #%d (Owner: %d, IsPrivate: %t, Name: %s)",
						tierNames[tierIdx], selectedGroup, selectedChannel.Id, selectedChannel.OwnerUserId,
						selectedChannel.IsPrivate, selectedChannel.Name,
					))
					return selectedChannel, selectedGroup, nil
				}
			}
		}
	}

	return nil, "", errors.New(fmt.Sprintf("no satisfied channel found in any routing group after filtering (DB), groups: %v, model: %s", routingGroups, model))
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	return DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingSQLite {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
