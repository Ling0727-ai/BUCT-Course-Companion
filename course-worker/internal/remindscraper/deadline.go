package scraper

import (
	"log"
	"strings"
	"time"
)

// formatDeadline 将中文日期格式转为 "2025-09-23 23:59:00"
// 支持: "2025年9月23日 23:59:00" 和 "2025年09月23日 23:59:00"
// 对应 Python: _format_deadline
func formatDeadline(s string) string {
	if s == "" {
		return ""
	}
	// 先尝试双位月日（09月03日）
	if t, err := time.ParseInLocation("2006年01月02日 15:04:05", s, time.Local); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	// 再尝试单位月日（9月3日）- Go 的格式化数字参考月=1，日=2
	if t, err := time.ParseInLocation("2006年1月2日 15:04:05", s, time.Local); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	// 混合：双位月单位日 / 单位月双位日
	for _, layout := range []string{
		"2006年01月2日 15:04:05",
		"2006年1月02日 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	log.Printf("[scraper] 作业时间解析失败，返回原始值: %q", s)
	return s
}

// formatTestDeadline 兼容多种格式，对应 Python: _format_test_deadline
func formatTestDeadline(s string) string {
	if s == "" {
		return ""
	}
	cleaned := strings.TrimSuffix(s, "Z")
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006年01月02日 15:04:05",
		"2006年1月2日 15:04:05",
		"2006年01月2日 15:04:05",
		"2006年1月02日 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
	} {
		if t, err := time.ParseInLocation(layout, cleaned, time.Local); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	log.Printf("[scraper] 测试时间格式无法解析: %q", s)
	return s
}
