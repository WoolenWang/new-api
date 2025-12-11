package model

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
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

// IsModelAllowedForChannel checks if a specific model is allowed for a channel based on AllowedModels whitelist
// Semantics:
//   - If AllowedModels is set and non-empty: use it as the permission whitelist (returns true only if model is in the list)
//   - If AllowedModels is nil or empty: fallback to Models field as the permission whitelist (backward compatibility)
//   - For platform channels (OwnerUserId == 0) or owner accessing their own channel: this check can be bypassed by caller
//
// This function implements the separation of "capability declaration" (Models) and "permission whitelist" (AllowedModels)
func IsModelAllowedForChannel(channel *Channel, model string) bool {
	// Determine which field to use as the permission whitelist
	var permissionWhitelist string
	if channel.AllowedModels != nil && *channel.AllowedModels != "" {
		// Use AllowedModels as the permission whitelist (explicit P2P sharing control)
		permissionWhitelist = *channel.AllowedModels
	} else {
		// Fallback to Models field (backward compatibility: capability = permission)
		permissionWhitelist = channel.Models
	}

	// If whitelist is empty, deny by default (defensive)
	if permissionWhitelist == "" {
		return false
	}

	// Split models by comma and check if requested model is in the list
	allowedModelList := strings.Split(permissionWhitelist, ",")
	for _, allowedModel := range allowedModelList {
		if strings.TrimSpace(allowedModel) == model {
			return true
		}
	}

	return false
}

func CheckChannelAccess(channel *Channel, userId int, userGroup string, routingGroups []string, model string, clientIP string) bool {
	// Determine whether this request carries an effective P2P 约束
	// We only inject "p2p_{id}" routing groups when the Token 显式配置了 p2p_group_id
	// 且用户在该组内有有效成员关系，因此这里可以安全地以是否包含 "p2p_" 来判断
	// 「本次请求是否受 Token 级 P2P 限制」。
	hasP2PConstraint := false
	for _, g := range routingGroups {
		if strings.HasPrefix(g, "p2p_") {
			hasP2PConstraint = true
			break
		}
	}

	// Owner always has access to their own channels (bypass all restrictions)
	if channel.OwnerUserId == userId && userId != 0 {
		return true
	}

	// If channel is marked as private, only owner can access
	if channel.IsPrivate {
		return false
	}

	// For non-owner users accessing P2P channels, check model-level permission first
	// This enforces the separation of "capability" (Models) and "permission" (AllowedModels)
	if !IsModelAllowedForChannel(channel, model) {
		return false
	}

	// Check IP whitelist (if configured)
	ipWhitelist := channel.GetIPWhitelist()
	if len(ipWhitelist) > 0 {
		if !common.IsIPInWhitelist(ipWhitelist, clientIP) {
			// IP not in whitelist, deny access
			return false
		}
	}

	// Track whitelist configuration & matches
	hasWhitelist := false
	whitelistMatched := false

	// Check allowed groups whitelist (supports both system groups and P2P group IDs)
	p2pGroupMatched := false
	if channel.AllowedGroups != nil && *channel.AllowedGroups != "" {
		hasWhitelist = true
		// Try to parse as JSON array (P2P group IDs)
		allowedGroupIDs := channel.GetAllowedGroupIDs()
		if len(allowedGroupIDs) > 0 {
			// P2P group ID mode: check if user's routingGroups contains any allowed P2P group
			for _, groupID := range allowedGroupIDs {
				p2pGroupName := fmt.Sprintf("p2p_%d", groupID)
				for _, routingGroup := range routingGroups {
					if routingGroup == p2pGroupName {
						p2pGroupMatched = true
						whitelistMatched = true
						break
					}
				}
				if p2pGroupMatched {
					break
				}
			}
		} else {
			// Fallback: treat as comma-separated system group names (legacy mode, system-group whitelisting)
			allowedGroups := strings.Split(*channel.AllowedGroups, ",")
			for _, allowedGroup := range allowedGroups {
				trimmedGroup := strings.TrimSpace(allowedGroup)
				if trimmedGroup == "" {
					continue
				}
				// Check against user's system group
				if trimmedGroup == userGroup {
					whitelistMatched = true
					break
				}
				// Also check against routingGroups (for flexibility)
				for _, routingGroup := range routingGroups {
					if routingGroup == trimmedGroup {
						whitelistMatched = true
						break
					}
				}
				if whitelistMatched {
					break
				}
			}
		}
	}

	// P2P AND semantics:
	// 当本次请求携带有效的 P2P 分组约束时（routingGroups 中包含 p2p_*）：
	//   1. 渠道必须显式声明其 P2P 授权（AllowedGroups 为 JSON 数组 ID 模式）；
	//   2. 且其授权的 P2P 组 ID 必须与请求的 P2P 组有交集。
	//
	// 注意：这里不再区分平台渠道（owner_user_id = 0）与非平台渠道——
	// 只要 Token 受 P2P 限制，所有被选出的渠道都必须属于对应的 P2P 组，
	// 否则会造成「有 P2P 限制却退回系统计费分组公共渠道」的错误行为。
	if hasP2PConstraint {
		// 未配置 AllowedGroups：视为未加入任何 P2P 组，在有 P2P 约束时不允许访问
		if channel.AllowedGroups == nil || *channel.AllowedGroups == "" {
			return false
		}

		allowedGroupIDs := channel.GetAllowedGroupIDs()
		// 仅在显式使用 P2P 组 ID 模式时才认为渠道加入了某个 P2P 组；
		// 旧的字符串分组模式在有 P2P 约束时不再视为有效的 P2P 归属。
		if len(allowedGroupIDs) == 0 || !p2pGroupMatched {
			return false
		}
	}

	// If any whitelist is configured, user must match at least one whitelist rule
	if hasWhitelist {
		return whitelistMatched
	}

	// If no access control is set (not private, no whitelist), it's a shared/public P2P channel
	return true
}

// GetRandomSatisfiedChannelWithPriority selects channels with P2P priority routing:
// Priority 1 (Private): User's own channels with is_private=true (owner_user_id=userId AND is_private=true)
// Priority 2 (Shared): Non-private P2P channels, including:
//   - User's own non-private channels (owner_user_id=userId AND is_private=false)
//   - Other users' shared channels that passed access control (owner_user_id != 0 AND owner_user_id != userId AND is_private=false)
//
// Priority 3 (Public): Platform channels (owner_user_id = 0)
//
// Within each priority tier, channels are further selected based on their priority level and weight.
// Access control is enforced via CheckChannelAccess, and risk control limits are applied for P2P channels.
// The excluded map allows callers (e.g. retry logic) to skip channels that
// have already been tried in the current request.
func GetRandomSatisfiedChannelWithPriority(group string, model string, userId int, userGroup string, clientIP string, retry int, excluded map[int]struct{}) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database with priority
	if !common.MemoryCacheEnabled {
		return GetChannelWithPriority(group, model, userId, userGroup, clientIP, retry, excluded)
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
	var privateChannels []*Channel // Tier 1: User's own channels
	var sharedChannels []*Channel  // Tier 2: Other users' shared channels
	var publicChannels []*Channel  // Tier 3: Platform public channels

	for _, channelId := range channels {
		// Skip channels that have already been used (e.g. in previous retries).
		if _, skip := excluded[channelId]; skip {
			continue
		}
		if channel, ok := channelsIDM[channelId]; ok {
			// Apply access control check - skip channels user cannot access
			// For single-group routing, pass group as a single-element routingGroups array
			if !CheckChannelAccess(channel, userId, userGroup, []string{group}, model, clientIP) {
				continue
			}

			// Apply unified risk control check for all channels using an estimated quota
			// This enables pre-filtering by total and time-window quota limits before selection.
			const estimatedQuota int64 = 2500 // Approximate quota for a typical request
			if err := CheckChannelRiskControl(channel, estimatedQuota); err != nil {
				// Check error type and log specially for different limit types
				var newAPIErr *types.NewAPIError
				if errors.As(err, &newAPIErr) {
					switch newAPIErr.GetErrorCode() {
					case types.ErrorCodeChannelConcurrencyExceeded:
						common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to concurrency limit: %s",
							channel.Id, channel.Name, err.Error()))
					case types.ErrorCodeChannelHourlyLimitExceeded:
						common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to hourly quota limit: %s",
							channel.Id, channel.Name, err.Error()))
					case types.ErrorCodeChannelDailyLimitExceeded:
						common.SysLog(fmt.Sprintf("Channel #%d (%s) skipped due to daily quota limit: %s",
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
				privateChannels = append(privateChannels, channel)
			} else if channel.OwnerUserId != 0 && !channel.IsPrivate {
				// Tier 2: Shared channels (both user's own public channels and others' shared channels)
				// User's own non-private channels and other users' channels that passed access control
				sharedChannels = append(sharedChannels, channel)
			} else if channel.OwnerUserId == 0 {
				// Tier 3: Platform public channels
				publicChannels = append(publicChannels, channel)
			}
		}
	}

	// Log channel tier distribution for debugging
	common.SysLog(fmt.Sprintf(
		"P2P Channel Routing - User: %d, Group: %s, Model: %s | Tiers: Private=%d, Shared=%d, Public=%d",
		userId, userGroup, model, len(privateChannels), len(sharedChannels), len(publicChannels),
	))

	// Try selecting from each tier in order: private -> shared -> public
	tierChannels := [][]*Channel{privateChannels, sharedChannels, publicChannels}
	tierNames := []string{"Private", "Shared", "Public"}

	for tierIdx, tier := range tierChannels {
		if len(tier) == 0 {
			continue // Skip empty tiers
		}

		// Within each tier, apply the original priority + weight selection logic
		selectedChannel := selectChannelFromTier(tier, retry)
		if selectedChannel != nil {
			common.SysLog(fmt.Sprintf(
				"P2P Channel Selected - Tier: %s, Channel: #%d (Owner: %d, IsPrivate: %t, Name: %s)",
				tierNames[tierIdx], selectedChannel.Id, selectedChannel.OwnerUserId,
				selectedChannel.IsPrivate, selectedChannel.Name,
			))
			return selectedChannel, nil
		}
	}

	return nil, errors.New(fmt.Sprintf("no satisfied channel found, group: %s, model: %s", group, model))
}

// GetRandomSatisfiedChannelWithPriorityMultiGroup selects a channel from multiple routing groups with P2P priority.
// This function implements the multi-group routing logic for P2P group decoupling:
// 1. Iterates over all routingGroups (BillingGroup + Active P2P Groups)
// 2. Collects channels from each group that support the target model
// 3. Deduplicates channels by ID
// 4. Applies the 3-tier priority sorting (Private > Shared > Public)
// 5. Returns the selected channel and the group it came from
func GetRandomSatisfiedChannelWithPriorityMultiGroup(routingGroups []string, model string, userId int, userGroup string, clientIP string, retry int) (*Channel, string, error) {
	if len(routingGroups) == 0 {
		return nil, "", errors.New("routing groups cannot be empty")
	}

	// If memory cache is disabled, use database query with multi-group support
	if !common.MemoryCacheEnabled {
		return GetChannelWithPriorityMultiGroup(routingGroups, model, userId, userGroup, clientIP, retry)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// Step 1: Collect all candidate channel IDs from all routing groups (with deduplication)
	channelIDSet := make(map[int]bool) // Use map for deduplication
	var allChannelIDs []int

	for _, group := range routingGroups {
		// Try exact model match first
		channels := group2model2channels[group][model]

		// If no channels found, try normalized model name
		if len(channels) == 0 {
			normalizedModel := ratio_setting.FormatMatchingModelName(model)
			channels = group2model2channels[group][normalizedModel]
		}

		// Add channels to set (automatic deduplication)
		for _, channelID := range channels {
			if !channelIDSet[channelID] {
				channelIDSet[channelID] = true
				allChannelIDs = append(allChannelIDs, channelID)
			}
		}
	}

	if len(allChannelIDs) == 0 {
		return nil, "", errors.New(fmt.Sprintf("no satisfied channel found in any routing group, groups: %v, model: %s", routingGroups, model))
	}

	// Step 2: Classify channels into three priority tiers (Private > Shared > Public)
	var privateChannels []*Channel // Tier 1: User's own private channels
	var sharedChannels []*Channel  // Tier 2: Other users' shared channels
	var publicChannels []*Channel  // Tier 3: Platform public channels

	// Track which group each channel came from for logging
	channelGroupMap := make(map[int]string)

	for _, channelID := range allChannelIDs {
		channel, ok := channelsIDM[channelID]
		if !ok {
			continue
		}

		// Apply access control check - skip channels user cannot access
		if !CheckChannelAccess(channel, userId, userGroup, routingGroups, model, clientIP) {
			continue
		}

		// Apply unified risk control check for all channels using an estimated quota
		const estimatedQuota int64 = 2500
		if err := CheckChannelRiskControl(channel, estimatedQuota); err != nil {
			// Log different limit types specially
			var newAPIErr *types.NewAPIError
			if errors.As(err, &newAPIErr) {
				switch newAPIErr.GetErrorCode() {
				case types.ErrorCodeChannelConcurrencyExceeded:
					common.SysLog(fmt.Sprintf("Multi-group routing: Channel #%d (%s) skipped due to concurrency limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelHourlyLimitExceeded:
					common.SysLog(fmt.Sprintf("Multi-group routing: Channel #%d (%s) skipped due to hourly quota limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelDailyLimitExceeded:
					common.SysLog(fmt.Sprintf("Multi-group routing: Channel #%d (%s) skipped due to daily quota limit: %s",
						channel.Id, channel.Name, err.Error()))
				case types.ErrorCodeChannelTotalQuotaExceeded:
					common.SysLog(fmt.Sprintf("Multi-group routing: Channel #%d (%s) skipped due to total quota limit: %s",
						channel.Id, channel.Name, err.Error()))
				default:
					common.SysLog(fmt.Sprintf("Multi-group routing: Channel #%d (%s) skipped due to risk control: %s",
						channel.Id, channel.Name, err.Error()))
				}
			}
			continue
		}

		// Find which group this channel belongs to (for logging)
		for _, group := range routingGroups {
			groupChannels := group2model2channels[group][model]
			if len(groupChannels) == 0 {
				normalizedModel := ratio_setting.FormatMatchingModelName(model)
				groupChannels = group2model2channels[group][normalizedModel]
			}
			for _, gChannelID := range groupChannels {
				if gChannelID == channelID {
					channelGroupMap[channelID] = group
					break
				}
			}
		}

		// Classify into appropriate tier based on ownership and privacy settings
		if channel.OwnerUserId == userId && userId != 0 && channel.IsPrivate {
			// Tier 1: User's own private channels (explicitly marked as private)
			privateChannels = append(privateChannels, channel)
		} else if channel.OwnerUserId != 0 && !channel.IsPrivate {
			// Tier 2: Shared channels (both user's own public channels and others' shared channels)
			sharedChannels = append(sharedChannels, channel)
		} else if channel.OwnerUserId == 0 {
			// Tier 3: Platform public channels
			publicChannels = append(publicChannels, channel)
		}
	}

	// Log channel tier distribution for debugging
	common.SysLog(fmt.Sprintf(
		"Multi-group P2P Routing - User: %d, Groups: %v, Model: %s | Tiers: Private=%d, Shared=%d, Public=%d",
		userId, routingGroups, model, len(privateChannels), len(sharedChannels), len(publicChannels),
	))

	// Step 3: Try selecting from each tier in order: private -> shared -> public
	tierChannels := [][]*Channel{privateChannels, sharedChannels, publicChannels}
	tierNames := []string{"Private", "Shared", "Public"}

	for tierIdx, tier := range tierChannels {
		if len(tier) == 0 {
			continue // Skip empty tiers
		}

		// Within each tier, apply the original priority + weight selection logic
		selectedChannel := selectChannelFromTier(tier, retry)
		if selectedChannel != nil {
			selectedGroup := channelGroupMap[selectedChannel.Id]
			common.SysLog(fmt.Sprintf(
				"Multi-group Channel Selected - Tier: %s, Group: %s, Channel: #%d (Owner: %d, IsPrivate: %t, Name: %s)",
				tierNames[tierIdx], selectedGroup, selectedChannel.Id, selectedChannel.OwnerUserId,
				selectedChannel.IsPrivate, selectedChannel.Name,
			))
			return selectedChannel, selectedGroup, nil
		}
	}

	return nil, "", errors.New(fmt.Sprintf("no satisfied channel found in any routing group after filtering, groups: %v, model: %s", routingGroups, model))
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
