package system

import "github.com/gin-gonic/gin"

// RegisterRoutes 把系统管理（RBAC）四个子模块注册到给定路由组。
//
// 调用方需保证该组已挂 AuthRequired + GuardByPath（见 main.go）。
// 路径刻意避开 ":id" 与静态段同级的情况（detail/:id、delete/:id 代替裸 :id），
// 以免依赖 gin 版本对"静态 vs 通配"兄弟节点的容忍度。
func RegisterRoutes(rg *gin.RouterGroup, h *H) {
	// ---- 权限下发（任何登录用户） ----
	rg.GET("/perm/my", h.PermMy)
	rg.GET("/perm/routes", h.PermRoutes)
	rg.GET("/perm/consistency", h.PermConsistency)
	// 全局拦截开关：声明为 @auth，真正的门在 handler 内的硬性超管校验 ——
	// 这样即使在 log（只记不拦）观察期，也不可能被非超管切到 off。
	rg.POST("/perm/enforce", h.SetEnforce)

	// ---- 用户管理 ----
	rg.GET("/user/list", h.ListUsers)
	rg.GET("/user/detail/:id", h.GetUser)
	rg.POST("/user", h.CreateUser)
	rg.PUT("/user", h.UpdateUser)
	rg.POST("/user/delete/:id", h.DeleteUser)
	rg.POST("/user/resetPwd", h.ResetPwd)
	rg.POST("/user/changeStatus", h.ChangeStatus)
	rg.POST("/user/authRole", h.AuthRole)

	// ---- 角色管理 ----
	rg.GET("/role/list", h.ListRoles)
	// 可分配角色选择器：任何能管理用户的人都必须能读到它（响应已按调用者权限裁剪），
	// 否则部门管理员的「新建/编辑用户」会因为读不到角色列表而无法分配角色。
	rg.GET("/role/options", h.RoleOptions)
	rg.GET("/role/detail/:id", h.GetRole)
	rg.POST("/role", h.CreateRole)
	rg.PUT("/role", h.UpdateRole)
	rg.POST("/role/delete/:id", h.DeleteRole)
	rg.GET("/role/menuTree/:roleId", h.RoleMenuTree)
	rg.PUT("/role/menu", h.UpdateRoleMenu)
	rg.GET("/role/deptTree/:roleId", h.RoleDeptTree)
	rg.PUT("/role/dataScope", h.UpdateRoleDataScope)
	rg.POST("/role/changeStatus", h.ChangeRoleStatus)

	// ---- 菜单 / 权限管理 ----
	rg.GET("/menu/list", h.ListMenus)
	rg.GET("/menu/detail/:id", h.GetMenu)
	rg.POST("/menu", h.CreateMenu)
	rg.PUT("/menu", h.UpdateMenu)
	rg.POST("/menu/delete/:id", h.DeleteMenu)

	// ---- 部门管理 ----
	rg.GET("/dept/tree", h.DeptTree)
	rg.GET("/dept/treeSelect", h.DeptTreeSelect)
	rg.POST("/dept", h.CreateDept)
	rg.PUT("/dept", h.UpdateDept)
	rg.POST("/dept/delete/:id", h.DeleteDept)
}
