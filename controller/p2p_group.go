package controller

import (
	"net/http"
	"strconv"

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

	// Validate required fields
	if group.Name == "" || group.OwnerId == 0 {
		common.ApiError(c, common.NewError("name and owner_id are required"))
		return
	}

	// Create group
	if err := model.CreateGroup(&group); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, group)
}

// GetUserOwnedGroups returns all groups created by a specific user
// GET /api/groups/self?user_id=123
func GetUserOwnedGroups(c *gin.Context) {
	userIdStr := c.Query("user_id")
	if userIdStr == "" {
		common.ApiError(c, common.NewError("user_id is required"))
		return
	}

	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		common.ApiError(c, common.NewError("invalid user_id"))
		return
	}

	groups, err := model.GetGroupsByOwner(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, groups)
}

// GetUserJoinedGroups returns all P2P groups a user has joined (Status=Active)
// GET /api/groups/joined?user_id=123
func GetUserJoinedGroups(c *gin.Context) {
	userIdStr := c.Query("user_id")
	if userIdStr == "" {
		common.ApiError(c, common.NewError("user_id is required"))
		return
	}

	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		common.ApiError(c, common.NewError("invalid user_id"))
		return
	}

	groups, err := model.GetUserGroups(userId, model.MemberStatusActive)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, groups)
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
		common.ApiError(c, common.NewError("group id is required"))
		return
	}

	// Verify group exists and get current owner
	existingGroup, err := model.GetGroupById(group.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Security check: Only owner can update group
	// This check can be bypassed by admin token, handled in middleware
	if existingGroup.OwnerId != group.OwnerId && group.OwnerId != 0 {
		common.ApiError(c, common.NewError("only group owner can update group"))
		return
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
			common.ApiError(c, common.NewError("group id is required"))
			return
		}
		idStr = strconv.Itoa(req.Id)
	}

	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, common.NewError("invalid group id"))
		return
	}

	// Verify group exists
	group, err := model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
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
// GET /api/groups/public?page=1&page_size=20&keyword=searchterm
func GetPublicGroups(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	groups, total, err := model.GetPublicSharedGroups(
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
		keyword,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(groups)
	common.ApiSuccess(c, pageInfo)
}
