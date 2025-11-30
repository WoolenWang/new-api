package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ApplyToJoinGroup handles user application to join a P2P group
// POST /api/groups/apply
// Request body: { group_id, user_id, password }
// Returns: { status: "active" | "pending" | "rejected" }
func ApplyToJoinGroup(c *gin.Context) {
	var req struct {
		GroupId  int    `json:"group_id" binding:"required"`
		UserId   int    `json:"user_id"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	myUserId := c.GetInt("id")

	// If user_id is not specified, use authenticated user's ID
	if req.UserId == 0 {
		req.UserId = myUserId
	} else if req.UserId != myUserId {
		// Only admins can apply on behalf of other users
		if myRole != common.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "只有超级管理员可以为其他用户申请加入分组",
			})
			return
		}
	}

	// Get group information
	group, err := model.GetGroupById(req.GroupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Apply to join (handles password validation and status setting)
	status, err := model.ApplyToJoinGroup(req.UserId, req.GroupId, req.Password)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// If status is Active (password matched or invite-only), invalidate cache immediately
	if status == model.MemberStatusActive {
		if err := model.InvalidateUserGroupCache(req.UserId); err != nil {
			common.SysLog("failed to invalidate user group cache: " + err.Error())
		}
	}

	var statusText string
	switch status {
	case model.MemberStatusActive:
		statusText = "active"
	case model.MemberStatusPending:
		statusText = "pending"
	case model.MemberStatusRejected:
		statusText = "rejected"
	default:
		statusText = "unknown"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "application submitted",
		"data": gin.H{
			"group_id": req.GroupId,
			"user_id":  req.UserId,
			"status":   statusText,
			"group":    group,
		},
	})
}

// GetGroupMembers returns all members of a group with their status and user information
// GET /api/groups/members?group_id=101&status=0&page=1&page_size=20
func GetGroupMembers(c *gin.Context) {
	groupIdStr := c.Query("group_id")
	if groupIdStr == "" {
		common.ApiError(c, errors.New("group_id is required"))
		return
	}

	groupId, err := strconv.Atoi(groupIdStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group_id"))
		return
	}

	// Optional status filter
	var status *int
	statusStr := c.Query("status")
	if statusStr != "" {
		statusVal, err := strconv.Atoi(statusStr)
		if err != nil {
			common.ApiError(c, errors.New("invalid status"))
			return
		}
		status = &statusVal
	}

	// Get pagination parameters
	pageInfo := common.GetPageQuery(c)

	// Get members with user info
	members, total, err := model.GetGroupMembersWithUserInfo(
		groupId,
		status,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(members)
	common.ApiSuccess(c, pageInfo)
}

// UpdateMemberStatus handles approval/rejection/ban operations
// PUT /api/groups/members
// Request body: { group_id, user_id, status, role }
func UpdateMemberStatus(c *gin.Context) {
	var req struct {
		GroupId int `json:"group_id" binding:"required"`
		UserId  int `json:"user_id" binding:"required"`
		Status  int `json:"status"` // Optional: new status (1=Active, 2=Rejected, 3=Banned)
		Role    int `json:"role"`   // Optional: new role (0=Member, 1=Admin)
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// Get current member info
	currentMember, err := model.GetMemberInfo(req.GroupId, req.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Update status if provided
	if req.Status != 0 {
		if err := model.UpdateMemberStatus(req.GroupId, req.UserId, req.Status); err != nil {
			common.ApiError(c, err)
			return
		}

		// Invalidate user's group cache when status changes
		if err := model.InvalidateUserGroupCache(req.UserId); err != nil {
			common.SysLog("failed to invalidate user group cache: " + err.Error())
		}
	}

	// Update role if provided
	if req.Role != 0 {
		if err := model.UpdateMemberRole(req.GroupId, req.UserId, req.Role); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	// Get updated member info
	updatedMember, err := model.GetMemberInfo(req.GroupId, req.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "member updated successfully",
		"data": gin.H{
			"before": currentMember,
			"after":  updatedMember,
		},
	})
}

// LeaveGroup handles user voluntarily leaving a group
// POST /api/groups/leave
// Request body: { group_id, user_id }
func LeaveGroup(c *gin.Context) {
	var req struct {
		GroupId int `json:"group_id" binding:"required"`
		UserId  int `json:"user_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	myUserId := c.GetInt("id")

	// If user_id is not specified, use authenticated user's ID
	if req.UserId == 0 {
		req.UserId = myUserId
	} else if req.UserId != myUserId {
		// Only admins can remove other users from groups
		if myRole != common.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "只有超级管理员可以让其他用户退出分组",
			})
			return
		}
	}

	// Get group info before leaving
	group, err := model.GetGroupById(req.GroupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Prevent owner from leaving their own group
	if group.OwnerId == req.UserId {
		common.ApiError(c, errors.New("group owner cannot leave, please transfer ownership or delete the group"))
		return
	}

	// Leave group (sets status to Left)
	if err := model.LeaveGroup(req.UserId, req.GroupId); err != nil {
		common.ApiError(c, err)
		return
	}

	// Invalidate user's group cache
	if err := model.InvalidateUserGroupCache(req.UserId); err != nil {
		common.SysLog("failed to invalidate user group cache: " + err.Error())
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "left group successfully",
		"data": gin.H{
			"group_id": req.GroupId,
			"user_id":  req.UserId,
		},
	})
}

// GetMemberInfo returns detailed information about a specific member in a group
// GET /api/groups/member?group_id=101&user_id=123
func GetMemberInfo(c *gin.Context) {
	groupIdStr := c.Query("group_id")
	userIdStr := c.Query("user_id")

	if groupIdStr == "" || userIdStr == "" {
		common.ApiError(c, errors.New("group_id and user_id are required"))
		return
	}

	groupId, err := strconv.Atoi(groupIdStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group_id"))
		return
	}

	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid user_id"))
		return
	}

	member, err := model.GetMemberInfo(groupId, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, member)
}
