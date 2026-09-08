// Package logger 提供分级运行日志：同时写入文件（崩溃可查，即使数据库损坏也能诊断）、
// 标准错误（控制台）以及可选的业务库（RegisterDBSink，用于界面查看）。
//
// 设计要点：
//   - 文件日志不依赖数据库，是系统崩溃时最重要的取证来源。
//   - DB 写入为异步 best-effort，失败时仅降级（不影响主流程）。
//   - 级别：INFO / WARN / ERROR / FATAL。
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level 日志级别
type Level string

const (
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
	LevelFatal Level = "FATAL"
)

// DBSink 将一条日志写入业务库（由 handlers 包注册，避免 logger 反向依赖 db）。
type DBSink func(level Level, source, message, detail string)

var (
	mu     sync.Mutex
	file   *os.File
	dbSink DBSink
)

// Init 初始化日志文件。logFilePath 为空时不写文件（仅控制台+DB）。
// 若目录不存在会自动创建；打开失败则降级为仅控制台。
func Init(logFilePath string) {
	if logFilePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "[logger] 创建日志目录失败: %v\n", err)
		return
	}
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[logger] 打开日志文件失败: %v\n", err)
		return
	}
	mu.Lock()
	file = f
	mu.Unlock()
	fmt.Fprintf(os.Stderr, "[logger] 运行日志已写入文件: %s\n", logFilePath)
}

// RegisterDBSink 注册业务库写入回调（应在 db 初始化完成后调用一次）。
func RegisterDBSink(fn DBSink) {
	mu.Lock()
	dbSink = fn
	mu.Unlock()
}

func write(level Level, source, msg, detail string) {
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	src := source
	if src == "" {
		src = "-"
	}
	line := fmt.Sprintf("%s [%s] %s %s", ts, level, src, msg)

	// 1) 控制台
	fmt.Fprintln(os.Stderr, line)
	// 2) 文件
	mu.Lock()
	if file != nil {
		fmt.Fprintln(file, line)
		if detail != "" {
			fmt.Fprintln(file, detail)
		}
	}
	mu.Unlock()
	// 3) 业务库（异步、best-effort）
	mu.Lock()
	sink := dbSink
	mu.Unlock()
	if sink != nil {
		go sink(level, src, msg, detail)
	}
}

// Info 记录一般信息
func Info(source, format string, a ...interface{}) {
	write(LevelInfo, source, fmt.Sprintf(format, a...), "")
}

// Warn 记录告警
func Warn(source, format string, a ...interface{}) {
	write(LevelWarn, source, fmt.Sprintf(format, a...), "")
}

// Error 记录错误（无堆栈）
func Error(source, format string, a ...interface{}) {
	write(LevelError, source, fmt.Sprintf(format, a...), "")
}

// ErrorDetail 记录错误并附带详情（如堆栈、原始错误）
func ErrorDetail(source, format string, a []interface{}, detail string) {
	write(LevelError, source, fmt.Sprintf(format, a...), detail)
}

// Fatal 记录致命错误并退出进程
func Fatal(source, format string, a ...interface{}) {
	write(LevelFatal, source, fmt.Sprintf(format, a...), "")
	os.Exit(1)
}
