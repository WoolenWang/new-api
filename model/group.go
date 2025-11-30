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
			// 允许重新申请,更新状态
			existing.Status = MemberStatusPending
			existing.UpdatedAt = common.GetTimestamp()
			return MemberStatusPending, DB.Save(&existing).Error
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
