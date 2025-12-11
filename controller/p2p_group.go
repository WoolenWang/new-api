package controller

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/constant"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// CreateP2PGroup creates a new P2P group
// POST /api/groups
// Request body: { name, display_name, owner_id, type, join_method, join_key, description }
func CreateP2PGroup(c *gin.Context) {
	var group model.Group
	if err := c.ShouldBindJSON(&group); err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	myUserId := c.GetInt("id")

	if group.OwnerId == 0 {
		group.OwnerId = myUserId
	} else if group.OwnerId != myUserId {
		if myRole != common.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "只有超级管理员可以为其他用户添加分组",
			})
			return
		}
	}

	// Validate required fields
	if group.Name == "" || group.OwnerId == 0 {
		common.ApiError(c, errors.New("name and owner_id are required"))
		return
	}

	// Check group limit (only for non-root users)
	if myRole != common.RoleRootUser {
		count, err := model.CountGroupsByOwner(group.OwnerId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		maxGroups := constant.MaxP2PGroupsPerUser
		if count >= int64(maxGroups) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("每个用户最多只能创建 %d 个分组，您已达到上限", maxGroups),
			})
			return
		}
	}

	// Create group
	if err := model.CreateGroup(&group); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, group)
}

// GetUserOwnedGroups returns all groups created by a specific user (Admin API)
// GET /api/groups/owned?user_id=123&page=1&page_size=20
func GetUserOwnedGroups(c *gin.Context) {
	userIdStr := c.Query("user_id")
	if userIdStr == "" {
		common.ApiError(c, errors.New("user_id is required"))
		return
	}

	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid user_id"))
		return
	}

	// Get pagination parameters
	pageInfo := common.GetPageQuery(c)

	// Get paginated groups
	groups, total, err := model.GetGroupsByOwnerPaginated(
		userId,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}

// GetSelfOwnedGroups returns all groups created by current authenticated user (User Self-Service API)
// GET /api/groups/self/owned?page=1&page_size=20
func GetSelfOwnedGroups(c *gin.Context) {
	userId := c.GetInt("id")

	// Get pagination parameters
	pageInfo := common.GetPageQuery(c)

	// Get paginated groups with member count
	groups, total, err := model.GetGroupsByOwnerWithMemberCount(
		userId,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}

// GetUserJoinedGroups returns all P2P groups a user has joined (Status=Active) (Admin API)
// GET /api/groups/joined?user_id=123&page=1&page_size=20
func GetUserJoinedGroups(c *gin.Context) {
	userIdStr := c.Query("user_id")
	if userIdStr == "" {
		common.ApiError(c, errors.New("user_id is required"))
		return
	}

	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid user_id"))
		return
	}

	// Get pagination parameters
	pageInfo := common.GetPageQuery(c)

	// Get paginated joined groups
	groups, total, err := model.GetUserGroupsPaginated(
		userId,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}

// GetSelfJoinedGroups returns all P2P groups current user has joined (Status=Active) (User Self-Service API)
// GET /api/groups/self/joined?page=1&page_size=20
func GetSelfJoinedGroups(c *gin.Context) {
	userId := c.GetInt("id")

	// Get pagination parameters
	pageInfo := common.GetPageQuery(c)

	// Get paginated joined groups with member count
	groups, total, err := model.GetUserGroupsWithMemberCount(
		userId,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}

// UpdateP2PGroup updates group information
// PUT /api/groups
// Request body: { id, name, display_name, type, join_method, join_key, description }
func UpdateP2PGroup(c *gin.Context) {
	var group model.Group
	if err := c.ShouldBindJSON(&group); err != nil {
		common.ApiError(c, err)
		return
	}

	if group.Id == 0 {
		common.ApiError(c, errors.New("group id is required"))
		return
	}

	// Verify group exists and get current owner
	existingGroup, err := model.GetGroupById(group.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	myUserId := c.GetInt("id")

	isRoot := myRole == common.RoleRootUser
	isOwner := existingGroup.OwnerId == myUserId

	// Security check: Only group owner or root admin can update group metadata.
	if !isRoot && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "只有分组所有者或超级管理员可以更新分组",
		})
		return
	}

	// Owner transfer support:
	// - If owner_id is omitted (0), keep existing owner.
	// - If owner_id is provided and different, allow:
	//   * root admin to transfer ownership to any user
	//   * current owner to transfer ownership to another user
	if group.OwnerId == 0 {
		group.OwnerId = existingGroup.OwnerId
	}

	// Update group
	if err := model.UpdateGroup(&group); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, group)
}

// DeleteP2PGroup deletes a group and all associated member relationships
// DELETE /api/groups?id=101
func DeleteP2PGroup(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		// Try to get from body
		var req struct {
			Id int `json:"id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			common.ApiError(c, errors.New("group id is required"))
			return
		}
		idStr = strconv.Itoa(req.Id)
	}

	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group id"))
		return
	}

	// Verify group exists
	group, err := model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Permission check:
	// - Group owner can delete their own group
	// - Root admin can delete any group
	// - Other users must NOT be able to delete the group (PM-01)
	myRole := c.GetInt("role")
	myUserId := c.GetInt("id")
	if myRole != common.RoleRootUser && group.OwnerId != myUserId {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "只有分组所有者或超级管理员可以删除分组",
		})
		return
	}

	// Invalidate all member caches before deletion
	if err := model.InvalidateGroupMembersCache(groupId); err != nil {
		common.SysLog("failed to invalidate group member caches: " + err.Error())
		// Continue deletion even if cache invalidation fails
	}

	// Delete group (cascade delete members handled in model layer)
	if err := model.DeleteGroup(groupId); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "group deleted successfully",
		"data":    group,
	})
}

// GetPublicGroups returns paginated list of public shared groups (Type=Shared)
// GET /api/groups/public?page=1&page_size=20&keyword=searchterm&group_ids=1,2,3
func GetPublicGroups(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")
	groupIdsStr := c.Query("group_ids")

	// Parse comma-separated group_ids
	var groupIds []int
	if groupIdsStr != "" {
		parts := strings.Split(groupIdsStr, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if id, err := strconv.Atoi(part); err == nil {
				groupIds = append(groupIds, id)
			}
		}
	}

	groups, total, err := model.GetPublicSharedGroupsWithMemberCount(
		pageInfo.Page,
		pageInfo.GetPageSize(),
		keyword,
		groupIds,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}
