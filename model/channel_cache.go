package model

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
var channelSyncLock sync.RWMutex

func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	var channels []*Channel
	DB.Find(&channels)
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
	}
	var abilities []*Ability
	DB.Find(&abilities)
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for group := range groups {
		newGroup2model2channels[group] = make(map[string][]int)
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue // skip disabled channels
		}
		groups := strings.Split(channel.Group, ",")
		for _, group := range groups {
			models := strings.Split(channel.Models, ",")
			for _, model := range models {
				if _, ok := newGroup2model2channels[group][model]; !ok {
					newGroup2model2channels[group][model] = make([]int, 0)
				}
				newGroup2model2channels[group][model] = append(newGroup2model2channels[group][model], channel.Id)
			}
		}
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return newChannelId2channel[channels[i]].GetPriority() > newChannelId2channel[channels[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	channelSyncLock.Unlock()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func GetRandomSatisfiedChannel(group string, model string, retry int) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return GetChannel(group, model, retry)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	channels := group2model2channels[group][model]

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels = group2model2channels[group][normalizedModel]
	}

	if len(channels) == 0 {
		return nil, nil
	}

	if len(channels) == 1 {
		if channel, ok := channelsIDM[channels[0]]; ok {
			return channel, nil
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0])
	}

	uniquePriorities := make(map[int]bool)
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			uniquePriorities[int(channel.GetPriority())] = true
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	// get the priority for the given retry number
	var sumWeight = 0
	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if channel.GetPriority() == targetPriority {
				sumWeight += channel.GetWeight()
				targetChannels = append(targetChannels, channel)
			}
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}

	if len(targetChannels) == 0 {
		return nil, errors.New(fmt.Sprintf("no channel found, group: %s, model: %s, priority: %d", group, model, targetPriority))
	}

	// smoothing factor and adjustment
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		// when all channels have weight 0, set sumWeight to the number of channels and set smoothing adjustment to 100
		// each channel's effective weight = 100
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		// when the average weight is less than 10, set smoothing factor to 100
		smoothingFactor = 100
	}

	// Calculate the total weight of all channels up to endIdx
	totalWeight := sumWeight * smoothingFactor

	// Generate a random value in the range [0, totalWeight)
	randomWeight := rand.Intn(totalWeight)

	// Find a channel based on its weight
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return channel, nil
		}
	}
	// return null if no channel is not found
	return nil, errors.New("channel not found")
}

// CheckChannelAccess checks if a user has access to a specific channel based on access control settings
// Returns true if user has access, false otherwise
func CheckChannelAccess(channel *Channel, userId int, userGroup string) bool {
	// Platform channels (owner_user_id = 0) are always accessible
	if channel.OwnerUserId == 0 {
		return true
	}

	// Owner always has access to their own channels
	if channel.OwnerUserId == userId && userId != 0 {
		return true
	}

	// If channel is marked as private, only owner can access
	if channel.IsPrivate {
		return false
	}

	// Check allowed users whitelist
	if channel.AllowedUsers != nil && *channel.AllowedUsers != "" {
		allowedUsers := strings.Split(*channel.AllowedUsers, ",")
		userIdStr := strconv.Itoa(userId)
		for _, allowedUser := range allowedUsers {
			if strings.TrimSpace(allowedUser) == userIdStr {
				return true
			}
		}
	}

	// Check allowed groups whitelist
	if channel.AllowedGroups != nil && *channel.AllowedGroups != "" {
		allowedGroups := strings.Split(*channel.AllowedGroups, ",")
		for _, allowedGroup := range allowedGroups {
			if strings.TrimSpace(allowedGroup) == userGroup {
				return true
			}
		}
	}

	// If channel has either allowed_users or allowed_groups set, but user didn't match, deny access
	if (channel.AllowedUsers != nil && *channel.AllowedUsers != "") ||
	   (channel.AllowedGroups != nil && *channel.AllowedGroups != "") {
		return false
	}

	// If no access control is set (not private, no whitelist), it's a shared/public P2P channel
	return true
}

// GetRandomSatisfiedChannelWithPriority selects channels with P2P priority routing:
// Priority 1: Private channels (user's own channels with is_private=true OR owner_user_id=userId)
// Priority 2: Shared channels (other users' channels with owner_user_id != 0 AND owner_user_id != userId)
// Priority 3: Public channels (platform channels with owner_user_id = 0)
func GetRandomSatisfiedChannelWithPriority(group string, model string, userId int, userGroup string, retry int) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database with priority
	if !common.MemoryCacheEnabled {
		return GetChannelWithPriority(group, model, userId, userGroup, retry)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	channels := group2model2channels[group][model]

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels = group2model2channels[group][normalizedModel]
	}

	if len(channels) == 0 {
		return nil, nil
	}

	// Separate channels into three priority tiers based on ownership and access control
	var privateChannels []*Channel   // Tier 1: User's own channels
	var sharedChannels []*Channel    // Tier 2: Other users' shared channels
	var publicChannels []*Channel    // Tier 3: Platform public channels

	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			// Apply access control check - skip channels user cannot access
			if !CheckChannelAccess(channel, userId, userGroup) {
				continue
			}

			// Apply risk control check for P2P channels - skip channels that exceed limits
			if channel.OwnerUserId != 0 {
				if err := CheckChannelRiskControl(channel); err != nil {
					// Skip this channel if it exceeds risk control limits
					continue
				}
			}

			// Classify into appropriate tier based on ownership
			if channel.OwnerUserId == userId && userId != 0 {
				// User's own channels go to private tier
				privateChannels = append(privateChannels, channel)
			} else if channel.OwnerUserId != 0 && channel.OwnerUserId != userId {
				// Other users' channels that passed access control go to shared tier
				sharedChannels = append(sharedChannels, channel)
			} else if channel.OwnerUserId == 0 {
				// Platform channels go to public tier
				publicChannels = append(publicChannels, channel)
			}
		}
	}

	// Try selecting from each tier in order: private -> shared -> public
	tierChannels := [][]*Channel{privateChannels, sharedChannels, publicChannels}

	for _, tier := range tierChannels {
		if len(tier) == 0 {
			continue // Skip empty tiers
		}

		// Within each tier, apply the original priority + weight selection logic
		selectedChannel := selectChannelFromTier(tier, retry)
		if selectedChannel != nil {
			return selectedChannel, nil
		}
	}

	return nil, errors.New(fmt.Sprintf("no satisfied channel found, group: %s, model: %s", group, model))
}

// selectChannelFromTier selects a channel from a tier using priority and weight
func selectChannelFromTier(tierChannels []*Channel, retry int) *Channel {
	if len(tierChannels) == 0 {
		return nil
	}

	if len(tierChannels) == 1 {
		return tierChannels[0]
	}

	// Group channels by priority
	uniquePriorities := make(map[int]bool)
	for _, channel := range tierChannels {
		uniquePriorities[int(channel.GetPriority())] = true
	}

	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	// Select channels with target priority
	var sumWeight = 0
	var targetChannels []*Channel
	for _, channel := range tierChannels {
		if channel.GetPriority() == targetPriority {
			sumWeight += channel.GetWeight()
			targetChannels = append(targetChannels, channel)
		}
	}

	if len(targetChannels) == 0 {
		return nil
	}

	// Apply weight-based selection
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		smoothingFactor = 100
	}

	totalWeight := sumWeight * smoothingFactor
	randomWeight := rand.Intn(totalWeight)

	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return channel
		}
	}

	return nil
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return c, nil
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return &c.ChannelInfo, nil
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.Status = status
	}
	if status != common.ChannelStatusEnabled {
		// delete the channel from group2model2channels
		for group, model2channels := range group2model2channels {
			for model, channels := range model2channels {
				for i, channelId := range channels {
					if channelId == id {
						// remove the channel from the slice
						group2model2channels[group][model] = append(channels[:i], channels[i+1:]...)
						break
					}
				}
			}
		}
	}
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel == nil {
		return
	}

	println("CacheUpdateChannel:", channel.Id, channel.Name, channel.Status, channel.ChannelInfo.MultiKeyPollingIndex)

	println("before:", channelsIDM[channel.Id].ChannelInfo.MultiKeyPollingIndex)
	channelsIDM[channel.Id] = channel
	println("after :", channelsIDM[channel.Id].ChannelInfo.MultiKeyPollingIndex)
}
