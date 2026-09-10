package db

import (
	"path/filepath"
	"testing"

	"shiftworkbench/internal/config"
	"shiftworkbench/internal/models"
)

func TestScheduleRuleTablesMigrate(t *testing.T) {
	dir := t.TempDir()
	config.C = &config.Config{DBPath: filepath.Join(dir, "t.db")}
	if err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, m := range []interface{}{
		&models.ShiftRule{}, &models.UserShiftPref{},
		&models.ShiftRequest{}, &models.SpecialWorkDay{},
	} {
		if !DB.Migrator().HasTable(m) {
			t.Fatalf("表未创建: %T", m)
		}
	}
	// 默认值断言：写入一条仅带 DeptID 的规则，其余应取 gorm default
	r := models.ShiftRule{DeptID: 999}
	if err := DB.Create(&r).Error; err != nil {
		t.Fatalf("Create: %v", err)
	}
	var got models.ShiftRule
	DB.First(&got, r.ID)
	if got.MaxRestStreak != 3 || got.MaxWorkStreak != 6 || got.MonthWorkDays != 22 {
		t.Fatalf("默认值不符: rest=%d work=%d month=%d",
			got.MaxRestStreak, got.MaxWorkStreak, got.MonthWorkDays)
	}
	t.Logf("默认值正确: 连续休%d 连续上%d 月出勤%d", got.MaxRestStreak, got.MaxWorkStreak, got.MonthWorkDays)
}
