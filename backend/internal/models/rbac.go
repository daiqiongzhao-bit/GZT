package models

import "time"

// ============================================================================
// RBAC 实体（v0.39.0）：角色 / 菜单 / 三张关联表
//
// 命名说明：models.Role 已被"角色枚举"占用（super_admin/dept_admin/executor），
// 因此实体统一加 Sys 前缀；但 TableName() 仍按方案 §2.3 的约定返回
// roles / menus / user_roles / role_menus / role_depts（与 RuoYi 对照表一致）。
//
// 依赖约束（方案 §7.3）：本文件只 import 标准库，保持 models 是叶子包。
// ============================================================================

// 菜单类型：M 目录 / C 菜单 / F 按钮。
// 方案 §5.1（v1.1）明确删除 v1.0 提出的第 4 类"接口 I"：
// 接口权限的唯一事实来源是代码，把它建成菜单节点只会造成双份配置漂移。
const (
	MenuTypeDir    = "M"
	MenuTypeMenu   = "C"
	MenuTypeButton = "F"
)

// 数据范围五档（方案 §4.5）。数值直接落库到 roles.data_scope。
const (
	DataScopeAll         = 1 // 全部（通常只给超管）
	DataScopeCustom      = 2 // 自定义部门（必须至少 1 个部门，否则写入侧拒绝）
	DataScopeDept        = 3 // 本部门
	DataScopeDeptAndChil = 4 // 本部门及以下（部门管理员标准档）
	DataScopeSelf        = 5 // 仅本人（默认档，最安全）
)

// 内置角色标识（方案 §13.4）。内置角色不可删除、role_key 不可改、不可停用。
const (
	RoleKeySuperAdmin = "super_admin"
	RoleKeyDeptAdmin  = "dept_admin"
	RoleKeyExecutor   = "executor"
)

// SysRole 角色。role_key 是稳定的程序标识；role_name 只是显示名，可自由改。
type SysRole struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	RoleName          string    `json:"role_name" gorm:"size:64;not null"`
	RoleKey           string    `json:"role_key" gorm:"size:64;not null;uniqueIndex:uk_roles_role_key"`
	RoleSort          int       `json:"role_sort" gorm:"default:0"`
	DataScope         int       `json:"data_scope" gorm:"default:5"`
	MenuCheckStrictly int       `json:"menu_check_strictly" gorm:"default:1"`
	DeptCheckStrictly int       `json:"dept_check_strictly" gorm:"default:1"`
	Status            int       `json:"status" gorm:"default:0"` // 0 正常 / 1 停用
	Remark            string    `json:"remark" gorm:"size:255"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (SysRole) TableName() string { return "roles" }

// SysMenu 菜单/按钮/权限标识。
type SysMenu struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ParentID  uint      `json:"parent_id" gorm:"default:0;index:idx_menus_parent"`
	MenuName  string    `json:"menu_name" gorm:"size:64;not null"`
	OrderNum  int       `json:"order_num" gorm:"default:0"`
	Path      string    `json:"path" gorm:"size:128"`      // 路由地址（M/C）
	Component string    `json:"component" gorm:"size:128"` // 组件路径（C）
	Query     string    `json:"query" gorm:"size:255"`     // 路由参数 JSON
	IsFrame   int       `json:"is_frame" gorm:"default:0"` // 外链
	IsCache   int       `json:"is_cache" gorm:"default:0"` // keep-alive
	MenuType  string    `json:"menu_type" gorm:"size:2;not null;index:idx_menus_type"`
	Visible   int       `json:"visible" gorm:"default:0"` // 0 显示 / 1 隐藏
	Status    int       `json:"status" gorm:"default:0"`  // 0 正常 / 1 停用
	Perms     string    `json:"perms" gorm:"size:128"`    // 权限标识 module:res:action
	Icon      string    `json:"icon" gorm:"size:64"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SysMenu) TableName() string { return "menus" }

// SysUserRole 用户 ↔ 角色（多对多）。
type SysUserRole struct {
	UserID uint `json:"user_id" gorm:"primaryKey"`
	RoleID uint `json:"role_id" gorm:"primaryKey;index:idx_user_roles_role"`
}

func (SysUserRole) TableName() string { return "user_roles" }

// SysRoleMenu 角色 ↔ 菜单（功能权限链）。
type SysRoleMenu struct {
	RoleID uint `json:"role_id" gorm:"primaryKey"`
	MenuID uint `json:"menu_id" gorm:"primaryKey;index:idx_role_menus_menu"`
}

func (SysRoleMenu) TableName() string { return "role_menus" }

// SysRoleDept 角色 ↔ 自定义部门（仅 data_scope=2 时生效）。
type SysRoleDept struct {
	RoleID uint `json:"role_id" gorm:"primaryKey"`
	DeptID uint `json:"dept_id" gorm:"primaryKey;index:idx_role_depts_dept"`
}

func (SysRoleDept) TableName() string { return "role_depts" }
