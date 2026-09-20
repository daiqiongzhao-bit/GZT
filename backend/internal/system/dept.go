package system

import (
	"net/http"
	"strings"

	"shiftworkbench/internal/models"
	"shiftworkbench/internal/rbac"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============================================================================
// 部门管理（方案 §6）
//
// 部门是唯一被"功能权限"和"数据权限"两条链共用的实体：
//   - 用户的归属维度（users.dept_id）
//   - 数据权限的边界载体（ancestors 决定"本部门及以下"展开成什么）
//
// 三条铁律（§6.3）：新增补 ancestors；移动级联重写子孙 ancestors；停用不影响存量用户。
// ============================================================================

type deptNode struct {
	ID        uint       `json:"id"`
	Name      string     `json:"name"`
	ParentID  uint       `json:"parent_id"`
	Ancestors string     `json:"ancestors"`
	Status    int        `json:"status"`
	Leader    string     `json:"leader"`
	OrderNum  int        `json:"order_num"`
	UserCount int64      `json:"user_count"`
	Checked   bool       `json:"checked"`
	Children  []deptNode `json:"children,omitempty"`
}

// deptTree 构建部门树。onlyActive=true 时过滤掉停用部门（用于"选择器"，不用于权限计算）。
func (h *H) deptTree(checked map[uint]bool, onlyActive bool) []deptNode {
	var all []models.Department
	q := h.DB.Order("parent_id, order_num, id")
	if onlyActive {
		q = q.Where("status = 0")
	}
	_ = q.Find(&all).Error

	cnt := map[uint]int64{}
	var rows []struct {
		DeptID uint
		N      int64
	}
	_ = h.DB.Raw(`SELECT dept_id, COUNT(*) AS n FROM users GROUP BY dept_id`).Scan(&rows).Error
	for _, r := range rows {
		cnt[r.DeptID] = r.N
	}
	var build func(parent uint) []deptNode
	build = func(parent uint) []deptNode {
		var out []deptNode
		for _, d := range all {
			if d.ParentID != parent {
				continue
			}
			out = append(out, deptNode{
				ID: d.ID, Name: d.Name, ParentID: d.ParentID, Ancestors: d.Ancestors,
				Status: d.Status, Leader: d.Leader, OrderNum: d.OrderNum,
				UserCount: cnt[d.ID], Checked: checked != nil && checked[d.ID],
				Children: build(d.ID),
			})
		}
		return out
	}
	return build(0)
}

// DeptTree 部门管理用（需要 system:dept:list）。
func (h *H) DeptTree(c *gin.Context) {
	ok(c, gin.H{"tree": h.deptTree(nil, false)})
}

// DeptTreeSelect 表单部门选择器：登录即可，但**按当前用户数据范围过滤**（方案 §9.3：
// 下拉/树选择器也属于"返回数据的接口"，必须过滤）。停用部门不出现在选择器里（§4.6）。
func (h *H) DeptTreeSelect(c *gin.Context) {
	scope, err := ScopeOf(c)
	if err != nil {
		fail(c, http.StatusInternalServerError, "解析数据范围失败："+err.Error())
		return
	}
	full := h.deptTree(nil, true)
	if scope.All {
		ok(c, gin.H{"tree": full})
		return
	}
	allowed := map[uint]bool{}
	for _, d := range scope.DeptIDs {
		allowed[uint(d)] = true
	}
	// 「仅本人」/ 兜底 / 自定义部门为空时 DeptIDs 为空，但表单选择器仍需给出本人所属部门，
	// 否则这类角色的用户打开用户管理会得到一个空下拉。语义与 ScopeIDsOf 的兜底保持一致。
	// filterDeptTree 会保留命中节点的祖先链，因此这里只需补上锚点部门本身。
	if len(scope.DeptIDs) == 0 && scope.DeptID != 0 {
		allowed[uint(scope.DeptID)] = true
	}
	ok(c, gin.H{"tree": filterDeptTree(full, allowed)})
}

func filterDeptTree(nodes []deptNode, allowed map[uint]bool) []deptNode {
	var out []deptNode
	for _, n := range nodes {
		n.Children = filterDeptTree(n.Children, allowed)
		if allowed[n.ID] || len(n.Children) > 0 {
			out = append(out, n)
		}
	}
	return out
}

type deptSaveReq struct {
	ID       uint   `json:"id"`
	ParentID uint   `json:"parent_id"`
	Name     string `json:"name"`
	Status   int    `json:"status"`
	Leader   string `json:"leader"`
	OrderNum int    `json:"order_num"`
}

func (h *H) CreateDept(c *gin.Context) {
	var req deptSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		badReq(c, "部门名称不能为空")
		return
	}
	anc := "0"
	if req.ParentID != 0 {
		var p models.Department
		if err := h.DB.First(&p, req.ParentID).Error; err != nil {
			badReq(c, "上级部门不存在")
			return
		}
		if p.Status != 0 {
			badReq(c, "上级部门已停用，不能在其下新增部门")
			return
		}
		// 铁律 1：child.ancestors = parent.ancestors + ',' + parent.id
		anc = p.Ancestors
		if anc == "" {
			anc = "0"
		}
		anc = anc + "," + itoa(uint64(p.ID))
	}
	d := models.Department{
		Name: name, ParentID: req.ParentID, Ancestors: anc,
		Status: req.Status, Leader: req.Leader, OrderNum: req.OrderNum,
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		var n int64
		tx.Model(&models.Department{}).Where("parent_id = ? AND name = ?", req.ParentID, name).Count(&n)
		if n > 0 {
			return errStr("同级下已存在同名部门")
		}
		return tx.Create(&d).Error
	})
	if err != nil {
		badReq(c, "创建失败："+err.Error())
		return
	}
	rbac.InvalidateDeptCache()
	// 部门树变更影响数据范围解析 → 全员失效（方案 §10.3 最隐蔽的坑）
	BumpPermEpoch()
	writeAudit(c, "dept", d.ID, d.Name, "create", nil, gin.H{"parent_id": d.ParentID, "ancestors": d.Ancestors})
	ok(c, gin.H{"id": d.ID, "ancestors": d.Ancestors})
}

func (h *H) UpdateDept(c *gin.Context) {
	var req deptSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badReq(c, "参数解析失败："+err.Error())
		return
	}
	var d models.Department
	if err := h.DB.First(&d, req.ID).Error; err != nil {
		fail(c, http.StatusNotFound, "部门不存在")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		badReq(c, "部门名称不能为空")
		return
	}
	if req.ParentID == d.ID {
		badReq(c, "上级部门不能是自己")
		return
	}
	// 防成环：新上级不能是"自己或自己的子孙"
	if req.ParentID != 0 {
		ids, err := rbac.GormDeptTree{DB: h.DB}.SubtreeIDs(int64(d.ID))
		if err == nil && rbac.ContainsInt64(ids, int64(req.ParentID)) {
			badReq(c, "不能把部门移动到它自己或它的子部门下")
			return
		}
	}
	before := gin.H{"name": d.Name, "parent_id": d.ParentID, "ancestors": d.Ancestors, "status": d.Status}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&models.Department{}).Where("id = ?", d.ID).Updates(map[string]interface{}{
			"name": name, "parent_id": req.ParentID, "status": req.Status,
			"leader": req.Leader, "order_num": req.OrderNum,
		}).Error; err != nil {
			return err
		}
		// 铁律 2：移动后必须级联重写所有子孙的 ancestors
		return backfillAncestorsTx(tx)
	})
	if err != nil {
		badReq(c, "更新失败："+err.Error())
		return
	}
	rbac.InvalidateDeptCache()
	BumpPermEpoch()
	writeAudit(c, "dept", d.ID, name, "update", before,
		gin.H{"name": name, "parent_id": req.ParentID, "status": req.Status})
	ok(c, nil)
}

// DeleteDept 删除部门：有子部门或有用户 → 拒绝并返回数量（方案 §4.6 / §6.1）。
func (h *H) DeleteDept(c *gin.Context) {
	id := queryID(c, "id")
	var d models.Department
	if err := h.DB.First(&d, id).Error; err != nil {
		fail(c, http.StatusNotFound, "部门不存在")
		return
	}
	var children, users int64
	h.DB.Model(&models.Department{}).Where("parent_id = ?", d.ID).Count(&children)
	h.DB.Model(&models.User{}).Where("dept_id = ?", d.ID).Count(&users)
	if children > 0 {
		badReq(c, "该部门下有 "+itoa(uint64(children))+" 个子部门，请先删除子部门")
		return
	}
	if users > 0 {
		badReq(c, "该部门下有 "+itoa(uint64(users))+" 个用户，请先把用户调整到其他部门")
		return
	}
	err := rbac.WithWrite(h.DB, func(tx *gorm.DB) error {
		if err := tx.Where("dept_id = ?", d.ID).Delete(&models.SysRoleDept{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Department{}, d.ID).Error
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除失败："+err.Error())
		return
	}
	rbac.InvalidateDeptCache()
	BumpPermEpoch()
	writeAudit(c, "dept", d.ID, d.Name, "remove", gin.H{"name": d.Name, "ancestors": d.Ancestors}, nil)
	ok(c, nil)
}

// backfillAncestorsTx 在事务内重算 ancestors（与 backfillAncestors 同逻辑，复用同一实现）。
func backfillAncestorsTx(tx *gorm.DB) error {
	var all []models.Department
	if err := tx.Find(&all).Error; err != nil {
		return err
	}
	byID := map[uint]models.Department{}
	for _, d := range all {
		byID[d.ID] = d
	}
	path := map[uint]string{}
	var walk func(id uint, guard map[uint]bool) string
	walk = func(id uint, guard map[uint]bool) string {
		if p, ok := path[id]; ok {
			return p
		}
		d, ok := byID[id]
		if !ok || guard[id] {
			return "0"
		}
		guard[id] = true
		var p string
		if d.ParentID == 0 {
			p = "0"
		} else if pp := walk(d.ParentID, guard); pp == "0" {
			p = "0," + itoa(uint64(d.ParentID))
		} else {
			p = pp + "," + itoa(uint64(d.ParentID))
		}
		path[id] = p
		return p
	}
	for _, d := range all {
		walk(d.ID, map[uint]bool{})
	}
	for _, d := range all {
		want := path[d.ID]
		if d.Ancestors == want {
			continue
		}
		if err := tx.Model(&models.Department{}).Where("id = ?", d.ID).
			Update("ancestors", want).Error; err != nil {
			return err
		}
	}
	return nil
}
