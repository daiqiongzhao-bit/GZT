package wecompush

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Scheduler 常驻调度器：每 30 秒扫描一次，命中「当前 HH:MM」的任务即执行。
// 用 fired 记录「任务 + 日期 + 类型」防止同一天内重复触发（含漏跑补偿）。
type Scheduler struct {
	db *gorm.DB
	h  *H
	loc *time.Location
	mu sync.Mutex
	// fired key 形如 "09:00@2026-09-19/3"（正常触发）或 "catchup@2026-09-19/3"（漏跑补偿）
	fired map[string]bool
}

func NewScheduler(d *gorm.DB, h *H, loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	s := &Scheduler{db: d, h: h, loc: loc, fired: map[string]bool{}}
	s.cleanup()
	return s
}

func (s *Scheduler) Start() {
	go func() {
		// 启动后先等 5 秒，避免与容器启动/迁移抢资源，随后立即进入扫描
		time.Sleep(5 * time.Second)
		for {
			s.tick()
			time.Sleep(30 * time.Second)
		}
	}()
	log.Println("企微推送调度器已启动（每 30 秒扫描一次，含漏跑补偿）")
}

func (s *Scheduler) tick() {
	now := time.Now().In(s.loc)
	hhmm := now.Format("15:04")

	var tasks []WpTask
	if err := s.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Printf("企微推送调度器读取任务失败：%v", err)
		return
	}
	for i := range tasks {
		t := tasks[i]
		// 1) 正常定时触发：当前分钟精确命中 send_time
		if t.SendTime == hhmm {
			key := t.SendTime + "@" + now.Format("2006-01-02") + "/" + itoa(t.ID)
			s.mu.Lock()
			already := s.fired[key]
			if !already {
				s.fired[key] = true
			}
			s.mu.Unlock()
			if !already {
				s.fire(t, "schedule")
			}
			continue
		}
		// 2) 漏跑补偿：今天 send_time 已过，但今天尚未执行过 → 补推一次
		s.maybeCatchUp(t, now)
	}
}

// maybeCatchUp 检测某任务今天是否漏跑（容器在 send_time 那一分钟没起来），
// 若漏跑且今天还没跑过，则补推一次；每天至多补推一次（由 fired 去重）。
func (s *Scheduler) maybeCatchUp(t WpTask, now time.Time) {
	var hh, mm int
	if _, err := fmt.Sscanf(t.SendTime, "%d:%d", &hh, &mm); err != nil {
		return
	}
	sendDT := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, s.loc)
	// 今天的 send_time 还没到 → 不补偿
	if !now.After(sendDT) {
		return
	}
	// 今天已经执行过（含正常触发 / 手动运行）→ 不再补偿，避免重复推送
	if t.LastRunAt != nil && t.LastRunAt.In(s.loc).Format("2006-01-02") == now.Format("2006-01-02") {
		return
	}
	key := "catchup@" + now.Format("2006-01-02") + "/" + itoa(t.ID)
	s.mu.Lock()
	already := s.fired[key]
	if !already {
		s.fired[key] = true
	}
	s.mu.Unlock()
	if already {
		return
	}
	log.Printf("企微推送漏跑补偿：任务「%s」今日 %s 未执行，立即补推", t.Name, t.SendTime)
	s.fire(t, "catchup")
}

// fire 异步执行任务并记录结果
func (s *Scheduler) fire(t WpTask, trigger string) {
	go func(task WpTask) {
		log.Printf("企微推送触发（%s）：%s", trigger, task.Name)
		entry := s.h.ExecuteTask(&task, trigger, false)
		log.Printf("企微推送任务「%s」执行完成 status=%s count=%d msg=%s",
			task.Name, entry.Status, entry.Count, entry.Message)
	}(t)
}

// cleanup 定期清掉过期的触发标记，避免内存无限增长
func (s *Scheduler) cleanup() {
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			today := time.Now().In(s.loc).Format("2006-01-02")
			s.mu.Lock()
			for k := range s.fired {
				// key 形如 "09:00@2026-09-19/3" 或 "catchup@2026-09-19/3"
				parts := strings.Split(k, "@")
				if len(parts) != 2 {
					delete(s.fired, k)
					continue
				}
				datePart := strings.Split(parts[1], "/")[0]
				if datePart != today {
					delete(s.fired, k)
				}
			}
			s.mu.Unlock()
		}
	}()
}

func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
