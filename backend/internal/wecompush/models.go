package wecompush

import "time"

// WpTask 一条「从智能表格筛选 → 生成 Excel → 发到群」的定时任务。
// 表名强制为 wp_tasks，与 GZT 既有表零冲突；AutoMigrate 只增不删，不影响 swb.db 现有数据。
type WpTask struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Name       string `gorm:"size:120" json:"name"`
	Enabled    bool   `gorm:"default:true" json:"enabled"`
	DocID      string `gorm:"size:120" json:"doc_id"`
	SheetTitle string `gorm:"size:120" json:"sheet_title"`

	// 筛选：日期列 = 今天 + 提前天数
	DateField  string `gorm:"size:120" json:"date_field"`
	OffsetDays int    `gorm:"default:0" json:"offset_days"`

	// DateCols 需要按日期格式化的列（JSON 数组）。这些列底层是 Excel 序列号，
	// 输出到 Excel 前必须还原成 YYYY-MM-DD；为空时仅处理 DateField。
	DateCols string `gorm:"size:600" json:"date_cols"`

	// 附加条件（可多个）：JSON 数组 [{"field","op","value"}]
	// op 取值 is_null / not_null / eq / ne / contains / not_contains / in / date_eq
	Conditions string `gorm:"size:2000" json:"conditions"`

	// 兼容旧数据（Conditions 为空时才生效）
	ExtraField string `gorm:"size:120" json:"extra_field"`
	ExtraOp    string `gorm:"size:24" json:"extra_op"`
	ExtraValue string `gorm:"size:200" json:"extra_value"`

	// 输出
	Columns    string `gorm:"size:2000" json:"columns"`    // JSON 数组，空=取全部字段
	FilePrefix string `gorm:"size:120" json:"file_prefix"` // 文件名前缀，如「满25天到期明细」
	MsgTitle   string `gorm:"size:200" json:"msg_title"`   // 消息前缀
	EmptyText  string `gorm:"size:500" json:"empty_text"`  // 0 条时的提示语

	// v0.40.8 模板化（空 = 兼容上面的前缀拼接逻辑）：
	// FileTpl 文件名模板，可用变量 {date} {task} {prefix}，如「满25天明细_{date}」
	// MsgTemplate 消息模板，可用变量 {title} {date} {count} {filename} {task}
	FileTpl     string `gorm:"size:200" json:"file_name_template"`
	MsgTemplate string `gorm:"size:1000" json:"msg_template"`

	// 是否发送说明文字；关闭则只发表格附件，不发下方文案。默认 true 兼容旧任务。
	SendText bool `gorm:"default:true" json:"send_text"`

	// 投递
	GroupName string `gorm:"size:120" json:"group_name"`
	SendTime  string `gorm:"size:8" json:"send_time"` // HH:MM

	// 最近一次运行概况
	LastRunAt  *time.Time `json:"last_run_at"`
	LastStatus string     `gorm:"size:16" json:"last_status"`
	LastCount  int        `json:"last_count"`
	LastError  string     `gorm:"size:1000" json:"last_error"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (WpTask) TableName() string { return "wp_tasks" }

// WpLog 运行日志。表名 wp_logs。
type WpLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TaskID     uint      `gorm:"index" json:"task_id"`
	TaskName   string    `gorm:"size:120" json:"task_name"`
	RunAt      time.Time `json:"run_at"`
	Trigger    string    `gorm:"size:16" json:"trigger"` // schedule | manual | dry-run | catchup
	Count      int       `json:"count"`
	Status     string    `gorm:"size:16" json:"status"` // success | empty | failed
	FileName   string    `gorm:"size:200" json:"file_name"`
	Message    string    `gorm:"size:1000" json:"message"`
	DurationMs int64     `json:"duration_ms"`
}

func (WpLog) TableName() string { return "wp_logs" }

// WpSetting 企微推送模块自己的键值配置（与 GZT 既有 Setting 表完全隔离）。
// 存放品牌信息（兼容原登录页）与失败通知开关 / 通知群等。
type WpSetting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"size:2000" json:"value"`
}

func (WpSetting) TableName() string { return "wp_settings" }
