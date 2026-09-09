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
	ClientWeb       ClientType = "web"
	ClientPWA       ClientType = "pwa"
	ClientExtension ClientType = "extension"
)

// 任务类型
const (
	TaskTypeDaily   = "daily"
	TaskTypeMonthly = "monthly"
	TaskTypeOnce    = "once"
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
	ColorKey  string    `json:"color_key" gorm:"size:20"`     // matrix color key
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID            uint        `json:"id" gorm:"primaryKey"`
	Username      string      `json:"username" gorm:"size:64;not null;uniqueIndex"`
	PasswordHash  string      `json:"-" gorm:"size:128;not null"`
	Name          string      `json:"name" gorm:"size:64;not null"`
	EmpNo         string      `json:"emp_no" gorm:"size:32;index"` // 工号：通知与名单展示用
	Mobile        string      `json:"mobile" gorm:"size:20"`       // 手机号：企业微信@提醒用
	Role          Role        `json:"role" gorm:"size:24;not null;default:executor"`
	DeptID        uint        `json:"dept_id"`
	Frozen        bool        `json:"frozen"`                               // 冻结：禁止登录
	OnLeave       bool        `json:"on_leave" gorm:"default:false"`        // 休假/停职：不计入「全员」当班与推送
	MustChangePwd bool        `json:"must_change_pwd" gorm:"default:false"` // 必须修改密码：弱密码/管理员重置后登录强制改密
	InGroup       bool        `json:"in_group" gorm:"default:false"`        // 已加入企业微信通知群：推送@对象，名单中不重复列出
	TokenVersion  uint        `json:"token_version"`                        // 令牌版本：自增即令所有已签发token失效
	LastLoginAt   *time.Time  `json:"last_login_at"`                        // 最近登录时间（登录成功时写入）
	Dept          *Department `json:"dept,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
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

// Task 任务：每日/每周/每月/临时单次，支持逾期。
// Shift 班次归属：早班/中班/晚班/早晚/全员（谁当班谁负责）
// WeekDays 为「按周执行」：Type=daily 时可勾选星期几触发（1=周一…7=周日，空=每天都执行）
type Task struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Title        string    `json:"title" gorm:"size:255;not null"`
	Type         string    `json:"type" gorm:"size:16;not null"`
	Shift        string    `json:"shift" gorm:"size:16;default:all"` // 早班/中班/晚班/早晚/全员
	Time         string    `json:"time" gorm:"size:8"`               // 每日任务执行时间 HH:MM
	Deadline     string    `json:"deadline" gorm:"size:16"`          // 截止时间 YYYY-MM-DDTHH:MM
	WeekDays     string    `json:"week_days" gorm:"size:32"`         // 按周执行：逗号分隔的星期，如 "1,3,5"（1=周一…7=周日）；空=每天
	Assignee     string    `json:"assignee" gorm:"size:64"`          // 负责人姓名展示（多人用顿号连接；兼容旧数据）
	AssigneeID   uint      `json:"assignee_id"`                      // 负责人用户ID（首个，0=未分配/部门公共；权限判断用）
	Assignees    string    `json:"assignees" gorm:"type:text"`       // 负责人（单人/多人）姓名：JSON 数组 ["甲","乙"]
	AssigneeIDs  string    `json:"assignee_ids" gorm:"type:text"`    // 负责人用户ID：JSON 数组 [1,2]
	CompletedBy  string    `json:"completed_by" gorm:"size:64"`      // 完成人姓名（谁打的☑️）
	CompletedAt  time.Time `json:"completed_at"`                     // 最近一次完成时间
	Status       string    `json:"status" gorm:"size:16;not null;default:todo"`
	Priority     string    `json:"priority" gorm:"size:16;default:medium"` // high/medium/low
	Note         string    `json:"note" gorm:"type:text"`
	DeptID       uint      `json:"dept_id" gorm:"index"`
	Overdue      bool      `json:"overdue" gorm:"-"`        // 瞬态：是否逾期
	DueToday     bool      `json:"due_today" gorm:"-"`      // 瞬态：今日是否应处理
	DueThisMonth bool      `json:"due_this_month" gorm:"-"` // 瞬态：本月是否应处理
	SoonOverdue  bool      `json:"soon_overdue" gorm:"-"`   // v0.9.2 瞬态：30 分钟内将逾期（橙色提醒）
	CreatedAt    time.Time `json:"created_at"`
}

// Webhook 部门机器人推送地址（AES 加密存储）
// Type: wecom 企业微信 / dingtalk 钉钉 / feishu 飞书；Secret 为对应加签密钥（加密存储）
type Webhook struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:64;not null"`
	Type      string    `json:"type" gorm:"size:16;not null;default:wecom"` // wecom|dingtalk|feishu
	URL       string    `json:"url" gorm:"type:text;not null"`              // 加密后
	Secret    string    `json:"-" gorm:"type:text"`                         // 加密后（加签密钥）
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
	ID          uint      `json:"id" gorm:"primaryKey"`
	TaskID      uint      `json:"task_id" gorm:"index"`
	TaskTitle   string    `json:"task_title" gorm:"size:255"`
	UserID      uint      `json:"user_id"`
	UserName    string    `json:"user_name" gorm:"size:64"`
	DeptID      uint      `json:"dept_id" gorm:"index"`
	CompletedAt time.Time `json:"completed_at"`
}

// Setting 企业品牌设置（单行）
type Setting struct {
	ID          uint   `json:"-" gorm:"primaryKey"`
	CompanyName string `json:"company_name" gorm:"size:128"`
	Slogan      string `json:"slogan" gorm:"size:255"`
	Version     string `json:"version" gorm:"size:16"`
	Copyright   string `json:"copyright" gorm:"size:255"`
	Logo        string `json:"logo" gorm:"size:128"` // 企业 Logo 文件名，存于数据目录下
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
	// 每日任务汇总推送：每天 09:00 自动向所有 Webhook 推送今日任务汇总的开关（默认开）
	DailySummaryEnabled bool `json:"daily_summary_enabled" gorm:"default:true"`
	// 逾期宽限期（分钟）：每日/月度定时任务的「开始时间」+ 宽限后才算逾期；0=到点即逾期（默认 30）
	OverdueGraceMinutes int `json:"overdue_grace_minutes" gorm:"default:30"`
}

// Log 系统操作日志
type Log struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Action    string    `json:"action" gorm:"size:255"`
	UserID    uint      `json:"user_id"`
	UserName  string    `json:"user_name" gorm:"size:64"`
	IP        string    `json:"ip" gorm:"size:64"`     // 操作来源 IP（全部操作留痕）
	UA        string    `json:"ua" gorm:"size:255"`    // 操作来源 User-Agent
	Client    string    `json:"client" gorm:"size:16"` // 操作来源: web/pwa/extension（v0.0.6）
	CreatedAt time.Time `json:"created_at"`
}

// SystemLog 系统运行日志：记录服务端运行期事件，重点用于「系统崩溃/异常」排查。
// 与 Log（用户操作审计）不同，这里记录的是程序自身的信息/告警/错误/堆栈。
type SystemLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Level     string    `json:"level" gorm:"size:16;index"`  // INFO / WARN / ERROR / FATAL
	Source    string    `json:"source" gorm:"size:32;index"` // server / scheduler / handler / backup / import ...
	Message   string    `json:"message" gorm:"size:512"`
	Detail    string    `json:"detail" gorm:"type:text"` // 堆栈 / 原始错误
	CreatedAt time.Time `json:"created_at"`
}

// NotifAttachment 通知附件（广播通知可携带，永久保存）
type NotifAttachment struct {
	FileName   string `json:"file_name"`
	StoredName string `json:"stored_name"` // 不可猜测的文件名，下载端点据此定位
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
}

// Notification 站内通知：管理员修改与用户相关信息时推送给当事人
type Notification struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	UserID      uint      `json:"user_id" gorm:"index"` // 接收人
	Kind        string    `json:"kind" gorm:"size:16"`  // schedule / user / password / broadcast
	Title       string    `json:"title" gorm:"size:128"` // 短标题
	Content     string    `json:"content" gorm:"size:512"` // 详情：谁、何时、改了啥
	Link        string    `json:"link" gorm:"size:512"`     // 附带的超链接（广播/定向发送可选）
	Attachments string    `json:"attachments" gorm:"type:text"` // JSON 数组：[]NotifAttachment（广播附件）
	ActorID     uint      `json:"actor_id"`                  // 操作人
	ActorName   string    `json:"actor_name" gorm:"size:64"` // 操作人姓名
	BroadcastID string     `json:"broadcast_id" gorm:"size:32;index"` // 同一次广播聚合键；空=非广播通知
	RequireAck  bool       `json:"require_ack" gorm:"default:false"`  // 广播要求接收人「确认收到」
	Ack         bool       `json:"ack" gorm:"default:false"`          // 接收人是否已确认收到
	AckedAt     *time.Time `json:"acked_at"`                          // 确认时间
	Read        bool      `json:"read" gorm:"default:false"` // 是否已读
	CreatedAt   time.Time `json:"created_at"`
}

// Claims JWT 载荷
type Claims struct {
	jwt.RegisteredClaims
	UserID   uint       `json:"uid"`
	Username string     `json:"username"`
	Role     Role       `json:"role"`
	DeptID   uint       `json:"dept_id"`
	Version  uint       `json:"ver"`    // 令牌版本，需与 user.token_version 一致
	Client   ClientType `json:"client"` // 客户端类型：web/pwa/extension
}

// ============ 工作台：知识库 / 工作日志 / 交接接力 ============

// WorkspaceScope 可见范围
type WorkspaceScope string

const (
	ScopePrivate    WorkspaceScope = "private"    // 仅创建者本人可见
	ScopeDepartment WorkspaceScope = "department" // 同部门用户共享可见
)

// WorkItem 工作台通用条目（知识库 / 工作日志 / 交接单共用基础字段）
// 三种类型在各自专属表中（见下），此处仅做通用状态约定。
const (
	WorkTypeKnowledge = "knowledge" // 迷你知识库
	WorkTypeLog       = "log"       // 工作日志
	WorkTypeHandover  = "handover"  // 交接接力
)

// HandoverStatus 交接单状态
const (
	HandoverPending    = "pending"     // 已发出，等待接收人处理
	HandoverInProgress = "in_progress" // 接收人已接手处理中
	HandoverDone       = "done"        // 已完成
)

// KnowledgeEntry 迷你知识库条目：方法/流程/制度等长期内容
type KnowledgeEntry struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Title     string         `json:"title" gorm:"size:255;not null"`
	Category  string         `json:"category" gorm:"size:64"`  // 分类标签（可自由填）
	Content   string         `json:"content" gorm:"type:text"` // 正文（多行）
	Scope     WorkspaceScope `json:"scope" gorm:"size:16;default:department"`
	OwnerID   uint           `json:"owner_id" gorm:"index"`     // 创建者
	OwnerName string         `json:"owner_name" gorm:"size:64"` // 创建者姓名（展示用）
	DeptID    uint           `json:"dept_id" gorm:"index"`      // 所属部门（隔离范围）
	UpdatedAt time.Time      `json:"updated_at"`
	CreatedAt time.Time      `json:"created_at"`
}

// KnowledgeAttachment 知识条目附件：文件存服务器磁盘（与数据库同盘目录），仅存元数据
type KnowledgeAttachment struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	EntryID    uint      `json:"entry_id" gorm:"index;not null"` // 所属知识条目
	FileName   string    `json:"file_name" gorm:"size:255"`      // 原始文件名（下载展示用）
	StoredName string    `json:"stored_name" gorm:"size:128"`    // 磁盘存储文件名（唯一）
	Mime       string    `json:"mime" gorm:"size:64"`            // Content-Type
	Size       int64     `json:"size"`                           // 字节
	OwnerID    uint      `json:"owner_id" gorm:"index"`          // 上传者
	OwnerName  string    `json:"owner_name" gorm:"size:64"`
	CreatedAt  time.Time `json:"created_at"`
}

// KnowledgeTempAttachment 知识附件「中转缓存」：条目尚未保存（新建）时，
// 富文本粘贴/选择的图片与文件先落到此表与临时目录；待用户确认「保存」新建条目时，
// 由 CreateKnowledge/UpdateKnowledge 把它们「转正」为正式 KnowledgeAttachment。
// 这样新建条目也能在保存前就粘贴/上传图片，且误关页面不会留孤儿（由懒清理回收）。
type KnowledgeTempAttachment struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	FileName   string    `json:"file_name" gorm:"size:255"`
	StoredName string    `json:"stored_name" gorm:"size:128"`
	Mime       string    `json:"mime" gorm:"size:64"`
	Size       int64     `json:"size"`
	OwnerID    uint      `json:"owner_id" gorm:"index"`
	OwnerName  string    `json:"owner_name" gorm:"size:64"`
	CreatedAt  time.Time `json:"created_at"`
}

// KnowledgeChangeLog 知识条目变更/协作日志：完整记录操作人、操作时间、修改前后内容（审计追溯）
type KnowledgeChangeLog struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	EntryID      uint      `json:"entry_id" gorm:"index;not null"` // 所属知识条目
	Action       string    `json:"action" gorm:"size:24"`          // create / update / delete / attachment_upload / attachment_delete
	OperatorID   uint      `json:"operator_id" gorm:"index"`       // 操作人
	OperatorName string    `json:"operator_name" gorm:"size:64"`
	DeptID       uint      `json:"dept_id" gorm:"index"`    // 操作人所属部门
	Detail       string    `json:"detail" gorm:"type:text"` // 变更说明：修改了哪些字段、修改前/修改后内容
	CreatedAt    time.Time `json:"created_at"`
}

// WorkLog 工作日志：某日记录当天做了什么 / 还没做完的（一天可多篇）
type WorkLog struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	LogDate   string         `json:"log_date" gorm:"size:10;index"` // YYYY-MM-DD
	Title     string         `json:"title" gorm:"size:255"`         // 本篇主题（如"早班开档""活动跟进"）
	Done      string         `json:"done" gorm:"type:text"`         // 今天做了什么
	Pending   string         `json:"pending" gorm:"type:text"`      // 还没做完的 / 待办遗留
	Scope     WorkspaceScope `json:"scope" gorm:"size:16;default:private"`
	OwnerID   uint           `json:"owner_id" gorm:"index"`
	OwnerName string         `json:"owner_name" gorm:"size:64"`
	DeptID    uint           `json:"dept_id" gorm:"index"`
	UpdatedAt time.Time      `json:"updated_at"`
	CreatedAt time.Time      `json:"created_at"`
}

// WorkHandover 交接接力：把"做到哪 + 还没做完"转给接收人继续
// 支持多人接收：AssigneeIDs 存全部接收人 ID（JSON 数组），AssigneeID 保留为第一个接收人（兼容旧数据/旧版接口）
type WorkHandover struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	Title         string     `json:"title" gorm:"size:255;not null"`
	FromProgress  string     `json:"from_progress" gorm:"type:text"` // 当前进展（已完成 / 进行到哪）
	Todo          string     `json:"todo" gorm:"type:text"`          // 需要接收人继续做的事
	Scope         WorkspaceScope `json:"scope" gorm:"size:16;default:department"`
	SenderID      uint       `json:"sender_id" gorm:"index"`        // 发出人
	SenderName    string     `json:"sender_name" gorm:"size:64"`
	AssigneeID    uint       `json:"assignee_id" gorm:"index"`      // 主接收人（第一个，兼容）
	AssigneeName  string     `json:"assignee_name" gorm:"size:64"`   // 主接收人姓名
	AssigneeIDs   string     `json:"assignee_ids" gorm:"type:text"`  // 全部接收人 ID（JSON 数组）
	AssigneeNames string     `json:"assignee_names" gorm:"type:text"`// 全部接收人姓名（JSON 数组，给前端直接展示）
	DeptID        uint       `json:"dept_id" gorm:"index"`          // 发出人所属部门
	Status        string     `json:"status" gorm:"size:16;default:pending"`
	Note          string     `json:"note" gorm:"type:text"`         // 接收人完成时的备注
	CompletedAt   *time.Time `json:"completed_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ============ 通知增强：定时广播 / 浏览器推送（v0.13.0） ============

// ScheduledBroadcast 定时广播：到点自动向目标部门发送站内广播（可单次/每日/按周）。
// Repeat: once 单次（发送后停用） / daily 每天 / weekly 按周（WeekDays 逗号分隔 1-7）
type ScheduledBroadcast struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Title       string    `json:"title" gorm:"size:128;not null"`
	Content     string    `json:"content" gorm:"size:512;not null"`
	Link        string    `json:"link" gorm:"size:512"`
	RequireAck  bool      `json:"require_ack" gorm:"default:false"`
	AllDepts    bool      `json:"all_depts" gorm:"default:false"` // 全部部门
	DeptIDs     string    `json:"dept_ids" gorm:"type:text"`      // JSON []uint：目标部门（顶层/用户所选，发送时含子孙）
	Repeat      string    `json:"repeat" gorm:"size:8;default:once"` // once|daily|weekly
	WeekDays    string    `json:"week_days" gorm:"size:32"`       // weekly：逗号分隔 1-7（1=周一…7=周日）
	SendAt      time.Time `json:"send_at"`                        // 下次触发时间（发送成功后按 repeat 前移到下一次）
	CreatorID   uint      `json:"creator_id"`
	CreatorName string    `json:"creator_name" gorm:"size:64"`
	Active      bool      `json:"active" gorm:"default:true"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PushSubscription 浏览器 Web Push 订阅（每个用户可多设备）
type PushSubscription struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index;not null"`
	Endpoint  string    `json:"endpoint" gorm:"type:text;not null"`
	P256dh    string    `json:"p256dh" gorm:"type:text;not null"`
	Auth      string    `json:"auth" gorm:"type:text;not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
