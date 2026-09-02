package models

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Role 角色：super_admin 超级管理员 / dept_admin 部门管理员 / executor 执行者
type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleDeptAdmin  Role = "dept_admin"
	RoleExecutor   Role = "executor"
)

// ClientType 令牌客户端类型，用于多端令牌隔离（架构预留，当前仅 web 使用）
type ClientType string

const (
	ClientWeb      ClientType = "web"
	ClientPWA      ClientType = "pwa"
	ClientExtension ClientType = "extension"
)

// 任务类型
const (
	TaskTypeDaily  = "daily"
	TaskTypeMonthly = "monthly"
	TaskTypeOnce   = "once"
)

// 任务状态
const (
	TaskStatusTodo = "todo"
	TaskStatusDone = "done"
)

type Department struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:64;not null;uniqueIndex"`
	ParentID  uint      `json:"parent_id" gorm:"default:0;index"` // 上级部门：0=顶级部门
	CreatedAt time.Time `json:"created_at"`
}

// ShiftConfig 部门班次定义：班次名称 + 上下班时间（各部门可不同）
type ShiftConfig struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	DeptID    uint      `json:"dept_id" gorm:"index"`
	Name      string    `json:"name" gorm:"size:16;not null"` // 早班 / 晚班 / 中班 ...
	StartTime string    `json:"start_time" gorm:"size:5"`     // 09:00
	EndTime   string    `json:"end_time" gorm:"size:5"`       // 18:00
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"size:64;not null;uniqueIndex"`
	PasswordHash string    `json:"-" gorm:"size:128;not null"`
	Name         string    `json:"name" gorm:"size:64;not null"`
	EmpNo        string    `json:"emp_no" gorm:"size:32;index"` // 工号：通知与名单展示用
	Mobile       string    `json:"mobile" gorm:"size:20"` // 手机号：企业微信@提醒用
	Role         Role      `json:"role" gorm:"size:24;not null;default:executor"`
	DeptID       uint      `json:"dept_id"`
	Frozen       bool      `json:"frozen"` // 冻结：禁止登录
	InGroup      bool      `json:"in_group" gorm:"default:false"` // 已加入企业微信通知群：推送@对象，名单中不重复列出
	TokenVersion uint      `json:"token_version"` // 令牌版本：自增即令所有已签发token失效
	Dept         *Department `json:"dept,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Schedule 班表：某日某部门某班次的多名当班人员
type Schedule struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Date      string    `json:"date" gorm:"size:10;not null;index"` // YYYY-MM-DD
	Shift     string    `json:"shift" gorm:"size:32;not null"`
	People    string    `json:"people" gorm:"type:text"` // JSON 数组：["林晓","陈默"]
	DeptID    uint      `json:"dept_id" gorm:"index"`
	CreatedAt time.Time `json:"created_at"`
}

// Task 任务：每日/每月/临时单次，支持逾期。
// Shift 班次归属：早班/晚班/早晚/全员（谁当班谁负责）
type Task struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	Title      string    `json:"title" gorm:"size:255;not null"`
	Type       string    `json:"type" gorm:"size:16;not null"`
	Shift      string    `json:"shift" gorm:"size:16;default:all"` // 早班/晚班/早晚/全员
	Time       string    `json:"time" gorm:"size:8"`              // 每日任务执行时间 HH:MM
	Deadline   string    `json:"deadline" gorm:"size:16"`         // 截止时间 YYYY-MM-DDTHH:MM
	Assignee   string    `json:"assignee" gorm:"size:64"`         // 负责人姓名（兼容旧数据，新任务不再必填）
	AssigneeID uint      `json:"assignee_id"`                      // 负责人用户ID（0=未分配/部门公共）
	CompletedBy string    `json:"completed_by" gorm:"size:64"`      // 完成人姓名（谁打的☑️）
	CompletedAt time.Time `json:"completed_at"`                      // 最近一次完成时间
	Status     string    `json:"status" gorm:"size:16;not null;default:todo"`
	Priority   string    `json:"priority" gorm:"size:16;default:medium"` // high/medium/low
	Note       string    `json:"note" gorm:"type:text"`
	DeptID     uint      `json:"dept_id" gorm:"index"`
	Overdue       bool      `json:"overdue" gorm:"-"`          // 瞬态：是否逾期
	DueToday      bool      `json:"due_today" gorm:"-"`        // 瞬态：今日是否应处理
	DueThisMonth  bool      `json:"due_this_month" gorm:"-"`   // 瞬态：本月是否应处理
	CreatedAt  time.Time `json:"created_at"`
}

// Webhook 部门机器人推送地址（AES 加密存储）
// Type: wecom 企业微信 / dingtalk 钉钉 / feishu 飞书；Secret 为对应加签密钥（加密存储）
type Webhook struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:64;not null"`
	Type      string    `json:"type" gorm:"size:16;not null;default:wecom"` // wecom|dingtalk|feishu
	URL       string    `json:"url" gorm:"type:text;not null"`             // 加密后
	Secret    string    `json:"-" gorm:"type:text"`                        // 加密后（加签密钥）
	DeptID    uint      `json:"dept_id" gorm:"index"`
	CreatedAt time.Time `json:"created_at"`
}

// Template 导入模板（任务 / 班表）：超管可修改，管理员可查看下载
type Template struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Type      string    `json:"type" gorm:"size:16;not null;index"` // task | schedule
	Name      string    `json:"name" gorm:"size:128;not null"`
	Content   string    `json:"content" gorm:"type:text"` // CSV 文本内容
	DeptID    uint      `json:"dept_id" gorm:"index"`
	CreatedBy string    `json:"created_by" gorm:"size:64"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskCompletion 任务完成记录（审计日志：谁在何时完成）
type TaskCompletion struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	TaskID     uint      `json:"task_id" gorm:"index"`
	TaskTitle  string    `json:"task_title" gorm:"size:255"`
	UserID     uint      `json:"user_id"`
	UserName   string    `json:"user_name" gorm:"size:64"`
	DeptID     uint      `json:"dept_id" gorm:"index"`
	CompletedAt time.Time `json:"completed_at"`
}

// Setting 企业品牌设置（单行）
type Setting struct {
	ID           uint   `json:"-" gorm:"primaryKey"`
	CompanyName  string `json:"company_name" gorm:"size:128"`
	Slogan       string `json:"slogan" gorm:"size:255"`
	Version      string `json:"version" gorm:"size:16"`
	Copyright    string `json:"copyright" gorm:"size:255"`
	Logo         string `json:"logo" gorm:"size:128"` // 企业 Logo 文件名，存于数据目录下
	// 邮件通知（SMTP）配置
	SmtpHost     string `json:"smtp_host" gorm:"size:128"`
	SmtpPort     int    `json:"smtp_port"`
	SmtpUser     string `json:"smtp_user" gorm:"size:128"`
	SmtpPass     string `json:"-"`
	SmtpFrom     string `json:"smtp_from" gorm:"size:128"`
	NotifyEmails string `json:"notify_emails" gorm:"type:text"` // 逗号分隔的接收邮箱
	// 审计日志保留天数（0 表示永久保留）
	LogRetentionDays int `json:"log_retention_days" gorm:"default:90"`
	// 系统时区（影响任务逾期/今日判定/到点推送）
	Timezone string `json:"timezone" gorm:"size:64;default:Asia/Shanghai"`
}

// Log 系统操作日志
type Log struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Action    string    `json:"action" gorm:"size:255"`
	UserID    uint      `json:"user_id"`
	UserName  string    `json:"user_name" gorm:"size:64"`
	IP        string    `json:"ip" gorm:"size:64"`  // 操作来源 IP（全部操作留痕）
	UA        string    `json:"ua" gorm:"size:255"` // 操作来源 User-Agent
	CreatedAt time.Time `json:"created_at"`
}

// Notification 站内通知：管理员修改与用户相关信息时推送给当事人
type Notification struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index"`          // 接收人
	Kind      string    `json:"kind" gorm:"size:16"`           // schedule / user / password
	Title     string    `json:"title" gorm:"size:128"`         // 短标题
	Content   string    `json:"content" gorm:"size:512"`       // 详情：谁、何时、改了啥
	ActorID   uint      `json:"actor_id"`                      // 操作人
	ActorName string    `json:"actor_name" gorm:"size:64"`     // 操作人姓名
	Read      bool      `json:"read" gorm:"default:false"`     // 是否已读
	CreatedAt time.Time `json:"created_at"`
}

// Claims JWT 载荷
type Claims struct {
	jwt.RegisteredClaims
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
	DeptID   uint   `json:"dept_id"`
	Version  uint   `json:"ver"` // 令牌版本，需与 user.token_version 一致
	Client   ClientType `json:"client"` // 客户端类型：web/pwa/extension
}
