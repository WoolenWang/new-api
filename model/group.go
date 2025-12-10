package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// P2P分组类型常量
const (
	GroupTypePrivate = 1 // 私有分组
	GroupTypeShared  = 2 // 共享分组
)

// P2P分组加入方式常量
const (
	JoinMethodInvite   = 0 // 仅邀请
	JoinMethodApproval = 1 // 公开审核
	JoinMethodPassword = 2 // 密码加入
)

// 成员状态常量
const (
	MemberStatusPending  = 0 // 申请中
	MemberStatusActive   = 1 // 已加入
	MemberStatusRejected = 2 // 已拒绝
	MemberStatusBanned   = 3 // 已踢出
	MemberStatusLeft     = 4 // 主动退出
)

// 成员角色常量
const (
	MemberRoleMember = 0 // 普通成员
	MemberRoleAdmin  = 1 // 管理员
)

// Group P2P分组表 - 存储分组元数据
type Group struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string `json:"name" gorm:"type:varchar(50);not null;index"` // 分组唯一标识/代号
	DisplayName string `json:"display_name" gorm:"type:varchar(100)"`       // 显示名称
	OwnerId     int    `json:"owner_id" gorm:"type:int;not null;index"`     // 拥有者ID (NewAPI User ID)
	Type        int    `json:"type" gorm:"type:int;default:1"`              // 类型: 1=Private, 2=Shared
	JoinMethod  int    `json:"join_method" gorm:"type:int;default:0"`       // 加入方式: 0=邀请, 1=审核, 2=密码
	JoinKey     string `json:"join_key" gorm:"type:varchar(50)"`            // 加入密码/Key
	Description string `json:"description" gorm:"type:text"`                // 描述
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

// GroupWithMemberCount 分组信息（包含成员数量）
type GroupWithMemberCount struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	OwnerId     int    `json:"owner_id"`
	Type        int    `json:"type"`
	JoinMethod  int    `json:"join_method"`
	JoinKey     string `json:"join_key"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	MemberCount int64  `json:"member_count"` // 成员数量（仅包含Active状态的成员）
}

// UserGroup 用户-分组关联表 - 存储成员关系及状态
type UserGroup struct {
	Id        int   `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int   `json:"user_id" gorm:"type:int;not null;index;uniqueIndex:idx_user_group"`  // 成员ID
	GroupId   int   `json:"group_id" gorm:"type:int;not null;index;uniqueIndex:idx_user_group"` // 分组ID
	Role      int   `json:"role" gorm:"type:int;default:0"`                                     // 角色: 0=成员, 1=管理员
	Status    int   `json:"status" gorm:"type:int;default:0;index"`                             // 状态: 0=申请中, 1=已加入, 2=已拒绝, 3=已踢出, 4=主动退出
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// GroupMemberWithUser 分组成员信息（包含用户详情）
type GroupMemberWithUser struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id"`
	GroupId     int    `json:"group_id"`
	Role        int    `json:"role"`
	Status      int    `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// ========== Group CRUD ==========

// CreateGroup 创建新分组
func CreateGroup(group *Group) error {
	if group.Name == "" {
		return errors.New("分组名称不能为空")
	}
	if group.OwnerId == 0 {
		return errors.New("拥有者ID不能为空")
	}

	// 设置时间戳
	now := common.GetTimestamp()
	group.CreatedAt = now
	group.UpdatedAt = now

	// 检查分组名是否重复 (同一owner下不允许重复)
	var existingGroup Group
	err := DB.Where("owner_id = ? AND name = ?", group.OwnerId, group.Name).First(&existingGroup).Error
	if err == nil {
		return fmt.Errorf("分组名称 %s 已存在", group.Name)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return DB.Create(group).Error
}

// GetGroupById 根据ID获取分组
func GetGroupById(id int) (*Group, error) {
	if id == 0 {
		return nil, errors.New("分组ID不能为空")
	}
	var group Group
	err := DB.Where("id = ?", id).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// GetGroupsByOwner 获取指定用户创建的所有分组
func GetGroupsByOwner(ownerId int) ([]*Group, error) {
	var groups []*Group
	err := DB.Where("owner_id = ?", ownerId).Find(&groups).Error
	return groups, err
}

// CountGroupsByOwner 计算指定用户创建的分组数量
func CountGroupsByOwner(ownerId int) (int64, error) {
	var count int64
	err := DB.Model(&Group{}).Where("owner_id = ?", ownerId).Count(&count).Error
	return count, err
}

// GetGroupsByOwnerPaginated 获取指定用户创建的所有分组 (分页版本)
func GetGroupsByOwnerPaginated(ownerId int, startIdx int, pageSize int) ([]*Group, int64, error) {
	var groups []*Group
	var total int64

	query := DB.Model(&Group{}).Where("owner_id = ?", ownerId)

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	err = query.Order("created_at DESC").Limit(pageSize).Offset(startIdx).Find(&groups).Error
	return groups, total, err
}

// GetPublicSharedGroups 获取所有公开的共享分组 (用于分组广场)
func GetPublicSharedGroups(page, pageSize int, keyword string) ([]*Group, int64, error) {
	var groups []*Group
	var total int64

	query := DB.Model(&Group{}).Where("type = ?", GroupTypeShared)

	// 关键词搜索
	if keyword != "" {
		likePattern := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR display_name LIKE ? OR description LIKE ?",
			likePattern, likePattern, likePattern)
	}

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err = query.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&groups).Error
	return groups, total, err
}

// GetGroupsByOwnerWithMemberCount 获取指定用户创建的所有分组（带成员数量，分页版本）
func GetGroupsByOwnerWithMemberCount(ownerId int, startIdx int, pageSize int) ([]*GroupWithMemberCount, int64, error) {
	var groups []*Group
	var total int64

	// 第一步：分页查询分组列表
	query := DB.Model(&Group{}).Where("owner_id = ?", ownerId)

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	err = query.Order("created_at DESC").Limit(pageSize).Offset(startIdx).Find(&groups).Error
	if err != nil {
		return nil, 0, err
	}

	// 第二步：为当前页的分组批量查询成员数量
	if len(groups) == 0 {
		return []*GroupWithMemberCount{}, total, nil
	}

	// 提取分组ID列表
	groupIds := make([]int, len(groups))
	for i, g := range groups {
		groupIds[i] = g.Id
	}

	// 批量查询成员数量
	type MemberCountResult struct {
		GroupId int   `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	var memberCounts []MemberCountResult
	err = DB.Table("user_groups").
		Select("group_id, COUNT(*) as count").
		Where("group_id IN ? AND status = ?", groupIds, MemberStatusActive).
		Group("group_id").
		Scan(&memberCounts).Error
	if err != nil {
		return nil, 0, err
	}

	// 构建成员数量映射
	countMap := make(map[int]int64)
	for _, mc := range memberCounts {
		countMap[mc.GroupId] = mc.Count
	}

	// 组装结果
	result := make([]*GroupWithMemberCount, len(groups))
	for i, g := range groups {
		result[i] = &GroupWithMemberCount{
			Id:          g.Id,
			Name:        g.Name,
			DisplayName: g.DisplayName,
			OwnerId:     g.OwnerId,
			Type:        g.Type,
			JoinMethod:  g.JoinMethod,
			JoinKey:     g.JoinKey,
			Description: g.Description,
			CreatedAt:   g.CreatedAt,
			UpdatedAt:   g.UpdatedAt,
			MemberCount: countMap[g.Id], // 默认为0（如果不在map中）
		}
	}

	return result, total, nil
}

// GetUserGroupsWithMemberCount 获取用户加入的所有分组（带成员数量，仅Active状态，分页版本）
func GetUserGroupsWithMemberCount(userId int, startIdx int, pageSize int) ([]*GroupWithMemberCount, int64, error) {
	var groups []*Group
	var total int64

	// 第一步：分页查询用户加入的分组列表
	query := DB.Table("groups").
		Joins("INNER JOIN user_groups ON groups.id = user_groups.group_id").
		Where("user_groups.user_id = ? AND user_groups.status = ?", userId, MemberStatusActive)

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	err = query.Order("groups.created_at DESC").
		Limit(pageSize).
		Offset(startIdx).
		Find(&groups).Error
	if err != nil {
		return nil, 0, err
	}

	// 第二步：为当前页的分组批量查询成员数量
	if len(groups) == 0 {
		return []*GroupWithMemberCount{}, total, nil
	}

	// 提取分组ID列表
	groupIds := make([]int, len(groups))
	for i, g := range groups {
		groupIds[i] = g.Id
	}

	// 批量查询成员数量
	type MemberCountResult struct {
		GroupId int   `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	var memberCounts []MemberCountResult
	err = DB.Table("user_groups").
		Select("group_id, COUNT(*) as count").
		Where("group_id IN ? AND status = ?", groupIds, MemberStatusActive).
		Group("group_id").
		Scan(&memberCounts).Error
	if err != nil {
		return nil, 0, err
	}

	// 构建成员数量映射
	countMap := make(map[int]int64)
	for _, mc := range memberCounts {
		countMap[mc.GroupId] = mc.Count
	}

	// 组装结果
	result := make([]*GroupWithMemberCount, len(groups))
	for i, g := range groups {
		result[i] = &GroupWithMemberCount{
			Id:          g.Id,
			Name:        g.Name,
			DisplayName: g.DisplayName,
			OwnerId:     g.OwnerId,
			Type:        g.Type,
			JoinMethod:  g.JoinMethod,
			JoinKey:     g.JoinKey,
			Description: g.Description,
			CreatedAt:   g.CreatedAt,
			UpdatedAt:   g.UpdatedAt,
			MemberCount: countMap[g.Id], // 默认为0（如果不在map中）
		}
	}

	return result, total, nil
}

// GetPublicSharedGroupsWithMemberCount 获取所有公开的共享分组（带成员数量，用于分组广场）
// 支持通过groupIds和keyword过滤，如果同时提供两个参数，则返回交集
func GetPublicSharedGroupsWithMemberCount(page, pageSize int, keyword string, groupIds []int) ([]*GroupWithMemberCount, int64, error) {
	var groups []*Group
	var total int64

	// 第一步：分页查询公开分组列表
	query := DB.Model(&Group{}).Where("type = ?", GroupTypeShared)

	// 如果提供了groupIds，先按ID过滤
	if len(groupIds) > 0 {
		query = query.Where("id IN ?", groupIds)
	}

	// 关键词搜索（在ID过滤之后应用，实现交集效果）
	if keyword != "" {
		likePattern := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR display_name LIKE ? OR description LIKE ?",
			likePattern, likePattern, likePattern)
	}

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err = query.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&groups).Error
	if err != nil {
		return nil, 0, err
	}

	// 第二步：为当前页的分组批量查询成员数量
	if len(groups) == 0 {
		return []*GroupWithMemberCount{}, total, nil
	}

	// 提取分组ID列表
	resultGroupIds := make([]int, len(groups))
	for i, g := range groups {
		resultGroupIds[i] = g.Id
	}

	// 批量查询成员数量
	type MemberCountResult struct {
		GroupId int   `gorm:"column:group_id"`
		Count   int64 `gorm:"column:count"`
	}
	var memberCounts []MemberCountResult
	err = DB.Table("user_groups").
		Select("group_id, COUNT(*) as count").
		Where("group_id IN ? AND status = ?", resultGroupIds, MemberStatusActive).
		Group("group_id").
		Scan(&memberCounts).Error
	if err != nil {
		return nil, 0, err
	}

	// 构建成员数量映射
	countMap := make(map[int]int64)
	for _, mc := range memberCounts {
		countMap[mc.GroupId] = mc.Count
	}

	// 组装结果
	result := make([]*GroupWithMemberCount, len(groups))
	for i, g := range groups {
		result[i] = &GroupWithMemberCount{
			Id:          g.Id,
			Name:        g.Name,
			DisplayName: g.DisplayName,
			OwnerId:     g.OwnerId,
			Type:        g.Type,
			JoinMethod:  g.JoinMethod,
			JoinKey:     g.JoinKey,
			Description: g.Description,
			CreatedAt:   g.CreatedAt,
			UpdatedAt:   g.UpdatedAt,
			MemberCount: countMap[g.Id], // 默认为0（如果不在map中）
		}
	}

	return result, total, nil
}

// UpdateGroup 更新分组信息
func UpdateGroup(group *Group) error {
	if group.Id == 0 {
		return errors.New("分组ID不能为空")
	}

	group.UpdatedAt = common.GetTimestamp()
	return DB.Model(&Group{}).Where("id = ?", group.Id).Updates(group).Error
}

// DeleteGroup 删除分组 (会级联删除关联关系)
func DeleteGroup(id int) error {
	if id == 0 {
		return errors.New("分组ID不能为空")
	}

	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 删除所有成员关系
	if err := tx.Where("group_id = ?", id).Delete(&UserGroup{}).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 删除分组
	if err := tx.Delete(&Group{}, id).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 提交事务
	return tx.Commit().Error
}

// ========== UserGroup (成员管理) ==========

// ApplyToJoinGroup 申请加入分组
func ApplyToJoinGroup(userId, groupId int, password string) (int, error) {
	if userId == 0 || groupId == 0 {
		return 0, errors.New("用户ID和分组ID不能为空")
	}

	// 获取分组信息
	group, err := GetGroupById(groupId)
	if err != nil {
		return 0, err
	}

	// 检查是否已存在记录
	var existing UserGroup
	err = DB.Where("user_id = ? AND group_id = ?", userId, groupId).First(&existing).Error
	if err == nil {
		// 记录已存在,检查状态
		switch existing.Status {
		case MemberStatusActive:
			return 0, errors.New("您已经是该分组的成员")
		case MemberStatusPending:
			return 0, errors.New("您的申请正在审核中")
		case MemberStatusRejected, MemberStatusBanned, MemberStatusLeft:
			// 允许重新申请,但需要根据加入方式决定状态
			// 如果是密码模式,需要验证密码
			if group.JoinMethod == JoinMethodPassword {
				if password != group.JoinKey {
					return 0, errors.New("密码错误")
				}
				existing.Status = MemberStatusActive // 密码正确,直接激活
			} else {
				existing.Status = MemberStatusPending // 其他模式,待审核
			}
			existing.UpdatedAt = common.GetTimestamp()
			return existing.Status, DB.Save(&existing).Error
		}
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	// 创建新的成员关系
	userGroup := &UserGroup{
		UserId:    userId,
		GroupId:   groupId,
		Role:      MemberRoleMember,
		CreatedAt: common.GetTimestamp(),
		UpdatedAt: common.GetTimestamp(),
	}

	// 根据加入方式决定初始状态
	switch group.JoinMethod {
	case JoinMethodPassword:
		// 密码模式:验证密码
		if password != group.JoinKey {
			return 0, errors.New("密码错误")
		}
		userGroup.Status = MemberStatusActive // 直接通过
	case JoinMethodApproval:
		// 审核模式:待审核
		userGroup.Status = MemberStatusPending
	case JoinMethodInvite:
		// 仅邀请:默认待审核 (需要owner手动通过)
		userGroup.Status = MemberStatusPending
	default:
		userGroup.Status = MemberStatusPending
	}

	err = DB.Create(userGroup).Error
	return userGroup.Status, err
}

// GetGroupMembers 获取分组的所有成员
func GetGroupMembers(groupId int) ([]*UserGroup, error) {
	var members []*UserGroup
	err := DB.Where("group_id = ?", groupId).Find(&members).Error
	return members, err
}

// GetGroupMembersWithUserInfo 获取分组成员列表（包含用户信息、支持分页和状态过滤）
func GetGroupMembersWithUserInfo(groupId int, status *int, startIdx int, pageSize int) ([]*GroupMemberWithUser, int64, error) {
	var members []*GroupMemberWithUser
	var total int64

	// 构建查询
	query := DB.Table("user_groups").
		Select("user_groups.id, user_groups.user_id, user_groups.group_id, user_groups.role, user_groups.status, user_groups.created_at, user_groups.updated_at, users.username, users.display_name, users.email").
		Joins("LEFT JOIN users ON user_groups.user_id = users.id").
		Where("user_groups.group_id = ?", groupId)

	// 如果指定了status，添加状态过滤
	if status != nil {
		query = query.Where("user_groups.status = ?", *status)
	}

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	err = query.Order("user_groups.created_at DESC").
		Limit(pageSize).
		Offset(startIdx).
		Scan(&members).Error

	return members, total, err
}

// GetUserGroups 获取用户加入的所有分组 (仅Active状态)
func GetUserGroups(userId int) ([]*UserGroup, error) {
	var userGroups []*UserGroup
	err := DB.Where("user_id = ? AND status = ?", userId, MemberStatusActive).Find(&userGroups).Error
	return userGroups, err
}

// GetUserGroupsPaginated 获取用户加入的所有分组的完整信息 (仅Active状态, 分页版本)
// 返回完整的Group对象，而不是UserGroup关系对象
func GetUserGroupsPaginated(userId int, startIdx int, pageSize int) ([]*Group, int64, error) {
	var groups []*Group
	var total int64

	// 使用JOIN查询获取完整分组信息
	query := DB.Table("groups").
		Joins("INNER JOIN user_groups ON groups.id = user_groups.group_id").
		Where("user_groups.user_id = ? AND user_groups.status = ?", userId, MemberStatusActive)

	// 获取总数
	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	// 分页查询
	err = query.Order("groups.created_at DESC").Limit(pageSize).Offset(startIdx).Find(&groups).Error
	return groups, total, err
}

// GetUserActiveGroupIds 获取用户加入的所有分组ID (仅Active状态)
func GetUserActiveGroupIds(userId int) ([]int, error) {
	var groupIds []int
	err := DB.Model(&UserGroup{}).
		Where("user_id = ? AND status = ?", userId, MemberStatusActive).
		Pluck("group_id", &groupIds).Error
	return groupIds, err
}

// UpdateMemberStatus 更新成员状态 (审批/踢出等)
func UpdateMemberStatus(groupId, userId, newStatus int) error {
	if groupId == 0 || userId == 0 {
		return errors.New("分组ID和用户ID不能为空")
	}

	// 验证新状态
	if newStatus < MemberStatusPending || newStatus > MemberStatusLeft {
		return errors.New("无效的状态值")
	}

	return DB.Model(&UserGroup{}).
		Where("group_id = ? AND user_id = ?", groupId, userId).
		Updates(map[string]interface{}{
			"status":     newStatus,
			"updated_at": common.GetTimestamp(),
		}).Error
}

// UpdateMemberRole 更新成员角色
func UpdateMemberRole(groupId, userId, newRole int) error {
	if groupId == 0 || userId == 0 {
		return errors.New("分组ID和用户ID不能为空")
	}

	if newRole != MemberRoleMember && newRole != MemberRoleAdmin {
		return errors.New("无效的角色值")
	}

	return DB.Model(&UserGroup{}).
		Where("group_id = ? AND user_id = ?", groupId, userId).
		Updates(map[string]interface{}{
			"role":       newRole,
			"updated_at": common.GetTimestamp(),
		}).Error
}

// LeaveGroup 用户主动退出分组
func LeaveGroup(userId, groupId int) error {
	if userId == 0 || groupId == 0 {
		return errors.New("用户ID和分组ID不能为空")
	}

	return UpdateMemberStatus(groupId, userId, MemberStatusLeft)
}

// IsGroupOwner 检查用户是否为分组所有者
func IsGroupOwner(userId, groupId int) (bool, error) {
	var group Group
	err := DB.Where("id = ? AND owner_id = ?", groupId, userId).First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetMemberInfo 获取成员信息
func GetMemberInfo(groupId, userId int) (*UserGroup, error) {
	var userGroup UserGroup
	err := DB.Where("group_id = ? AND user_id = ?", groupId, userId).First(&userGroup).Error
	if err != nil {
		return nil, err
	}
	return &userGroup, nil
}
